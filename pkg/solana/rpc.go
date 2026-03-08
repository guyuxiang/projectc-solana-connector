package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrRPCNotFound = errors.New("solana rpc resource not found")

type Client struct {
	endpoints    []string
	httpClient   *http.Client
	retryTimes   int
	retryBackoff time.Duration
	commitment   string
	nextIndex    uint32
	requestID    uint64
}

func NewClient(endpoints []string, timeout time.Duration, retryTimes int, retryBackoff time.Duration, commitment string) *Client {
	cleaned := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if trimmed := strings.TrimSpace(endpoint); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return &Client{
		endpoints: cleaned,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retryTimes:   retryTimes,
		retryBackoff: retryBackoff,
		commitment:   commitment,
	}
}

func (c *Client) Commitment() string {
	return c.commitment
}

func (c *Client) Call(ctx context.Context, method string, params interface{}, out interface{}) error {
	if len(c.endpoints) == 0 {
		return errors.New("no solana rpc endpoints configured")
	}

	var lastErr error
	maxAttempts := len(c.endpoints) * (c.retryTimes + 1)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		endpoint := c.pickEndpoint(attempt)
		err := c.callEndpoint(ctx, endpoint, method, params, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.Is(err, ErrRPCNotFound) {
			return err
		}
		if c.retryBackoff > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryBackoff):
			}
		}
	}
	return lastErr
}

func (c *Client) pickEndpoint(attempt int) string {
	start := int(atomic.AddUint32(&c.nextIndex, 1)-1) % len(c.endpoints)
	return c.endpoints[(start+attempt)%len(c.endpoints)]
}

func (c *Client) callEndpoint(ctx context.Context, endpoint string, method string, params interface{}, out interface{}) error {
	body := rpcRequest{
		JSONRPC: "2.0",
		ID:      atomic.AddUint64(&c.requestID, 1),
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("rpc status=%d endpoint=%s body=%s", resp.StatusCode, endpoint, strings.TrimSpace(string(rawBody)))
	}

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		if rpcResp.Error.Code == -32000 || rpcResp.Error.Code == -32004 || strings.Contains(strings.ToLower(rpcResp.Error.Message), "not found") {
			return ErrRPCNotFound
		}
		return fmt.Errorf("rpc method=%s code=%d message=%s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      uint64      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type TransactionResult struct {
	Slot        uint64               `json:"slot"`
	BlockTime   *int64               `json:"blockTime"`
	Meta        TransactionMeta      `json:"meta"`
	Transaction ParsedTransactionRaw `json:"transaction"`
}

type TransactionMeta struct {
	Err               interface{}          `json:"err"`
	Fee               uint64               `json:"fee"`
	PreBalances       []uint64             `json:"preBalances"`
	PostBalances      []uint64             `json:"postBalances"`
	PreTokenBalances  []TokenBalanceRecord `json:"preTokenBalances"`
	PostTokenBalances []TokenBalanceRecord `json:"postTokenBalances"`
	LogMessages       []string             `json:"logMessages"`
}

type TokenBalanceRecord struct {
	AccountIndex  uint64            `json:"accountIndex"`
	Mint          string            `json:"mint"`
	Owner         string            `json:"owner"`
	UITokenAmount UITokenAmountInfo `json:"uiTokenAmount"`
}

type UITokenAmountInfo struct {
	UIAmountString string `json:"uiAmountString"`
	Decimals       uint8  `json:"decimals"`
}

type ParsedTransactionRaw struct {
	Signatures []string         `json:"signatures"`
	Message    ParsedMessageRaw `json:"message"`
}

type ParsedMessageRaw struct {
	AccountKeys  []ParsedAccountKey  `json:"accountKeys"`
	Instructions []ParsedInstruction `json:"instructions"`
}

type ParsedAccountKey struct {
	Pubkey   string `json:"pubkey"`
	Signer   bool   `json:"signer"`
	Writable bool   `json:"writable"`
	Source   string `json:"source"`
}

type ParsedInstruction struct {
	Program   string          `json:"program"`
	ProgramID string          `json:"programId"`
	Parsed    json.RawMessage `json:"parsed"`
}

type SignatureInfo struct {
	Signature string      `json:"signature"`
	Slot      uint64      `json:"slot"`
	Err       interface{} `json:"err"`
	BlockTime *int64      `json:"blockTime"`
}

type LatestBlockhashResponse struct {
	Context struct {
		Slot uint64 `json:"slot"`
	} `json:"context"`
	Value struct {
		Blockhash            string `json:"blockhash"`
		LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
	} `json:"value"`
}

type BlockResponse struct {
	BlockTime    *int64              `json:"blockTime"`
	BlockHeight  *uint64             `json:"blockHeight"`
	Blockhash    string              `json:"blockhash"`
	ParentSlot   uint64              `json:"parentSlot"`
	Transactions []TransactionResult `json:"transactions"`
}

type SignatureStatusResponse struct {
	Value []*struct {
		Slot               uint64      `json:"slot"`
		Confirmations      *uint64     `json:"confirmations"`
		Err                interface{} `json:"err"`
		ConfirmationStatus string      `json:"confirmationStatus"`
	} `json:"value"`
}

var _ sync.Locker
