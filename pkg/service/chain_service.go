package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
	"github.com/guyuxiang/projectc-solana-connector/pkg/store"
	"gorm.io/gorm"
)

type ChainService interface {
	SendSignedTransaction(ctx context.Context, txSignResult string) (string, error)
	Faucet(ctx context.Context, req models.FaucetRequest) (string, error)
	QueryTransaction(ctx context.Context, txCode string) (*models.TxQueryResponse, error)
	GetAddressBalance(ctx context.Context, address string) (*models.AddressBalanceResponse, error)
	GetTokenSupply(ctx context.Context, tokenCode string) (*models.TokenSupplyResponse, error)
	GetTokenBalance(ctx context.Context, tokenCode string, address string) (*models.TokenBalanceResponse, error)
	AddToken(ctx context.Context, req models.TokenAddRequest) (*models.TokenResponse, error)
	GetToken(ctx context.Context, tokenCode string) (*models.TokenResponse, error)
	ListTokens(ctx context.Context, req models.TokenListRequest) (*models.TokenListResponse, error)
	DeleteToken(ctx context.Context, tokenCode string) error
	GetLatestBlock(ctx context.Context) (*models.LatestBlockResponse, error)
	GetTransactionReceipt(ctx context.Context, txCode string) (*models.ChainTx, []models.ChainEvent, bool, error)
	FetchAddressSignatures(ctx context.Context, address string, opts solana.SignatureQueryOptions) ([]solana.SignatureInfo, error)
	FetchBlockTransactions(ctx context.Context, slot uint64) ([]models.TxCallbackMessage, error)
	CheckSignatureLive(ctx context.Context, txCode string) (bool, error)
	GetSignatureStatus(ctx context.Context, txCode string) (*solana.SignatureStatus, error)
	WatchSignature(ctx context.Context, txCode string) (*solana.SignatureNotification, error)
	WatchAccount(ctx context.Context, account string, onSubscribed func() error, handler func(solana.AccountNotification) error) error
}

func NewChainService(cfg *config.Config, tokenStore store.TokenStore) ChainService {
	timeout := time.Duration(defaultRequestTimeoutMs) * time.Millisecond
	backoff := time.Duration(defaultRetryBackoffMs) * time.Millisecond
	clients := make(map[string]*solana.Client, 1)
	wsClients := make(map[string]*solana.WSClient, 1)
	network := mustResolveSingleNetwork(cfg)
	clients[network.Code] = solana.NewClient([]string{network.RPCURL}, timeout, defaultRetryTimes, backoff, defaultRPCCommitment)
	wsEndpoints := []string{}
	if network.WSURL != "" {
		wsEndpoints = []string{network.WSURL}
	} else {
		wsEndpoints = solana.DeriveWSEndpoints([]string{network.RPCURL})
	}
	idleTimeout := time.Duration(defaultWSIdleTimeoutMs) * time.Millisecond
	wsClients[network.Code] = solana.NewWSClient(wsEndpoints, timeout, idleTimeout)
	return &chainService{
		cfg:              cfg,
		tokenStore:       tokenStore,
		clients:          clients,
		wsClients:        wsClients,
		network:          network,
		idempotencyStore: newIdempotencyStore(time.Duration(defaultIdempotencyTTLSeconds) * time.Second),
	}
}

type chainService struct {
	cfg        *config.Config
	tokenMu    sync.RWMutex
	tokenStore store.TokenStore
	clients    map[string]*solana.Client
	wsClients  map[string]*solana.WSClient
	network    *config.SolanaNetwork

	idempotencyStore *idempotencyStore
}

