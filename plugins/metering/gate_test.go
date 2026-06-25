package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/commerce/metering"
)

// fakeCommerce is a per-org balance ledger over the commerce billing contract,
// so the base gate can be proven against a real authorize+debit loop in-process.
type fakeCommerce struct {
	mu       sync.Mutex
	balances map[string]int64
	debits   []struct {
		org, user string
		cents     int64
	}
}

func (f *fakeCommerce) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org := r.Header.Get("X-Hanzo-Org")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/billing/balance":
			f.mu.Lock()
			avail := f.balances[org]
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"available": avail, "balance": avail, "currency": "usd", "user": org})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/billing/usage":
			var body struct {
				User   string `json:"user"`
				Amount int64  `json:"amount"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.balances[org] -= body.Amount
			f.debits = append(f.debits, struct {
				org, user string
				cents     int64
			}{org, body.User, body.Amount})
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"transactionId": "tx", "amount": body.Amount, "user": body.User, "type": "withdraw"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// statusRec is a ResponseWriter that tracks the status code via a Status()
// method, satisfying base's router.StatusTracker so e.Status() works in tests
// exactly as the real router-wrapped writer does. It defaults to 200 — the
// outcome of a successful handler — so the post-Next price() sees success
// without a terminal handler (which the test harness can't register because
// hook.Event.next is unexported).
type statusRec struct {
	*httptest.ResponseRecorder
	status int
}

func newStatusRec() *statusRec {
	return &statusRec{ResponseRecorder: httptest.NewRecorder(), status: http.StatusOK}
}
func (s *statusRec) Status() int { return s.status }
func (s *statusRec) WriteHeader(code int) {
	s.status = code
	s.ResponseRecorder.WriteHeader(code)
}

func newReq(method, path, org, user string) (*core.RequestEvent, *statusRec) {
	req := httptest.NewRequest(method, path, nil)
	if org != "" {
		req.Header.Set("X-Org-Id", org)
	}
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	rec := newStatusRec()
	return &core.RequestEvent{Event: router.Event{Request: req, Response: rec}}, rec
}

func newPlugin(t *testing.T, commerceURL string) *plugin {
	t.Helper()
	c, err := metering.New(metering.Config{BaseURL: commerceURL, Token: "svc", Org: "hanzo"})
	if err != nil {
		t.Fatalf("metering.New: %v", err)
	}
	return &plugin{client: c, provider: DefaultProvider, price: DefaultPrice, skip: DefaultSkip}
}

func TestGate_DeniesUnfundedOrg_402(t *testing.T) {
	fc := &fakeCommerce{balances: map[string]int64{}} // nothing funded
	srv := fc.server()
	defer srv.Close()
	p := newPlugin(t, srv.URL)

	e, rec := newReq(http.MethodPost, "/v1/collections/posts/records", "broke", "bob")
	if err := p.gate(e); err != nil {
		t.Fatalf("gate returned err (should write 402, not error): %v", err)
	}
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (unfunded org gated)", rec.Code)
	}
}

func TestGate_AllowsFundedOrg_AndDebitsPerOrg(t *testing.T) {
	fc := &fakeCommerce{balances: map[string]int64{"acme": 100}}
	srv := fc.server()
	defer srv.Close()
	p := newPlugin(t, srv.URL)

	// A record write by acme/alice: gate passes (funded), Next() is a no-op
	// terminal (returns nil), e.Status() defaults to 200 -> 1c recorded.
	e, rec := newReq(http.MethodPost, "/v1/collections/posts/records", "acme", "alice")
	if err := p.gate(e); err != nil {
		t.Fatalf("gate: %v", err)
	}
	// Not denied: the recorder wasn't written with a billing error.
	if rec.Code == http.StatusPaymentRequired || rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("funded org must not be denied, got %d", rec.Code)
	}

	// Record is async; poll for the debit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fc.mu.Lock()
		n := len(fc.debits)
		fc.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.debits) != 1 {
		t.Fatalf("debits = %d, want 1", len(fc.debits))
	}
	d := fc.debits[0]
	// Per-org: the debit user IS the org slug, and it hits the org's ledger.
	if d.org != "acme" || d.user != "acme" || d.cents != 1 {
		t.Errorf("debit = {org:%s user:%s cents:%d}, want {acme acme 1}", d.org, d.user, d.cents)
	}
	if fc.balances["acme"] != 99 {
		t.Errorf("acme balance = %d, want 99 (100 - 1c record write)", fc.balances["acme"])
	}
}

func TestGate_SkipsHealthAndIAM_NoCharge(t *testing.T) {
	fc := &fakeCommerce{balances: map[string]int64{}} // unfunded: would deny if gated
	srv := fc.server()
	defer srv.Close()
	p := newPlugin(t, srv.URL)

	for _, path := range []string{"/healthz", "/v1/iam/oauth/token"} {
		e, rec := newReq(http.MethodPost, path, "broke", "bob")
		if err := p.gate(e); err != nil {
			t.Fatalf("gate(%s): %v", path, err)
		}
		if rec.Code == http.StatusPaymentRequired {
			t.Errorf("%s should bypass the gate, got 402", path)
		}
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.debits) != 0 {
		t.Errorf("skipped paths must not be charged, got %d debits", len(fc.debits))
	}
}

func TestGate_ReadIsFree(t *testing.T) {
	fc := &fakeCommerce{balances: map[string]int64{"acme": 100}}
	srv := fc.server()
	defer srv.Close()
	p := newPlugin(t, srv.URL)

	// A GET (read) on records: gate passes, but DefaultPrice returns 0 -> no debit.
	e, _ := newReq(http.MethodGet, "/v1/collections/posts/records", "acme", "alice")
	if err := p.gate(e); err != nil {
		t.Fatalf("gate: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // give any (erroneous) async record a chance
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.debits) != 0 {
		t.Errorf("reads must be free, got %d debits", len(fc.debits))
	}
}
