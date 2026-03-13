package solana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type SignatureNotification struct {
	Slot uint64
	Err  interface{}
}

type LogsNotification struct {
	Signature string
	Slot      uint64
	Err       interface{}
	Logs      []string
}

type AccountNotification struct {
	Pubkey string
	Slot   uint64
}

type WSClient struct {
	endpoints    []string
	requestID    uint64
	timeout      time.Duration
	pongWait     time.Duration
	pingInterval time.Duration
}

func NewWSClient(endpoints []string, timeout time.Duration, pongWait time.Duration) *WSClient {
	cleaned := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if trimmed := strings.TrimSpace(endpoint); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if pongWait <= 0 {
		pongWait = 90 * time.Second
	}
	pingInterval := pongWait / 3
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}
	return &WSClient{
		endpoints:    cleaned,
		timeout:      timeout,
		pongWait:     pongWait,
		pingInterval: pingInterval,
	}
}

func DeriveWSEndpoints(httpEndpoints []string) []string {
	out := make([]string, 0, len(httpEndpoints))
	for _, endpoint := range httpEndpoints {
		u, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil || u.Scheme == "" {
			continue
		}
		switch u.Scheme {
		case "https":
			u.Scheme = "wss"
		case "http":
			u.Scheme = "ws"
		default:
			continue
		}
		out = append(out, u.String())
	}
	return out
}

func (c *WSClient) WaitSignatureNotification(ctx context.Context, signature string, commitment string) (*SignatureNotification, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	go closeConnOnDone(ctx, conn)
	stopHeartbeat := c.startHeartbeat(ctx, conn)
	defer stopHeartbeat()

	subscriptionID, err := c.subscribe(ctx, conn, "signatureSubscribe", []interface{}{
		signature,
		map[string]interface{}{
			"commitment":                 commitment,
			"enableReceivedNotification": false,
		},
	})
	if err != nil {
		return nil, err
	}
	defer c.unsubscribe(conn, "signatureUnsubscribe", subscriptionID)

	for {
		var envelope wsEnvelope
		if err := conn.ReadJSON(&envelope); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if envelope.Method != "signatureNotification" || len(envelope.Params.Result) == 0 {
			continue
		}
		var payload struct {
			Context struct {
				Slot uint64 `json:"slot"`
			} `json:"context"`
			Value struct {
				Err interface{} `json:"err"`
			} `json:"value"`
		}
		if err := json.Unmarshal(envelope.Params.Result, &payload); err != nil {
			return nil, err
		}
		return &SignatureNotification{
			Slot: payload.Context.Slot,
			Err:  payload.Value.Err,
		}, nil
	}
}

// 1. 建立 WS 连接
// connect(ctx) 会从配置的 wsEndpoints 里拨号，连接成功后配置 pong handler 和读超时，见 pkg/solana/ws.go:188。
// 2. 启动心跳
// startHeartbeat 会定时发 ping，收到 pong 会刷新 read deadline；如果 ping 发送失败，会主动关连接，让上层 watcher 重连，见 pkg/solana/ws.go:228。
// 3. 发送 logsSubscribe 请求
// 订阅参数是：
// - mentions: [programId]
// - commitment: "confirmed"
//
// 所以它监听的是“提到这个 programId 的 confirmed 日志流”，见 pkg/solana/ws.go:140。
// 4. 等待订阅确认
// subscribe(...) 会阻塞读取返回，直到拿到这次请求对应的 subscription id；如果服务端返回 rpc error，会直接失败返回，见 pkg/solana/ws.go:257。
// 5. 执行 onSubscribed
// 订阅 ack 成功后，不是立刻进入消息循环，而是先执行 onSubscribed()。你现在的实现里，这一步用来做“WS 建连成功后的二次 backfill”，补上 backfill 和建连之间的漏窗，见 pkg/service/subscription_service.go:370。
// 6. 进入消息循环
// 之后持续 ReadJSON 读取 WS 消息，只处理 logsNotification，其他消息忽略，见 pkg/solana/ws.go:157。
// 7. 反序列化成统一结构
// 每条 logsNotification 会被转成：
// - Signature
// - Slot
// - Err
// - Logs
//
// 然后交给上层传进来的 handler(notification)，见 pkg/solana/ws.go:169。
func (c *WSClient) StreamLogsNotifications(ctx context.Context, mention string, commitment string, onSubscribed func() error, handler func(LogsNotification) error) error {
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	go closeConnOnDone(ctx, conn)
	stopHeartbeat := c.startHeartbeat(ctx, conn)
	defer stopHeartbeat()

	subscriptionID, err := c.subscribe(ctx, conn, "logsSubscribe", []interface{}{
		map[string]interface{}{
			"mentions": []string{mention},
		},
		map[string]interface{}{
			"commitment": commitment,
		},
	})
	if err != nil {
		return err
	}
	defer c.unsubscribe(conn, "logsUnsubscribe", subscriptionID)
	if onSubscribed != nil {
		if err := onSubscribed(); err != nil {
			return err
		}
	}

	for {
		var envelope wsEnvelope
		if err := conn.ReadJSON(&envelope); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if envelope.Method != "logsNotification" || len(envelope.Params.Result) == 0 {
			continue
		}
		var payload struct {
			Context struct {
				Slot uint64 `json:"slot"`
			} `json:"context"`
			Value struct {
				Signature string      `json:"signature"`
				Err       interface{} `json:"err"`
				Logs      []string    `json:"logs"`
			} `json:"value"`
		}
		if err := json.Unmarshal(envelope.Params.Result, &payload); err != nil {
			return err
		}
		if err := handler(LogsNotification{
			Signature: payload.Value.Signature,
			Slot:      payload.Context.Slot,
			Err:       payload.Value.Err,
			Logs:      payload.Value.Logs,
		}); err != nil {
			return err
		}
	}
}

