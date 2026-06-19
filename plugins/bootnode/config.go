package bootnode

import (
	"os"
	"strings"
)

// Config is the bootnode plugin configuration. It ports the environment-driven
// idioms of the Python bootnode/config.py (pydantic-settings) without the
// commodity-chain RPC catalogue — those URLs belong in the chain registry, not
// plugin config. Only what must vary between environments lives here.
//
// IAM and KMS endpoints are intentionally shared with the platform plugin: a
// bootnode deployment is always co-resident with platform, so it reuses the
// same IAM client surface (github.com/hanzoai/base/iam).
type Config struct {
	// Enabled gates the whole plugin. A zero-value Config is disabled; callers
	// opt in explicitly. Mirrors the waitlist plugin convention.
	Enabled bool

	// IAMEndpoint is the Hanzo IAM base URL (default https://hanzo.id).
	IAMEndpoint string

	// IAMClientID / IAMClientSecret are the bootnode service's IAM application
	// credentials, used for the OAuth2 authorization-code exchange.
	IAMClientID     string
	IAMClientSecret string

	// AllowedOrgs restricts which IAM orgs may authenticate. Empty means the
	// canonical four (hanzo, zoo, lux, pars).
	AllowedOrgs []string

	// FrontendURL is the default OAuth redirect base when a request omits an
	// explicit redirect_uri (default http://localhost:3001).
	FrontendURL string

	// APIKeySalt is mixed into the SHA-256 of bootnode-issued API keys (bn_…)
	// before storage. Required in production; the plugin refuses to start with
	// the insecure default when IAMEndpoint points at a non-local host.
	APIKeySalt string

	// KubeNamespace is the namespace bootno.de CRs are applied into
	// (default "bootnode").
	KubeNamespace string

	// CommerceURL / CommerceAPIKey configure the Hanzo Commerce billing client.
	// An empty API key disables billing (no-op).
	CommerceURL    string
	CommerceAPIKey string
}

const insecureSaltDefault = "change-me-in-production"

// canonicalOrgs is the default allow-list of IAM orgs.
var canonicalOrgs = []string{"hanzo", "zoo", "lux", "pars"}

// resolve fills defaults. It is idempotent.
func (c *Config) resolve() {
	if c.IAMEndpoint == "" {
		c.IAMEndpoint = "https://hanzo.id"
	}
	if c.FrontendURL == "" {
		c.FrontendURL = "http://localhost:3001"
	}
	if c.KubeNamespace == "" {
		c.KubeNamespace = "bootnode"
	}
	if c.CommerceURL == "" {
		c.CommerceURL = "https://commerce.hanzo.ai"
	}
	if c.APIKeySalt == "" {
		c.APIKeySalt = insecureSaltDefault
	}
	if len(c.AllowedOrgs) == 0 {
		c.AllowedOrgs = canonicalOrgs
	}
}

// orgAllowed reports whether org is in the allow-list.
func (c *Config) orgAllowed(org string) bool {
	for _, o := range c.AllowedOrgs {
		if o == org {
			return true
		}
	}
	return false
}

// isProductionIAM reports whether the IAM endpoint is a non-local host, which
// the plugin treats as "production" for fail-fast secret validation.
func (c *Config) isProductionIAM() bool {
	e := strings.ToLower(c.IAMEndpoint)
	return !(strings.Contains(e, "localhost") || strings.Contains(e, "127.0.0.1"))
}

// ConfigFromEnv builds a Config from the standard bootnode environment
// variables. Used by the default Base wiring; tests construct Config directly.
func ConfigFromEnv() Config {
	return Config{
		Enabled:         os.Getenv("BOOTNODE_ENABLED") == "true",
		IAMEndpoint:     os.Getenv("IAM_URL"),
		IAMClientID:     os.Getenv("IAM_CLIENT_ID"),
		IAMClientSecret: os.Getenv("IAM_CLIENT_SECRET"),
		FrontendURL:     os.Getenv("FRONTEND_URL"),
		APIKeySalt:      os.Getenv("BOOTNODE_API_KEY_SALT"),
		KubeNamespace:   os.Getenv("BOOTNODE_K8S_NAMESPACE"),
		CommerceURL:     os.Getenv("COMMERCE_URL"),
		CommerceAPIKey:  os.Getenv("COMMERCE_API_KEY"),
		AllowedOrgs:     splitNonEmpty(os.Getenv("BOOTNODE_ALLOWED_ORGS")),
	}
}

// splitNonEmpty splits a comma-separated env value, trimming blanks.
func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
