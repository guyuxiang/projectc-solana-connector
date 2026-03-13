package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
)

type httpPublisher struct {
	baseURL string
	client  *http.Client
}

func newHTTPPublisher(cfg *config.CallbackConfig) CallbackPublisher {
	baseURL := ""
	if cfg != nil {
		baseURL = strings.TrimRight(cfg.HTTPURL, "/")
	}
	log.Infof("New http publisher: %s", baseURL)
	return &httpPublisher{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *httpPublisher) PublishTx(msg models.TxCallbackMessage) error {
	return p.post("/tx", msg)
}

func (p *httpPublisher) PublishRollback(msg models.TxRollbackMessage) error {
	return p.post("/rollback", msg)
}

func (p *httpPublisher) post(path string, payload interface{}) error {
	if p.baseURL == "" {
		return fmt.Errorf("http callback base url is empty")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := p.baseURL + path
	log.Infof("http callback post url=%s payload=%s", url, string(body))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http callback status=%d body=%s", resp.StatusCode, string(raw))
	}

	log.Infof("http callback response url=%s status=%d body=%s", url, resp.StatusCode, string(raw))
	return nil
}