func (s *chainService) SendSignedTransaction(ctx context.Context, txSignResult string) (string, error) {
	client, err := s.resolveClient()
	if err != nil {
		return "", err
	}

	encoding := "base64"
	if _, err := base64.StdEncoding.DecodeString(txSignResult); err != nil {
		if _, err := solana.DecodeBase58(txSignResult); err == nil {
			encoding = "base58"
		} else {
			return "", fmt.Errorf("txSignResult must be base64 or base58 encoded")
		}
	}

	var signature string // 交易hash
	params := []interface{}{
		txSignResult,
		map[string]interface{}{
			"encoding":            encoding,
			"preflightCommitment": client.Commitment(),
			"skipPreflight":       false,
			"maxRetries":          defaultRetryTimes,
			"minContextSlot":      nil,
		},
	}
	if err := client.Call(ctx, "sendTransaction", params, &signature); err != nil {
		return "", err
	}
	return signature, nil
}

func (s *chainService) Faucet(ctx context.Context, req models.FaucetRequest) (string, error) {
	client, err := s.resolveClient()
	if err != nil {
		return "", err
	}
	if s.cfg.Wallet == nil || s.cfg.Wallet.PrivateKeyBase58 == "" {
		return "", errors.New("wallet.privateKeyBase58 is required")
	}

	if txCode, ok := s.idempotencyStore.Get(req.IdempotencyKey); ok {
		return txCode, nil
	}

	lamports, err := toLamports(req.Value, solanaLamportsPerSOL)
	if err != nil {
		return "", err
	}

	var latest solana.LatestBlockhashResponse
	params := []interface{}{map[string]interface{}{"commitment": client.Commitment()}}
	if err := client.Call(ctx, "getLatestBlockhash", params, &latest); err != nil {
		return "", err
	}

	encodedTx, fromAddress, err := solana.BuildNativeTransferTx(s.cfg.Wallet.PrivateKeyBase58, req.AcceptAddress, latest.Value.Blockhash, lamports)
	if err != nil {
		return "", err
	}
	if s.cfg.Wallet.FromAddress != "" && !strings.EqualFold(s.cfg.Wallet.FromAddress, fromAddress) {
		return "", errors.New("wallet private key does not match configured fromAddress")
	}

	txCode, err := s.SendSignedTransaction(ctx, encodedTx)
	if err != nil {
		return "", err
	}
	s.idempotencyStore.Put(req.IdempotencyKey, txCode)
	return txCode, nil
}

func (s *chainService) QueryTransaction(ctx context.Context, txCode string) (*models.TxQueryResponse, error) {
	tx, events, onchain, err := s.GetTransactionReceipt(ctx, txCode)
	if err != nil {
		return nil, err
	}
	return &models.TxQueryResponse{
		IfTxOnchain: onchain,
		Tx:          tx,
		TxEvents:    events,
	}, nil
}

func (s *chainService) GetAddressBalance(ctx context.Context, address string) (*models.AddressBalanceResponse, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}
	network := s.network

	var resp struct {
		Context struct{} `json:"context"`
		Value   uint64   `json:"value"`
	}
	params := []interface{}{
		address,
		map[string]interface{}{
			"commitment": client.Commitment(),
		},
	}
	if err := client.Call(ctx, "getBalance", params, &resp); err != nil {
		return nil, err
	}
	return &models.AddressBalanceResponse{
		Balance:     fromLamports(resp.Value, solanaLamportsPerSOL),
		BalanceUnit: network.NativeSymbol,
	}, nil
}

func (s *chainService) GetTokenSupply(ctx context.Context, tokenCode string) (*models.TokenSupplyResponse, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}
	token, err := s.resolveToken(tokenCode)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Context struct{} `json:"context"`
		Value   struct {
			UIAmountString string `json:"uiAmountString"`
		} `json:"value"`
	}
	params := []interface{}{
		token.MintAddress,
		map[string]interface{}{
			"commitment": client.Commitment(),
		},
	}
	if err := client.Call(ctx, "getTokenSupply", params, &resp); err != nil {
		return nil, err
	}
	value, err := strconv.ParseFloat(resp.Value.UIAmountString, 64)
	if err != nil {
		return nil, err
	}
	return &models.TokenSupplyResponse{Value: value}, nil
}

