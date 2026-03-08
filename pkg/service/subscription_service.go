package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/store"
)

type SubscriptionService interface {
	RegisterTxSubscription(req models.TxSubscribeRequest) error
	RegisterAddressSubscription(req models.AddressSubscribeRequest) error
	CancelTxSubscription(txCode string) error
	CancelAddressSubscription(address string, endBlockNumber uint64) error
	SyncBlockRange(ctx context.Context, begin uint64, end uint64) error
}

func NewSubscriptionService(cfg *config.Config, chain ChainService, publisher CallbackPublisher, subscriptionStore store.SubscriptionStore) SubscriptionService {
	s := &subscriptionService{
		cfg:            cfg,
		chain:          chain,
		publisher:      publisher,
		store:          subscriptionStore,
		txSubs:         make(map[string]*models.TxSubscription),
		addressSubs:    make(map[string]*models.AddressSubscription),
		publishedState: make(map[string]models.PublishedTxState),
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

	mu             sync.RWMutex
	txSubs         map[string]*models.TxSubscription
	addressSubs    map[string]*models.AddressSubscription
	publishedState map[string]models.PublishedTxState
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
	if existing, ok := s.txSubs[req.TxCode]; ok && !existing.CreatedAt.IsZero() {
		sub.CreatedAt = existing.CreatedAt
	}
	if err := s.store.SaveTxSubscription(context.Background(), sub); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.txSubs[req.TxCode] = sub
	return nil
}

func (s *subscriptionService) RegisterAddressSubscription(req models.AddressSubscribeRequest) error {
	startBlock := uint64(0)
	if req.SubscribeRange.StartBlockNumber != nil {
		startBlock = *req.SubscribeRange.StartBlockNumber
	}

	s.mu.Lock()
	sub, ok := s.addressSubs[req.Address]
	if !ok {
		sub = &models.AddressSubscription{
			CreatedAt:        time.Now(),
			NetworkCode:      s.chainNetworkCode(),
			Address:          req.Address,
			StartBlockNumber: startBlock,
			EndBlockNumber:   req.SubscribeRange.EndBlockNumber,
			SeenTxs:          make(map[string]struct{}, s.cfg.Connector.SubscriptionBuffer),
		}
		s.addressSubs[req.Address] = sub
	} else {
		if startBlock < sub.StartBlockNumber {
			sub.StartBlockNumber = startBlock
		}
		sub.EndBlockNumber = req.SubscribeRange.EndBlockNumber
	}
	toSave := cloneAddressSub(sub)
	s.mu.Unlock()

	return s.store.SaveAddressSubscription(context.Background(), toSave)
}

func (s *subscriptionService) CancelTxSubscription(txCode string) error {
	return s.updateTxSubscriptionStatus(txCode, models.TxSubscriptionStatusCancelled, true)
}

func (s *subscriptionService) CancelAddressSubscription(address string, endBlockNumber uint64) error {
	s.mu.Lock()
	sub, ok := s.addressSubs[address]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	sub.EndBlockNumber = &endBlockNumber
	toSave := cloneAddressSub(sub)
	s.mu.Unlock()
	return s.store.SaveAddressSubscription(context.Background(), toSave)
}

func (s *subscriptionService) SyncBlockRange(ctx context.Context, begin uint64, end uint64) error {
	for slot := begin; slot <= end; slot++ {
		messages, err := s.chain.FetchBlockTransactions(ctx, slot)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			if err := s.publisher.PublishTx(msg); err != nil {
				return err
			}
			s.rememberPublished(msg.Tx.NetworkCode, msg.Tx.Code, msg.Tx.BlockNumber)
		}
	}
	return nil
}

func (s *subscriptionService) start() {
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
	log.Infof("subscription state restored txSubs=%d addressSubs=%d publishedStates=%d", len(s.txSubs), len(s.addressSubs), len(s.publishedState))
}

func (s *subscriptionService) poll(ctx context.Context) {
	s.pollTxSubscriptions(ctx)
	s.pollAddressSubscriptions(ctx)
	s.pollRollback(ctx)
}

