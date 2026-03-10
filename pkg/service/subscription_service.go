package service

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	SyncBlockRange(ctx context.Context, begin uint64, end uint64) error
}

func NewSubscriptionService(cfg *config.Config, chain ChainService, publisher CallbackPublisher, subscriptionStore store.SubscriptionStore) SubscriptionService {
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
	publisher CallbackPublisher
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
			LastObservedSlot:   0,
			LastObservedTxCode: "",
			SubscriptionStatus: models.TxSubscriptionStatusActive,
			SeenTxs:            make(map[string]struct{}, s.cfg.Connector.SubscriptionBuffer),
		}
	} else {
		sub.SubscriptionStatus = models.TxSubscriptionStatusActive
		sub.Completed = false
	}
	toSave := cloneAddressSub(sub)
	s.addressSubs[req.Address] = cloneAddressSub(sub)
	s.startAddressWatcherLocked(req.Address)
	s.mu.Unlock()

	return s.store.SaveAddressSubscription(context.Background(), toSave)
}

func (s *subscriptionService) CancelTxSubscription(txCode string) error {
	return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCancelled, true)
}

func (s *subscriptionService) CancelAddressSubscription(address string) error {
	return s.updateAddressSubscriptionStatus(address, models.TxSubscriptionStatusCancelled, true)
}

func (s *subscriptionService) SyncBlockRange(ctx context.Context, begin uint64, end uint64) error {
	for slot := begin; slot <= end; slot++ {
		messages, err := s.chain.FetchBlockTransactions(ctx, slot)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			if msg.Tx == nil {
				continue
			}
			if err := s.transitionTxState(msg.Tx.Code, models.TxStateFinalized, msg.Tx, msg.TxEvents, msg.Tx.BlockNumber); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *subscriptionService) start() {
	s.resumeWatchers()

	ticker := time.NewTicker(time.Duration(s.cfg.Connector.PollIntervalMs) * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Connector.RequestTimeoutMs)*time.Millisecond)
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
	s.gcTrackedTransactions(latest.BlockNumber)
}

func (s *subscriptionService) pollTxSubscriptions(ctx context.Context, latestBlock uint64) {
	s.mu.RLock()
	subs := make([]*models.TxSubscription, 0, len(s.txSubs))
	for _, sub := range s.txSubs {
		subs = append(subs, cloneTxSub(sub))
	}
	s.mu.RUnlock()

	for _, sub := range subs {
		if err := s.advanceTxState(ctx, sub.TxCode, sub.EndBlockNumber, true, latestBlock); err != nil {
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
		if err := s.advanceTxState(ctx, txCode, 0, false, latestBlock); err != nil {
			log.Warningf("advance tracked tx failed network=%s tx=%s err=%v", state.NetworkCode, txCode, err)
		}
	}
}

func (s *subscriptionService) gcTrackedTransactions(latestBlock uint64) {
	s.mu.RLock()
	states := make(map[string]models.PublishedTxState, len(s.publishedState))
	for txCode, state := range s.publishedState {
		states[txCode] = state
	}
	s.mu.RUnlock()

	for txCode, state := range states {
		if !isTerminalTxState(state.State) {
			continue
		}
		if latestBlock <= state.BlockNumber+s.cfg.Connector.ReorgDepth {
			continue
		}
		s.deleteTrackedState(txCode)
	}
}

func (s *subscriptionService) advanceTxState(ctx context.Context, txCode string, endBlock uint64, allowDrop bool, latestBlock uint64) error {
	status, err := s.chain.GetSignatureStatus(ctx, txCode)
	if err != nil {
		return err
	}
	current := s.getTrackedState(txCode)

	if status.Exists {
		target := mapSignatureStatusToState(status)
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

		for _, state := range txTransitionPath(current.State, target) {
			if err := s.transitionTxState(txCode, state, tx, txEvents, status.Slot); err != nil {
				return err
			}
		}

		if target == models.TxStateFinalized {
			return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCompleted, true)
		}
		return nil
	}

	if current.State != "" && !isTerminalTxState(current.State) {
		if err := s.transitionTxState(txCode, models.TxStateReverted, nil, nil, maxUint64(current.BlockNumber, latestBlock)); err != nil {
			return err
		}
		_ = s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCompleted, true)
		return nil
	}

	if allowDrop && latestBlock > endBlock {
		if err := s.transitionTxState(txCode, models.TxStateDropped, nil, nil, latestBlock); err != nil {
			return err
		}
		return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusExpired, true)
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
			if !sleepWithContext(ctx, time.Duration(s.cfg.Connector.RetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		_ = notification
	}
}

