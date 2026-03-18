package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
)

func TestDeriveTrackedAccountsIncludesWalletAndTokenAccounts(t *testing.T) {
	s := &subscriptionService{
		cfg: &config.Config{
			Tokens: map[string]*config.Token{
				"USDC": {
					Networkcode: "solana",
					Mintaddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
				},
				"OTHER": {
					Networkcode: "other",
					Mintaddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
				},
			},
		},
		chain: &chainService{
			network: &config.SolanaNetwork{Networkcode: "solana"},
		},
	}

	address := "7dHbWXad2mL2K5uJc3n6iVYw6B8D7Jxq3n4of8wW8Re1"
	tokenATA, err := solana.DeriveAssociatedTokenAddress(address, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", solana.TokenProgramID)
	if err != nil {
		t.Fatalf("derive token ata: %v", err)
	}
	token2022ATA, err := solana.DeriveAssociatedTokenAddress(address, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", solana.Token2022ProgramID)
	if err != nil {
		t.Fatalf("derive token2022 ata: %v", err)
	}

	got := s.deriveTrackedAccounts(address)
	want := []string{address, token2022ATA, tokenATA}
	if len(got) != len(want) {
		t.Fatalf("tracked accounts len=%d want=%d accounts=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tracked accounts[%d]=%s want=%s all=%v", i, got[i], want[i], got)
		}
	}
}

type testCallbackPublisher struct {
	txErr         error
	txCalls       int
	rollbackErr   error
	rollbackCalls int
}

func (p *testCallbackPublisher) PublishTx(msg models.TxCallbackMessage) error {
	p.txCalls++
	return p.txErr
}

func (p *testCallbackPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	p.rollbackCalls++
	return p.rollbackErr
}

type testSubscriptionStore struct {
	pending map[string]*models.PendingCallback
}

func (s *testSubscriptionStore) Load(ctx context.Context) (*models.SubscriptionSnapshot, error) {
	return &models.SubscriptionSnapshot{
		TxSubs:           map[string]*models.TxSubscription{},
		AddressSubs:      map[string]*models.AddressSubscription{},
		PublishedState:   map[string]models.PublishedTxState{},
		PendingCallbacks: map[string]*models.PendingCallback{},
	}, nil
}

func (s *testSubscriptionStore) SaveTxSubscription(ctx context.Context, sub *models.TxSubscription) error {
	return nil
}

func (s *testSubscriptionStore) UpdateTxSubscriptionStatus(ctx context.Context, txCode string, status string) error {
	return nil
}

func (s *testSubscriptionStore) SaveAddressSubscription(ctx context.Context, sub *models.AddressSubscription) error {
	return nil
}

func (s *testSubscriptionStore) UpdateAddressSubscriptionStatus(ctx context.Context, address string, status string) error {
	return nil
}

func (s *testSubscriptionStore) SavePublishedState(ctx context.Context, txCode string, state models.PublishedTxState) error {
	return nil
}

func (s *testSubscriptionStore) SavePendingCallback(ctx context.Context, pending *models.PendingCallback) error {
	if s.pending == nil {
		s.pending = make(map[string]*models.PendingCallback)
	}
	copy := *pending
	s.pending[pending.TaskID] = &copy
	return nil
}

func (s *testSubscriptionStore) DeletePendingCallback(ctx context.Context, taskID string) error {
	delete(s.pending, taskID)
	return nil
}

func TestTransitionTxStatePersistsFailedCallbackAndRetries(t *testing.T) {
	publisher := &testCallbackPublisher{txErr: errors.New("callback down")}
	store := &testSubscriptionStore{pending: make(map[string]*models.PendingCallback)}
	s := &subscriptionService{
		cfg:              &config.Config{Connector: &config.Connector{Pollintervalms: 1}},
		publisher:        publisher,
		store:            store,
		publishedState:   make(map[string]models.PublishedTxState),
		pendingCallbacks: make(map[string]*models.PendingCallback),
	}

	txCode := "tx123"
	tx := &models.ChainTx{Code: txCode, NetworkCode: "solana"}
	if err := s.transitionTxState(txCode, models.TxStateConfirmed, tx, nil, 100); err != nil {
		t.Fatalf("transitionTxState failed: %v", err)
	}

	taskID := callbackTaskID(models.CallbackKindTx, txCode)
	if _, ok := s.pendingCallbacks[taskID]; !ok {
		t.Fatalf("expected pending callback in memory for %s", taskID)
	}
	if _, ok := store.pending[taskID]; !ok {
		t.Fatalf("expected pending callback in store for %s", taskID)
	}
	if publisher.txCalls != 1 {
		t.Fatalf("PublishTx calls=%d want=1", publisher.txCalls)
	}

	publisher.txErr = nil
	time.Sleep(2 * time.Millisecond)
	s.retryPendingCallbacks()

	if _, ok := s.pendingCallbacks[taskID]; ok {
		t.Fatalf("expected pending callback removed after retry")
	}
	if _, ok := store.pending[taskID]; ok {
		t.Fatalf("expected store pending callback removed after retry")
	}
	if publisher.txCalls != 2 {
		t.Fatalf("PublishTx calls=%d want=2", publisher.txCalls)
	}
}
