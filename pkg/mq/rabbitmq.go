package mq

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	txCallbackExchange   = "tx_callback_fanout_exchange"
	txCancelExchange     = "tx_callback_cancel_fanout_exchange"
	callbackExchangeType = "fanout"
	callbackRoutingKey   = ""
	callbackDurable      = true
	callbackMandatory    = false
	callbackPersistent   = true
	callbackConfirm      = true
	publishTimeout       = 5 * time.Second
)

type CallbackPublisher interface {
	PublishTx(msg models.TxCallbackMessage) error
	PublishRollback(msg models.TxRollbackMessage) error
}

func NewCallbackPublisher(cfg *config.Config) CallbackPublisher {
	if cfg != nil && cfg.MQ != nil && cfg.MQ.Mode == "rabbitmq" {
		return newRabbitMQPublisher(cfg.MQ)
	}
	mode := "log"
	if cfg != nil && cfg.MQ != nil && cfg.MQ.Mode != "" {
		mode = cfg.MQ.Mode
	}
	return &logPublisher{
		mode:         mode,
		exchangeType: callbackExchangeType,
	}
}

type logPublisher struct {
	mode         string
	exchangeType string
}

func (p *logPublisher) PublishTx(msg models.TxCallbackMessage) error {
	_, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// log.Infof("mq publish type=tx mode=%s exchange=%s exchangeType=%s payload=%s", p.mode, txCallbackExchange, p.exchangeType, string(payload))
	return nil
}

func (p *logPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	log.Warningf("mq publish type=rollback mode=%s exchange=%s exchangeType=%s payload=%s", p.mode, txCancelExchange, p.exchangeType, string(payload))
	return nil
}

type rabbitMQPublisher struct {
	cfg *config.MQConfig

	conn          *amqp.Connection
	ch            *amqp.Channel
	notifyClose   chan *amqp.Error
	notifyReturn  chan amqp.Return
	notifyConfirm chan amqp.Confirmation
}

func newRabbitMQPublisher(cfg *config.MQConfig) CallbackPublisher {
	return &rabbitMQPublisher{cfg: cfg}
}

func (p *rabbitMQPublisher) PublishTx(msg models.TxCallbackMessage) error {
	return p.publish("tx", txCallbackExchange, msg)
}

func (p *rabbitMQPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	return p.publish("rollback", txCancelExchange, msg)
}

func (p *rabbitMQPublisher) publish(kind string, exchange string, msg interface{}) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if kind == "tx" {
		log.Infof("mq publish type=%s exchange=%s exchangeType=%s payload=%s", kind, exchange, callbackExchangeType, string(payload))
	} else {
		log.Warningf("mq publish type=%s exchange=%s exchangeType=%s payload=%s", kind, exchange, callbackExchangeType, string(payload))
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	if err := p.ensureConnected(); err != nil {
		return err
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         payload,
		Timestamp:    time.Now(),
		DeliveryMode: amqp.Transient,
		Type:         kind,
	}
	if callbackPersistent {
		publishing.DeliveryMode = amqp.Persistent
	}

	if err := p.ch.PublishWithContext(ctx, exchange, callbackRoutingKey, callbackMandatory, false, publishing); err != nil {
		p.close()
		return err
	}

	if callbackMandatory {
		select {
		case ret := <-p.notifyReturn:
			p.close()
			return errors.New("rabbitmq returned message: " + ret.ReplyText)
		default:
		}
	}

	if callbackConfirm {
		select {
		case confirm, ok := <-p.notifyConfirm:
			if !ok {
				p.close()
				return errors.New("rabbitmq confirm channel closed")
			}
			if !confirm.Ack {
				return errors.New("rabbitmq publish not acknowledged")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (p *rabbitMQPublisher) ensureConnected() error {
	if p.ch != nil && p.conn != nil && !p.conn.IsClosed() {
		select {
		case err := <-p.notifyClose:
			if err != nil {
				log.Warningf("rabbitmq connection closed: %v", err)
			}
			p.close()
		default:
			return nil
		}
	}

	if p.cfg == nil || p.cfg.URL == "" {
		return errors.New("rabbitmq mq.url is required when mq.mode=rabbitmq")
	}

	conn, err := amqp.DialConfig(p.cfg.URL, amqp.Config{
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}

	if err := declareFanoutExchange(ch, txCallbackExchange); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err := declareFanoutExchange(ch, txCancelExchange); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}

	if callbackConfirm {
		if err := ch.Confirm(false); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
		p.notifyConfirm = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	} else {
		p.notifyConfirm = nil
	}

	p.notifyReturn = ch.NotifyReturn(make(chan amqp.Return, 1))
	p.notifyClose = conn.NotifyClose(make(chan *amqp.Error, 1))
	p.conn = conn
	p.ch = ch
	log.Infof("rabbitmq publisher connected exchanges=%s,%s type=%s", txCallbackExchange, txCancelExchange, callbackExchangeType)
	return nil
}

func (p *rabbitMQPublisher) close() {
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	p.notifyClose = nil
	p.notifyReturn = nil
	p.notifyConfirm = nil
}

func declareFanoutExchange(ch *amqp.Channel, exchange string) error {
	return ch.ExchangeDeclare(
		exchange,
		callbackExchangeType,
		callbackDurable,
		false,
		false,
		false,
		nil,
	)
}
