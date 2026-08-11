package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		if r.URL.Path != "/v1/iam/oauth/userinfo" {
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
