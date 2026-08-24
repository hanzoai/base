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

// writeOK writes IAM's {status, msg, data} envelope.
func writeOK(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": data})
}

// writeErr writes IAM's status:"error" envelope at HTTP 200.
// This is the "already exists" path — IAM returns 200, not 409.
func writeErr(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "msg": msg})
}

// ──────────────────────────────────────────────────────────────────────
// LookupByAttribute
// ──────────────────────────────────────────────────────────────────────

// writePage writes the user collection's page: the records themselves, under no
// envelope.
func writePage(w http.ResponseWriter, users ...map[string]any) {
	if users == nil {
		users = []map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"users": users, "total": len(users)})
}

func TestLookupByAttribute_Hit(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "svc" || secret != "shh" {
			t.Errorf("credential did not arrive as Basic: id=%q ok=%v", id, ok)
		}
		if got := r.URL.Query().Get("email"); got != "alice@x.com" {
			t.Errorf("email: got %q want alice@x.com", got)
		}
		if got := r.URL.Query().Get("owner"); got != "hanzo" {
			t.Errorf("owner: got %q want hanzo", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit: got %q want 10", got)
		}
		writePage(w, map[string]any{"id": "u-1", "name": "alice", "email": "alice@x.com", "owner": "hanzo"})
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
}

func TestLookupByAttribute_Miss(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		writePage(w)
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

// An attribute the collection cannot narrow on is refused BEFORE the request. A
// filter IAM does not offer is one it ignores, so sending it would answer with
// the org's first page and every one of those rows would read as a match.
func TestLookupByAttribute_RefusesUnfilterableAttribute(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, map[string]any{"id": "u-9", "name": "somebody", "email": "else@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	for _, attr := range []string{"phone", "name", ""} {
		out, err := c.LookupByAttribute(context.Background(), attr, "+16125551234", "", 10)
		if err == nil {
			t.Fatalf("attr %q: want refusal, got %+v", attr, out)
		}
		if out != nil {
			t.Fatalf("attr %q: refusal must yield no users, got %+v", attr, out)
		}
	}
	if got := f.callCount("/v1/iam/users"); got != 0 {
		t.Fatalf("an unfilterable attribute reached IAM %d times", got)
	}
}

func TestLookupByAttribute_RequiresAdminCreds(t *testing.T) {
	c := iam.NewClient("http://unused.invalid")
	_, err := c.LookupByAttribute(context.Background(), "email", "x@y.com", "hanzo", 10)
	if err == nil || !strings.Contains(err.Error(), "admin credentials not configured") {
		t.Fatalf("want admin-credentials error, got %v", err)
	}
}

func TestLookupByAttribute_IAMRefusalPropagates(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"forbidden: this credential is scoped to organization other"}`))
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	out, err := c.LookupByAttribute(context.Background(), "email", "x@y.com", "", 10)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want the refusal to propagate, got %+v / %v", out, err)
	}
	if out != nil {
		t.Fatalf("a refusal must yield no users, got %+v", out)
	}
}

// ──────────────────────────────────────────────────────────────────────
// EnsureUser
// ──────────────────────────────────────────────────────────────────────

func TestEnsureUser_Create(t *testing.T) {
	f := newFakeIAM(t)
	var reads, writes int64
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt64(&reads, 1)
			writePage(w)
			return
		}
		atomic.AddInt64(&writes, 1)
		if id, secret, ok := r.BasicAuth(); !ok || id != "svc" || secret != "shh" {
			t.Errorf("credential did not arrive as Basic: id=%q ok=%v", id, ok)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			User map[string]any `json:"user"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		// The profile rides under "user"; a flat body would decode to nothing.
		if payload.User == nil {
			t.Fatalf("create body carried no user object: %s", body)
		}
		if payload.User["email"] != "new@x.com" {
			t.Errorf("email: got %v want new@x.com", payload.User["email"])
		}
		if payload.User["owner"] != "hanzo" {
			t.Errorf("owner: got %v want hanzo", payload.User["owner"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "new-id", "name": "new", "email": "new@x.com", "owner": "hanzo",
		})
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
	if atomic.LoadInt64(&reads) != 1 || atomic.LoadInt64(&writes) != 1 {
		t.Errorf("reads=%d writes=%d, want 1 and 1", reads, writes)
	}
}

// The address is read FIRST, so an account that is already there is returned
// without a write. IAM's create refuses a duplicate username and admits a
// duplicate address, so creating first would file a second row nothing can
// resolve by address again.
func TestEnsureUser_ExistingIsReturnedWithoutWriting(t *testing.T) {
	f := newFakeIAM(t)
	var writes int64
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			atomic.AddInt64(&writes, 1)
			t.Errorf("EnsureUser wrote for an address already present")
			return
		}
		if got := r.URL.Query().Get("email"); got != "dup@x.com" {
			t.Errorf("email: got %q want dup@x.com", got)
		}
		writePage(w, map[string]any{"id": "existing-id", "name": "dup", "email": "dup@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "dup@x.com", Name: "dup"})
	if err != nil {
		t.Fatalf("EnsureUser on existing: %v", err)
	}
	if user.ID != "existing-id" {
		t.Errorf("user.ID: got %q want existing-id", user.ID)
	}
	if atomic.LoadInt64(&writes) != 0 {
		t.Errorf("writes: got %d want 0", writes)
	}
}

// The username was taken between the read and the write — the racing provision
// that lost. Both agree on the address, so it resolves by that.
func TestEnsureUser_RaceLosesOnNameAndResolvesByAddress(t *testing.T) {
	f := newFakeIAM(t)
	var reads int64
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"title":"user hanzo/dup already exists"}`))
			return
		}
		if atomic.AddInt64(&reads, 1) == 1 {
			writePage(w) // the read that lost the race saw nothing
			return
		}
		writePage(w, map[string]any{"id": "existing-id", "name": "dup", "email": "dup@x.com", "owner": "hanzo"})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "dup@x.com"})
	if err != nil {
		t.Fatalf("EnsureUser 409: %v", err)
	}
	if user.ID != "existing-id" {
		t.Errorf("user.ID: got %q want existing-id", user.ID)
	}
}

// An address naming two accounts names none: refuse rather than pick one, and
// write nothing.
func TestEnsureUser_RefusesAmbiguousAddress(t *testing.T) {
	f := newFakeIAM(t)
	var writes int64
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			atomic.AddInt64(&writes, 1)
			return
		}
		writePage(w,
			map[string]any{"id": "u-1", "name": "a", "email": "two@x.com", "owner": "hanzo"},
			map[string]any{"id": "u-2", "name": "b", "email": "two@x.com", "owner": "hanzo"},
		)
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "two@x.com"})
	if err == nil {
		t.Fatalf("want refusal on an ambiguous address, got %+v", user)
	}
	if user != nil {
		t.Fatalf("a refusal must yield no user, got %+v", user)
	}
	if atomic.LoadInt64(&writes) != 0 {
		t.Errorf("writes: got %d want 0", writes)
	}
}

func TestEnsureUser_PropagatesCreateRefusal(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePage(w)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"organization not found"}`))
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.EnsureUser(context.Background(), iam.EnsureUserSpec{Email: "x@y.com"})
	if err == nil || !strings.Contains(err.Error(), "organization not found") {
		t.Fatalf("want the refusal to propagate, got %+v / %v", user, err)
	}
}

