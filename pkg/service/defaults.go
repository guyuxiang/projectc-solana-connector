package service

const (
	defaultRPCCommitment         = "confirmed"
	defaultRequestTimeoutMs      = 15000
	defaultBackfillTimeoutMs     = 30000
	defaultRetryTimes            = 2
	defaultRetryBackoffMs        = 300
	defaultWSIdleTimeoutMs       = 90000
	defaultIdempotencyTTLSeconds = 3600
	defaultTxCallbackExchange    = "tx_callback_fanout_exchange"
	defaultTxCancelExchange      = "tx_callback_cancel_fanout_exchange"
	defaultCallbackDurable       = true
	defaultCallbackConfirm       = true
	defaultCallbackExchangeType  = "fanout"
)
