package platform

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
)

// logOnlyApp embeds core.App (panics on unimplemented methods) but overrides
// Logger() which is the only method the auth proxy uses.
type logOnlyApp struct {
	core.App
}

func (a *logOnlyApp) Logger() *slog.Logger {
	return slog.Default()
}

func newTestPlugin(iamURL string) *plugin {
	return &plugin{
		config: PlatformConfig{
			IAMEndpoint: iamURL,
			IAMOrg:      "test-org",
			IAMApp:      "test-org/test-app",
		},
	}
}

func newTestPluginWithApp(iamURL string) *plugin {
	p := newTestPlugin(iamURL)
	p.app = &logOnlyApp{}
	return p
}

// newMockIAM creates a mock IAM server that handles auth proxy endpoints.
func newMockIAM(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		switch r.URL.Path {
		case "/api/send-verification-code":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var req map[string]any
			json.Unmarshal(body, &req)
			if req["applicationId"] == nil || req["dest"] == nil || req["type"] != "phone" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "missing fields"})
				return
			}
			w.Header().Set("X-IAM-Test", "verify-phone")
			json.NewEncoder(w).Encode(map[string]string{"status": "sent"})

		case "/api/login":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("X-IAM-Test", "login")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "test-token-123",
				"user_id":      "user-abc",
			})

		case "/api/signup":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("X-IAM-Test", "signup")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"user_id": "new-user-456"})

		case "/api/userinfo":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			auth := r.Header.Get("Authorization")
			if auth == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("X-IAM-Test", "userinfo")
			json.NewEncoder(w).Encode(map[string]any{
				"id":    "user-abc",
				"email": "user@test.com",
				"name":  "Test User",
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func makeEvent(method, path string, body string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{}
	e.Response = rec
	e.Request = req
	return e, rec
}

func TestProxyToIAM(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)

	e, rec := makeEvent(http.MethodPost, "/test", `{"test":true}`)
	e.Request.Header.Set("Authorization", "Bearer my-token")

	err := p.proxyToIAM(e, http.MethodPost, "/api/login", []byte(`{"username":"test"}`))
	if err != nil {
		t.Fatalf("proxyToIAM returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("X-IAM-Test"); got != "login" {
		t.Errorf("expected X-IAM-Test=login, got %q", got)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["access_token"] != "test-token-123" {
		t.Errorf("unexpected response body: %v", result)
	}
}

func TestProxyToIAM_ForwardsHeaders(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestPlugin(srv.URL)
	e, _ := makeEvent(http.MethodPost, "/test", `{}`)
	e.Request.Header.Set("Authorization", "Bearer abc")

	p.proxyToIAM(e, http.MethodPost, "/api/login", []byte(`{}`))

	if gotAuth != "Bearer abc" {
		t.Errorf("Authorization not forwarded: got %q", gotAuth)
	}
}

func TestProxyToIAM_Unreachable(t *testing.T) {
	p := newTestPluginWithApp("http://127.0.0.1:1")

	e, rec := makeEvent(http.MethodGet, "/test", "")

	err := p.proxyToIAM(e, http.MethodGet, "/api/userinfo", nil)
	if err != nil {
		t.Fatalf("proxyToIAM should not return Go error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandleVerifyPhone(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/verify-phone", `{"countryCode":"+1","phone":"5551234567"}`)

	err := p.handleVerifyPhone(e)
	if err != nil {
		t.Fatalf("handleVerifyPhone returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "sent" {
		t.Errorf("expected status=sent, got %v", result)
	}
}

func TestHandleVerifyPhone_EmptyPhone(t *testing.T) {
	p := newTestPlugin("http://unused")
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/verify-phone", `{"countryCode":"+1","phone":""}`)

	err := p.handleVerifyPhone(e)
	if err != nil {
		t.Fatalf("expected no Go error, got: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleVerifyCode(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/verify-code", `{"phone":"5551234567","code":"123456"}`)

	err := p.handleVerifyCode(e)
	if err != nil {
		t.Fatalf("handleVerifyCode returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["access_token"] != "test-token-123" {
		t.Errorf("expected access_token, got %v", result)
	}
}

func TestHandleVerifyCode_MissingFields(t *testing.T) {
	p := newTestPlugin("http://unused")
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/verify-code", `{"phone":"5551234567","code":""}`)

	err := p.handleVerifyCode(e)
	if err != nil {
		t.Fatalf("expected no Go error, got: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleLogin(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/login", `{"username":"user@test.com","password":"secret"}`)

	err := p.handleLogin(e)
	if err != nil {
		t.Fatalf("handleLogin returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleSignup(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodPost, "/api/platform/auth/signup", `{"email":"new@test.com","password":"secret123"}`)

	err := p.handleSignup(e)
	if err != nil {
		t.Fatalf("handleSignup returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
}

func TestHandleUserinfo(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodGet, "/api/platform/auth/userinfo", "")
	e.Request.Header.Set("Authorization", "Bearer test-token")

	err := p.handleUserinfo(e)
	if err != nil {
		t.Fatalf("handleUserinfo returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("X-IAM-Test"); got != "userinfo" {
		t.Errorf("expected X-IAM-Test=userinfo, got %q", got)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["id"] != "user-abc" {
		t.Errorf("expected user id user-abc, got %v", result)
	}
}

func TestHandleUserinfo_NoAuth(t *testing.T) {
	iam := newMockIAM(t)
	defer iam.Close()

	p := newTestPlugin(iam.URL)
	e, rec := makeEvent(http.MethodGet, "/api/platform/auth/userinfo", "")
	// No Authorization header.

	err := p.handleUserinfo(e)
	if err != nil {
		t.Fatalf("handleUserinfo returned error: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthProxyConfig(t *testing.T) {
	cfg := AuthProxyConfig{
		IAMEndpoint: "https://hanzo.id",
		IAMOrg:      "myorg",
		IAMApp:      "myorg/myapp",
	}
	if cfg.IAMEndpoint != "https://hanzo.id" {
		t.Errorf("unexpected IAMEndpoint: %s", cfg.IAMEndpoint)
	}
	if cfg.IAMOrg != "myorg" {
		t.Errorf("unexpected IAMOrg: %s", cfg.IAMOrg)
	}
	if cfg.IAMApp != "myorg/myapp" {
		t.Errorf("unexpected IAMApp: %s", cfg.IAMApp)
	}
}

func TestPlatformConfigHasAuthFields(t *testing.T) {
	cfg := PlatformConfig{
		IAMEndpoint: "https://hanzo.id",
		IAMOrg:      "myorg",
		IAMApp:      "myorg/myapp",
	}
	if cfg.IAMOrg != "myorg" {
		t.Errorf("IAMOrg not set correctly: %s", cfg.IAMOrg)
	}
	if cfg.IAMApp != "myorg/myapp" {
		t.Errorf("IAMApp not set correctly: %s", cfg.IAMApp)
	}
}