func (s *subscriptionService) pollTxSubscriptions(ctx context.Context) {
	s.mu.RLock()
	subs := make([]*models.TxSubscription, 0, len(s.txSubs))
	for _, sub := range s.txSubs {
		subs = append(subs, cloneTxSub(sub))
	}
	s.mu.RUnlock()

	for _, sub := range subs {
		resp, err := s.chain.QueryTransaction(ctx, sub.TxCode)
		if err != nil {
			log.Warningf("poll tx subscription failed network=%s tx=%s err=%v", sub.NetworkCode, sub.TxCode, err)
			continue
		}
		if !resp.IfTxOnchain || resp.Tx == nil {
			latest, err := s.chain.GetLatestBlock(ctx)
			if err != nil {
				continue
			}
			if latest.BlockNumber > sub.EndBlockNumber {
				if err := s.updateTxSubscriptionStatus(sub.TxCode, models.TxSubscriptionStatusExpired, true); err != nil {
					log.Warningf("expire tx subscription failed network=%s tx=%s err=%v", sub.NetworkCode, sub.TxCode, err)
				}
			}
			continue
		}

		if err := s.publisher.PublishTx(models.TxCallbackMessage{Tx: *resp.Tx, TxEvents: resp.TxEvents}); err != nil {
			log.Warningf("publish tx subscription failed network=%s tx=%s err=%v", sub.NetworkCode, sub.TxCode, err)
			continue
		}
		s.rememberPublished(sub.NetworkCode, sub.TxCode, resp.Tx.BlockNumber)
		if err := s.updateTxSubscriptionStatus(sub.TxCode, models.TxSubscriptionStatusCompleted, true); err != nil {
			log.Warningf("complete tx subscription failed network=%s tx=%s err=%v", sub.NetworkCode, sub.TxCode, err)
		}
	}
}

func (s *subscriptionService) pollAddressSubscriptions(ctx context.Context) {
	s.mu.RLock()
	subs := make([]*models.AddressSubscription, 0, len(s.addressSubs))
	for _, sub := range s.addressSubs {
		subs = append(subs, cloneAddressSub(sub))
	}
	s.mu.RUnlock()

	for _, sub := range subs {
		before := ""
		if !sub.HistoryComplete {
			before = sub.LastBefore
		}
		signatures, err := s.chain.FetchAddressSignatures(ctx, sub.Address, 100, before)
		if err != nil {
			log.Warningf("poll address subscription failed network=%s address=%s err=%v", sub.NetworkCode, sub.Address, err)
			continue
		}
		if len(signatures) == 0 {
			continue
		}

		sort.Slice(signatures, func(i, j int) bool { return signatures[i].Slot < signatures[j].Slot })
		for _, sig := range signatures {
			if sig.Slot < sub.StartBlockNumber {
				continue
			}
			if sub.EndBlockNumber != nil && sig.Slot > *sub.EndBlockNumber {
				continue
			}
			if _, seen := sub.SeenTxs[sig.Signature]; seen {
				continue
			}
			receipt, err := s.chain.QueryTransaction(ctx, sig.Signature)
			if err != nil || !receipt.IfTxOnchain || receipt.Tx == nil {
				continue
			}
			if err := s.publisher.PublishTx(models.TxCallbackMessage{Tx: *receipt.Tx, TxEvents: receipt.TxEvents}); err != nil {
				log.Warningf("publish address subscription failed network=%s address=%s tx=%s err=%v", sub.NetworkCode, sub.Address, sig.Signature, err)
				continue
			}

			s.rememberPublished(sub.NetworkCode, sig.Signature, receipt.Tx.BlockNumber)
			sub.SeenTxs[sig.Signature] = struct{}{}
			s.trimSeen(sub)
			if err := s.persistAddressSnapshot(sub); err != nil {
				log.Warningf("persist address subscription failed network=%s address=%s err=%v", sub.NetworkCode, sub.Address, err)
			}
		}

		oldest := signatures[0].Slot
		newest := signatures[len(signatures)-1].Slot
		if !sub.HistoryComplete {
			sub.LastBefore = signatures[0].Signature
			if oldest <= sub.StartBlockNumber || len(signatures) < 100 {
				sub.HistoryComplete = true
				sub.LastBefore = ""
			}
		}

		if sub.EndBlockNumber != nil && newest >= *sub.EndBlockNumber {
			if err := s.store.DeleteAddressSubscription(context.Background(), sub.Address); err != nil {
				log.Warningf("delete address subscription failed network=%s address=%s err=%v", sub.NetworkCode, sub.Address, err)
			}
			s.mu.Lock()
			delete(s.addressSubs, sub.Address)
			s.mu.Unlock()
			continue
		}

		if err := s.persistAddressSnapshot(sub); err != nil {
			log.Warningf("persist address subscription failed network=%s address=%s err=%v", sub.NetworkCode, sub.Address, err)
		}
	}
}

