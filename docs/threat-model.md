# What Base defends, and what you configure

Base is one binary holding many tenants' data. This is what it defends on its
own, what it deliberately leaves to the deployment, and the knob for each. Read
it before Base listens on anything but loopback.

Nothing here is a finding. Every line is a standing property of the software
with the thing you do about it.

## The boundary

**Identity comes from Hanzo IAM and from nowhere else.** Base hosts no login,
stores no password and issues no token. A request is whoever IAM's signed token
says it is, verified against IAM's JWKS. `IAM_ENDPOINT` is required; a Base with
no IAM refuses to boot rather than falling back to something weaker.

**A tenant is a file.** The org on a verified token selects
`{DataDir}/orgs/{org}/data.db`, and every read and write in `/v1` lands there.
Two tenants cannot appear in one query because they are not in one database. The
org comes from the token's membership — identity headers arriving from a client
(`X-Org-Id`, `X-User-Id`, `X-User-Email`) are deleted at ingress, because
inbound they are a claim rather than a fact.

**Collection rules are predicates, not filters applied afterwards.** A list rule
is ANDed into the query before pagination and before the count, a joined
collection contributes its own rule, and `?expand=` applies the related
collection's view rule. A collection with no rule is superuser-only: the default
is closed.

**Platform authority is one predicate.** Membership of IAM's reserved `admin`
org, asked once through `PlatformSudo()`. An `admin` role on an ordinary org is
a different authority and grants none of it.

## At rest

**The platform database is not encrypted by the driver.** `DefaultDBConnect`
opens `data.db` with pragmas only — no key. Per-org shards are different: they
open under a per-org DEK derived from the master key, SQLCipher under cgo and a
pure-Go codec envelope otherwise. **Put the data directory on an encrypted
volume.** Anyone who can read the file can read the platform Base with any
SQLite client.

**Settings are plaintext JSON unless you set the encryption env.** SMTP
passwords, S3 secrets and OAuth2 client secrets live in `_params`. With
`EncryptionEnv` named and set, that row is AES-256-GCM; without it, it is
readable JSON. **Set it before you configure any of those.** An unset key does
not fail — it writes plaintext and says nothing — so the only signal you get is
the one you go looking for.

**Backups are plain zip archives.** No encryption, no signature. A backup is as
sensitive as the database inside it. **Keep them in a bucket with
server-side encryption and tight access**, and treat a restore as trusting
whoever could write to that bucket.

**A backup taken on the platform Base spans the deployment.** It archives
`DataDir`, which contains every tenant's file. A tenant's own backup is scoped
to its own directory. **Do not hand a platform backup to one tenant.**

## On the wire

**TLS is on when you configure HTTPS, and TLS 1.2 is the floor.** ACME and
Let's Encrypt are built in. TLS settings apply only when HTTPS is configured, so
plain-HTTP health probes keep working.

**No `Strict-Transport-Security` header.** Base does not send one. **Set HSTS at
your ingress.**

**CORS defaults to `*`.** With no `AllowedOrigins`, any origin may call the API.
Two things keep that from being worse than it reads: `AllowCredentials` is false
unless you set it, and the middleware refuses a wildcard origin together with
credentials unless you also set `UnsafeWildcardOriginWithAllowCredentials`,
which is named that way on purpose. Base's auth is header-based rather than
cookie-based for the same reason. **Name your origins in production**, and do
not reach for that flag.

**Rate limiting is off by default.** The rules exist and are sensible
(`*:auth` 2 per 3s, `*:create` 20 per 5s) but `Enabled` is false on a fresh
install. **Turn it on before you expose an auth endpoint.** The limits and their
counters belong to the process, not to a tenant, so one org cannot spend
another's budget.

## Files

Uploaded files are typed by their **content, not their `Content-Type` header**.
Only an allowlist — images, video, audio, PDF — is served inline; everything
else gets `Content-Disposition: attachment`. Every download carries
`default-src 'none'; media-src 'self'; style-src 'unsafe-inline'; sandbox`. An
HTML file uploaded as `.png` is detected as HTML, served as an attachment, and
could not run script even if it were not.

## Extensions

**A JavaScript extension runs with the process's authority.** The `jsvm` host
binds `$os` with `readFile`, `writeFile`, `exec` and `exit`, and `routerAdd`
registers a route at any path the extension names. There is no sandbox between
an extension and the machine. **Load extensions you wrote or audited, and treat
the right to add one as equivalent to shell on the host.** If you accept
extensions from tenants, do not use `jsvm`.

## Deliberately not defended

**SQL built from your own strings.** `DeleteTable(dangerousName)` and its
siblings do not parameterize table and column names — the `dangerous` prefix is
the warning. Record data, filters and rules all bind as parameters; these
low-level methods are the exception, and they are yours to call safely.

**Timing on client-supplied filters.** Filterable fields are subject to timing
inference. **Mark `secret`, `code`, `token` and similar fields Hidden** so they
cannot be used in a client filter.

**Concurrent edits to one record.** Base minimizes transactions to avoid lock
contention, so a read and a write are not wrapped together by default. Use the
batch API, or an explicit transaction, where you need one.

## Reporting

Found something not on this page? `.github/SECURITY.md`.
