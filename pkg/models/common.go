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

type TokenAddRequest struct {
	Code        string `json:"code" binding:"required"`
	NetworkCode string `json:"networkCode" binding:"required"`
	MintAddress string `json:"mintAddress" binding:"required"`
	Decimals    uint8  `json:"decimals" binding:"required"`
}

type TokenGetRequest struct {
	Code string `json:"code" binding:"required"`
}

type TokenDeleteRequest struct {
	Code string `json:"code" binding:"required"`
}

type TokenListRequest struct {
	NetworkCode string `json:"networkCode"`
}

type TokenResponse struct {
	Code        string `json:"code"`
	NetworkCode string `json:"networkCode"`
	MintAddress string `json:"mintAddress"`
	Decimals    uint8  `json:"decimals"`
}

type TokenListResponse struct {
	Tokens []TokenResponse `json:"tokens"`
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
	Address string `json:"address" binding:"required"`
}

type TxSubscribeCancelRequest struct {
	TxCode string `json:"txCode" binding:"required"`
}

type AddressSubscribeCancelRequest struct {
	Address string `json:"address" binding:"required"`
}

type TxCallbackMessage struct {
	Tx       *ChainTx     `json:"tx,omitempty"`
	TxEvents []ChainEvent `json:"txEvents,omitempty"`
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

const (
	TxStateConfirmed = "CONFIRMED"
	TxStateFinalized = "FINALIZED"
	TxStateDropped   = "DROPPED"
	TxStateReverted  = "REVERTED"
)

type TxSubscription struct {
	CreatedAt          time.Time `json:"created_at"`
	NetworkCode        string    `json:"networkCode"`
	TxCode             string    `json:"txCode"`
	EndBlockNumber     uint64    `json:"endBlockNumber"`
	SubscriptionStatus string    `json:"subscriptionStatus"`
}

type AddressSubscription struct {
	CreatedAt          time.Time                    `json:"created_at"`
	NetworkCode        string                       `json:"networkCode"`
	Address            string                       `json:"address"`
	TrackedAccounts    []string                     `json:"trackedAccounts,omitempty"`
	AccountCheckpoints map[string]AddressCheckpoint `json:"accountCheckpoints,omitempty"`
	SubscriptionStatus string                       `json:"subscriptionStatus"`
}

type AddressCheckpoint struct {
	LastObservedSlot   uint64 `json:"lastObservedSlot"`
	LastObservedTxCode string `json:"lastObservedTxCode"`
}

type PublishedTxState struct {
	CreatedAt   time.Time `json:"created_at"`
	NetworkCode string    `json:"networkCode"`
	BlockNumber uint64    `json:"blockNumber"`
	State       string    `json:"state"`
}

const (
	CallbackKindTx       = "tx"
	CallbackKindRollback = "rollback"
)

type PendingCallback struct {
	TaskID      string    `json:"taskId"`
	Kind        string    `json:"kind"`
	TxCode      string    `json:"txCode"`
	NetworkCode string    `json:"networkCode"`
	PayloadJSON string    `json:"payloadJson"`
	RetryCount  uint64    `json:"retryCount"`
	LastError   string    `json:"lastError"`
	NextRetryAt time.Time `json:"nextRetryAt"`
	CreatedAt   time.Time `json:"created_at"`
}

type SubscriptionSnapshot struct {
	TxSubs           map[string]*TxSubscription
	AddressSubs      map[string]*AddressSubscription
	PublishedState   map[string]PublishedTxState
	PendingCallbacks map[string]*PendingCallback
}