func (s *chainService) GetTokenBalance(ctx context.Context, tokenCode string, address string) (*models.TokenBalanceResponse, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}
	token, err := s.resolveToken(tokenCode)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Context struct{} `json:"context"`
		Value   []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								UIAmountString string `json:"uiAmountString"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	params := []interface{}{
		address,
		map[string]interface{}{
			"mint": token.MintAddress,
		},
		map[string]interface{}{
			"encoding":   "jsonParsed",
			"commitment": client.Commitment(),
		},
	}
	if err := client.Call(ctx, "getTokenAccountsByOwner", params, &resp); err != nil {
		return nil, err
	}
	var total float64
	for _, item := range resp.Value {
		if item.Account.Data.Parsed.Info.TokenAmount.UIAmountString == "" {
			continue
		}
		value, err := strconv.ParseFloat(item.Account.Data.Parsed.Info.TokenAmount.UIAmountString, 64)
		if err != nil {
			return nil, err
		}
		total += value
	}
	return &models.TokenBalanceResponse{Value: total}, nil
}

func (s *chainService) AddToken(ctx context.Context, req models.TokenAddRequest) (*models.TokenResponse, error) {
	token := &config.Token{
		NetworkCode: req.NetworkCode,
		MintAddress: req.MintAddress,
		Decimals:    req.Decimals,
	}
	if err := s.tokenStore.Save(ctx, req.Code, token); err != nil {
		return nil, err
	}

	s.tokenMu.Lock()
	if s.cfg.Tokens == nil {
		s.cfg.Tokens = make(map[string]*config.Token)
	}
	s.cfg.Tokens[req.Code] = token
	s.tokenMu.Unlock()

	return &models.TokenResponse{
		Code:        req.Code,
		NetworkCode: req.NetworkCode,
		MintAddress: req.MintAddress,
		Decimals:    req.Decimals,
	}, nil
}

func (s *chainService) GetToken(ctx context.Context, tokenCode string) (*models.TokenResponse, error) {
	s.tokenMu.RLock()
	token, ok := s.cfg.Tokens[tokenCode]
	s.tokenMu.RUnlock()
	if !ok || token == nil {
		var err error
		token, err = s.tokenStore.Get(ctx, tokenCode)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("tokenCode=%s not configured", tokenCode)
			}
			return nil, err
		}
		s.tokenMu.Lock()
		if s.cfg.Tokens == nil {
			s.cfg.Tokens = make(map[string]*config.Token)
		}
		s.cfg.Tokens[tokenCode] = token
		s.tokenMu.Unlock()
	}

	return &models.TokenResponse{
		Code:        tokenCode,
		NetworkCode: token.NetworkCode,
		MintAddress: token.MintAddress,
		Decimals:    token.Decimals,
	}, nil
}

func (s *chainService) ListTokens(ctx context.Context, req models.TokenListRequest) (*models.TokenListResponse, error) {
	tokens, err := s.tokenStore.List(ctx, req.NetworkCode)
	if err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(tokens))
	for code := range tokens {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	items := make([]models.TokenResponse, 0, len(codes))
	for _, code := range codes {
		token := tokens[code]
		if token == nil {
			continue
		}
		items = append(items, models.TokenResponse{
			Code:        code,
			NetworkCode: token.NetworkCode,
			MintAddress: token.MintAddress,
			Decimals:    token.Decimals,
		})
	}
	return &models.TokenListResponse{Tokens: items}, nil
}

func (s *chainService) DeleteToken(ctx context.Context, tokenCode string) error {
	if err := s.tokenStore.Delete(ctx, tokenCode); err != nil {
		return err
	}
	s.tokenMu.Lock()
	delete(s.cfg.Tokens, tokenCode)
	s.tokenMu.Unlock()
	return nil
}

func (s *chainService) GetLatestBlock(ctx context.Context) (*models.LatestBlockResponse, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}

	var slot uint64
	params := []interface{}{map[string]interface{}{"commitment": client.Commitment()}}
	if err := client.Call(ctx, "getSlot", params, &slot); err != nil {
		return nil, err
	}

	var blockTime *int64
	if err := client.Call(ctx, "getBlockTime", []interface{}{slot}, &blockTime); err != nil && !errors.Is(err, solana.ErrRPCNotFound) {
		return nil, err
	}

	timestamp := int64(0)
	if blockTime != nil {
		timestamp = *blockTime * 1000
	}
	return &models.LatestBlockResponse{
		BlockNumber: slot,
		Timestamp:   timestamp,
	}, nil
}

func (s *chainService) GetTransactionReceipt(ctx context.Context, txCode string) (*models.ChainTx, []models.ChainEvent, bool, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, nil, false, err
	}
	network := s.network

	var result solana.TransactionResult
	params := []interface{}{
		txCode,
		map[string]interface{}{
			"encoding":                       "jsonParsed",
			"commitment":                     client.Commitment(),
			"maxSupportedTransactionVersion": 0,
		},
	}
	err = client.Call(ctx, "getTransaction", params, &result)
	if errors.Is(err, solana.ErrRPCNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}

	tx := toChainTx(network, result)
	events := toChainEvents(s.cfg, network, result)
	return &tx, events, true, nil
}

func (s *chainService) FetchAddressSignatures(ctx context.Context, address string, opts solana.SignatureQueryOptions) ([]solana.SignatureInfo, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}

	cfg := map[string]interface{}{
		"commitment": client.Commitment(),
	}
	if opts.Limit > 0 {
		cfg["limit"] = opts.Limit
	}
	if opts.Before != "" {
		cfg["before"] = opts.Before
	}
	if opts.Until != "" {
		cfg["until"] = opts.Until
	}
	if opts.MinContextSlot > 0 {
		cfg["minContextSlot"] = opts.MinContextSlot
	}

	var result []solana.SignatureInfo
	if err := client.Call(ctx, "getSignaturesForAddress", []interface{}{address, cfg}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *chainService) FetchBlockTransactions(ctx context.Context, slot uint64) ([]models.TxCallbackMessage, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}

	var result solana.BlockResponse
	params := []interface{}{
		slot,
		map[string]interface{}{
			"encoding":                       "jsonParsed",
			"transactionDetails":             "full",
			"rewards":                        false,
			"maxSupportedTransactionVersion": 0,
			"commitment":                     client.Commitment(),
		},
	}
	if err := client.Call(ctx, "getBlock", params, &result); err != nil {
		if errors.Is(err, solana.ErrRPCNotFound) {
			return nil, nil
		}
		return nil, err
	}

	clientNetwork := s.network
	messages := make([]models.TxCallbackMessage, 0, len(result.Transactions))
	for _, tx := range result.Transactions {
		chainTx := toChainTx(clientNetwork, tx)
		events := toChainEvents(s.cfg, clientNetwork, tx)
		messages = append(messages, models.TxCallbackMessage{
			Tx:       &chainTx,
			TxEvents: events,
		})
	}
	return messages, nil
}

func (s *chainService) CheckSignatureLive(ctx context.Context, txCode string) (bool, error) {
	status, err := s.GetSignatureStatus(ctx, txCode)
	if err != nil {
		return false, err
	}
	return status.Exists, nil
}

func (s *chainService) GetSignatureStatus(ctx context.Context, txCode string) (*solana.SignatureStatus, error) {
	client, err := s.resolveClient()
	if err != nil {
		return nil, err
	}

	var resp solana.SignatureStatusResponse
	params := []interface{}{
		[]string{txCode},
		map[string]interface{}{
			"searchTransactionHistory": true,
		},
	}
	if err := client.Call(ctx, "getSignatureStatuses", params, &resp); err != nil {
		return nil, err
	}
	if len(resp.Value) == 0 || resp.Value[0] == nil {
		return &solana.SignatureStatus{}, nil
	}
	item := resp.Value[0]
	return &solana.SignatureStatus{
		Exists:             true,
		Slot:               item.Slot,
		Confirmations:      item.Confirmations,
		Err:                item.Err,
		ConfirmationStatus: item.ConfirmationStatus,
	}, nil
}

func (s *chainService) WatchSignature(ctx context.Context, txCode string) (*solana.SignatureNotification, error) {
	client, err := s.resolveWSClient()
	if err != nil {
		return nil, err
	}
	return client.WaitSignatureNotification(ctx, txCode, "confirmed")
}

func (s *chainService) WatchAccount(ctx context.Context, account string, onSubscribed func() error, handler func(solana.AccountNotification) error) error {
	client, err := s.resolveWSClient()
	if err != nil {
		return err
	}
	return client.StreamAccountNotifications(ctx, account, "confirmed", onSubscribed, handler)
}

func (s *chainService) resolveClient() (*solana.Client, error) {
	client, ok := s.clients[s.network.Code]
	if !ok {
		return nil, fmt.Errorf("solana rpc client not initialized")
	}
	return client, nil
}

func (s *chainService) resolveWSClient() (*solana.WSClient, error) {
	client, ok := s.wsClients[s.network.Code]
	if !ok {
		return nil, fmt.Errorf("solana websocket client not initialized")
	}
	return client, nil
}

func (s *chainService) resolveToken(tokenCode string) (*config.Token, error) {
	s.tokenMu.RLock()
	token, ok := s.cfg.Tokens[tokenCode]
	s.tokenMu.RUnlock()
	if !ok || token == nil {
		return nil, fmt.Errorf("tokenCode=%s not configured", tokenCode)
	}
	if token.NetworkCode != "" && token.NetworkCode != s.network.Code {
		return nil, fmt.Errorf("tokenCode=%s does not belong to solana network", tokenCode)
	}
	return token, nil
}

type idempotencyStore struct {
	ttl   time.Duration
	mu    sync.Mutex
	items map[string]idempotencyItem
}

type idempotencyItem struct {
	TxCode    string
	ExpiredAt time.Time
}

func newIdempotencyStore(ttl time.Duration) *idempotencyStore {
	return &idempotencyStore{
		ttl:   ttl,
		items: make(map[string]idempotencyItem),
	}
}

func (s *idempotencyStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	item, ok := s.items[key]
	if !ok {
		return "", false
	}
	return item.TxCode, true
}

func (s *idempotencyStore) Put(key string, txCode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = idempotencyItem{
		TxCode:    txCode,
		ExpiredAt: time.Now().Add(s.ttl),
	}
}

func mustResolveSingleNetwork(cfg *config.Config) *config.SolanaNetwork {
	if cfg.Networks == nil {
		panic("solana connector network config is empty")
	}
	if cfg.Networks.RPCURL == "" {
		panic("solana connector network endpoint is empty")
	}
	return cfg.Networks
}

func (s *idempotencyStore) gcLocked() {
	now := time.Now()
	for key, item := range s.items {
		if !item.ExpiredAt.IsZero() && now.After(item.ExpiredAt) {
			delete(s.items, key)
		}
	}
}

func toLamports(value float64, lamportsPerToken uint64) (uint64, error) {
	if value <= 0 {
		return 0, errors.New("value must be positive")
	}
	lamportsFloat := value * float64(lamportsPerToken)
	if lamportsFloat > math.MaxUint64 || math.IsNaN(lamportsFloat) || math.IsInf(lamportsFloat, 0) {
		return 0, errors.New("value overflow")
	}
	return uint64(math.Round(lamportsFloat)), nil
}

func fromLamports(lamports uint64, lamportsPerToken uint64) float64 {
	if lamportsPerToken == 0 {
		return 0
	}
	return float64(lamports) / float64(lamportsPerToken)
}
