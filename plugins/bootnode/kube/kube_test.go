package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient points a Client at a test server (out-of-cluster path).
func newTestClient(serverURL string) *Client {
	c := New("bootnode")
	c.apiServer = serverURL
	c.token = "test-token"
	c.available = true
	return c
}

func TestUnavailableClientErrors(t *testing.T) {
	c := &Client{available: false}
	if c.Available() {
		t.Fatal("zero client must be unavailable")
	}
	if _, err := c.Apply(context.Background(), NetworkGVR, "x", nil, map[string]any{}); err == nil {
		t.Fatal("Apply must error when no cluster configured")
	}
}

func TestApplyServerSideApplyShape(t *testing.T) {
	var gotMethod, gotContentType, gotPath, gotFieldManager string
	var gotBody CustomResource
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotFieldManager = r.URL.Query().Get("fieldManager")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(gotBody)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	name, err := c.Apply(context.Background(), NetworkGVR, "lux-mainnet",
		map[string]string{"bootnode.dev/org": "lux"},
		map[string]any{"tier": "pro", "region": "sfo3"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if name != "lux-mainnet" {
		t.Fatalf("want name lux-mainnet, got %q", name)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("SSA must PATCH, got %s", gotMethod)
	}
	if gotContentType != "application/apply-patch+yaml" {
		t.Errorf("SSA content-type wrong: %q", gotContentType)
	}
	if gotFieldManager != "bootnode" {
		t.Errorf("fieldManager must be bootnode, got %q", gotFieldManager)
	}
	wantPath := "/apis/bootno.de/v1/namespaces/bootnode/networks/lux-mainnet"
	if gotPath != wantPath {
		t.Errorf("path: want %s got %s", wantPath, gotPath)
	}
	if gotBody.APIVersion != "bootno.de/v1" || gotBody.Kind != "Network" {
		t.Errorf("apiVersion/kind wrong: %s/%s", gotBody.APIVersion, gotBody.Kind)
	}
	if gotBody.Metadata.Namespace != "bootnode" {
		t.Errorf("namespace not injected: %q", gotBody.Metadata.Namespace)
	}
	if gotBody.Spec["tier"] != "pro" {
		t.Errorf("spec not propagated: %v", gotBody.Spec)
	}
	if gotBody.Metadata.Labels["bootnode.dev/org"] != "lux" {
		t.Errorf("labels not propagated: %v", gotBody.Metadata.Labels)
	}
}

func TestApplyPropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"conflict"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.Apply(context.Background(), NodeFleetGVR, "f1", nil, map[string]any{}); err == nil {
		t.Fatal("Apply must surface a 409 as an error")
	}
}

func TestGetNotFoundReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	found, err := c.Get(context.Background(), NetworkGVR, "missing", nil)
	if err != nil {
		t.Fatalf("Get on 404 must not error: %v", err)
	}
	if found {
		t.Fatal("Get must report not found")
	}
}

func TestDeleteIdempotentOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("want DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if err := c.Delete(context.Background(), KMSSecretGVR, "gone"); err != nil {
		t.Fatalf("Delete on 404 must be idempotent (no error), got %v", err)
	}
}
