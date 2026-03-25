package callback

import (
	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
)

type CallbackPublisher interface {
	PublishTx(msg models.TxCallbackMessage) error
	PublishRollback(msg models.TxRollbackMessage) error
}

func NewCallbackPublisher(cfg *config.Config) CallbackPublisher {
	if cfg == nil {
		return newHTTPPublisher(nil)
	}
	if shouldUseRabbitMQ(cfg.RabbitMQ) {
		return newRabbitMQPublisher(cfg.RabbitMQ)
	}
	return newHTTPPublisher(cfg.Callback)
}
