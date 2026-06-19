// Package workers ports the bootnode background workers. It currently provides
// webhook delivery (the Go equivalent of bootnode/workers/webhook.py): sign a
// payload with the webhook's HMAC secret, POST it, and return the delivery
// outcome for the caller to persist.
//
// This is a library, not a daemon. Base does not run arq; delivery is driven by
// the plugin (synchronously on the event, or from a Base task/scheduler). The
// dispatch logic — signing, timeout, retry accounting — lives here, decoupled
// from any queue.
package workers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SignatureHeader is the header carrying the HMAC-SHA256 signature of the body.
const SignatureHeader = "X-Bootnode-Signature"

// Delivery is the outcome of a single webhook delivery attempt. The plugin
// persists it to the _bootnode_webhook_deliveries collection.
type Delivery struct {
	StatusCode   int
	ResponseBody string
	Success      bool
	Error        string
}

// Dispatcher delivers signed webhook payloads.
type Dispatcher struct {
	http    *http.Client
	maxBody int64
}

// NewDispatcher constructs a Dispatcher with the given per-request timeout.
// A zero timeout defaults to 30s (matching the Python webhook_timeout).
func NewDispatcher(timeout time.Duration) *Dispatcher {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Dispatcher{
		http:    &http.Client{Timeout: timeout},
		maxBody: 64 << 10, // cap recorded response bodies at 64 KiB
	}
}

// Sign returns the hex-encoded HMAC-SHA256 of payload under secret. Exposed so
// receivers (and tests) can verify deliveries.
func Sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Deliver POSTs payload to url, signing it with secret. The returned Delivery
// records the outcome regardless of HTTP status — a non-2xx response is a
// delivery with Success=false, not a Go error. A Go error is returned only for
// payload marshaling failures (a programming error).
func (d *Dispatcher) Deliver(ctx context.Context, url, secret string, payload any) (*Delivery, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("workers: marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		// A malformed URL is a delivery failure, not a transport error to retry
		// on infrastructure grounds — record it as such.
		return &Delivery{Success: false, Error: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bootnode-webhooks/1")
	req.Header.Set(SignatureHeader, Sign(secret, body))

	resp, err := d.http.Do(req)
	if err != nil {
		return &Delivery{Success: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, d.maxBody))
	return &Delivery{
		StatusCode:   resp.StatusCode,
		ResponseBody: string(respBody),
		Success:      resp.StatusCode >= 200 && resp.StatusCode < 300,
	}, nil
}
