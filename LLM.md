# LLM.md - Hanzo Base

## Identity

Hanzo Base is the **local-first application runtime for Lux** — and the
**universal backend** for any app: App Platform, CMS, CRM, or a one-off
internal tool. One binary, AI-native, SQLite by default, upgradable per
instance. Web5 client runtime at its core.

Not a fork, not a wrapper.

## Program: Base as the universal backend (roadmap)

The north star: **anyone can stand up any modern backend on Base** — a CRM, a
CMS, an app platform, or a small internal tool — and it just works out of the
box on embedded SQLite, then scales per-instance without a rewrite. AI-native,
with flows/automations as first-class. Reaches parity with best-in-class CRM and
headless-CMS products; their code is reference only, the brand is Hanzo only.

### Storage tiering — one model, per-instance upgrade

Out of the box every Base (and every tenant/org/user shard) is **embedded
SQLite / in-memory** — zero-config, fast, the SaaS default. Each instance (or
per-org / per-user DB) can be **upgraded in place** along one axis, no app
rewrite — the data plane (`/v1` collections/records/auth/files/SQL/realtime) is
identical across tiers:

| Tier | Backend | When | Status |
|------|---------|------|--------|
| 0 (default) | embedded SQLite / `:memory:` | everything out of box | core `dialect.go` |
| 1 | `hanzoai/sql` (PostgreSQL) | relational scale, multi-writer | core `dialect_postgres.go` + `db_connect_postgres.go` + `plugins/cloudsql`; **selector SHIPPED** (`BASE_DB_TIER=sql`) |
| 2 | `hanzoai/datastore` | true horizontal OLAP analytics | repo exists; backend adapter = TODO |
| +doc | `hanzoai/docdb` (FerretDB on `hanzoai/sql`/Postgres) | Mongo-style document API | repo exists; ship as a Base **plugin** = TODO |

The dialect abstraction (SQLite + Postgres) and the per-org/per-user encrypted DB
provisioner (`plugins/platform/org_db.go`) already exist — Tier-0/1 are real
today. Tier-2 (datastore) and the docdb plugin are the wiring gaps.

### App layer — App Platform / CMS / CRM on one schema engine

Base's `collections` + `records` + rules + auth + files + realtime already ARE a
headless backend. The program adds the **product surfaces** on top, all rendered
from the same metadata:

- **Objects/records UI** (CRM/app): record views, filters, kanban/table/board,
  relations, command-menu, dashboards — parity target: the reference CRM's
  `object-record` / `views` / `workflow` / `dashboards` / `command-menu` modules.
- **Publishing/CMS**: draft→publish, content models, scheduled publish, asset
  pipeline (Contentful-class) — built on collections + the file API + scheduler.
- **Flows/automations + AI**: `plugins/functions` (event workers on CRDT ops /
  chain receipts) + `plugins/scheduler` + `plugins/tasks` + the polyglot
  `extruntime` runtimes (gojavm/pyvm/v8vm/wasmvm/starkvm) are the engine; the
  visual workflow + AI-native authoring UI is the gap.

### UI rebuild — `@hanzo/ui` over `@hanzo/gui`

The current admin (`ui-react/`, TanStack Router: Collections/Records/Settings) is
replaced by shared **`@hanzo/ui`** components (powered by **`@hanzo/gui`**),
Hanzo-branded, so the SAME components render the Base admin, the embedded console2
surface, and any app built on Base. Admin mount stays configurable
(`BASE_ADMIN_UI_PATH`, default `/_/`). Goes live in **console2** as the Base
product (the tenant orchestrator embed already ships; this is the full app
surface).

### Execution

Phased, not one-shot. P0: storage-tier selector + docdb plugin scaffold. P1:
`@hanzo/ui` Base-admin foundation (objects/records/views). P2: flows/automations
+ AI authoring. P3: publishing/CMS. P4: parity hardening + console2 go-live. Each
phase ships buildable + verified; no fabricated surfaces. Large fan-out (per
feature module) suits a multi-agent workflow.

