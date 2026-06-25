package metering

import (
	"net/http"
	"testing"
)

func TestDefaultPrice_ChargesPerRecordWrite(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		want         int64
	}{
		// Writes to a record collection on success -> 1c.
		{"POST", "/v1/collections/posts/records", 200, 1},
		{"PATCH", "/v1/collections/posts/records/abc", 200, 1},
		{"DELETE", "/v1/collections/posts/records/abc", 204, 1},
		{"PUT", "/v1/collections/posts/records/abc", 200, 1},
		// Works regardless of BASE_API_PREFIX (segment match).
		{"POST", "/v1/base/collections/posts/records", 200, 1},
		// Reads are free.
		{"GET", "/v1/collections/posts/records", 200, 0},
		{"GET", "/v1/collections/posts/records/abc", 200, 0},
		// Non-record paths are free.
		{"POST", "/v1/settings", 200, 0},
		{"POST", "/v1/collections", 200, 0}, // collection mgmt, not a record write
		// Failures are never charged.
		{"POST", "/v1/collections/posts/records", 400, 0},
		{"POST", "/v1/collections/posts/records", 500, 0},
		{"DELETE", "/v1/collections/posts/records/abc", 403, 0},
	}
	for _, c := range cases {
		if got := DefaultPrice(c.method, c.path, c.status); got != c.want {
			t.Errorf("DefaultPrice(%s %s %d) = %d, want %d", c.method, c.path, c.status, got, c.want)
		}
	}
}

func TestDefaultSkip(t *testing.T) {
	skip := []string{"/healthz", "/v1/iam/oauth/token", "/_/", "/v1/realtime"}
	for _, p := range skip {
		if !DefaultSkip(http.MethodGet, p) {
			t.Errorf("DefaultSkip should bypass %q", p)
		}
	}
	dont := []string{"/v1/collections/posts/records", "/v1/files/x"}
	for _, p := range dont {
		if DefaultSkip(http.MethodPost, p) {
			t.Errorf("DefaultSkip should NOT bypass %q", p)
		}
	}
}

func TestIsRecordPath(t *testing.T) {
	yes := []string{
		"/v1/collections/c/records",
		"/v1/collections/c/records/id",
		"/anything/collections/c/records",
	}
	for _, p := range yes {
		if !isRecordPath(p) {
			t.Errorf("isRecordPath(%q) = false, want true", p)
		}
	}
	no := []string{"/v1/collections", "/v1/collections/c", "/v1/settings", "/records"}
	for _, p := range no {
		if isRecordPath(p) {
			t.Errorf("isRecordPath(%q) = true, want false", p)
		}
	}
}
