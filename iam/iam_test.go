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

// Route names, spelled once. IAM's user surface is noun-shaped: the collection
// is POSTed to for a create, and one person is read at its /get leaf.
const (
	routeUsers        = "/v1/iam/users"
	routeUserGet      = "/v1/iam/users/get"
	routeKeyPrincipal = "/v1/iam/keys/principal"
)

// writeUser writes a user record the way IAM's user routes answer: the masked
// record itself, at the top level, with no envelope around it.
func writeUser(w http.ResponseWriter, u map[string]any) {
	_ = json.NewEncoder(w).Encode(u)
}

// refuse writes IAM's refusal: the HTTP status carries the outcome, and the body
// is zip's {status,error} shape. A refusal is never a 200 here.
func refuse(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "error": msg})
}

// wantBasic asserts the request presented the service's own credentials as
// client_secret_basic. This is the only client-credential transport IAM reads,
// so without it every call below would be a 401 — and a fake that ignores
// authentication would report the migration green while production refused it.
func wantBasic(t *testing.T, r *http.Request) {
	t.Helper()
	id, secret, ok := r.BasicAuth()
	if !ok {
		t.Errorf("no Basic credential presented (Authorization: %q)", r.Header.Get("Authorization"))
		return
	}
	if id != "svc" || secret != "shh" {
		t.Errorf("Basic credential: got %q:%q want svc:shh", id, secret)
	}
	if r.URL.Query().Get("clientSecret") != "" {
		t.Errorf("a secret must never ride in the query string: %s", r.URL.RawQuery)
	}
}

// ──────────────────────────────────────────────────────────────────────
// LookupByAttribute
// ──────────────────────────────────────────────────────────────────────