## Architecture

```
Base = local encrypted SQLite + CRDT sync + chain-anchored trust
Lux  = trust/control plane (identity, keys, policy, anchors)
```

Per user/app/org:
- Local encrypted SQLite file as the primary database
- CRDT op log for sync/merge (encrypted ops — peers see ciphertext only)
- Portable identity from Lux I-Chain (IdentityVM)
- Key wrapping/recovery/sharing from Lux K-Chain (KeyVM) + T-Chain (ThresholdVM)
- Chain anchors for integrity, policy, receipts, portability
- Cloud sync as encrypted blob/oplog replication, NOT as source of truth

The server is a relay/index/cache layer, not the owner of truth.

## Core Plugins

| Plugin | Path | Purpose |
|--------|------|---------|
| vault | plugins/vault/ | Per-user encrypted SQLite shards, DEK/KEK, CRDT sync, chain anchor |
| kms | plugins/kms/ | Client-side KMS integration (talks to K-Chain or cloud HSM) |
| zap | plugins/zap/ | ZAP transport (8.7us latency) |
| platform | plugins/platform/ | Hanzo platform integration |
| bootnode | plugins/bootnode/ | Blockchain dev platform (Go port of Python bootnode): /v1 multi-network OAuth, bn_ project keys, teams, network/node/key provisioning via bootno.de/v1 CRDs (dependency-free kube REST client, no client-go). Reuses iam + platform per-org SQLite isolation. Opt-in via BOOTNODE_ENABLED=true |
| commerce | plugins/commerce/ | Typed client for Hanzo Commerce HTTP API (Square billing). Client interface; bootnode depends on it, never the reverse |
| functions | plugins/functions/ | Event workers (on CRDT ops, chain receipts) |
| jsvm | plugins/jsvm/ | JS hook host (.base.js hook files) — still goja-native |
| gojavm | plugins/gojavm/ | `runtime: goja` extensions — delegates to zip's JSRuntime |

## JS Runtime — ONE engine, via zip

Per HIP-0106, there is exactly **one** goja engine in the stack:
`github.com/zap-proto/zip/runtime` (`*runtime.JSRuntime`). base, cloud and
every zip consumer share it.

- `plugins/extruntime/` is the polyglot extension SPI
  (`Runtime`/`Module`/`Loader`). zip re-exports `Loader`/`Module` as type
  aliases of it — it is the seam, not duplication. pyvm/v8vm/wasmvm/
  starkvm all implement it.
- `plugins/gojavm/` is the **goja** implementation of that SPI. It no
  longer carries its own goja pool / VM lifecycle — `NewRuntime()` builds
  a `zipruntime.JSRuntime`, `Load` registers each extension's
  (esbuild-bundled) source via `LoadModule`, and `Invoke` runs the fn
  through `Eval`. gojavm owns only manifest loading, TS/JSX/ESM bundling
  and the JSON-bytes wire.
- `plugins/jsvm/` (the `.base.js` hook host) is unchanged — collapsing it
  onto zip needs base's host-API binds lifted into zip first.

Two thin shims remain in gojavm with `TODO(zip/runtime)` markers (tracked
on zap-proto/zip PR #9): ctx-aware Eval, and multi-file bundling transpile.
The HTTP layer stays on base's `tools/router` (Base-native,
`http.Handler` via `BuildMux`); cloud mounts it under `/v1/base/*` via
`zip.AdaptNetHTTP` (see `cloud/mounts/base/mount.go`). A native-fiber
rewrite of the router is a later, separate step.

## Vault SDK (plugins/vault/)

5 primitives, 18 tests:

