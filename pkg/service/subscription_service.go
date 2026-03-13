package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/callback"
	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
	"github.com/guyuxiang/projectc-solana-connector/pkg/store"
)

type SubscriptionService interface {
	RegisterTxSubscription(req models.TxSubscribeRequest) error
	RegisterAddressSubscription(req models.AddressSubscribeRequest) error
	CancelTxSubscription(txCode string) error
	CancelAddressSubscription(address string) error
}

func NewSubscriptionService(cfg *config.Config, chain ChainService, publisher callback.CallbackPublisher, subscriptionStore store.SubscriptionStore) SubscriptionService {
	s := &subscriptionService{
		cfg:             cfg,
		chain:           chain,
		publisher:       publisher,
		store:           subscriptionStore,
		txSubs:          make(map[string]*models.TxSubscription),
		addressSubs:     make(map[string]*models.AddressSubscription),
		publishedState:  make(map[string]models.PublishedTxState),
		txWatchers:      make(map[string]context.CancelFunc),
		addressWatchers: make(map[string]context.CancelFunc),
	}
	s.restore()
	s.start()
	return s
}

type subscriptionService struct {
	cfg       *config.Config
	chain     ChainService
	publisher callback.CallbackPublisher
	store     store.SubscriptionStore

	mu              sync.RWMutex
	txSubs          map[string]*models.TxSubscription
	addressSubs     map[string]*models.AddressSubscription
	publishedState  map[string]models.PublishedTxState
	txWatchers      map[string]context.CancelFunc
	addressWatchers map[string]context.CancelFunc
}

func (s *subscriptionService) RegisterTxSubscription(req models.TxSubscribeRequest) error {
	if req.SubscribeRange.EndBlockNumber == nil {
		return fmt.Errorf("subscribeRange.endBlockNumber is required")
	}

	sub := &models.TxSubscription{
		CreatedAt:          time.Now(),
		NetworkCode:        s.chainNetworkCode(),
		TxCode:             req.TxCode,
		EndBlockNumber:     *req.SubscribeRange.EndBlockNumber,
		SubscriptionStatus: models.TxSubscriptionStatusActive,
	}

	s.mu.RLock()
	if existing, ok := s.txSubs[req.TxCode]; ok && !existing.CreatedAt.IsZero() {
		sub.CreatedAt = existing.CreatedAt
	}
	s.mu.RUnlock()

	if err := s.store.SaveTxSubscription(context.Background(), sub); err != nil {
		return err
	}

	s.mu.Lock()
	s.txSubs[req.TxCode] = sub
	s.startTxWatcherLocked(req.TxCode)
	s.mu.Unlock()
	return nil
}

func (s *subscriptionService) RegisterAddressSubscription(req models.AddressSubscribeRequest) error {
	s.mu.Lock()
	sub, ok := s.addressSubs[req.Address]
	if !ok {
		sub = &models.AddressSubscription{
			CreatedAt:          time.Now(),
			NetworkCode:        s.chainNetworkCode(),
			Address:            req.Address,
			TrackedAccounts:    nil,
			AccountCheckpoints: make(map[string]models.AddressCheckpoint),
			SubscriptionStatus: models.TxSubscriptionStatusActive,
		}
	} else {
		sub.SubscriptionStatus = models.TxSubscriptionStatusActive
	}
	s.reconcileAddressTargetsLocked(sub)
	toSave := cloneAddressSub(sub)
	s.addressSubs[req.Address] = cloneAddressSub(sub)
	s.startAddressWatcherLocked(req.Address)
	s.mu.Unlock()

	return s.store.SaveAddressSubscription(context.Background(), toSave)
}

func (s *subscriptionService) CancelTxSubscription(txCode string) error {
	return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCancelled)
}

func (s *subscriptionService) CancelAddressSubscription(address string) error {
	return s.updateAddressSubscriptionStatus(address, models.TxSubscriptionStatusCancelled)
}

func (s *subscriptionService) start() {
	s.resumeWatchers()

	ticker := time.NewTicker(time.Duration(s.cfg.Connector.PollIntervalMs) * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(defaultRequestTimeoutMs)*time.Millisecond)
			s.poll(ctx)
			cancel()
		}
	}()
}

