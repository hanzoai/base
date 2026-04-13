package cmd_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/cmd"
)

// stubServer returns an httptest.Server that mimics the Base API.
func stubServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/collections", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "col1", "name": "users", "type": "auth"},
				{"id": "col2", "name": "posts", "type": "base"},
			},
			"page":       1,
			"perPage":    30,
			"totalItems": 2,
			"totalPages": 1,
		})
	})

	mux.HandleFunc("GET /api/collections/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "col1",
			"name": name,
			"type": "base",
			"fields": []map[string]any{
				{"name": "title", "type": "text"},
			},
		})
	})

	mux.HandleFunc("GET /api/collections/{col}/records", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "rec1", "title": "hello"},
			},
			"page":       1,
			"perPage":    30,
			"totalItems": 1,
			"totalPages": 1,
		})
	})

	mux.HandleFunc("GET /api/collections/{col}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":    r.PathValue("id"),
			"title": "found",
		})
	})

	mux.HandleFunc("POST /api/collections/{col}/records", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		body["id"] = "new123"
		json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("PATCH /api/collections/{col}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		body["id"] = r.PathValue("id")
		json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("DELETE /api/collections/{col}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})

	mux.HandleFunc("POST /api/collections/{col}/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"token":  "jwt-test-token",
			"record": map[string]any{"id": "su1", "email": "admin@test.com"},
		})
	})

	mux.HandleFunc("GET /api/crons", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "cleanup", "expression": "0 0 * * *"},
		})
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "API is healthy.",
			"data":    map[string]any{},
		})
	})

	return httptest.NewServer(mux)
}

func runCLI(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	cliCmd := cmd.NewCLICommand()

	var buf bytes.Buffer
	cliCmd.SetOut(&buf)
	cliCmd.SetErr(&buf)

	fullArgs := append([]string{"--url", serverURL, "--format", "json"}, args...)
	cliCmd.SetArgs(fullArgs)

	err := cliCmd.Execute()
	return buf.String(), err
}

func TestCLICollectionList(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	out, err := runCLI(t, ts.URL, "collection", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	// output goes to os.Stdout in the command, but we can verify no error
}

func TestCLICollectionGet(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "collection", "get", "posts")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLICollectionSchema(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "collection", "schema", "posts")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIRecordList(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "record", "list", "posts", "--filter", "title='hello'", "--limit", "5")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIRecordGet(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "record", "get", "posts", "rec1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIRecordCreate(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "record", "create", "posts", `{"title":"new post"}`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIRecordUpdate(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "record", "update", "posts", "rec1", `{"title":"updated"}`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIRecordDelete(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "record", "delete", "posts", "rec1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLICronsList(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "crons", "list")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIWhoami(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "--token", "test-jwt", "whoami")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLILoginMissingFlags(t *testing.T) {
	t.Parallel()
	ts := stubServer()
	defer ts.Close()

	_, err := runCLI(t, ts.URL, "login")
	if err == nil {
		t.Fatal("expected error for missing --email and --password")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected 'required' in error, got: %v", err)
	}
}