1. **Identity** — `OpenUser(userID)` -> resolve DID, derive DEK, bind device
2. **Key Access** — DEK/KEK hierarchy: Master KEK -> Org KEK -> User DEK
3. **Local DB** — `Put(key, value)`, `Get(key)`, `Delete(key)` -> encrypted SQLite
4. **Sync** — `Sync()`, `Merge(ops)` -> CRDT over ZAP (encrypted ops)
5. **Anchor** — `Anchor()` -> merkle root to chain, audit receipt

Key hierarchy:
```
Cloud HSM / K-Chain ML-KEM
  +-- Master KEK (never on disk)
        +-- Org KEK = HMAC-SHA256(master, "vault:org:" + orgID)
              +-- User DEK = HMAC-SHA256(orgKEK, "vault:user:" + userID)
                    +-- AES-256-GCM per entry (random nonce)
```

## What Goes On-Chain (Lux)

- DID / identity roots
- Key handles and rotation events
- Capability / policy state
- Sync checkpoint Merkle roots
- Audit receipts
- Payment / metering records
- Provider registry

## What Stays Local (Base)

- All mutable app data (SQLite)
- CRDT operation logs
- Decrypted user state
- Device key material
- Blob references
- App-specific indexes

## The Firebase Replacement

| Firebase | Web5 (Base + Lux) |
|----------|-------------------|
| Auth | DID + capability/session gateway |
| Firestore | Encrypted SQLite shard per user |
| Offline sync | CRDT (already local-first) |
| Storage | Content-addressed encrypted blobs |
| Functions | Workers on CRDT ops / chain receipts |
| Security Rules | Signed capabilities + chain policy |

## Roadmap

**v1 (shipped)**: vault plugin, encrypted SQLite, DEK/KEK, CRDT sync, anchor
**v2**: org sharing, multi-device enrollment, threshold recovery, per-collection sharing
**v3**: provider marketplace, pay-for-sync/storage/recovery, portable exports
**v4**: FHE/ZK policy modules for selected confidential compute workloads

## Build & Run

```bash
go build ./...
go test ./...
go test ./plugins/vault/  # 18 tests
go test ./cmd/cli/        # 39 tests (network flags, cluster, operator, config, etc.)
go test ./cmd/            # integration tests (collection, record, login, superuser)
```

## CLI Surface (2026-04-13)

Every Base-derived daemon uses `cmd.AddCLISubcommands(root)` to get:

| Command | Purpose | Lux CLI Equivalent |
|---------|---------|-------------------|
| `cluster init/start/stop/status/leader/replicate/failover` | Manage base-ha HA groups | `lux network start/stop/status` |
| `operator apply/status/describe/upgrade/logs` | Manage K8s operator CRDs | `lux chain deploy` |
| `config show/set-env/set-org/init` | CLI config (~/.config/base/config.json) | `lux config` |
| `status` | Daemon health + cluster state | `lux status` |
| `self version/doctor` | Binary management | `lux self` |
| `rpc get/post/patch/delete` | Direct API passthrough | `lux rpc` |

### Network Flags

All commands accept `--mainnet/-m`, `--testnet/-t`, `--devnet/-d`, `--dev`. Exactly one may be set.
Fallback: `$BASE_ENV` -> default `local`.

### Config File

`~/.config/base/config.json` (respects `$XDG_CONFIG_HOME`). Contains default env, per-env URLs, default org.

### Cluster (HA)

Local mode (`--dev`): spawns N `base-ha` processes with auto-filled `BASE_PEERS`.
K8s mode (`--mainnet/--testnet/--devnet`): `kubectl scale` against the correct GKE context.
Consensus: `--consensus lux` (default) or `--consensus pubsub`.

### Operator (K8s CRDs)

Wraps kubectl against `hanzo.ai/v1alpha1` CRDs. Context map per env:
- devnet: `gke_<project>-devnet_us-central1_dev`
- testnet: `gke_<project>-testnet_us-central1_test`
- mainnet: `gke_<project>-mainnet_us-central1_main`

## FHE Position

FHE is NOT the default execution model. Use it for:
- Encrypted policy checks
- Encrypted scoring/matching
- Private collaborative computations
- Sensitive server-side transforms

