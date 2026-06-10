// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const turnstileEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileVerifyResp struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

// turnstileVerifier verifies a Cloudflare Turnstile token. If secret is
// empty, verify is a no-op (used in dev).
type turnstileVerifier struct {
	secret string
	client *http.Client
	url    string // injectable for tests
}

func newTurnstileVerifier(secret string) *turnstileVerifier {
	return &turnstileVerifier{
		secret: strings.TrimSpace(secret),
		client: &http.Client{Timeout: 5 * time.Second},
		url:    turnstileEndpoint,
	}
}

// verify returns nil on success, a non-nil error otherwise. Empty secret
// is treated as "verification disabled" and always succeeds.
func (v *turnstileVerifier) verify(ctx context.Context, token, remoteIP string) error {
	if v == nil || v.secret == "" {
		return nil
	}
	if token == "" {
		return errors.New("MISSING_TOKEN")
	}

	body, _ := json.Marshal(map[string]string{
		"secret":   v.secret,
		"response": token,
		"remoteip": remoteIP,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out turnstileVerifyResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Success {
		if len(out.ErrorCodes) > 0 {
			return errors.New(strings.Join(out.ErrorCodes, ","))
		}
		return errors.New("VERIFICATION_FAILED")
	}
	return nil
}