func (s *subscriptionService) pollRollback(ctx context.Context) {
	s.mu.RLock()
	states := make(map[string]models.PublishedTxState, len(s.publishedState))
	for txCode, state := range s.publishedState {
		states[txCode] = state
	}
	s.mu.RUnlock()

	for txCode, state := range states {
		latest, err := s.chain.GetLatestBlock(ctx)
		if err == nil && latest.BlockNumber > state.BlockNumber+s.cfg.Connector.ReorgDepth {
			s.mu.Lock()
			delete(s.publishedState, txCode)
			s.mu.Unlock()
			if err := s.store.DeletePublishedState(context.Background(), txCode); err != nil {
				log.Warningf("delete published state failed network=%s tx=%s err=%v", state.NetworkCode, txCode, err)
			}
			continue
		}
		live, err := s.chain.CheckSignatureLive(ctx, txCode)
		if err != nil {
			continue
		}
		if live {
			continue
		}
		if err := s.publisher.PublishRollback(models.TxRollbackMessage{
			TxCode:      txCode,
			NetworkCode: state.NetworkCode,
		}); err != nil {
			log.Warningf("publish rollback failed network=%s tx=%s err=%v", state.NetworkCode, txCode, err)
			continue
		}
		s.mu.Lock()
		delete(s.publishedState, txCode)
		s.mu.Unlock()
		if err := s.store.DeletePublishedState(context.Background(), txCode); err != nil {
			log.Warningf("delete published state failed network=%s tx=%s err=%v", state.NetworkCode, txCode, err)
		}
	}
}

func (s *subscriptionService) rememberPublished(networkCode string, txCode string, blockNumber uint64) {
	state := models.PublishedTxState{
		CreatedAt:   time.Now(),
		NetworkCode: networkCode,
		BlockNumber: blockNumber,
	}
	if existing, ok := s.publishedState[txCode]; ok && !existing.CreatedAt.IsZero() {
		state.CreatedAt = existing.CreatedAt
	}
	s.mu.Lock()
	s.publishedState[txCode] = state
	s.mu.Unlock()
	if err := s.store.SavePublishedState(context.Background(), txCode, state); err != nil {
		log.Warningf("persist published state failed network=%s tx=%s err=%v", networkCode, txCode, err)
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
	if sub, ok := s.txSubs[txCode]; ok {
		sub.SubscriptionStatus = status
		sub.Completed = completed
	}
	delete(s.txSubs, txCode)
	return nil
}

func cloneAddressSub(sub *models.AddressSubscription) *models.AddressSubscription {
	if sub == nil {
		return nil
	}
	copy := *sub
	if sub.EndBlockNumber != nil {
		value := *sub.EndBlockNumber
		copy.EndBlockNumber = &value
	}
	copy.SeenTxs = make(map[string]struct{}, len(sub.SeenTxs))
	for key := range sub.SeenTxs {
		copy.SeenTxs[key] = struct{}{}
	}
	return &copy
}