Do NOT make "FHE SQLite" the baseline. Local SQLite is decrypted locally.
Cloud sees ciphertext. Chain sees commitments. FHE is opt-in compute.

## Key Principle

> Web5 = local-first apps with blockchain as the trust layer.
> Put trust on-chain, keep state local, sync privately, make identity portable.

## Ecosystem Alignment (2026-04-10)

See the full alignment guide below. Summary of conflicts:

| Area | Base Current | Ecosystem Standard | Status |
|------|-------------|-------------------|--------|
| Timestamp fields | `created`/`updated` | `createdAt`/`updatedAt` | CONFLICT |
| API prefix | configurable via `BASE_API_PREFIX` (default `/v1`) | `/v1` or `/v1/<app>` | DONE |
| Soft delete | Hard delete only | `Deleted bool` flag | MISSING |
| Multi-tenancy | None | Per-org SQLite + CEK | MISSING |
| Auth | Built-in auth collections | Hanzo IAM (OIDC/JWKS, mandatory) | DONE (platform plugin, one way) |
| SSE event name | `CONNECT` | `CONNECT` | OK (server + SDK aligned) |
| Error format | `{status, message, data}` | `{status, message, data}` | OK |
| Pagination | `{items, page, perPage, totalItems, totalPages}` | Same | OK |

Migration path: 5 phases, backward-compatible aliases first.
Full details: research brief produced by scientist agent on 2026-04-10.

## IAM-native auth (one and only one way)

Hanzo IAM is the **only** auth source. There is no `BASE_AUTH_MODE` toggle,
no built-in password / OTP / MFA / OAuth2 / email-change / password-reset
surface, no legacy `/_/auth/oidc/*` parallel path. The platform plugin
(`plugins/platform/`) is mandatory and registers IAM unconditionally;
booting without `IAM_ENDPOINT` (or `IAM_MODE=embedded`) errors at startup.

Two ways to host IAM, identical OIDC contract from the client side:

1. **External** (default): set `IAM_ENDPOINT=https://hanzo.id` (or your
   tenant). `/v1/iam/*` is a transparent reverse proxy to that endpoint.
   Full Hanzo IAM features: federation, MFA, social, magic links,
   multi-tenant orgs.
2. **Embedded**: set `IAM_MODE=embedded`. `/v1/iam/*` is served in-process
   by the minimal OIDC provider in `plugins/platform/iam_embedded.go`
   (email+password only, no federation). For single-tenant solo
   deployments. See section below for the surface details.

Both modes expose the same six paths under `/v1/iam/*` (discovery, JWKS,
authorize, login, token, userinfo) and the same RS256 JWT shape. The
`@hanzo/iam/browser` SDK does PKCE redirect against either with no
client-side branching — only the feature ceiling differs.

The middleware mirrors IAM identity into `_superusers` by email, so an
admin row with the JWT's email automatically authorizes admin endpoints
(collections, settings, logs, backups). Identity from IAM; admin
privilege from a `_superusers` row keyed by email.

Store keys: `StoreKeyExternalAuthOnly` (always true once platform
registers), `StoreKeyJWKSURL` (external mode), `StoreKeyAuthUsersCollection`
(default `"users"`).

## Mount prefix (`BASE_API_PREFIX`)

One knob for where the app's data plane lives. Default `/v1`. For
multi-app deployments where a gateway routes by path, set
`BASE_API_PREFIX=/v1/<app>` (e.g. `/v1/base`, `/v1/team`).

The SPA client must match: `VITE_API_PREFIX` (in `gui/apps/admin-*/vite.config.ts`
`define` block) is the client-side counterpart. Both are configured at
deploy together.

**IAM is always a fixed sibling at `/v1/iam`** regardless of
`BASE_API_PREFIX`. In production a gateway typically routes `/v1/iam/*`
to the central IAM service; in solo/dev mode `IAM_MODE=embedded` serves
it in-process. Apps do NOT mount their own IAM at `/v1/<app>/iam`.

