package commerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDisabledClientIsNoOp(t *testing.T) {
	c := New(Config{}) // no API key
	if c.Enabled() {
		t.Fatal("client with no API key must be disabled")
	}
	err := c.ReportUsage(context.Background(), "sub_1", 10, "idem")
	cerr, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	if cerr.Status != http.StatusServiceUnavailable {
		t.Fatalf("disabled client must return 503, got %d", cerr.Status)
	}
}

func TestGetOrCreateCustomer_CreatesWhenAbsent(t *testing.T) {
	var sawLookup, sawCreate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Source") != "bootnode" {
			t.Errorf("missing X-Source header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/user":
			sawLookup = true
			// empty data => not found
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/user":
			sawCreate = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			meta, _ := body["metadata"].(map[string]any)
			if meta["iam_id"] != "iam-123" {
				t.Errorf("iam_id not propagated to customer metadata: %v", meta)
			}
			_ = json.NewEncoder(w).Encode(Customer{ID: "cust_1", Email: "z@hanzo.ai", Name: "Z", Org: "hanzo"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	cust, err := c.GetOrCreateCustomer(context.Background(), User{ID: "iam-123", Email: "z@hanzo.ai", Name: "Z", Org: "hanzo"})
	if err != nil {
		t.Fatalf("GetOrCreateCustomer: %v", err)
	}
	if cust.ID != "cust_1" {
		t.Fatalf("want customer cust_1, got %q", cust.ID)
	}
	if !sawLookup || !sawCreate {
		t.Fatalf("expected lookup then create; lookup=%v create=%v", sawLookup, sawCreate)
	}
}

func TestGetOrCreateCustomer_ReturnsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Errorf("must not POST when customer already exists")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []Customer{{ID: "cust_existing", Email: "a@b.c"}},
		})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k"})
	cust, err := c.GetOrCreateCustomer(context.Background(), User{ID: "x", Email: "a@b.c"})
	if err != nil {
		t.Fatalf("GetOrCreateCustomer: %v", err)
	}
	if cust.ID != "cust_existing" {
		t.Fatalf("want cust_existing, got %q", cust.ID)
	}
}

func TestReportUsage_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "card declined"})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k"})
	err := c.ReportUsage(context.Background(), "sub_1", 5, "idem-1")
	cerr, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *Error, got %T", err)
	}
	if cerr.Status != http.StatusPaymentRequired || cerr.Message != "card declined" {
		t.Fatalf("error not propagated: %+v", cerr)
	}
}

func TestCancelSubscriptionImmediate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("cancel must use DELETE, got %s", r.Method)
		}
		if r.URL.Query().Get("immediate") != "true" {
			t.Errorf("immediate flag not set")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err := c.CancelSubscription(context.Background(), "sub_9", true); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
}
