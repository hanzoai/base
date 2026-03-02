# Hanzo Base Security Audit

**Date**: 2026-04-07
**Auditor**: Blue Team (Defensive Security Architect)
**Scope**: core/, apis/, tools/security/, plugins/ — encryption at rest, auth, collection security, token management
**Commit**: fcc87c0 (main)

---

## Executive Summary

Hanzo Base is a well-structured embedded app framework with solid security fundamentals. Password hashing uses bcrypt (not plaintext), JWT signing is HS256 with per-collection secrets, the JWKS/OIDC integration is sound, and the collection rule system provides granular access control. However, **SQLite encryption at rest is NOT implemented** — this is the most significant finding. The `modernc.org/sqlite` driver does not support page-level encryption, and no sqlcipher or equivalent is in use.

**Overall Risk**: MEDIUM — mitigated by application-layer encryption in the KMS plugin and the settings encryption system, but the raw SQLite database files contain plaintext record data for non-KMS-encrypted fields.

---

## Findings

### CRITICAL — C1: No SQLite Encryption at Rest

**File**: `core/db_connect.go`

The default database connection uses `modernc.org/sqlite v1.48.0`, a pure-Go SQLite3 implementation. This library does **not** support page-level database encryption (sqlcipher/SEE). The connection string contains only WAL/journal pragmas:

```go
pragmas := "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(200000000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-32000)"
db, err := dbx.Open("sqlite", dbPath+pragmas)
```

No `cipher_page_size`, `key`, or `rekey` pragmas. No sqlcipher dependency in `go.mod`.

**Impact**: Anyone with filesystem access to `hz_data/data.db` or `hz_data/auxiliary.db` can read all record data, auth tokens, collection schemas, and settings (unless the settings encryption env is configured) using any SQLite client.

**Mitigating factors**:
1. The settings encryption system (`EncryptionEnv`) encrypts the `_params.settings` row using AES-256-GCM when the env var is set
2. The KMS plugin (`plugins/kms/`) provides transparent field-level AES-256-GCM encryption with AAD for configured collections
3. Password fields are always bcrypt-hashed before storage
4. Token secrets are stored in the settings blob (which can be encrypted)
5. The `DBConnect` config is pluggable — callers CAN supply a sqlcipher driver

**Recommendation**: For production deployments storing sensitive data:
- Use the KMS plugin to encrypt sensitive fields at the application layer
- Always set the `EncryptionEnv` environment variable for settings encryption
- Consider building with a sqlcipher-backed driver via the `DBConnectFunc` config
- For the platform plugin's `OrgIsolation: "sqlite"` mode, per-org encryption keys are derived via HMAC-SHA256 — verify this actually uses sqlcipher (it currently does not; the `OrgEncryptionKey` config exists but the underlying SQLite driver cannot use it for page encryption)

**Severity**: CRITICAL for deployments storing PII/financial data in non-KMS-encrypted fields. LOW for deployments where all sensitive fields are KMS-encrypted and the server runs in a hardened container with no local disk access.

---

### HIGH — H1: bcrypt 72-Byte Truncation Not Enforced at Input Validation

**File**: `core/field_password.go:181-195`

The password field validates length in *runes* (multi-byte characters) but bcrypt truncates at 72 *bytes*. The code acknowledges this:

```go
// note2: technically multi-byte strings could produce bigger length than the bcrypt limit
// but it should be fine as it will be just truncated
```

With a max default of 71 characters and multi-byte UTF-8 (up to 4 bytes/char), input can reach 284 bytes — but bcrypt silently truncates to 72 bytes. Two different passwords sharing the same first 72 bytes would authenticate identically.