// ──────────────────────────────────────────────────────────────────────
// ResolveAPIKey
// ──────────────────────────────────────────────────────────────────────

// A key resolves to the account the key endpoint names, and to nothing when it
// does not resolve: a refusal must never become a default principal.
func TestResolveAPIKey_ResolvesAndRefuses(t *testing.T) {
	f := newFakeIAM(t)
	f.setHandler("/v1/iam/keys/principal", func(w http.ResponseWriter, r *http.Request) {
		if id, secret, ok := r.BasicAuth(); !ok || id != "svc" || secret != "shh" {
			t.Errorf("credential did not arrive as Basic: id=%q ok=%v", id, ok)
		}
		if r.URL.Query().Get("accessKey") != "sk-good" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "error", "msg": "the entity does not exist", "code": "not-found",
			})
			return
		}
		writeOK(w, map[string]any{
			"owner": "hanzo", "name": "svc", "email": "svc@hanzo.ai", "isAdmin": false,
		})
	})

	c := iam.NewClient(f.server.URL)
	c.SetAdminCreds(iam.AdminCreds{ClientID: "svc", ClientSecret: "shh", Owner: "hanzo"})

	user, err := c.ResolveAPIKey("sk-good")
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if user.ID == "" {
		t.Fatalf("a resolved key must carry a subject, got %+v", user)
	}
	if user.ID != "hanzo/svc" || len(user.OrgIDs) != 1 || user.OrgIDs[0] != "hanzo" {
		t.Fatalf("resolved %+v", user)
	}

	if bad, err := c.ResolveAPIKey("sk-bad"); err == nil {
		t.Fatalf("an unresolvable key must refuse, got %+v", bad)
	}
}

// With no credential configured the resolver refuses rather than asking IAM
// anonymously and reading whatever comes back.
func TestResolveAPIKey_RequiresAdminCreds(t *testing.T) {
	c := iam.NewClient("http://unused.invalid")
	if user, err := c.ResolveAPIKey("sk-x"); err == nil {
		t.Fatalf("want refusal without credentials, got %+v", user)
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
// (raw user object, not the {status, msg, data} envelope).
func writeUserJSON(w http.ResponseWriter, u *iam.User) {
	_ = json.NewEncoder(w).Encode(u)
}

// guard: make sure the imports compile even if unused in some builds.
var _ = fmt.Sprintf
