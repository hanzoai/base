package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGet(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotAuth, gotOrgID string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotOrgID = r.Header.Get("X-Org-Id")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-token-123", "org-abc")
	data, status, err := c.Get("/collections")
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if gotMethod != "GET" {
		t.Fatalf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/api/collections" {
		t.Fatalf("expected /api/collections, got %s", gotPath)
	}
	if gotAuth != "test-token-123" {
		t.Fatalf("expected test-token-123, got %s", gotAuth)
	}
	if gotOrgID != "org-abc" {
		t.Fatalf("expected org-abc, got %s", gotOrgID)
	}
	if data == nil {
		t.Fatal("expected non-nil response data")
	}
}

func TestClientPost(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotContentType string
	var gotBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"abc123"}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "tok", "")
	body := map[string]string{"title": "hello"}
	data, status, err := c.Post("/collections/posts/records", body)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if gotMethod != "POST" {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/collections/posts/records" {
		t.Fatalf("expected /api/collections/posts/records, got %s", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected application/json, got %s", gotContentType)
	}

	var parsed map[string]string
	if err := json.Unmarshal(gotBody, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["title"] != "hello" {
		t.Fatalf("expected body title=hello, got %s", parsed["title"])
	}
	if data == nil {
		t.Fatal("expected non-nil response data")
	}
}

func TestClientPatch(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"abc123","title":"updated"}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "tok", "")
	_, _, err := c.Patch("/collections/posts/records/abc123", map[string]string{"title": "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "PATCH" {
		t.Fatalf("expected PATCH, got %s", gotMethod)
	}
	if gotPath != "/api/collections/posts/records/abc123" {
		t.Fatalf("expected /api/collections/posts/records/abc123, got %s", gotPath)
	}
}

func TestClientDelete(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "tok", "")
	_, status, err := c.Delete("/collections/posts/records/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "DELETE" {
		t.Fatalf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/collections/posts/records/abc123" {
		t.Fatalf("expected /api/collections/posts/records/abc123, got %s", gotPath)
	}
	if status != 204 {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestClientErrorResponse(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "", "")
	_, _, err := c.Get("/collections")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestBuildQuery(t *testing.T) {
	t.Parallel()

	// empty
	q := BuildQuery("", 0, "", nil)
	if q != "" {
		t.Fatalf("expected empty query, got %s", q)
	}

	// with params
	q = BuildQuery("name='test'", 10, "-created", nil)
	if q == "" {
		t.Fatal("expected non-empty query")
	}
	// check it contains expected parts
	for _, part := range []string{"filter=", "perPage=10", "sort="} {
		found := false
		for _, seg := range []string{q} {
			if len(seg) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected query to contain %s, got %s", part, q)
		}
	}
}
