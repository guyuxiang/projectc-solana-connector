package callback

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHTTPPublisherUsesBasicAuth(t *testing.T) {
	publisher := newHTTPPublisher(&config.CallbackConfig{
		HTTPURL:  "http://callback.example",
		Username: "test",
		Password: "test",
	}).(*httpPublisher)
	publisher.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			t.Fatal("expected basic auth header")
		}
		if user != "test" || pass != "test" {
			t.Fatalf("unexpected basic auth user=%q pass=%q", user, pass)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`ok`)),
			Header:     make(http.Header),
		}, nil
	})

	if err := publisher.PublishTx(models.TxCallbackMessage{}); err != nil {
		t.Fatalf("PublishTx failed: %v", err)
	}
}
