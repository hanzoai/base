package tasks

import (
	"context"
	"os"
	"strings"

	"github.com/hanzoai/tasks/pkg/sdk/client"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// iamTokenSource mints and auto-refreshes a short-lived IAM client_credentials
// bearer from this process's own service identity, for cloud's identity-gated
// tasks engine (Embedded.ServeGated, RequireIdentity). The bearer rides every
// RPC and the gated engine org-scopes the request to the token owner (tasks
// CONTRACT §6) — the credential that lets a hanzo-base app run durable work on
// cloud instead of the retired standalone tasksd.
//
// Credentials + endpoint use the canonical base convention, the same env
// base/plugins/platform reads: $IAM_CLIENT_ID, $IAM_CLIENT_SECRET, and
// $IAM_ENDPOINT (falling back to $IAM_URL). The endpoint MUST resolve to the
// in-cluster IAM Service: a server-side POST to the public issuer host is 403'd
// by Cloudflare (edge 1006). The token's iss stays the configured public issuer
// regardless of the mint URL, matching cloud's validator. AuthStyleInParams puts
// the credentials in the form body — hanzo.id (Casdoor) does not accept Basic.
//
// Returns nil when the identity is unconfigured: the caller then dials the engine
// ungated, correct for a dev/loopback frontend.
func iamTokenSource() client.TokenSource {
	clientID := strings.TrimSpace(os.Getenv("IAM_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("IAM_CLIENT_SECRET"))
	base := strings.TrimSpace(os.Getenv("IAM_ENDPOINT"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("IAM_URL"))
	}
	if clientID == "" || clientSecret == "" || base == "" {
		return nil
	}
	cc := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     strings.TrimRight(base, "/") + "/v1/iam/oauth/token",
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	ts := cc.TokenSource(context.Background()) // caches + refreshes on expiry
	return func(context.Context) (string, error) {
		t, err := ts.Token()
		if err != nil {
			return "", err
		}
		return t.AccessToken, nil
	}
}