func (c *WSClient) StreamAccountNotifications(ctx context.Context, pubkey string, commitment string, onSubscribed func() error, handler func(AccountNotification) error) error {
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	go closeConnOnDone(ctx, conn)
	stopHeartbeat := c.startHeartbeat(ctx, conn)
	defer stopHeartbeat()

	subscriptionID, err := c.subscribe(ctx, conn, "accountSubscribe", []interface{}{
		pubkey,
		map[string]interface{}{
			"commitment": commitment,
			"encoding":   "base64",
		},
	})
	if err != nil {
		return err
	}
	defer c.unsubscribe(conn, "accountUnsubscribe", subscriptionID)
	if onSubscribed != nil {
		if err := onSubscribed(); err != nil {
			return err
		}
	}

	for {
		var envelope wsEnvelope
		if err := conn.ReadJSON(&envelope); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if envelope.Method != "accountNotification" || len(envelope.Params.Result) == 0 {
			continue
		}
		var payload struct {
			Context struct {
				Slot uint64 `json:"slot"`
			} `json:"context"`
		}
		if err := json.Unmarshal(envelope.Params.Result, &payload); err != nil {
			return err
		}
		if err := handler(AccountNotification{
			Pubkey: pubkey,
			Slot:   payload.Context.Slot,
		}); err != nil {
			return err
		}
	}
}

func (c *WSClient) connect(ctx context.Context) (*websocket.Conn, error) {
	if len(c.endpoints) == 0 {
		return nil, errors.New("no solana websocket endpoints configured")
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: c.timeout,
		Proxy:            http.ProxyFromEnvironment,
	}

	var lastErr error
	for _, endpoint := range c.endpoints {
		conn, _, err := dialer.DialContext(ctx, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if err := c.configureHeartbeat(conn); err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		return conn, nil
	}
	return nil, lastErr
}

func (c *WSClient) configureHeartbeat(conn *websocket.Conn) error {
	if c.pongWait <= 0 {
		return nil
	}
	if err := conn.SetReadDeadline(time.Now().Add(c.pongWait)); err != nil {
		return err
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.pongWait))
	})
	return nil
}

func (c *WSClient) startHeartbeat(ctx context.Context, conn *websocket.Conn) func() {
	if c.pingInterval <= 0 || c.pongWait <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(c.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(c.timeout)); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

func (c *WSClient) subscribe(ctx context.Context, conn *websocket.Conn, method string, params interface{}) (uint64, error) {
	requestID := atomic.AddUint64(&c.requestID, 1)
	if err := conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  method,
		"params":  params,
	}); err != nil {
		return 0, err
	}

	for {
		var response wsResponse
		if err := conn.ReadJSON(&response); err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			return 0, err
		}
		if response.ID != requestID {
			continue
		}
		if response.Error != nil {
			return 0, fmt.Errorf("ws method=%s code=%d message=%s", method, response.Error.Code, response.Error.Message)
		}
		var subscriptionID uint64
		if err := json.Unmarshal(response.Result, &subscriptionID); err != nil {
			return 0, err
		}
		return subscriptionID, nil
	}
}

func (c *WSClient) unsubscribe(conn *websocket.Conn, method string, subscriptionID uint64) {
	_ = conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      atomic.AddUint64(&c.requestID, 1),
		"method":  method,
		"params":  []interface{}{subscriptionID},
	})
}

func closeConnOnDone(ctx context.Context, conn *websocket.Conn) {
	<-ctx.Done()
	_ = conn.Close()
}

type wsEnvelope struct {
	Method string `json:"method"`
	Params struct {
		Result       json.RawMessage `json:"result"`
		Subscription uint64          `json:"subscription"`
	} `json:"params"`
}

type wsResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