**Impact**: LOW in practice (attackers rarely control the first 72 bytes of a user's password), but it is a correctness violation. Users who set very long passwords with multi-byte characters may have weaker protection than expected.

**Recommendation**: Pre-hash with SHA-256 before bcrypt (the "bcrypt + prehash" pattern) or enforce a 72-byte limit at the byte level. This is a known bcrypt limitation and the current behavior is standard across most bcrypt implementations.

---

### HIGH — H2: PostgreSQL Connection Example Uses sslmode=disable

**File**: `core/db_connect_postgres.go:15`

The DSN example in the doc comment reads:

```go
// "postgres://user:pass@host:5432/dbname?sslmode=disable"
```

While this is just a comment/example, it normalizes insecure connections. There is no enforcement of `sslmode=verify-full` or `sslmode=require` in the `PostgresDBConnect` function. The function accepts any DSN the caller provides.

**Impact**: If a developer copies this example for production, database connections would be unencrypted.

**Recommendation**: Change the example to `sslmode=verify-full` and consider validating that the DSN does not contain `sslmode=disable` in production mode (`!IsDev`).

---

### MEDIUM — M1: No HSTS Header

**File**: `apis/middlewares.go:546-555`

The security headers middleware sets `X-XSS-Protection`, `X-Content-Type-Options`, and `X-Frame-Options`, but does NOT set `Strict-Transport-Security`. There is an explicit TODO:

```go
// @todo consider a default HSTS?
```

**Impact**: Clients are not instructed to always use HTTPS, making them vulnerable to SSL stripping attacks on first connection.

**Recommendation**: Add `Strict-Transport-Security: max-age=63072000; includeSubDomains` when HTTPS is configured. This is safe — Base already supports ACME/Let's Encrypt.

---

### MEDIUM — M2: CORS Default is Wildcard Origin

**File**: `apis/serve.go:60-61`, `apis/middlewares_cors.go:123-126`

```go
if len(config.AllowedOrigins) == 0 {
    config.AllowedOrigins = []string{"*"}
}
```

The default CORS policy allows all origins. While `AllowCredentials` defaults to `false` (which prevents cookie-based CSRF from arbitrary origins), this is overly permissive for production.

**Impact**: Any website can make cross-origin API requests. This is acceptable for public APIs but dangerous for admin endpoints.

**Recommendation**: Consider restricting default CORS for the `/_/` admin dashboard routes. The current behavior is typical for BaaS platforms but should be documented as a conscious decision.

---

### MEDIUM — M3: Rate Limiting Disabled by Default

**File**: `core/settings_model.go:173`

```go
RateLimits: RateLimitsConfig{
    Enabled: false,
```

Rate limiting is configured with sensible rules (2 req/3s for auth, 300 req/10s general) but disabled by default.

**Impact**: Fresh installations are vulnerable to brute-force attacks on auth endpoints until an admin explicitly enables rate limiting.

**Recommendation**: Enable by default for new installations. The comment says "once tested enough" — after years of use this should be enabled.

---

### MEDIUM — M4: Installer Superuser Token Has 30-Minute Window

**File**: `apis/installer.go:25-26`

```go
token, err := systemSuperuser.NewStaticAuthToken(30 * time.Minute)
url := fmt.Sprintf("%s/_/#/baseinstal/%s", strings.TrimRight(baseURL, "/"), token)
```

The installer creates a superuser with `DefaultInstallerEmail` (`__hzinstaller@example.com`) and generates a 30-minute static auth token embedded in a URL. This URL is printed to the console and optionally opened in a browser.

**Mitigating factors**:
- The installer superuser is deleted when a real superuser is created (line 54-65 in `record_model_superusers.go`)
- The token is short-lived (30 min)
- Only runs when zero non-installer superusers exist

**Impact**: If the console output is logged or the URL is intercepted, an attacker has 30 minutes to create a superuser account.

**Recommendation**: Consider reducing to 5-10 minutes or requiring the token to be manually copied rather than auto-opened in a browser.

---

### LOW — L1: JWT Signing Uses HS256 (Symmetric)

**File**: `tools/security/jwt.go:46-58`

All internal JWT tokens (auth, verification, password reset, email change, file) use HS256 with per-collection secrets. The signing key is `tokenKey + collection.AuthToken.Secret`.

This is acceptable for a self-contained system where the same process signs and verifies. The JWKS integration for external tokens correctly supports RS256/RS384/RS512 (asymmetric).

**Impact**: LOW — HS256 is appropriate here. The per-record `tokenKey` provides implicit token revocation (changes on password change).

---

### LOW — L2: Settings Encryption is Optional

**File**: `core/settings_model.go:260-269`, `core/settings_query.go:57-88`

Settings encryption requires an environment variable (`EncryptionEnv`). If not set, settings (including SMTP passwords, S3 secrets, OAuth2 client secrets, backup S3 secrets) are stored as plaintext JSON in the `_params` table.

The code tries plaintext JSON parse first, then attempts decryption:

```go
plainDecodeErr := json.Unmarshal(param.Value, s)
if plainDecodeErr != nil {
    encryptionKey := os.Getenv(app.EncryptionEnv())
    // ...
}
```

**Impact**: In development mode or misconfigured deployments, sensitive configuration data is stored unencrypted.

**Recommendation**: Warn loudly at startup if `EncryptionEnv` is not set and `IsDev` is false.

---

### LOW — L3: CSP Only on Admin Dashboard

**File**: `apis/serve.go:93-94`

A Content-Security-Policy header is set for the `/_/` admin dashboard routes, but NOT for API responses or custom collection endpoints.

**Impact**: LOW — API responses are typically JSON and not rendered in browsers. The admin dashboard CSP is correct and includes appropriate source restrictions.

---

## Positive Findings

### P1: Password Hashing is Correct
- bcrypt with configurable cost (default 10, min 4, max 31)
- Passwords are hashed before storage, never stored in plaintext
- Plain password values are cleared from memory after create/update
- Password validation uses `bcrypt.CompareHashAndPassword` (constant-time)

### P2: CSRF Protection is Sound
- Token-based auth (no cookies by default) eliminates most CSRF vectors
- `SameSite` is not relevant since auth is header-based
- The CORS implementation correctly refuses credentials with wildcard origins (unless explicitly opted in with the `UnsafeWildcardOriginWithAllowCredentials` flag)

### P3: Superuser Protections are Well-Designed
- Cannot delete the last superuser (enforced in transaction)
- OAuth2 is explicitly disabled for superusers (prevents accidental account creation)
- Password auth is always forced on for superusers
- OTP requires MFA for superusers
- Installer superuser is auto-deleted when a real superuser is created

### P4: Token System is Well-Structured
- Per-record `tokenKey` provides implicit revocation on password change
- Separate secrets per token type per collection (auth, verification, password reset, email change, file)
- Token type claim prevents cross-purpose token use
- `collectionId` claim prevents cross-collection token use
- Secrets are 50 random characters from `[A-Za-z0-9]` (using `crypto/rand`)

### P5: Random Generation Uses crypto/rand
- `security.RandomString` and `security.RandomStringWithAlphabet` use `crypto/rand.Int`
- `PseudorandomString` (math/rand) is explicitly documented as non-security and only used for model IDs

### P6: AES-256-GCM Implementation is Correct
- `tools/security/encrypt.go` uses proper nonce generation from `crypto/rand`
- Nonce is prepended to ciphertext (standard pattern)
- No IV reuse — fresh random nonce per encryption

### P7: JWKS Integration is Secure
- Response body limited to 256KB (prevents DoS)
- Cache with TTL prevents repeated fetches
- Cache eviction at 100 entries prevents memory growth
- Token algorithm restricted to key's `alg` field when specified
- `kid` required in token header (prevents key confusion)

### P8: KMS Plugin Field Encryption is Sound
- AES-256-GCM with AAD scoped to `org:collection:record_id`
- AAD prevents ciphertext transplant between records
- CEK is cleared after use (`defer clear(cek)`)
- Locked state prevents silent data leaks (returns encrypted data, not error)

### P9: External Auth (IAM/OIDC) Integration is Well-Guarded
- `externalAuthGuard` middleware blocks built-in auth endpoints when IAM-only mode is active
- Superusers are exempt (admin panel needs local auth)
- Publishable keys (pk-) are restricted to read-only operations
- API key type enforcement runs at middleware priority +3 (after identity resolution)

### P10: Collection Access Rules are Deny-by-Default
- `nil` rule = superuser only (deny all non-superusers)
- Empty string rule = allow all authenticated and guest
- Non-empty rule = evaluated as filter expression
- Hidden fields are not searchable by non-superusers

### P11: Constant-Time Hash Comparison
- `security.Equal` uses `subtle.ConstantTimeCompare` (no timing leaks)

### P12: TLS Configuration is Correct
- MinVersion set to TLS 1.2
- ACME/Let's Encrypt auto-cert management
- TLS config only applied when HTTPS is explicitly configured (prevents breaking HTTP health probes)

---

## Architecture Notes

### Encryption Layers (Defense in Depth)
1. **Transport**: TLS 1.2+ (when HTTPS configured)
2. **Settings**: AES-256-GCM via `EncryptionEnv` (optional)
3. **Field-level**: AES-256-GCM via KMS plugin with per-record AAD
4. **Passwords**: bcrypt (always)
5. **Database**: NONE (no page-level SQLite encryption)
6. **Backup**: Unencrypted zip files (relies on S3 SSE if configured)

### Auth Flow Chain
1. `loadAuthToken` middleware extracts Bearer token
2. Local JWT validation (HS256, per-record key)
3. Falls back to JWKS validation if configured
4. `platformAPIKeyAuth` resolves IAM API keys (hk-/pk-/sk-)
5. `platformIdentityHeaders` injects X-User-Id/X-Org-Id/X-User-Email
6. `platformKeyTypeEnforcement` blocks writes for pk- keys
7. Collection rules evaluated per-request with auth context

---

## Red Handoff

**What I audited**: Hanzo Base core security model — SQLite encryption, password hashing, JWT tokens, JWKS/OIDC, collection access rules, superuser protections, settings encryption, KMS field-level encryption, CORS, CSP, security headers, rate limiting, installer flow, PostgreSQL connection security.

**Threat model**:
- **Assets**: User records, auth credentials, API tokens, collection schemas, SMTP/S3/OAuth2 secrets in settings, file uploads
- **Adversaries**: Unauthenticated remote attackers (brute force, CSRF, XSS), authenticated users with escalation intent, local attackers with filesystem access, compromised backup files
- **Attack surface**: HTTP API, WebSocket realtime, admin dashboard, SQLite database files, backup archives

**Confidence levels**:
- HIGH: Password hashing, token system, collection rules, JWKS integration, KMS field encryption
- MEDIUM: Settings encryption (depends on env config), CORS (permissive but documented)
- LOW: SQLite encryption at rest (confirmed absent), backup encryption (not present)

**Suggested attack vectors for Red**:
1. Dump SQLite database file and extract non-KMS-encrypted PII
2. Brute-force auth endpoints with rate limiting disabled (default)
3. CORS exploitation against admin dashboard endpoints with wildcard origin
4. bcrypt truncation: craft two passwords with same first 72 bytes
5. Installer token interception during the 30-minute window
6. PostgreSQL connection MITM if `sslmode=disable` is used
7. Backup file exfiltration — backups are unencrypted zip archives
8. Token replay after password change (verify tokenKey rotation actually invalidates)
9. Collection rule bypass via filter injection in search parameters
10. JWKS cache poisoning — can an attacker serve a malicious JWKS endpoint?

**Open questions**:
- Is the `OrgEncryptionKey` in the platform plugin actually used for anything if the SQLite driver cannot do page encryption?
- Are backup archives signed or integrity-checked before restore?
- Is there audit logging for superuser impersonation?
- What happens to active sessions when a superuser is deleted (besides the last-superuser check)?
