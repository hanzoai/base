package platform

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// AuthProxyConfig holds the subset of PlatformConfig needed by the auth proxy.
type AuthProxyConfig struct {
	IAMEndpoint string
	IAMOrg      string
	IAMApp      string
}

// registerAuthRoutes registers IAM auth proxy routes under /api/platform/auth.
func (p *plugin) registerAuthRoutes(r *router.Router[*core.RequestEvent]) {
	auth := r.Group("/api/platform/auth")

	auth.POST("/verify-phone", p.handleVerifyPhone)
	auth.POST("/verify-code", p.handleVerifyCode)
	auth.POST("/login", p.handleLogin)
	auth.POST("/signup", p.handleSignup)
	auth.GET("/userinfo", p.handleUserinfo)
}

// proxyToIAM forwards a request to the IAM service and copies the response back.
func (p *plugin) proxyToIAM(e *core.RequestEvent, method, path string, body []byte) error {
	endpoint := strings.TrimRight(p.config.IAMEndpoint, "/")
	url := endpoint + path

	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(string(body))
	}

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(e.Request.Context(), method, url, reqBody)
	if err != nil {
		p.app.Logger().Error("auth proxy: failed to build request",
			slog.String("path", path),
			slog.String("error", err.Error()),
		)
		return e.JSON(http.StatusBadGateway, map[string]any{"message": "proxy error"})
	}
	req.Header.Set("Content-Type", "application/json")

	// Forward auth headers from the original request.
	for _, h := range []string{"Authorization", "tenant-authorization"} {
		if v := e.Request.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		p.app.Logger().Warn("auth proxy: IAM request failed",
			slog.String("path", path),
			slog.String("error", err.Error()),
		)
		return e.JSON(http.StatusBadGateway, map[string]any{"message": "IAM unavailable"})
	}
	defer resp.Body.Close()

	// Copy all response headers back.
	for k, vv := range resp.Header {
		for _, v := range vv {
			e.Response.Header().Add(k, v)
		}
	}

	// Copy status and body.
	e.Response.WriteHeader(resp.StatusCode)
	io.Copy(e.Response, resp.Body)
	return nil
}

// handleVerifyPhone proxies a phone verification request to IAM.
//
// Maps { countryCode, phone } to IAM /api/send-verification-code.
func (p *plugin) handleVerifyPhone(e *core.RequestEvent) error {
	raw, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "failed to read request body"})
	}

	var body struct {
		CountryCode string `json:"countryCode"`
		Phone       string `json:"phone"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "invalid request body"})
	}

	phone := strings.TrimSpace(body.Phone)
	if phone == "" {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "phone is required"})
	}

	iamBody, _ := json.Marshal(map[string]any{
		"applicationId": p.config.IAMApp,
		"dest":          phone,
		"type":          "phone",
		"countryCode":   body.CountryCode,
		"method":        "login",
	})

	return p.proxyToIAM(e, http.MethodPost, "/api/send-verification-code", iamBody)
}

// handleVerifyCode proxies OTP verification to IAM login.
//
// Maps { phone, code } to IAM /api/login.
func (p *plugin) handleVerifyCode(e *core.RequestEvent) error {
	raw, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "failed to read request body"})
	}

	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "invalid request body"})
	}

	phone := strings.TrimSpace(body.Phone)
	code := strings.TrimSpace(body.Code)
	if phone == "" || code == "" {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "phone and code are required"})
	}

	iamBody, _ := json.Marshal(map[string]any{
		"application":  p.config.IAMApp,
		"organization": p.config.IAMOrg,
		"username":     phone,
		"code":         code,
		"type":         "code",
	})

	return p.proxyToIAM(e, http.MethodPost, "/api/login", iamBody)
}

// handleLogin proxies login credentials to IAM.
func (p *plugin) handleLogin(e *core.RequestEvent) error {
	raw, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "failed to read request body"})
	}

	return p.proxyToIAM(e, http.MethodPost, "/api/login", raw)
}

// handleSignup proxies signup to IAM.
func (p *plugin) handleSignup(e *core.RequestEvent) error {
	raw, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"message": "failed to read request body"})
	}

	return p.proxyToIAM(e, http.MethodPost, "/api/signup", raw)
}

// handleUserinfo proxies a userinfo request to IAM.
func (p *plugin) handleUserinfo(e *core.RequestEvent) error {
	return p.proxyToIAM(e, http.MethodGet, "/api/userinfo", nil)
}