Root liveness probe stays at `/healthz` (outside the mount prefix) so
ops doesn't have to know the app name.

## Admin UI path (`BASE_ADMIN_UI_PATH`)

One knob for where the admin dashboard mounts. Default `/_/`. Set
`BASE_ADMIN_UI_PATH=/admin/` (any leading/trailing slashes are normalized to a
single `/x/` form) to relocate it — `apis/serve.go` `adminUIPath()` drives the
static mount, the root redirect, and the start-banner line from this one value.

The SPA client must match (same contract as `BASE_API_PREFIX` ↔ `VITE_API_PREFIX`):
`ui-react/vite.config.ts` `base` reads the SAME `BASE_ADMIN_UI_PATH` env, so the
SPA's absolute asset URLs line up with the mount. Build + serve with the same
value. The committed `ui-react/dist` is built for the default `/_/`; to ship a
relocated admin, rebuild with `BASE_ADMIN_UI_PATH=/admin/ pnpm --dir ui-react build`.
The admin UI is still gated by `BASE_ENABLE_ADMIN_UI=1` (off by default —
production services are headless `/v1` APIs); the `/v1` data plane is always on.

## Storage tier (`BASE_DB_TIER`)

One knob picks the data backend for a Base instance — default `sqlite`,
upgradable in place with no app rewrite (the `/v1` data plane is identical
across tiers). Resolved once in `core.ResolveStorageTier()` (called from
`base.NewWithConfig`) → applied to `BaseAppConfig.DataDSN`/`AuxDSN`, built on the
existing dialect + `PostgresDBConnect` plumbing.

| `BASE_DB_TIER` | Backend | Extra env |
|---|---|---|
| `sqlite` (default / unset) | embedded SQLite (or `:memory:`) | — |
| `sql` | `hanzoai/sql` (PostgreSQL) | `BASE_DB_URL` (DSN); optional `BASE_AUX_DSN` |
| `datastore` | `hanzoai/datastore` (OLAP) | reserved — errors until the adapter ships |

Misconfig fails loudly at startup (sql without `BASE_DB_URL`, the not-yet-wired
`datastore`, or an unknown value) rather than silently running the wrong DB —
same convention as the platform plugin's IAM check. Per-org tiering (each
`plugins/platform/org_db.go` shard on its own tier) builds on this selector.

## SQLite driver — one driver, OUR way (`github.com/hanzoai/sqlite`)

Base opens SQLite through EXACTLY ONE driver, `github.com/hanzoai/sqlite`, which
registers the `"sqlite"` database/sql name under BOTH build configs — modernc
(pure Go, `!cgo`) and mattn/SQLCipher (`cgo`, encrypted at rest). Base MUST NOT
import `modernc.org/sqlite` directly: a cgo consumer (e.g. commerce, which needs
SQLCipher for per-tenant money DBs) links hanzoai/sqlite→mattn AND base's modernc
→ two `sql.Register("sqlite")` → `panic: sql: Register called twice for driver
sqlite` at init. So every direct modernc import was removed (v1.5.5); `go mod why
modernc.org/sqlite` now shows it ONLY transitively under `hanzoai/sqlite`.

- **Connect DSN**: `core.DefaultDBConnect` + the multitenant `store.defaultConnect`
  both open via `sqlite.PragmaDSN(path, sqlite.DefaultPragmas)`. That one pragma
  set (busy_timeout→WAL→journal_size_limit→NORMAL→FK→temp_store→cache_size) is
  encoded in the ACTIVE backend's DSN syntax (modernc `_pragma=name(value)` /
  mattn `_name=value`) — a single-form DSN is silently dropped by the other
  backend, so this is what makes busy_timeout+WAL actually apply under both.