func (s *subscriptionService) restore() {
	snapshot, err := s.store.Load(context.Background())
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txSubs = snapshot.TxSubs
	s.addressSubs = snapshot.AddressSubs
	s.publishedState = snapshot.PublishedState
	log.Infof("subscription state restored txSubs=%d addressSubs=%d trackedTxs=%d", len(s.txSubs), len(s.addressSubs), len(s.publishedState))
}

func (s *subscriptionService) resumeWatchers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for txCode := range s.txSubs {
		s.startTxWatcherLocked(txCode)
	}
	for address := range s.addressSubs {
		s.startAddressWatcherLocked(address)
	}
}

func (s *subscriptionService) poll(ctx context.Context) {
	latest, err := s.chain.GetLatestBlock(ctx)
	if err != nil {
		log.Warningf("poll latest block failed err=%v", err)
		return
	}
	s.pollTxSubscriptions(ctx, latest.BlockNumber)
	s.pollTrackedTransactions(ctx, latest.BlockNumber)
	s.pollAddressSubscriptions()
}

func (s *subscriptionService) pollTxSubscriptions(ctx context.Context, latestBlock uint64) {
	s.mu.RLock()
	subs := make([]*models.TxSubscription, 0, len(s.txSubs))
	for _, sub := range s.txSubs {
		subs = append(subs, cloneTxSub(sub))
	}
	s.mu.RUnlock()

	for _, sub := range subs {
		log.Infof("advanceTxSubscriptionState tx=%s", sub.TxCode)
		if err := s.advanceTxSubscriptionState(ctx, sub.TxCode, sub.EndBlockNumber, latestBlock); err != nil {
			log.Warningf("advance tx subscription failed network=%s tx=%s err=%v", sub.NetworkCode, sub.TxCode, err)
		}
	}
}

func (s *subscriptionService) pollTrackedTransactions(ctx context.Context, latestBlock uint64) {
	s.mu.RLock()
	states := make(map[string]models.PublishedTxState, len(s.publishedState))
	for txCode, state := range s.publishedState {
		states[txCode] = state
	}
	s.mu.RUnlock()

	for txCode, state := range states {
		if isTerminalTxState(state.State) {
			continue
		}
		if err := s.advanceTrackedTxState(ctx, txCode, state, latestBlock); err != nil {
			log.Warningf("advance tracked tx failed network=%s tx=%s err=%v", state.NetworkCode, txCode, err)
		}
	}
}

func (s *subscriptionService) advanceTxSubscriptionState(ctx context.Context, txCode string, endBlock uint64, latestBlock uint64) error {
	status, err := s.chain.GetSignatureStatus(ctx, txCode)
	if err != nil {
		return err
	}
	if status.Exists {

		target := ""
		switch status.ConfirmationStatus {
		case "finalized":
			target = models.TxStateFinalized
		case "confirmed":
			target = models.TxStateConfirmed
		default:
			return nil
		}

		current := s.getTrackedState(txCode)
		if target != "" && txStateRank(target) > txStateRank(current.State) {
			var tx *models.ChainTx
			var txEvents []models.ChainEvent
			resp, err := s.chain.QueryTransaction(ctx, txCode)
			if err != nil {
				return err
			}
			if resp != nil && resp.Tx != nil {
				tx = resp.Tx
				txEvents = resp.TxEvents
			}

			if current.State == "" && target == models.TxStateFinalized {
				if err := s.transitionTxState(txCode, models.TxStateConfirmed, tx, txEvents, status.Slot); err != nil {
					return err
				}
			}
			if err := s.transitionTxState(txCode, target, tx, txEvents, status.Slot); err != nil {
				return err
			}
		}

		if target == models.TxStateFinalized {
			return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCompleted)
		}
		return nil
	}

	if latestBlock > endBlock {
		if err := s.transitionTxState(txCode, models.TxStateDropped, nil, nil, latestBlock); err != nil {
			return err
		}
		return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusExpired)
	}
	return nil
}

