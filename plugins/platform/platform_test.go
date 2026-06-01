package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsValidSlug(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{"hello", true},
		{"my-org", true},
		{"org123", true},
		{"a", true},
		{"", false},
		{"-bad", false},
		{"bad-", false},
		{"Bad", false},
		{"no spaces", false},
		{"no_underscores", false},
		{"no.dots", false},
	}
	for _, tt := range tests {
		if got := isValidSlug(tt.slug); got != tt.want {
			t.Errorf("isValidSlug(%q) = %v, want %v", tt.slug, got, tt.want)
		}
	}
}

func TestRoleHasPermission(t *testing.T) {
	tests := []struct {
		role       string
		permission string
		want       bool
	}{
		{RoleOwner, "owner", true},
		{RoleOwner, "admin", true},
		{RoleOwner, "member", true},
		{RoleOwner, "read", true},
		{RoleAdmin, "owner", false},
		{RoleAdmin, "admin", true},
		{RoleAdmin, "member", true},
		{RoleAdmin, "read", true},
		{RoleMember, "owner", false},
		{RoleMember, "admin", false},
		{RoleMember, "member", true},
		{RoleMember, "read", true},
		{RoleViewer, "owner", false},
		{RoleViewer, "admin", false},
		{RoleViewer, "member", false},
		{RoleViewer, "read", true},
		{"invalid", "read", false},
		{RoleViewer, "invalid", false},
	}
	for _, tt := range tests {
		if got := roleHasPermission(tt.role, tt.permission); got != tt.want {
			t.Errorf("roleHasPermission(%q, %q) = %v, want %v", tt.role, tt.permission, got, tt.want)
		}
	}
}

func TestOrgPrefix(t *testing.T) {
	got := OrgPrefix("acme")
	want := "t_acme_"
	if got != want {
		t.Errorf("OrgPrefix(\"acme\") = %q, want %q", got, want)
	}
}

func TestScopedQuery(t *testing.T) {
	got := ScopedQuery("acme", "tasks")
	want := "t_acme_tasks"
	if got != want {
		t.Errorf("ScopedQuery(\"acme\", \"tasks\") = %q, want %q", got, want)
	}
}

func TestIAMClientValidateToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/userinfo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		json.NewEncoder(w).Encode(IAMUser{
			ID:     "user123",
			Email:  "test@example.com",
			Name:   "Test User",
			OrgIDs: []string{"org1", "org2"},
		})
	}))
	defer server.Close()

	client := NewIAMClient(server.URL)

	// Valid token.
	user, err := client.ValidateToken("good-token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.ID != "user123" {
		t.Errorf("expected user id user123, got %s", user.ID)
	}
	if user.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", user.Email)
	}
	if len(user.OrgIDs) != 2 {
		t.Errorf("expected 2 org IDs, got %d", len(user.OrgIDs))
	}

	// Should be cached.
	user2, err := client.ValidateToken("good-token")
	if err != nil {
		t.Fatalf("cached lookup failed: %v", err)
	}
	if user2.ID != "user123" {
		t.Errorf("cached user id mismatch")
	}

	// Invalid token.
	_, err = client.ValidateToken("bad-token")
	if err == nil {
		t.Fatal("expected error for bad token")
	}

	// Invalidate and re-validate.
	client.InvalidateToken("good-token")
	user3, err := client.ValidateToken("good-token")
	if err != nil {
		t.Fatalf("re-validation failed: %v", err)
	}
	if user3.ID != "user123" {
		t.Errorf("re-validated user id mismatch")
	}
}

func TestKMSClientGetSetDelete(t *testing.T) {
	// Canonical KMS surface: /v1/kms/orgs/{org}/secrets/{path}/{name}
	// with Authorization: Bearer test-kms-token. The base/platform
	// kms bridge short-circuits the IAM token exchange when a static
	// authToken is passed, so the test fixture only needs to validate
	// the bearer on the secret routes.
	type stored struct{ value string }
	secrets := make(map[string]stored)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-kms-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		const orgsPrefix = "/v1/kms/orgs/"
		if !strings.HasPrefix(r.URL.Path, orgsPrefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, orgsPrefix)
		// rest = "{org}/secrets[/{path}/{name}]"
		segs := strings.SplitN(rest, "/", 3)
		if len(segs) < 2 || segs[1] != "secrets" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		org := segs[0]
		switch r.Method {
		case http.MethodGet:
			if len(segs) < 3 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			key := org + "/" + segs[2]
			if v, ok := secrets[key]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"secret": map[string]string{"value": v.value},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)

		case http.MethodPost:
			var body struct {
				Path  string `json:"path"`
				Name  string `json:"name"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			key := org + "/" + body.Path + "/" + body.Name
			if body.Path == "" {
				key = org + "/" + body.Name
			}
			secrets[key] = stored{value: body.Value}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

		case http.MethodDelete:
			if len(segs) < 3 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			key := org + "/" + segs[2]
			delete(secrets, key)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client := NewKMSClient(server.URL, "test-kms-token")

	// Set.
	if err := client.SetSecret("org1", "db-password", "s3cret"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	// Get.
	val, err := client.GetSecret("org1", "db-password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if val != "s3cret" {
		t.Errorf("expected s3cret, got %s", val)
	}

	// Cached get — no server hop.
	val2, err := client.GetSecret("org1", "db-password")
	if err != nil {
		t.Fatalf("cached GetSecret: %v", err)
	}
	if val2 != "s3cret" {
		t.Errorf("cached value mismatch")
	}

	// Delete.
	if err := client.DeleteSecret("org1", "db-password"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
}

func TestKMSClientNoConfig(t *testing.T) {
	client := NewKMSClient("", "")

	_, err := client.GetSecret("t1", "key")
	if err == nil {
		t.Fatal("expected error with no config")
	}

	err = client.SetSecret("t1", "key", "val")
	if err == nil {
		t.Fatal("expected error with no config")
	}
}
