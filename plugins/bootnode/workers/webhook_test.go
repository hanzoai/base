package workers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeliverSignsAndSucceeds(t *testing.T) {
	secret := "whsec_test"
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get(SignatureHeader)
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher(5 * time.Second)
	res, err := d.Deliver(context.Background(), srv.URL, secret, map[string]any{"event": "block", "number": 42})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !res.Success || res.StatusCode != http.StatusOK {
		t.Fatalf("expected success 200, got %+v", res)
	}
	// Verify the signature the receiver got is a valid HMAC of the body.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Fatalf("signature mismatch: got %q want %q", gotSig, want)
	}
}

func TestDeliverNon2xxIsFailureNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	d := NewDispatcher(5 * time.Second)
	res, err := d.Deliver(context.Background(), srv.URL, "s", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("non-2xx must not be a Go error: %v", err)
	}
	if res.Success {
		t.Fatal("500 must be recorded as Success=false")
	}
	if res.StatusCode != http.StatusInternalServerError || res.ResponseBody != "boom" {
		t.Fatalf("delivery not recorded correctly: %+v", res)
	}
}

func TestDeliverBadURLIsFailure(t *testing.T) {
	d := NewDispatcher(time.Second)
	res, err := d.Deliver(context.Background(), "http://127.0.0.1:0/never", "s", map[string]any{})
	if err != nil {
		t.Fatalf("unreachable host must be a Delivery failure, not an error: %v", err)
	}
	if res.Success || res.Error == "" {
		t.Fatalf("expected failure with error message, got %+v", res)
	}
}