- **WAL/PITR commit hook**: `core/base_network.go` resolves the raw driver conn
  via `sqlite.CommitHookRegisterer` (build-tagged: bridges mattn `func() int` /
  modernc `func() int32` to one `CommitHookFn`) and adapts it to the network
  package's backend-agnostic `HookRegisterer`. `network/*` never touches a
  concrete driver type; test fakes implement `RegisterCommitHook(func() int32)`.
- `go.mod`: `require github.com/hanzoai/sqlite v0.2.1`; `replace
  github.com/mattn/go-sqlite3 => …v1.14.47` defeats the `v2.0.3+incompatible`
  poison pulled by beego under cgo. modernc is now `// indirect` (via
  hanzoai/sqlite). The old `modernc_versions_check.go` libc-pairing warning was
  removed — that pairing is hanzoai/sqlite's concern now, not base's.

## Embedded IAM Mode (`IAM_MODE=embedded`)

Set `IAM_MODE=embedded` to boot Base with an in-process OIDC provider
at `/v1/iam/*` instead of reverse-proxying to an external Hanzo IAM.
Same `@hanzo/iam/browser` PKCE contract from the client's perspective —
the path doesn't change, only the implementation. We use `/v1/iam`, not
`/api/iam` — this is NOT Casdoor.

Surface (minimal viable, NOT a full Hanzo IAM):

- `GET /v1/iam/.well-known/openid-configuration` — OIDC discovery (issuer derived from request Host)
- `GET /v1/iam/.well-known/jwks` — public RSA JWK
- `GET /v1/iam/oauth/authorize` — plain HTML login form
- `POST /v1/iam/oauth/login` — verifies email+password, redirects to `redirect_uri?code=...&state=...`
- `POST /v1/iam/oauth/token` — exchanges single-use code for RS256-signed JWT (1h TTL)
- `GET /v1/iam/oauth/userinfo` — bearer-validated user record

Signing key: `${DataDir}/iam.key` (RSA-2048 PEM, 0600). Generated on
first boot; back it up alongside the SQLite database — losing it
invalidates every outstanding JWT.

Users: `_iam_users` system collection (email + bcrypt-cost-12 password
+ name). Bootstrap via either:

- env: `EMBEDDED_IAM_ROOT_EMAIL=z@example.com EMBEDDED_IAM_ROOT_PASSWORD=...`
  on first boot (no-op if `_iam_users` already has rows)
- CLI: `./base iam-user create z@example.com` (prompts for password
  via stdin, or honor `IAM_USER_PASSWORD`)

Token validation runs in-process via the `platformEmbeddedAuth`
middleware bound at `DefaultLoadAuthTokenMiddlewarePriority - 1`. The
JWT is verified against the local signer (NOT the JWKS-over-HTTP path
external mode uses) and `re.Auth` is set to the matching `_iam_users`
record, so the standard identity-header pipeline keeps working
unchanged.

Out of scope (boot against an external Casdoor at `IAM_ENDPOINT` if
you need any of these): multi-tenant orgs, social federation
(Google/GitHub/SAML), MFA/OTP, password reset, refresh tokens, fancy
login UI.

## Network (Quasar replication) — singleton collapse (v0.48.1)

`BASE_NETWORK=quasar` only engages the Quasar cross-pod plane when at least
one peer is present. A pod started with `BASE_PEERS=""` (empty or unset)
collapses to the standalone noop: no ZAP listener, no self-dial, no
reconnect loop. Same binary scales 1 → N by adding peers to `BASE_PEERS`.

Env matrix:

| BASE_NETWORK | BASE_PEERS | Enabled | Behavior |
|--------------|------------|---------|----------|
| unset        | *          | false   | legacy single-node SQLite |
| `standalone` | *          | false   | explicit standalone |
| `quasar`     | empty      | false   | sole writer, no replication |
| `quasar`     | a,b,...    | true    | full Quasar quorum over ZAP |

