// Package iam is the canonical import path for Hanzo IAM client types and
// helpers. Downstream services should import:
//
//	import "github.com/hanzoai/base/iam"
//
// and use iam.Client / iam.User / iam.NewClient. The implementation lives in
// plugins/org; this package is the stable, brand-neutral surface.
//
// One way to talk to IAM. Type aliases — not copies — so plugins/org and
// iam are interchangeable at the type level: a *org.IAMClient IS an
// *iam.Client.
package iam

import "github.com/hanzoai/base/plugins/org"

// User is an authenticated user record from Hanzo IAM.
type User = org.IAMUser

// Client talks to a Hanzo IAM instance with token caching.
type Client = org.IAMClient

// Key is an API key record from IAM's Key table.
type Key = org.IAMKey

// Config is the platform configuration required by ValidateToken / ExchangeOAuth2.
type Config = org.Config

// AdminCreds are the service-level IAM application credentials used by
// server-to-server methods (LookupByAttribute, EnsureUser).
type AdminCreds = org.AdminCreds

// EnsureUserSpec describes a user to provision idempotently via EnsureUser.
type EnsureUserSpec = org.EnsureUserSpec

// NewClient constructs a Client pointed at the given IAM base URL.
// Empty baseURL defaults to https://hanzo.id. Trailing slashes are trimmed.
var NewClient = org.NewIAMClient

// NewClientWithCache constructs a Client with a custom cache capacity.
// Use this when the default 10,000-entry cache is the wrong size — large
// gateways or low-memory edge nodes.
var NewClientWithCache = org.NewIAMClientWithCache

// ValidateToken validates a bearer token against IAM userinfo without caching.
// Prefer Client.ValidateToken for production use.
var ValidateToken = org.ValidateIAMToken

// ExchangeOAuth2 exchanges an authorization code for access + refresh tokens
// via the IAM OAuth2 token endpoint.
var ExchangeOAuth2 = org.ExchangeOAuth2Token

// IsPublishable reports whether token has the publishable key prefix (pk-).
func IsPublishable(token string) bool { return org.IsPublishableKey(token) }

// IsSecret reports whether token has the secret key prefix (sk-).
func IsSecret(token string) bool { return org.IsSecretKey(token) }

// IsAPIKey reports whether token is any IAM API key (pk-/sk-/hk-).
func IsAPIKey(token string) bool { return org.IsAPIKey(token) }

// IsAnalytics reports whether token is an insights (hi-) or analytics (ha-) key.
func IsAnalytics(token string) bool { return org.IsAnalyticsKey(token) }

// IsWidget reports whether token is a widget embed key (hz-).
func IsWidget(token string) bool { return org.IsWidgetKey(token) }
