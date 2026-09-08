package org

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The door KMS actually serves.
//
// KMS answers /v1/kms over HTTP and authenticates an IAM application by its
// client credentials — the same pair this plugin already holds to talk to IAM,
// so a Base needs one identity rather than two. The ZAP transport beside this
// one is identity-signed and mnemonic-derived, which is the right shape where a
// KMS serves it; where it does not, a deployment that could only speak ZAP had
// no way to read a secret at all and fell back to reading keys from its own
// environment.
type httpSecrets struct {
	base string
	id   string
	sec  string
	hc   *http.Client

	mu    sync.Mutex
	token string
	until time.Time
}

// bearer returns a live access token, logging in when the held one is spent.
// The window is trimmed so a token cannot expire in flight between the check
// and the call it authorizes.
func (h *httpSecrets) bearer(ctx context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.token != "" && time.Now().Before(h.until) {
		return h.token, nil
	}
	body, _ := json.Marshal(map[string]string{"clientId": h.id, "clientSecret": h.sec})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+"/v1/kms/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := h.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("kms login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kms login: %s", resp.Status)
	}
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("kms login: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("kms login: no token in the answer")
	}
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}
	h.token, h.until = out.AccessToken, time.Now().Add(life-time.Minute)
	return h.token, nil
}

// coordinate composes the request path for one secret.
//
// ref() leads every path with orgs/<org> because that segment IS the base
// boundary on the ZAP side, where one connection serves every org. Over HTTP
// the org is not addressable: it comes from the credential, and a secret
// belonging to another tenant is unnameable rather than merely refused. So the
// prefix this side already knows is dropped here rather than being composed
// twice.
func coordinate(path, name string) string {
	p := strings.TrimPrefix(strings.Trim(path, "/"), "orgs/")
	if i := strings.Index(p, "/"); i >= 0 {
		p = p[i+1:]
	} else {
		p = ""
	}
	if p == "" {
		return url.PathEscape(name)
	}
	return p + "/" + url.PathEscape(name)
}

func (h *httpSecrets) do(ctx context.Context, method, path, name, env string, body []byte) (*http.Response, error) {
	tok, err := h.bearer(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/v1/kms/secrets/%s?env=%s", h.base, coordinate(path, name), url.QueryEscape(env))
	if method == http.MethodPost {
		u = fmt.Sprintf("%s/v1/kms/secrets", h.base)
	}
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	return h.hc.Do(req)
}

func (h *httpSecrets) GetAt(ctx context.Context, path, name, env string) (string, error) {
	resp, err := h.do(ctx, http.MethodGet, path, name, env, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kms get %s: %s", coordinate(path, name), resp.Status)
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// PutAt writes one secret. env is REQUIRED on a write and carries no default:
// it is part of the storage key, so a defaulted write lands where no reader
// looks and the stale value keeps being served.
func (h *httpSecrets) PutAt(ctx context.Context, path, name, env, value string) error {
	sub := strings.TrimSuffix(strings.TrimSuffix(coordinate(path, name), url.PathEscape(name)), "/")
	body, _ := json.Marshal(map[string]string{"path": "/" + sub, "name": name, "env": env, "value": value})
	resp, err := h.do(ctx, http.MethodPost, path, name, env, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kms put %s: %s", coordinate(path, name), resp.Status)
	}
	return nil
}

func (h *httpSecrets) DeleteAt(ctx context.Context, path, name, env string) error {
	resp, err := h.do(ctx, http.MethodDelete, path, name, env, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kms delete %s: %s", coordinate(path, name), resp.Status)
	}
	return nil
}

// Close releases nothing: an http.Client is pooled and shared, and dropping it
// on a transport error would throw away every idle connection the process holds.
func (h *httpSecrets) Close() {}