func (s *subscriptionService) runAddressWatcher(ctx context.Context, address string) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.backfillProgramGap(address); err != nil {
			log.Warningf("program backfill failed network=%s program=%s err=%v", s.chainNetworkCode(), address, err)
		}
		err := s.chain.WatchProgramLogs(ctx, address, func() error {
			return s.backfillProgramGap(address)
		}, func(notification solana.LogsNotification) error {
			return s.handleProgramNotification(address, notification)
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warningf("ws program watcher failed network=%s program=%s err=%v", s.chainNetworkCode(), address, err)
			if !sleepWithContext(ctx, time.Duration(s.cfg.Connector.RetryBackoffMs)*time.Millisecond) {
				return
			}
			continue
		}
		return
	}
}

func (s *subscriptionService) handleProgramNotification(address string, notification solana.LogsNotification) error {
	s.mu.RLock()
	sub, ok := s.addressSubs[address]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	sub = cloneAddressSub(sub)
	s.mu.RUnlock()

	if _, seen := sub.SeenTxs[notification.Signature]; seen {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Connector.RequestTimeoutMs)*time.Millisecond)
	defer cancel()
	resp, err := s.chain.QueryTransaction(ctx, notification.Signature)
	if err != nil {
		return err
	}
	if resp != nil && resp.Tx != nil {
		if err := s.transitionTxState(notification.Signature, models.TxStateConfirmed, resp.Tx, resp.TxEvents, notification.Slot); err != nil {
			return err
		}
	}

	sub.SeenTxs[notification.Signature] = struct{}{}
	if notification.Slot > sub.LastObservedSlot {
		sub.LastObservedSlot = notification.Slot
	}
	sub.LastObservedTxCode = notification.Signature
	s.trimSeen(sub)
	return s.persistAddressSnapshot(sub)
}

// backfillProgramGap 是在补 program 订阅断连窗口里漏掉的交易。
// 在 address-subscribe 的 WS watcher 启动或重连前，按 lastObservedSlot 到当前最新 slot 的区间，把这段时间里属于该 programId 的交易重新
//
//	扫一遍，避免 WS 断开期间漏消息。
func (s *subscriptionService) backfillProgramGap(address string) error {
	s.mu.RLock()
	// 读取当前 program 订阅快照
	sub, ok := s.addressSubs[address]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	sub = cloneAddressSub(sub)
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Connector.RequestTimeoutMs)*time.Millisecond)
	defer cancel()

	// 获取当前最新 slot
	latest, err := s.chain.GetLatestBlock(ctx)
	if err != nil {
		return err
	}

	// 还没有 checkpoint 时，不回扫历史，直接从当前链头开始跟踪未来。
	if sub.LastObservedSlot == 0 || sub.LastObservedTxCode == "" {
		sub.LastObservedSlot = latest.BlockNumber
		sub.LastObservedTxCode = ""
		return s.persistAddressSnapshot(sub)
	}
	if latest.BlockNumber <= sub.LastObservedSlot {
		return nil
	}

	const pageLimit = 1000
	before := ""
	until := sub.LastObservedTxCode
	minContextSlot := sub.LastObservedSlot
	seenNewest := false
	collected := make([]solana.SignatureInfo, 0, pageLimit)

	for {
		signatures, err := s.chain.FetchAddressSignatures(ctx, address, solana.SignatureQueryOptions{
			Limit:          pageLimit,
			Before:         before,
			Until:          until,
			MinContextSlot: minContextSlot,
		})
		if err != nil {
			return err
		}
		if len(signatures) == 0 {
			break
		}
		for _, sig := range signatures {
			if sig.Slot <= sub.LastObservedSlot {
				continue
			}
			collected = append(collected, sig)
			seenNewest = true
		}
		if len(signatures) < pageLimit {
			break
		}
		before = signatures[len(signatures)-1].Signature
		if before == "" {
			break
		}
	}

	if !seenNewest {
		sub.LastObservedSlot = latest.BlockNumber
		return s.persistAddressSnapshot(sub)
	}

	for i := len(collected) - 1; i >= 0; i-- {
		sig := collected[i]
		resp, err := s.chain.QueryTransaction(ctx, sig.Signature)
		if err != nil {
			return err
		}
		if resp != nil && resp.Tx != nil {
			if err := s.transitionTxState(sig.Signature, models.TxStateConfirmed, resp.Tx, resp.TxEvents, sig.Slot); err != nil {
				return err
			}
		}
		sub.SeenTxs[sig.Signature] = struct{}{}
		if sig.Slot > sub.LastObservedSlot {
			sub.LastObservedSlot = sig.Slot
			sub.LastObservedTxCode = sig.Signature
		}
	}
	if latest.BlockNumber > sub.LastObservedSlot {
		sub.LastObservedSlot = latest.BlockNumber
	}
	s.trimSeen(sub)
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

	msg := models.TxCallbackMessage{
		State:         newState,
		PreviousState: current.State,
		Tx:            tx,
		TxEvents:      txEvents,
	}
	if err := s.publisher.PublishTx(msg); err != nil {
		return err
	}

	s.mu.Lock()
	s.publishedState[txCode] = next
	s.mu.Unlock()
	if err := s.store.SavePublishedState(context.Background(), txCode, next); err != nil {
		log.Warningf("persist tracked tx state failed network=%s tx=%s state=%s err=%v", next.NetworkCode, txCode, newState, err)
	}

	if newState == models.TxStateReverted {
		if err := s.publisher.PublishRollback(models.TxRollbackMessage{
			TxCode:        txCode,
			NetworkCode:   next.NetworkCode,
			State:         newState,
			PreviousState: current.State,
		}); err != nil {
			log.Warningf("publish rollback failed network=%s tx=%s err=%v", next.NetworkCode, txCode, err)
		}
	}
	return nil
}

