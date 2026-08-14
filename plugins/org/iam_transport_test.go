package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zap-proto/zip"
)

// serveIAM stands up a zip app on a unix socket with no scheme, which is ZAP —
// the same wire Hanzo IAM's own `serve --zap` listens on. It answers the two
// routes IAMClient reads, and records the Authorization header it saw so a test
// can prove the credential crossed the wire rather than merely that bytes did.
func serveIAM(t *testing.T) (addr string, seen *string) {
	t.Helper()

	var got string
	app := zip.New(zip.Config{AppName: "iam-fake", DisableStartupMessage: true})
	app.Get("/v1/iam/oauth/userinfo", func(c *zip.Ctx) error {
		got = c.Header("Authorization")
		return c.JSON(200, map[string]any{
			"id":     "u-1",
			"email":  "alice@hanzo.ai",
			"name":   "alice",
			"orgIds": []string{"hanzo"},
		})
	})
	app.Get("/v1/iam/get-user", func(c *zip.Ctx) error {
		if c.Query("accessKey") != "sk-probe" {
			return c.JSON(404, map[string]string{"status": "error"})
		}
		return c.JSON(200, map[string]any{
			"data": map[string]string{
				"id": "u-2", "name": "svc", "email": "svc@hanzo.ai", "owner": "hanzo",
			},
		})
	})

	// A socket rather than a port: no allocation race, nothing left listening on
	// a shared address if a test fails.
	addr = filepath.Join(t.TempDir(), "iam.sock")
	if _, err := zip.Serve(app, addr); err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown() })

	// Serve returns once the listeners are STARTING; the socket appears a moment
	// later. Wait for the address to exist rather than race it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(addr); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("zap listener never bound %s", addr)
		}
		time.Sleep(2 * time.Millisecond)
	}

	return addr, &got
}

// A bare address selects ZAP, and every read IAMClient makes crosses it intact.
func TestIAMClientOverZAP(t *testing.T) {
	addr, seen := serveIAM(t)

	c := NewIAMClient(addr)

	user, err := c.ValidateToken("tok-abc")
	if err != nil {
		t.Fatalf("ValidateToken over zap: %v", err)
	}
	if user.ID != "u-1" || user.Email != "alice@hanzo.ai" {
		t.Fatalf("decoded %+v", user)
	}
	if *seen != "Bearer tok-abc" {
		t.Fatalf("authorization did not cross the wire: %q", *seen)
	}

	key, err := c.ResolveAPIKey("sk-probe")
	if err != nil {
		t.Fatalf("ResolveAPIKey over zap: %v", err)
	}
	if key.ID != "u-2" || len(key.OrgIDs) != 1 || key.OrgIDs[0] != "hanzo" {
		t.Fatalf("decoded %+v", key)
	}
}

// The endpoint's scheme is the only thing that selects the wire, and an http
// endpoint keeps net/http exactly as before.
func TestResolveIAMSelectsWireByScheme(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		wantURL  string
		wantZAP  bool
	}{
		{"", "https://hanzo.id", false},
		{"https://hanzo.id/", "https://hanzo.id", false},
		{"http://iam.hanzo.svc:8000", "http://iam.hanzo.svc:8000", false},
		{"iam.hanzo.svc:9653", "http://iam.hanzo.svc:9653", true},
		{"zap://iam.hanzo.svc:9653", "http://iam.hanzo.svc:9653", true},
		{"/run/hanzo/iam.sock", "http://iam", true},
	} {
		url, client := resolveIAM(tc.endpoint, 0)
		if url != tc.wantURL {
			t.Errorf("%q -> url %q, want %q", tc.endpoint, url, tc.wantURL)
		}
		_, isZAP := client.Transport.(*zapTransport)
		if isZAP != tc.wantZAP {
			t.Errorf("%q -> zap %v, want %v", tc.endpoint, isZAP, tc.wantZAP)
		}
	}
}

// A token is minted against the brand, whose host decides the issuer, so the
// in-cluster address is refused rather than quietly stamping the wrong brand.
func TestExchangeRefusesClusterAddress(t *testing.T) {
	_, _, err := ExchangeOAuth2Token("code", "https://app/cb", Config{
		IAMEndpoint: "iam.hanzo.svc:9653",
	})
	if err == nil {
		t.Fatal("expected a refusal for a non-http endpoint")
	}
	if !strings.Contains(err.Error(), "brand") {
		t.Fatalf("unhelpful error: %v", err)
	}
}
