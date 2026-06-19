// Package auth ports the bootnode authentication surface: the multi-network
// OAuth2 callback (lux-web3 shared client id) and bootnode-issued API keys.
//
// IAM token validation and pk-/sk-/hk- key resolution are NOT reimplemented
// here — they live in github.com/hanzoai/base/iam and are reused. This package
// adds only what is bootnode-specific: deriving the per-network IAM client id
// from the redirect_uri, and the bn_ project-scoped API key lifecycle.
package auth

import (
	"net/url"
	"strings"
)

// NetworkClientIDs maps a white-label network to its IAM application client id.
// All four cloud networks share a single IAM app (app-lux-web3, clientId
// "lux-web3") with per-network redirect URIs registered in IAM. This is the
// exact mapping from the Python bootnode/api/auth/oauth.py.
var NetworkClientIDs = map[string]string{
	"lux":   "lux-web3",
	"pars":  "lux-web3",
	"zoo":   "lux-web3",
	"hanzo": "lux-web3",
}

// apexNetworks maps a bare white-label apex (the brand's own TLD) to its
// network slug. These hosts have no network subdomain to parse — the brand IS
// the registrable domain. bootno.de is the canonical Bootnode brand and is
// served on the Lux primary network, matching the Python oauth.py default.
var apexNetworks = map[string]string{
	"bootno.de": "lux",
	"lux.cloud": "lux",
	"zoo.cloud": "zoo",
}

// NetworkFromRedirectURI extracts the network slug from an OAuth redirect_uri.
//
//	https://cloud.lux.network/auth/callback   → "lux"   (cloud.<net>.<tld>)
//	https://web3.hanzo.ai/auth/callback       → "hanzo" (web3.<net>.<tld>)
//	https://web3.zoo.ngo/auth/callback        → "zoo"
//	https://lux.cloud/auth/callback           → "lux"   (apex brand)
//	https://zoo.cloud/auth/callback           → "zoo"
//	https://bootno.de/...                      → "lux"   (primary brand)
//
// Returns "" when no network can be derived.
func NetworkFromRedirectURI(redirectURI string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	if net, ok := apexNetworks[host]; ok {
		return net
	}
	parts := strings.Split(host, ".")
	// <prefix>.<network>.<tld...> where prefix is a web entrypoint (cloud or
	// web3) → parts[1] is the network. Both prefixes resolve to the same
	// per-network IAM redirect; they are alternate brand surfaces.
	if len(parts) >= 3 && (parts[0] == "cloud" || parts[0] == "web3") {
		return parts[1]
	}
	return ""
}

// ClientIDForRedirect returns the IAM client id to use for a token exchange
// given the request's redirect_uri. It falls back to defaultClientID when the
// redirect maps to no known network.
func ClientIDForRedirect(redirectURI, defaultClientID string) string {
	if id, ok := NetworkClientIDs[NetworkFromRedirectURI(redirectURI)]; ok {
		return id
	}
	return defaultClientID
}