func (s *subscriptionService) getTrackedState(txCode string) models.PublishedTxState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publishedState[txCode]
}

func (s *subscriptionService) deleteTrackedState(txCode string) {
	s.mu.Lock()
	delete(s.publishedState, txCode)
	s.mu.Unlock()
	if err := s.store.DeletePublishedState(context.Background(), txCode); err != nil {
		log.Warningf("delete tracked tx state failed tx=%s err=%v", txCode, err)
	}
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

func (s *subscriptionService) trimSeen(sub *models.AddressSubscription) {
	if len(sub.SeenTxs) <= s.cfg.Connector.SubscriptionBuffer {
		return
	}
	for key := range sub.SeenTxs {
		delete(sub.SeenTxs, key)
		if len(sub.SeenTxs) <= s.cfg.Connector.SubscriptionBuffer {
			return
		}
	}
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

func (s *subscriptionService) updateTxSubscriptionStatus(txCode string, status string, completed bool) error {
	if err := s.store.UpdateTxSubscriptionStatus(context.Background(), txCode, status, completed); err != nil {
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
		sub.Completed = completed
	}
	delete(s.txSubs, txCode)
	return nil
}

func (s *subscriptionService) updateAddressSubscriptionStatus(address string, status string, completed bool) error {
	if err := s.store.UpdateAddressSubscriptionStatus(context.Background(), address, status, completed); err != nil {
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
		sub.Completed = completed
	}
	delete(s.addressSubs, address)
	return nil
}

// cloneAddressSub 的核心作用是把 AddressSubscription 做成“快照”，避免把内存里的同一个对象在多个 goroutine 之间直接共享。
//
// 这里必须 clone，主要有两个原因。
//
// 第一，AddressSubscription 里有 SeenTxs map[string]struct{}，这是引用类型。
// 如果只写 copy := *sub 而不深拷贝 SeenTxs，那么副本和原对象其实还会共享同一张 map。后面像 subscription_service.go:358 的 handleProgramNotification、subscription_service.go:398 的 backfillProgramGap 都会在解锁后修改 SeenTxs、
// LastObservedSlot、LastObservedTxCode。如果共享同一张 map，就很容易出现并发读写和脏数据。
//
// 第二，这个服务的锁粒度是“读出来后尽快解锁，再做 RPC 和持久化”。
// 比如 subscription_service.go:358 里先 RLock 取订阅，再立刻 cloneAddressSub(sub)，然后释放锁，后续才去 QueryTransaction、更新 checkpoint、落库。这样做的目的是：
//
// - 不把网络请求放在锁里面，避免阻塞其他订阅
// - 后续修改的是本地快照，不会直接改坏共享内存
// - persistAddressSnapshot 再把这个快照写库并替换回内存，见 subscription_service.go:561
//
// 所以它不是“多余复制”，而是在当前并发模型里保证这几点：
//
// - SeenTxs 不共享引用
// - 解锁后可以安全修改订阅快照
// - 落库时写的是一个稳定状态，不是正在被别的 watcher 改动的对象
//
// 如果去掉它，最直接的风险就是 SeenTxs 这张 map 产生数据竞争。
func cloneAddressSub(sub *models.AddressSubscription) *models.AddressSubscription {
	if sub == nil {
		return nil
	}
	copy := *sub
	copy.SeenTxs = make(map[string]struct{}, len(sub.SeenTxs))
	for key := range sub.SeenTxs {
		copy.SeenTxs[key] = struct{}{}
	}
	return &copy
}

func mapSignatureStatusToState(status *solana.SignatureStatus) string {
	if status == nil || !status.Exists {
		return ""
	}
	switch status.ConfirmationStatus {
	case "finalized":
		return models.TxStateFinalized
	case "confirmed", "processed":
		return models.TxStateConfirmed
	default:
		return ""
	}
}

func txTransitionPath(current string, target string) []string {
	if target == "" || txStateRank(target) <= txStateRank(current) {
		return nil
	}
	path := []string{models.TxStateConfirmed, models.TxStateFinalized}
	out := make([]string, 0, len(path))
	for _, state := range path {
		if txStateRank(state) <= txStateRank(current) {
			continue
		}
		if txStateRank(state) > txStateRank(target) {
			break
		}
		out = append(out, state)
	}
	return out
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
