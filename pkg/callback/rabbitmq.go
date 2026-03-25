package callback

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
)

const rabbitMQPublishTimeout = 5 * time.Second

type rabbitMQPublisher struct {
	cfg      *config.RabbitMQConfig
	mu       sync.Mutex
	conn     *amqp.Connection
	ch       *amqp.Channel
	returns  chan amqp.Return
	confirms chan amqp.Confirmation
}

func shouldUseRabbitMQ(cfg *config.RabbitMQConfig) bool {
	return cfg != nil && (cfg.Enabled || strings.TrimSpace(cfg.URL) != "")
}

func newRabbitMQPublisher(cfg *config.RabbitMQConfig) CallbackPublisher {
	return &rabbitMQPublisher{cfg: cfg}
}

func (p *rabbitMQPublisher) PublishTx(msg models.TxCallbackMessage) error {
	return p.publishJSON(p.cfg.TxExchange, "tx.callback", msg)
}

func (p *rabbitMQPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	return p.publishJSON(p.cfg.RollbackExchange, "tx.rollback", msg)
}

func (p *rabbitMQPublisher) publishJSON(exchange string, messageType string, payload interface{}) error {
	if p.cfg == nil || strings.TrimSpace(p.cfg.URL) == "" {
		return fmt.Errorf("rabbitmq.url is required")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnected(exchange); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), rabbitMQPublishTimeout)
	defer cancel()

	log.Infof("rabbitmq callback publish exchange=%s type=%s payload=%s", exchange, messageType, string(body))
	if err := p.ch.PublishWithContext(ctx, exchange, "", true, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		Type:         messageType,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}); err != nil {
		p.resetLocked()
		return err
	}

	select {
	case returned := <-p.returns:
		p.resetLocked()
		return fmt.Errorf("rabbitmq returned exchange=%s replyCode=%d replyText=%s routingKey=%s", exchange, returned.ReplyCode, returned.ReplyText, returned.RoutingKey)
	case confirm := <-p.confirms:
		if !confirm.Ack {
			p.resetLocked()
			return fmt.Errorf("rabbitmq publish nack exchange=%s deliveryTag=%d", exchange, confirm.DeliveryTag)
		}
		log.Infof("rabbitmq callback confirmed exchange=%s type=%s deliveryTag=%d", exchange, messageType, confirm.DeliveryTag)
		return nil
	case <-ctx.Done():
		p.resetLocked()
		return fmt.Errorf("rabbitmq publish confirm timeout exchange=%s: %w", exchange, ctx.Err())
	}
}

// 如果连接和 channel 还活着就直接复用；如果已断开或不可用，就重建连接、重新开启 confirm、重新注册 return/confirm 通道
func (p *rabbitMQPublisher) ensureConnected(exchange string) error {
	if p.ch != nil && !p.ch.IsClosed() && p.conn != nil && !p.conn.IsClosed() {
		return p.ch.ExchangeDeclare(exchange, "fanout", true, false, false, false, nil)
	}

	p.resetLocked()

	conn, err := amqp.Dial(p.cfg.URL)
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}

	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}

	if err := ch.ExchangeDeclare(exchange, "fanout", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}

	p.conn = conn
	p.ch = ch
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	log.Infof("rabbitmq publisher connected url=%s", p.cfg.URL)
	return nil
}

func (p *rabbitMQPublisher) resetLocked() {
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	p.returns = nil
	p.confirms = nil
}
