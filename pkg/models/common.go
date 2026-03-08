package models

import "time"

type Response struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ChainTx struct {
	Code        string `json:"code"`
	NetworkCode string `json:"networkCode"`
	BlockNumber uint64 `json:"blockNumber"`
	Timestamp   int64  `json:"timestamp"`
	Status      string `json:"status"`
	From        string `json:"from"`
	To          string `json:"to"`
	Amount      string `json:"amount"`
	Fee         string `json:"fee"`
}

type ChainEvent struct {
	Code        string      `json:"code"`
	NetworkCode string      `json:"networkCode"`
	BlockNumber uint64      `json:"blockNumber"`
	Timestamp   int64       `json:"timestamp"`
	Type        string      `json:"type"`
	Data        interface{} `json:"data"`
}

type TxSendRequest struct {
	TxSignResult string `json:"txSignResult" binding:"required"`
}

type TxSendResponse struct {
	TxCode string `json:"txCode"`
}

type FaucetRequest struct {
	AcceptAddress  string  `json:"acceptAddress" binding:"required"`
	IdempotencyKey string  `json:"idempotencyKey" binding:"required"`
	Value          float64 `json:"value" binding:"required,gt=0"`
}

type TxQueryRequest struct {
	TxCode string `json:"txCode" binding:"required"`
}

type TxQueryResponse struct {
	IfTxOnchain bool         `json:"ifTxOnchain"`
	Tx          *ChainTx     `json:"tx"`
	TxEvents    []ChainEvent `json:"txEvents"`
}

type AddressBalanceRequest struct {
	Address string `json:"address" binding:"required"`
}

type AddressBalanceResponse struct {
	Balance     float64 `json:"balance"`
	BalanceUnit string  `json:"balanceUnit"`
}

type TokenSupplyRequest struct {
	TokenCode string `json:"tokenCode" binding:"required"`
}

type TokenSupplyResponse struct {
	Value float64 `json:"value"`
}

type TokenBalanceRequest struct {
	TokenCode string `json:"tokenCode" binding:"required"`
	Address   string `json:"address" binding:"required"`
}

type TokenBalanceResponse struct {
	Value float64 `json:"value"`
}

type LatestBlockRequest struct{}

type LatestBlockResponse struct {
	BlockNumber uint64 `json:"blockNumber"`
	Timestamp   int64  `json:"timestamp"`
}

type SubscribeRange struct {
	StartBlockNumber *uint64 `json:"startBlockNumber,omitempty"`
	EndBlockNumber   *uint64 `json:"endBlockNumber,omitempty"`
}

type TxSubscribeRequest struct {
	TxCode         string         `json:"txCode" binding:"required"`
	SubscribeRange SubscribeRange `json:"subscribeRange" binding:"required"`
}

type AddressSubscribeRequest struct {
	Address        string         `json:"address" binding:"required"`
	SubscribeRange SubscribeRange `json:"subscribeRange" binding:"required"`
}

type TxSubscribeCancelRequest struct {
	TxCode string `json:"txCode" binding:"required"`
}

type AddressSubscribeCancelRequest struct {
	Address        string `json:"address" binding:"required"`
	EndBlockNumber uint64 `json:"endBlockNumber" binding:"required"`
}

type BlockSyncRequest struct {
	BeginBlockNumber uint64 `json:"beginBlockNumber" binding:"required"`
	EndBlockNumber   uint64 `json:"endBlockNumber" binding:"required"`
}

type TxCallbackMessage struct {
	Tx       ChainTx      `json:"tx"`
	TxEvents []ChainEvent `json:"txEvents"`
}

type TxRollbackMessage struct {
	TxCode      string `json:"txCode"`
	NetworkCode string `json:"networkCode"`
}

const (
	TxSubscriptionStatusActive    = "ACTIVE"
	TxSubscriptionStatusCompleted = "COMPLETED"
	TxSubscriptionStatusExpired   = "EXPIRED"
	TxSubscriptionStatusCancelled = "CANCELLED"
)

type TxSubscription struct {
	CreatedAt          time.Time `json:"created_at"`
	NetworkCode        string    `json:"networkCode"`
	TxCode             string    `json:"txCode"`
	EndBlockNumber     uint64    `json:"endBlockNumber"`
	SubscriptionStatus string    `json:"subscriptionStatus"`
	Completed          bool      `json:"completed"`
}

type AddressSubscription struct {
	CreatedAt        time.Time           `json:"created_at"`
	NetworkCode      string              `json:"networkCode"`
	Address          string              `json:"address"`
	StartBlockNumber uint64              `json:"startBlockNumber"`
	EndBlockNumber   *uint64             `json:"endBlockNumber,omitempty"`
	LastBefore       string              `json:"lastBefore"`
	HistoryComplete  bool                `json:"historyComplete"`
	SeenTxs          map[string]struct{} `json:"seenTxs"`
}

type PublishedTxState struct {
	CreatedAt   time.Time `json:"created_at"`
	NetworkCode string    `json:"networkCode"`
	BlockNumber uint64    `json:"blockNumber"`
}

type SubscriptionSnapshot struct {
	TxSubs         map[string]*TxSubscription
	AddressSubs    map[string]*AddressSubscription
	PublishedState map[string]PublishedTxState
}