func (s *subscriptionService) advanceTrackedTxState(ctx context.Context, txCode string, current models.PublishedTxState, latestBlock uint64) error {
	status, err := s.chain.GetSignatureStatus(ctx, txCode)
	if err != nil {
		return err
	}
	if status.Exists {
		if current.State == models.TxStateConfirmed && status.ConfirmationStatus == "finalized" {
			if err := s.transitionTxState(txCode, models.TxStateFinalized, nil, nil, status.Slot); err != nil {
				return err
			}
			return nil
		}
	} else {
		if current.State != "" && !isTerminalTxState(current.State) {
			if err := s.transitionTxState(txCode, models.TxStateReverted, nil, nil, maxUint64(current.BlockNumber, latestBlock)); err != nil {
				return err
			}
			_ = s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCompleted)
		}
	}

	return nil
}

func (s *subscriptionService) startTxWatcherLocked(txCode string) {
	if _, ok := s.txWatchers[txCode]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.txWatchers[txCode] = cancel
	go s.runTxWatcher(ctx, txCode)
}

func (s *subscriptionService) startAddressWatcherLocked(address string) {
	if _, ok := s.addressWatchers[address]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.addressWatchers[address] = cancel
	go s.runAddressWatcher(ctx, address)
}

func (s *subscriptionService) runTxWatcher(ctx context.Context, txCode string) {
	for {
		if ctx.Err() != nil {
			return
		}
		notification, err := s.chain.WatchSignature(ctx, txCode)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warningf("ws signature watcher failed network=%s tx=%s err=%v", s.chainNetworkCode(), txCode, err)
			if !sleepWithContext(ctx, time.Duration(defaultRetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}

		queryCtx, cancel := context.WithTimeout(context.Background(), time.Duration(defaultRequestTimeoutMs)*time.Millisecond)
		resp, err := s.chain.QueryTransaction(queryCtx, txCode)
		cancel()
		if err != nil {
			log.Warningf("load confirmed tx failed network=%s tx=%s slot=%d err=%v", s.chainNetworkCode(), txCode, notification.Slot, err)
			if !sleepWithContext(ctx, time.Duration(defaultRetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		if resp == nil || resp.Tx == nil {
			log.Warningf("confirmed tx detail not found yet network=%s tx=%s slot=%d", s.chainNetworkCode(), txCode, notification.Slot)
			if !sleepWithContext(ctx, time.Duration(defaultRetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		if err := s.transitionTxState(txCode, models.TxStateConfirmed, resp.Tx, resp.TxEvents, notification.Slot); err != nil {
			log.Warningf("publish confirmed tx failed network=%s tx=%s slot=%d err=%v", s.chainNetworkCode(), txCode, notification.Slot, err)
			if !sleepWithContext(ctx, time.Duration(defaultRetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		return
	}
}

func (s *subscriptionService) runAddressWatcher(ctx context.Context, address string) {
	sub := s.getAddressSub(address)
	if sub == nil {
		return
	}
	if len(sub.TrackedAccounts) == 0 {
		log.Warningf("address watcher has no tracked token accounts network=%s address=%s", s.chainNetworkCode(), address)
		<-ctx.Done()
		return
	}
	if err := s.backfillAddressAccounts(address, nil); err != nil {
		log.Warningf("address backfill failed network=%s address=%s err=%v", s.chainNetworkCode(), address, err)
	}

	var wg sync.WaitGroup
	for _, account := range sub.TrackedAccounts {
		wg.Add(1)
		go func(trackedAccount string) {
			defer wg.Done()
			s.runTrackedAccountWatcher(ctx, address, trackedAccount)
		}(account)
	}
	wg.Wait()
}

func (s *subscriptionService) runTrackedAccountWatcher(ctx context.Context, address string, account string) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := s.chain.WatchAccount(ctx, account, func() error {
			return s.backfillAddressAccounts(address, []string{account})
		}, func(notification solana.AccountNotification) error {
			return s.handleAccountNotification(address, notification)
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warningf("ws account watcher failed network=%s address=%s account=%s err=%v", s.chainNetworkCode(), address, account, err)
			if !sleepWithContext(ctx, time.Duration(defaultRetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		return
	}
}

func (s *subscriptionService) handleAccountNotification(address string, notification solana.AccountNotification) error {
	return s.backfillAddressAccounts(address, []string{notification.Pubkey})
}

func (s *subscriptionService) backfillAddressAccounts(address string, accounts []string) error {
	sub := s.getAddressSub(address)
	if sub == nil {
		return nil
	}
	if len(sub.TrackedAccounts) == 0 {
		return nil
	}

	ctx := context.Background()
	latest, err := s.chain.GetLatestBlock(ctx)
	if err != nil {
		return err
	}

	targetAccounts := accounts
	if len(targetAccounts) == 0 {
		targetAccounts = append([]string(nil), sub.TrackedAccounts...)
	}
	checkpointsChanged := false
	for _, account := range targetAccounts {
		checkpoint, ok := sub.AccountCheckpoints[account]
		if !ok {
			sub.AccountCheckpoints[account] = models.AddressCheckpoint{}
			checkpointsChanged = true
			continue
		}
		if checkpoint.LastObservedSlot == 0 && checkpoint.LastObservedTxCode == "" {
			sub.AccountCheckpoints[account] = models.AddressCheckpoint{LastObservedSlot: latest.BlockNumber}
			checkpointsChanged = true
		}
	}
	if checkpointsChanged {
		return s.persistAddressSnapshot(sub)
	}

	const pageLimit = 1000
	collected := make(map[string]solana.SignatureInfo)
	updatedCheckpoints := make(map[string]models.AddressCheckpoint, len(targetAccounts))
	for _, account := range targetAccounts {
		checkpoint := sub.AccountCheckpoints[account]
		if latest.BlockNumber <= checkpoint.LastObservedSlot {
			updatedCheckpoints[account] = checkpoint
			continue
		}
		before := ""
		newestCheckpoint := checkpoint
		for {
			signatures, err := s.chain.FetchAddressSignatures(ctx, account, solana.SignatureQueryOptions{
				Limit:          pageLimit,
				Before:         before,
				Until:          checkpoint.LastObservedTxCode,
				MinContextSlot: checkpoint.LastObservedSlot,
			})
			if err != nil {
				return err
			}
			if len(signatures) == 0 {
				break
			}
			for _, sig := range signatures {
				if sig.Slot <= checkpoint.LastObservedSlot {
					continue
				}
				if newestCheckpoint.LastObservedSlot < sig.Slot {
					newestCheckpoint = models.AddressCheckpoint{
						LastObservedSlot:   sig.Slot,
						LastObservedTxCode: sig.Signature,
					}
				}
				if existing, ok := collected[sig.Signature]; !ok || sig.Slot > existing.Slot {
					collected[sig.Signature] = sig
				}
			}
			if len(signatures) < pageLimit {
				break
			}
			before = signatures[len(signatures)-1].Signature
			if before == "" {
				break
			}
		}
		if newestCheckpoint == checkpoint && latest.BlockNumber > newestCheckpoint.LastObservedSlot {
			newestCheckpoint.LastObservedSlot = latest.BlockNumber
		}
		updatedCheckpoints[account] = newestCheckpoint
	}

	ordered := make([]solana.SignatureInfo, 0, len(collected))
	for _, sig := range collected {
		ordered = append(ordered, sig)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Slot == ordered[j].Slot {
			return ordered[i].Signature < ordered[j].Signature
		}
		return ordered[i].Slot < ordered[j].Slot
	})
	for _, sig := range ordered {
		resp, err := s.chain.QueryTransaction(ctx, sig.Signature)
		if err != nil {
			return err
		}
		if resp != nil && resp.Tx != nil {
			if err := s.transitionTxState(sig.Signature, models.TxStateConfirmed, resp.Tx, resp.TxEvents, sig.Slot); err != nil {
				return err
			}
		}
	}
	for account, checkpoint := range updatedCheckpoints {
		sub.AccountCheckpoints[account] = checkpoint
	}
	return s.persistAddressSnapshot(sub)
}

func (s *subscriptionService) transitionTxState(txCode string, newState string, tx *models.ChainTx, txEvents []models.ChainEvent, blockNumber uint64) error {
	current := s.getTrackedState(txCode)
	if !shouldTransition(current.State, newState) {
		return nil
	}

	next := models.PublishedTxState{
		CreatedAt:   time.Now(),
		NetworkCode: s.chainNetworkCode(),
		BlockNumber: blockNumber,
		State:       newState,
	}
	if current.CreatedAt.IsZero() {
		next.CreatedAt = time.Now()
	} else {
		next.CreatedAt = current.CreatedAt
		next.NetworkCode = current.NetworkCode
		if next.NetworkCode == "" {
			next.NetworkCode = s.chainNetworkCode()
		}
	}
	if tx != nil {
		next.NetworkCode = tx.NetworkCode
		if tx.BlockNumber > 0 {
			next.BlockNumber = tx.BlockNumber
		}
	}
	if next.BlockNumber == 0 {
		next.BlockNumber = current.BlockNumber
	}

	if newState == models.TxStateConfirmed {
		msg := models.TxCallbackMessage{
			Tx:       tx,
			TxEvents: txEvents,
		}
		if err := s.publisher.PublishTx(msg); err != nil {
			return err
		}
	}

	if newState == models.TxStateReverted {
		if err := s.publisher.PublishRollback(models.TxRollbackMessage{
			TxCode:      txCode,
			NetworkCode: next.NetworkCode,
		}); err != nil {
			log.Warningf("publish rollback failed network=%s tx=%s err=%v", next.NetworkCode, txCode, err)
		}
	}

	s.mu.Lock()
	s.publishedState[txCode] = next
	s.mu.Unlock()
	if err := s.store.SavePublishedState(context.Background(), txCode, next); err != nil {
		log.Warningf("persist tracked tx state failed network=%s tx=%s state=%s err=%v", next.NetworkCode, txCode, newState, err)
	}

	return nil
}

func (s *subscriptionService) getTrackedState(txCode string) models.PublishedTxState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publishedState[txCode]
}

func (s *subscriptionService) persistAddressSnapshot(sub *models.AddressSubscription) error {
	clone := cloneAddressSub(sub)
	if err := s.store.SaveAddressSubscription(context.Background(), clone); err != nil {
		return err
	}
	s.mu.Lock()
	s.addressSubs[sub.Address] = cloneAddressSub(sub)
	s.mu.Unlock()
	return nil
}

func (s *subscriptionService) chainNetworkCode() string {
	if chainSvc, ok := s.chain.(*chainService); ok && chainSvc.network != nil {
		return chainSvc.network.Code
	}
	return "solana"
}

func cloneTxSub(sub *models.TxSubscription) *models.TxSubscription {
	if sub == nil {
		return nil
	}
	copy := *sub
	return &copy
}

func (s *subscriptionService) updateTxSubscriptionStatus(txCode string, status string) error {
	if err := s.store.UpdateTxSubscriptionStatus(context.Background(), txCode, status); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.txWatchers[txCode]; ok {
		cancel()
		delete(s.txWatchers, txCode)
	}
	if sub, ok := s.txSubs[txCode]; ok {
		sub.SubscriptionStatus = status
	}
	delete(s.txSubs, txCode)
	return nil
}

func (s *subscriptionService) updateAddressSubscriptionStatus(address string, status string) error {
	if err := s.store.UpdateAddressSubscriptionStatus(context.Background(), address, status); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.addressWatchers[address]; ok {
		cancel()
		delete(s.addressWatchers, address)
	}
	if sub, ok := s.addressSubs[address]; ok {
		sub.SubscriptionStatus = status
	}
	delete(s.addressSubs, address)
	return nil
}

func cloneAddressSub(sub *models.AddressSubscription) *models.AddressSubscription {
	if sub == nil {
		return nil
	}
	copy := *sub
	if sub.TrackedAccounts != nil {
		copy.TrackedAccounts = append([]string(nil), sub.TrackedAccounts...)
	}
	if sub.AccountCheckpoints != nil {
		copy.AccountCheckpoints = make(map[string]models.AddressCheckpoint, len(sub.AccountCheckpoints))
		for account, checkpoint := range sub.AccountCheckpoints {
			copy.AccountCheckpoints[account] = checkpoint
		}
	}
	return &copy
}

func (s *subscriptionService) pollAddressSubscriptions() {
	s.mu.RLock()
	addresses := make([]string, 0, len(s.addressSubs))
	for address := range s.addressSubs {
		addresses = append(addresses, address)
	}
	s.mu.RUnlock()

	for _, address := range addresses {
		if err := s.refreshAddressSubscription(address); err != nil {
			log.Warningf("refresh address subscription failed network=%s address=%s err=%v", s.chainNetworkCode(), address, err)
			continue
		}
		if err := s.backfillAddressAccounts(address, nil); err != nil {
			log.Warningf("poll address subscription failed network=%s address=%s err=%v", s.chainNetworkCode(), address, err)
		}
	}
}

func (s *subscriptionService) refreshAddressSubscription(address string) error {
	s.mu.Lock()
	sub, ok := s.addressSubs[address]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	before := cloneAddressSub(sub)
	s.reconcileAddressTargetsLocked(sub)
	after := cloneAddressSub(sub)
	restart := !sameStrings(before.TrackedAccounts, after.TrackedAccounts)
	s.addressSubs[address] = cloneAddressSub(sub)
	if restart {
		if cancel, ok := s.addressWatchers[address]; ok {
			cancel()
			delete(s.addressWatchers, address)
		}
		s.startAddressWatcherLocked(address)
	}
	s.mu.Unlock()

	return s.store.SaveAddressSubscription(context.Background(), after)
}

func (s *subscriptionService) reconcileAddressTargetsLocked(sub *models.AddressSubscription) {
	if sub.AccountCheckpoints == nil {
		sub.AccountCheckpoints = make(map[string]models.AddressCheckpoint)
	}
	trackedAccounts := s.deriveTrackedTokenAccounts(sub.Address)
	current := make(map[string]struct{}, len(trackedAccounts))
	for _, account := range trackedAccounts {
		current[account] = struct{}{}
		if _, ok := sub.AccountCheckpoints[account]; !ok {
			sub.AccountCheckpoints[account] = models.AddressCheckpoint{}
		}
	}
	for account := range sub.AccountCheckpoints {
		if _, ok := current[account]; !ok {
			delete(sub.AccountCheckpoints, account)
		}
	}
	sub.TrackedAccounts = trackedAccounts
}

func (s *subscriptionService) deriveTrackedTokenAccounts(address string) []string {
	accounts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, token := range s.cfg.Tokens {
		if token == nil || token.MintAddress == "" {
			continue
		}
		if token.NetworkCode != "" && token.NetworkCode != s.chainNetworkCode() {
			continue
		}
		for _, programID := range []string{solana.TokenProgramID, solana.Token2022ProgramID} {
			account, err := solana.DeriveAssociatedTokenAddress(address, token.MintAddress, programID)
			if err != nil {
				continue
			}
			if _, ok := seen[account]; ok {
				continue
			}
			seen[account] = struct{}{}
			accounts = append(accounts, account)
		}
	}
	sort.Strings(accounts)
	return accounts
}

func (s *subscriptionService) getAddressSub(address string) *models.AddressSubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.addressSubs[address]
	if !ok {
		return nil
	}
	return cloneAddressSub(sub)
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func shouldTransition(current string, target string) bool {
	if target == "" || current == target {
		return false
	}
	if target == models.TxStateDropped {
		return current == ""
	}
	if target == models.TxStateReverted {
		return current != "" && current != models.TxStateDropped && current != models.TxStateReverted
	}
	return txStateRank(target) > txStateRank(current)
}

func txStateRank(state string) int {
	switch state {
	case models.TxStateConfirmed:
		return 1
	case models.TxStateFinalized:
		return 2
	default:
		return 0
	}
}

func isTerminalTxState(state string) bool {
	return state == models.TxStateFinalized || state == models.TxStateDropped || state == models.TxStateReverted
}

func maxUint64(a uint64, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