func TestLookupByAttribute_Hit(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler(routeUserGet, func(w http.ResponseWriter, r *http.Request) {
		wantBasic(t, r)
		// The address and the organization ride as separate parameters; there is
		// no packed owner/name id to split.
		if got := r.URL.Query().Get("email"); got != "alice@x.com" {
			t.Errorf("email: got %q want alice@x.com", got)
		}
		if got := r.URL.Query().Get("owner"); got != "hanzo" {
			t.Errorf("owner: got %q want hanzo", got)
		}
		writeUser(w, map[string]any{"id": "u-1", "name": "alice", "email": "alice@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	out, err := c.LookupByAttribute(context.Background(), "email", "alice@x.com", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute: %v", err)
	}
	if len(out) != 1 || out[0].ID != "u-1" {
		t.Fatalf("got %+v, want one user u-1", out)
	}
	if len(out[0].OrgIDs) != 1 || out[0].OrgIDs[0] != "hanzo" {
		t.Errorf("OrgIDs: got %v want [hanzo]", out[0].OrgIDs)
	}
}

func TestLookupByAttribute_ByUsername(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler(routeUserGet, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "alice" {
			t.Errorf("name: got %q want alice", got)
		}
		if got := r.URL.Query().Get("email"); got != "" {
			t.Errorf("a username lookup must not also send an address, got %q", got)
		}
		writeUser(w, map[string]any{"id": "u-1", "name": "alice", "email": "alice@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	out, err := c.LookupByAttribute(context.Background(), "name", "alice", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute: %v", err)
	}
	if len(out) != 1 || out[0].Name != "alice" {
		t.Fatalf("got %+v, want alice", out)
	}
}

func TestLookupByAttribute_Miss(t *testing.T) {
	// Nobody holds the address: IAM answers 404, and a miss is an ANSWER, not a
	// failure — the invite path branches on the empty result.
	f := newFakeIAM(t)
	f.setHandler(routeUserGet, func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusNotFound, "user hanzo/ghost@x.com not found")
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	out, err := c.LookupByAttribute(context.Background(), "email", "ghost@x.com", "", 10)
	if err != nil {
		t.Fatalf("LookupByAttribute on miss must not error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("got %d users, want 0", len(out))
	}
}

func TestLookupByAttribute_RefusesAnAttributeIAMCannotRead(t *testing.T) {
	// IAM reads a person by username or by address and offers no general
	// attribute search. A phone number therefore has no lookup to stand on, and
	// the refusal must happen HERE — reaching the network at all would mean the
	// caller had been handed some other question's answer.
	f := newFakeIAM(t)

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	_, err := c.LookupByAttribute(context.Background(), "phone", "+16125551234", "", 10)
	if err == nil || !strings.Contains(err.Error(), "has no lookup") {
		t.Fatalf("want a refusal naming the limitation, got %v", err)
	}
	if got := f.callCount(routeUserGet); got != 0 {
		t.Fatalf("refusal must not reach IAM, got %d calls", got)
	}
}

func TestLookupByAttribute_RequiresAdminCreds(t *testing.T) {
	c := iam.NewClient("http://unused.invalid")
	_, err := c.LookupByAttribute(context.Background(), "email", "x@y.com", "hanzo", 10)
	if err == nil || !strings.Contains(err.Error(), "admin credentials not configured") {
		t.Fatalf("want admin-credentials error, got %v", err)
	}
}

func TestLookupByAttribute_AmbiguousAddressPropagates(t *testing.T) {
	// One address naming two accounts is IAM's 409. It must reach the caller as
	// an error: handing back an arbitrary one of the two is how somebody joins a
	// team under a colleague's identity.
	f := newFakeIAM(t)
	f.setHandler(routeUserGet, func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusConflict, "email dup@x.com is ambiguous")
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	out, err := c.LookupByAttribute(context.Background(), "email", "dup@x.com", "", 10)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want the ambiguity to propagate, got (%+v, %v)", out, err)
	}
	if len(out) != 0 {
		t.Fatalf("an ambiguous address must resolve to nobody, got %+v", out)
	}
}

// ──────────────────────────────────────────────────────────────────────
// ResolveAPIKey
// ──────────────────────────────────────────────────────────────────────

func TestResolveAPIKey(t *testing.T) {
	// Resolving a secret key to the person it belongs to is an authentication
	// boundary, and IAM admits only a confidential service to it — so the
	// credential is as load-bearing here as the path.
	f := newFakeIAM(t)
	f.setHandler(routeKeyPrincipal, func(w http.ResponseWriter, r *http.Request) {
		wantBasic(t, r)
		if got := r.URL.Query().Get("accessKey"); got != "sk-live-abc" {
			t.Errorf("accessKey: got %q want sk-live-abc", got)
		}
		// This door kept its envelope: the projection rides under `data`.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"data": map[string]any{
				"owner": "hanzo", "name": "alice", "email": "alice@x.com",
				"isAdmin": false, "billing_account": "hanzo", "scope": "",
			},
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.ResolveAPIKey("sk-live-abc")
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if user.Name != "alice" || user.Email != "alice@x.com" {
		t.Errorf("got %+v want alice/alice@x.com", user)
	}
	if len(user.OrgIDs) != 1 || user.OrgIDs[0] != "hanzo" {
		t.Errorf("OrgIDs: got %v want [hanzo]", user.OrgIDs)
	}
	// Cached: a second resolve must not go back to IAM.
	if _, err := c.ResolveAPIKey("sk-live-abc"); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if got := f.callCount(routeKeyPrincipal); got != 1 {
		t.Errorf("resolve calls: got %d want 1 (second must be cached)", got)
	}
}

func TestResolveAPIKey_RefusalDoesNotPoisonTheCache(t *testing.T) {
	// IAM refuses an unknown or wrong-door key. That must reach the caller as an
	// error and leave nothing behind — a cached refusal would outlive the key
	// being minted.
	f := newFakeIAM(t)
	var calls int64
	f.setHandler(routeKeyPrincipal, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&calls, 1) == 1 {
			refuse(w, http.StatusBadRequest, "the entity does not exist")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"data":   map[string]any{"owner": "hanzo", "name": "alice", "email": "alice@x.com"},
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	if _, err := c.ResolveAPIKey("sk-live-abc"); err == nil {
		t.Fatal("a refused key must error")
	}
	user, err := c.ResolveAPIKey("sk-live-abc")
	if err != nil {
		t.Fatalf("retry after refusal: %v", err)
	}
	if user.Name != "alice" {
		t.Errorf("got %+v want alice", user)
	}
}

// ──────────────────────────────────────────────────────────────────────
// EnsureUser
// ──────────────────────────────────────────────────────────────────────

func TestEnsureUser_Create(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler(routeUsers, func(w http.ResponseWriter, r *http.Request) {
		wantBasic(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			User     map[string]any `json:"user"`
			Password string         `json:"password"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		// The profile NESTS under `user`. A flat body binds nothing on the real
		// route and is refused there, so asserting the nesting is what stops this
		// fake from passing a shape production rejects.
		if payload.User == nil {
			t.Fatalf("create body must nest the record under \"user\", got %s", body)
		}
		if payload.User["email"] != "new@x.com" {
			t.Errorf("email: got %v want new@x.com", payload.User["email"])
		}
		if payload.User["owner"] != "hanzo" {
			t.Errorf("owner: got %v want hanzo", payload.User["owner"])
		}
		if payload.Password != "" {
			t.Errorf("provisioning must not set a password, got %q", payload.Password)
		}
		// The create answers with the record it wrote, carrying the minted id.
		writeUser(w, map[string]any{"id": "new-id", "name": "new", "email": "new@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

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
	if got := f.callCount(routeUsers); got != 1 {
		t.Errorf("create calls: got %d want 1", got)
	}
	// The created record came back whole, so there is nothing left to look up.
	if got := f.callCount(routeUserGet); got != 0 {
		t.Errorf("a create that answers with the user must not be followed by a read, got %d", got)
	}
}

func TestEnsureUser_Idempotent_Conflict(t *testing.T) {
	// The account is already there. IAM says so with HTTP 409, and that is the
	// idempotent-replay signal: resolve the existing person by address.
	f := newFakeIAM(t)
	f.setHandler(routeUsers, func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusConflict, "user hanzo/dup already exists")
	})
	f.setHandler(routeUserGet, func(w http.ResponseWriter, r *http.Request) {
		wantBasic(t, r)
		if got := r.URL.Query().Get("email"); got != "dup@x.com" {
			t.Errorf("email: got %q want dup@x.com", got)
		}
		if got := r.URL.Query().Get("owner"); got != "hanzo" {
			t.Errorf("owner: got %q want hanzo", got)
		}
		writeUser(w, map[string]any{"id": "existing-id", "name": "dup", "email": "dup@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "dup@x.com", Name: "dup"})
	if err != nil {
		t.Fatalf("EnsureUser on duplicate: %v", err)
	}
	if user.ID != "existing-id" {
		t.Errorf("user.ID: got %q want existing-id", user.ID)
	}
	if got := f.callCount(routeUserGet); got != 1 {
		t.Errorf("conflict must resolve the existing account exactly once, got %d", got)
	}
}

func TestEnsureUser_PropagatesRefusal(t *testing.T) {
	// A refusal that is NOT a conflict must propagate — not get swallowed by the
	// idempotent-replay path, which would report a provisioned account that does
	// not exist.
	f := newFakeIAM(t)
	f.setHandler(routeUsers, func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusBadRequest, "organization not found")
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	_, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "x@y.com"})
	if err == nil || !strings.Contains(err.Error(), "organization not found") {
		t.Fatalf("want the refusal to propagate, got %v", err)
	}
	if got := f.callCount(routeUserGet); got != 0 {
		t.Errorf("a refusal must not be replayed as already-exists, got %d reads", got)
	}
}

func TestEnsureUser_RequiresAdminCreds(t *testing.T) {
	c := iam.NewClient("http://unused.invalid")
	_, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "x@y.com", Owner: "hanzo"})
	if err == nil || !strings.Contains(err.Error(), "admin credentials not configured") {
		t.Fatalf("want admin-credentials error, got %v", err)
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
	f.setHandler("/v1/iam/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
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
	f.setHandler("/v1/iam/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
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
	f.setHandler("/v1/iam/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
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
	f.setHandler("/v1/iam/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
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

// writeUserJSON encodes a user as IAM's /v1/iam/oauth/userinfo response shape
// (raw user object, not Casdoor envelope).
func writeUserJSON(w http.ResponseWriter, u *iam.User) {
	_ = json.NewEncoder(w).Encode(u)
}

// guard: make sure the imports compile even if unused in some builds.
var _ = fmt.Sprintf
