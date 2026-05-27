package iam_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanzoai/base/iam"
)

// startFakeIAM spins up a minimal httptest server that emulates the IAM
// endpoints we care about. Handlers are pluggable per-test via setHandler.
type fakeIAM struct {
	server   *httptest.Server
	handlers sync.Map // path → http.HandlerFunc
	calls    sync.Map // path → *int64 (call counter)
}

func newFakeIAM(t *testing.T) *fakeIAM {
	t.Helper()
	f := &fakeIAM{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cnt, _ := f.calls.LoadOrStore(r.URL.Path, new(int64))
		atomic.AddInt64(cnt.(*int64), 1)
		if h, ok := f.handlers.Load(r.URL.Path); ok {
			h.(http.HandlerFunc)(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeIAM) setHandler(path string, h http.HandlerFunc) {
	f.handlers.Store(path, h)
}

func (f *fakeIAM) callCount(path string) int64 {
	v, ok := f.calls.Load(path)
	if !ok {
		return 0
	}
	return atomic.LoadInt64(v.(*int64))
}

// writeOK writes a Casdoor-style {status, msg, data} envelope.
func writeOK(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": data})
}

// writeErr writes a Casdoor-style status:"error" envelope at HTTP 200.
// This is the "already exists" path — IAM returns 200, not 409.
func writeErr(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "msg": msg})
}

// ──────────────────────────────────────────────────────────────────────
// LookupByAttribute
// ──────────────────────────────────────────────────────────────────────

func TestLookupByAttribute_Hit(t *testing.T) {
	f := newFakeIAM(t)
	// Phone normalization probes multiple candidate formats: the canonical
	// `+16125551234`, the `+`-stripped `16125551234`, and the country-
	// code-stripped `6125551234`. Accept any of the three on the first
	// probe — the SDK's normalization order is its own internal choice
	// and the test should not be load-bearing on that order.
	validProbes := map[string]bool{
		"+16125551234": true,
		"16125551234":  true,
		"6125551234":   true,
	}
	f.setHandler("/api/get-users", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("field"); got != "phone" {
			t.Errorf("field: got %q want phone", got)
		}
		if got := r.URL.Query().Get("value"); !validProbes[got] {
			t.Errorf("value: got %q want one of {+16125551234, 16125551234, 6125551234}", got)
		}
		if got := r.URL.Query().Get("owner"); got != "liquidity" {
			t.Errorf("owner: got %q want liquidity", got)
		}
		writeOK(w, []map[string]any{
			{"id": "u-1", "name": "alice", "email": "alice@x.com", "owner": "liquidity"},
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	out, err := c.LookupByAttribute(context.Background(), "phone", "+16125551234", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute: %v", err)
	}
	if len(out) != 1 || out[0].ID != "u-1" {
		t.Fatalf("got %+v, want one user u-1", out)
	}
}

func TestLookupByAttribute_Miss(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/api/get-users", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, []map[string]any{})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	out, err := c.LookupByAttribute(context.Background(), "email", "ghost@x.com", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute on miss must not error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("got %d users, want 0", len(out))
	}
}

func TestLookupByAttribute_PhoneNormalization(t *testing.T) {
	// Verify the three-shape probe for phone: +16125551234 → 16125551234 → 6125551234.
	// The user actually exists under the raw US-national form ("6125551234"),
	// so the first two probes miss and the third hits.
	f := newFakeIAM(t)
	f.setHandler("/api/get-users", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("value") {
		case "6125551234":
			writeOK(w, []map[string]any{
				{"id": "u-7", "name": "bob", "email": "bob@x.com", "owner": "liquidity"},
			})
		default:
			writeOK(w, []map[string]any{})
		}
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	out, err := c.LookupByAttribute(context.Background(), "phone", "+16125551234", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute: %v", err)
	}
	if len(out) != 1 || out[0].ID != "u-7" {
		t.Fatalf("expected normalization to recover u-7, got %+v", out)
	}
	if got := f.callCount("/api/get-users"); got != 3 {
		t.Fatalf("expected 3 probes (raw, no-plus, no-US), got %d", got)
	}
}

func TestLookupByAttribute_RequiresAdminCreds(t *testing.T) {
	c := iam.NewClient("http://unused.invalid")
	_, err := c.LookupByAttribute(context.Background(), "email", "x@y.com", "liquidity", 10)
	if err == nil || !strings.Contains(err.Error(), "admin credentials not configured") {
		t.Fatalf("want admin-credentials error, got %v", err)
	}
}

func TestLookupByAttribute_IAMErrorPropagates(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/api/get-users", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, "field not supported")
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	_, err := c.LookupByAttribute(context.Background(), "bogus", "x", "", 10)
	if err == nil || !strings.Contains(err.Error(), "field not supported") {
		t.Fatalf("want IAM-side error propagation, got %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────
// EnsureUser
// ──────────────────────────────────────────────────────────────────────

func TestEnsureUser_Create(t *testing.T) {
	f := newFakeIAM(t)
	var addUserCalls, getUserCalls int64
	f.setHandler("/api/add-user", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&addUserCalls, 1)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload["email"] != "new@x.com" {
			t.Errorf("email: got %v want new@x.com", payload["email"])
		}
		if payload["owner"] != "liquidity" {
			t.Errorf("owner: got %v want liquidity", payload["owner"])
		}
		writeOK(w, "Affected")
	})
	f.setHandler("/api/get-user", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&getUserCalls, 1)
		writeOK(w, map[string]any{
			"id":    "new-id",
			"name":  "new",
			"email": "new@x.com",
			"owner": "liquidity",
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{
		Email: "new@x.com",
		Name:  "new",
		Phone: "+16125550000",
		Type:  "normal-user",
	})
	if err != nil {
		t.Fatalf("EnsureUser create: %v", err)
	}
	if user.ID != "new-id" {
		t.Errorf("user.ID: got %q want new-id", user.ID)
	}
	if atomic.LoadInt64(&addUserCalls) != 1 {
		t.Errorf("add-user calls: got %d want 1", addUserCalls)
	}
	if atomic.LoadInt64(&getUserCalls) != 1 {
		t.Errorf("get-user calls: got %d want 1", getUserCalls)
	}
}

func TestEnsureUser_Idempotent_AlreadyExists(t *testing.T) {
	// IAM/Casdoor returns HTTP 200 with status:"error" + "X already exists"
	// when the user is already present. EnsureUser must treat this as
	// idempotent-replay and resolve the user via GET /api/get-user.
	f := newFakeIAM(t)
	var addUserCalls int64
	f.setHandler("/api/add-user", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&addUserCalls, 1)
		writeErr(w, "user:Email already exists")
	})
	f.setHandler("/api/get-user", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("email"); got != "dup@x.com" {
			t.Errorf("email: got %q want dup@x.com", got)
		}
		writeOK(w, map[string]any{
			"id":    "existing-id",
			"name":  "dup",
			"email": "dup@x.com",
			"owner": "liquidity",
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "dup@x.com", Name: "dup"})
	if err != nil {
		t.Fatalf("EnsureUser on duplicate: %v", err)
	}
	if user.ID != "existing-id" {
		t.Errorf("user.ID: got %q want existing-id", user.ID)
	}
}

func TestEnsureUser_Idempotent_HTTP409(t *testing.T) {
	// Some IAM versions / proxies may return HTTP 409 directly. EnsureUser
	// handles both signals.
	f := newFakeIAM(t)
	f.setHandler("/api/add-user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"status":"error","msg":"already exists"}`))
	})
	f.setHandler("/api/get-user", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, map[string]any{
			"id":    "existing-id",
			"name":  "dup",
			"email": "dup@x.com",
			"owner": "liquidity",
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "dup@x.com"})
	if err != nil {
		t.Fatalf("EnsureUser 409: %v", err)
	}
	if user.ID != "existing-id" {
		t.Errorf("user.ID: got %q want existing-id", user.ID)
	}
}

func TestEnsureUser_PropagatesNonExistsError(t *testing.T) {
	// Errors that are NOT "already exists" must propagate to the caller —
	// not get silently swallowed by the idempotent-replay path.
	f := newFakeIAM(t)
	f.setHandler("/api/add-user", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, "organization not found")
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "liquidity"})

	_, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "x@y.com"})
	if err == nil || !strings.Contains(err.Error(), "organization not found") {
		t.Fatalf("want non-exists error to propagate, got %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Cache: LRU eviction + singleflight coalescing
// ──────────────────────────────────────────────────────────────────────

func TestCache_LRU_Evicts_When_Full(t *testing.T) {
	// Configure a tiny cache (cap=2). Validate 3 distinct tokens; the
	// oldest entry (t1) must be evicted, so a re-validation of t1 must
	// hit the upstream IAM again.
	f := newFakeIAM(t)
	var fetchCalls int64
	f.setHandler("/api/userinfo", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCalls, 1)
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		writeUserJSON(w, &iam.User{ID: tok, Email: tok + "@x.com", Name: tok})
	})

	c := iam.NewClientWithCache(f.server.URL, 2)

	for _, tok := range []string{"t1", "t2", "t3"} {
		if _, err := c.ValidateToken(tok); err != nil {
			t.Fatalf("ValidateToken(%s): %v", tok, err)
		}
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 3 {
		t.Fatalf("priming: got %d fetches, want 3", got)
	}

	// t2 and t3 should be cached; t1 should be evicted (LRU).
	for _, tok := range []string{"t2", "t3"} {
		if _, err := c.ValidateToken(tok); err != nil {
			t.Fatalf("ValidateToken(%s) post-fill: %v", tok, err)
		}
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 3 {
		t.Fatalf("cached hits should not refetch: got %d, want 3", got)
	}

	// t1 should refetch (was evicted).
	if _, err := c.ValidateToken("t1"); err != nil {
		t.Fatalf("ValidateToken(t1) after eviction: %v", err)
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 4 {
		t.Fatalf("t1 should have re-fetched: got %d, want 4", got)
	}
}

func TestCache_Singleflight_Collapses_Concurrent_Validates(t *testing.T) {
	// 100 goroutines validate the SAME token simultaneously. With
	// singleflight, only 1 upstream IAM call should fire — the other 99
	// reuse the inflight result.
	f := newFakeIAM(t)
	var fetchCalls int64
	f.setHandler("/api/userinfo", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCalls, 1)
		// Hold long enough that all 100 goroutines have time to land in
		// the singleflight slot before the first fetch completes.
		time.Sleep(50 * time.Millisecond)
		writeUserJSON(w, &iam.User{ID: "u-1", Email: "u@x.com", Name: "u"})
	})

	c := iam.NewClient(f.server.URL)

	const N = 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := c.ValidateToken("same-token")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ValidateToken: %v", err)
		}
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 1 {
		t.Fatalf("singleflight should collapse to 1 call, got %d", got)
	}
}

func TestCache_InvalidateToken_Forces_Refetch(t *testing.T) {
	f := newFakeIAM(t)
	var fetchCalls int64
	f.setHandler("/api/userinfo", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCalls, 1)
		writeUserJSON(w, &iam.User{ID: "u-1", Email: "u@x.com", Name: "u"})
	})

	c := iam.NewClient(f.server.URL)
	if _, err := c.ValidateToken("t"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.ValidateToken("t"); err != nil {
		t.Fatalf("second (cached): %v", err)
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 1 {
		t.Fatalf("cached call should not refetch: got %d", got)
	}

	c.InvalidateToken("t")

	if _, err := c.ValidateToken("t"); err != nil {
		t.Fatalf("post-invalidate: %v", err)
	}
	if got := atomic.LoadInt64(&fetchCalls); got != 2 {
		t.Fatalf("post-invalidate must refetch: got %d, want 2", got)
	}
}

func TestCache_FailedFetch_DoesNotPoisonCache(t *testing.T) {
	// A failed upstream fetch must not leave a stale entry. Subsequent
	// successful fetch must hit upstream and succeed.
	f := newFakeIAM(t)
	var calls int64
	f.setHandler("/api/userinfo", func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt64(&calls, 1)
		if c == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		writeUserJSON(w, &iam.User{ID: "u-1", Email: "u@x.com", Name: "u"})
	})

	c := iam.NewClient(f.server.URL)
	if _, err := c.ValidateToken("t"); err == nil {
		t.Fatalf("first call must error")
	}
	user, err := c.ValidateToken("t")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if user.ID != "u-1" {
		t.Errorf("user.ID: got %q want u-1", user.ID)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────

// writeUserJSON encodes a user as IAM's /api/userinfo response shape
// (raw user object, not Casdoor envelope).
func writeUserJSON(w http.ResponseWriter, u *iam.User) {
	_ = json.NewEncoder(w).Encode(u)
}

// guard: make sure the imports compile even if unused in some builds.
var _ = fmt.Sprintf
