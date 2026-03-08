package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

type CallbackPublisher interface {
	PublishTx(msg models.TxCallbackMessage) error
	PublishRollback(msg models.TxRollbackMessage) error
}

func NewCallbackPublisher(cfg *config.Config) CallbackPublisher {
	callback := cfg.Connector.Callback
	if callback.Mode == "rabbitmq" {
		return newRabbitMQPublisher(callback)
	}
	return &logPublisher{
		mode:         callback.Mode,
		exchange:     callback.Exchange,
		exchangeType: callback.ExchangeType,
	}
}

type logPublisher struct {
	mode         string
	exchange     string
	exchangeType string
}

func (p *logPublisher) PublishTx(msg models.TxCallbackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	log.Infof("callback type=tx mode=%s exchange=%s exchangeType=%s payload=%s", p.mode, p.exchange, p.exchangeType, string(payload))
	return nil
}

func (p *logPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	log.Warningf("callback type=rollback mode=%s exchange=%s exchangeType=%s payload=%s", p.mode, p.exchange, p.exchangeType, string(payload))
	return nil
}

type rabbitMQPublisher struct {
	cfg *config.CallbackConfig

	mu            sync.Mutex
	conn          *amqp.Connection
	ch            *amqp.Channel
	notifyClose   chan *amqp.Error
	notifyReturn  chan amqp.Return
	notifyConfirm chan amqp.Confirmation
}

func newRabbitMQPublisher(cfg *config.CallbackConfig) CallbackPublisher {
	return &rabbitMQPublisher{cfg: cfg}
}

func (p *rabbitMQPublisher) PublishTx(msg models.TxCallbackMessage) error {
	return p.publish("tx", msg)
}

func (p *rabbitMQPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	return p.publish("rollback", msg)
}

func (p *rabbitMQPublisher) publish(kind string, msg interface{}) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnectedLocked(); err != nil {
		return err
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         payload,
		Timestamp:    time.Now(),
		DeliveryMode: amqp.Transient,
		Type:         kind,
	}
	if p.cfg.Persistent {
		publishing.DeliveryMode = amqp.Persistent
	}

	if err := p.ch.PublishWithContext(ctx, p.cfg.Exchange, p.cfg.RoutingKey, p.cfg.Mandatory, false, publishing); err != nil {
		p.closeLocked()
		return err
	}

	if p.cfg.Mandatory {
		select {
		case ret := <-p.notifyReturn:
			p.closeLocked()
			return errors.New("rabbitmq returned message: " + ret.ReplyText)
		default:
		}
	}

	if p.cfg.Confirm {
		select {
		case confirm, ok := <-p.notifyConfirm:
			if !ok {
				p.closeLocked()
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

func (p *rabbitMQPublisher) ensureConnectedLocked() error {
	if p.ch != nil && p.conn != nil && !p.conn.IsClosed() {
		select {
		case err := <-p.notifyClose:
			if err != nil {
				log.Warningf("rabbitmq connection closed: %v", err)
			}
			p.closeLocked()
		default:
			return nil
		}
	}

	if p.cfg.URL == "" {
		return errors.New("rabbitmq callback.url is required when callback.mode=rabbitmq")
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
		conn.Close()
		return err
	}

	if err := ch.ExchangeDeclare(
		p.cfg.Exchange,
		p.cfg.ExchangeType,
		p.cfg.Durable,
		false,
		false,
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	if p.cfg.Confirm {
		if err := ch.Confirm(false); err != nil {
			ch.Close()
			conn.Close()
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
	log.Infof("rabbitmq publisher connected exchange=%s type=%s", p.cfg.Exchange, p.cfg.ExchangeType)
	return nil
}

func (p *rabbitMQPublisher) closeLocked() {
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