`BASE_PEERS` entries may be the operator-emitted pod FQDN
(`<svc>-0.<svc>-network.<ns>.svc.cluster.local:9999`) while
`BASE_NODE_ID` is the bare hostname; `isSelfPeer` matches on the first DNS
label so the transport never dials itself.

## Admin UI (ui-react) — @hanzo/ui true-black rebuild + Twenty-grade grid

The admin (`ui-react/`, React 19 + TanStack Router, embedded via `embed.go`
`//go:embed all:dist`, built with `pnpm --dir ui-react build`) is rebuilt on the
**@hanzo/ui true-black design system** with a Twenty-caliber inline-editable
record grid. Verified end-to-end in a browser against a real `/v1` server:
inline text edit (floating editor) and bool toggle both persist.

### @hanzo/ui consumability verdict (settled empirically)
- **Pin `@hanzo/ui@^5.7.1`** (shadcn/Tailwind/Radix, compiled dist). NOT
  `@hanzo/ui@8` / `@luxfi/ui@7` — those are the `@hanzo/gui` (Tamagui) line:
  raw-TS entry + a Tamagui compiler peer, not a clean Vite drop-in.
- The `@hanzo/ui` **barrel is NOT Vite-consumable**: `dist/index.mjs` hard-imports
  `next/image`, `next/link`, cmdk, sonner, vaul, react-resizable-panels. Granular
  subpaths resolve but several components are coupled to bespoke `@hanzo/ui` theme
  classes (`h-input`, `bg-bg-secondary`), which clash with a true-black shadcn token
  scheme. So we adopt the **token table + vendored shadcn primitives** in
  `src/components/ui/*` (the pattern @hanzo/ui's own `templates/vite-app` uses).
- Wiring: Tailwind v3 + `tailwind.config.cjs` (shadcn HSL token map, `darkMode:['class']`,
  Basel/Geist fonts, `content` scans `node_modules/@hanzo/ui/dist`), `src/index.css`
  token vars with true-black `.dark{--background:0 0% 0%}` + gui surface ladder
  (#0a0a0a/#171717/#1f1f1f), self-hosted Basel + Geist Mono. `.dark` pinned on `<html>`;
  router `basepath` bound to `import.meta.env.BASE_URL` (= `BASE_ADMIN_UI_PATH`).

### API layer
- One `/v1` fetch layer (`src/lib/api.ts`) + `BaseClient` object facade
  (`src/lib/base-client.ts`) exposing `base.collection(x).method()` / `settings` /
  `authStore`. Realtime = `/v1/realtime` SSE. The old `"/base"` SDK import is gone.

### Auth is IAM-native (confirmed in source + empirically)
- This fork **retires local `_superusers` password auth**: no `superuser` CLI, no
  password field, `auth-with-password` route unbound → 404 (or 410-Gone→IAM in
  external-only mode). Admin is meant to authenticate via **IAM PKCE**
  (`/v1/iam/oauth/authorize`, proxied by the platform plugin), HIP-0111.
- **Open (top phased item):** `src/routes/login.tsx` + `api.authWithPassword` still
  target the retired `_superusers/auth-with-password`. Rewire to `@hanzo/iam` PKCE.
  For local testing without IAM, mint a superuser token via
  `record.NewAuthToken()` and set `localStorage.base_auth_token`.

### Phased convergence (remaining views onto @hanzo/ui true-black)
Done: login, records grid, record editor, collections list, root sidebar.
Next, behind the working routes, component-by-component:
1. IAM PKCE login (replaces retired password auth) — unblocks real login.
2. `collections_.$id` schema editor (still raw inputs + react-hook-form) → @hanzo/ui.
3. `settings.*` (11 pages) + `logs` → @hanzo/ui (Input/Select/Switch/Table).
4. Grid depth: rich relation picker (fetch related presentable), column
   show/hide + width, saved views/filters (system collections exist), CSV export.
5. Retire `scripts/sync-admin-ui.sh` (obsolete `gui/apps/admin-base` sync path).
