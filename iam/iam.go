// Package iam is the canonical import path for Hanzo IAM client types and
// helpers. Downstream services should import:
//
//	import "github.com/hanzoai/base/iam"
//
// and use iam.Client / iam.User / iam.NewClient. The implementation lives in
// plugins/platform; this package is the stable, brand-neutral surface.
//
// One way to talk to IAM. Type aliases — not copies — so plugins/platform and
// iam are interchangeable at the type level: a *platform.IAMClient IS an
// *iam.Client.
package iam

import "github.com/hanzoai/base/plugins/platform"

// User is an authenticated user record from Hanzo IAM.
type User = platform.IAMUser

// Client talks to a Hanzo IAM instance with token caching.
type Client = platform.IAMClient

// Key is an API key record from IAM's Key table.
type Key = platform.IAMKey

// Config is the platform configuration required by ValidateToken / ExchangeOAuth2.
type Config = platform.PlatformConfig

// AdminCreds are the service-level IAM application credentials used by
// server-to-server methods (LookupByAttribute, EnsureUser).
type AdminCreds = platform.AdminCreds

// EnsureUserSpec describes a user to provision idempotently via EnsureUser.
type EnsureUserSpec = platform.EnsureUserSpec

// NewClient constructs a Client pointed at the given IAM base URL.
// Empty baseURL defaults to https://hanzo.id. Trailing slashes are trimmed.
var NewClient = platform.NewIAMClient

// ValidateToken validates a bearer token against IAM userinfo without caching.
// Prefer Client.ValidateToken for production use.
var ValidateToken = platform.ValidateIAMToken

// ExchangeOAuth2 exchanges an authorization code for access + refresh tokens
// via the IAM OAuth2 token endpoint.
var ExchangeOAuth2 = platform.ExchangeOAuth2Token

// IsPublishable reports whether token has the publishable key prefix (pk-).
func IsPublishable(token string) bool { return platform.IsPublishableKey(token) }

// IsSecret reports whether token has the secret key prefix (sk-).
func IsSecret(token string) bool { return platform.IsSecretKey(token) }

// IsAPIKey reports whether token is any IAM API key (pk-/sk-/hk-).
func IsAPIKey(token string) bool { return platform.IsAPIKey(token) }

// IsAnalytics reports whether token is an insights (hi-) or analytics (ha-) key.
func IsAnalytics(token string) bool { return platform.IsAnalyticsKey(token) }

// IsWidget reports whether token is a widget embed key (hz-).
func IsWidget(token string) bool { return platform.IsWidgetKey(token) }
