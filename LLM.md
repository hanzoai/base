# LLM.md — hanzoai/base

> Orientation for AI agents. Deep architecture, plugin, IAM, and env reference follows below the divider — keep it.

## What this is
Base is the embedded, **SQLite-first** application backend for the Hanzo cloud:
per-tenant data files, per-org KMS-derived DEK, WAL replication to age-encrypted
object storage, in-process extension runtimes (goja/wazero/pyvm/starkvm), and
**Hanzo IAM** as the one and only auth path (HIP-0111). One Go binary; the storage
substrate every multi-tenant Hanzo Go service builds on (HIP-0105/0106/0107/0302).

## Canonical role
Canonical impl repo — the real code lives here (`github.com/hanzoai/base`). It is
not an SDK; SDK clients and discovery repos link OUT to it. One impl, one place.

## Install / run
- Library: `import "github.com/hanzoai/base"` (Go 1.26+)
- Binary: `go install github.com/hanzoai/base/examples/base@latest && base serve`
- Test: `go test ./...`

## Key entry points
- `base.New()` / `base.NewWithConfig` — app constructor (root package)
- `examples/base/main.go` — the prebuilt binary
- `core/` — app, dialect, storage tier (`ResolveStorageTier`, `BASE_DB_TIER`)
- `plugins/org/` — Hanzo IAM (mandatory, one way), per-org Bases at `/v1/bases`
- `cmd/` — CLI (`AddCLISubcommands`), serve, superuser

## Brand rules (hard)
- Hanzo is a full **AI SDK / AI cloud**, never an "LLM gateway" or LiteLLM proxy.
- `/v1` only — never an `/api/` prefix.
- Zen models are our own family — never name upstream models.
- Voice: "Hanzo — the Open AI Cloud."

Canonical SDK model: `~/work/hanzo/SDK-ARCHITECTURE.md`.
---

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
provisioner (`plugins/org/org_db.go`) already exist — Tier-0/1 are real
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
surface, and any app built on Base. The admin is served at the root. Goes live in **console2** as the Base
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
| zap | plugins/zap/ | ZAP transport (8.7us latency) |
| org | plugins/org/ | Per-org Bases: orgs read from IAM, per-org encrypted SQLite, and per-org secrets from KMS over native ZAP (github.com/luxfi/kms) |
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
surface, no legacy parallel auth path. The platform plugin
(`plugins/org/`) is mandatory and registers IAM unconditionally;
booting without `IAM_ENDPOINT` (or `IAM_MODE=embedded`) errors at startup.

Two ways to host IAM, identical OIDC contract from the client side:

1. **External** (default): set `IAM_ENDPOINT=https://hanzo.id` (or your
   tenant). `/v1/iam/*` is a transparent reverse proxy to that endpoint.
   Full Hanzo IAM features: federation, MFA, social, magic links,
   multi-tenant orgs.
2. **Embedded**: set `IAM_MODE=embedded`. `/v1/iam/*` is served in-process
   by the minimal OIDC provider in `plugins/org/iam_embedded.go`
   (email+password only, no federation). For single-tenant solo
   deployments. See section below for the surface details.

Both modes expose the same six paths under `/v1/iam/*` (discovery, JWKS,
authorize, login, token, userinfo) and the same RS256 JWT shape. The
`@hanzo/iam/browser` SDK does PKCE redirect against either with no
client-side branching — only the feature ceiling differs.

`resolveJWKSToken` (`apis/middlewares.go`) mirrors the verified token into an
ephemeral auth record — nothing is written, IAM stays the user store. The
collection it lands in is the whole authorization decision: `_superusers` for a
member of IAM's reserved `admin` org, the users collection for everyone else.
That membership is the only cross-tenant scope, and Base does not decide what it
means — it decodes the verified claims into `authz.Claims` and asks
`PlatformSudo()`, the estate's published predicate, the same one the gateway
mints `X-User-IsAdmin` from and cloud reads. A second definition here is how
platform authority comes to mean two things in two places.

Not the `owner` claim: IAM stamps it with the organization of the **application**
a token was minted through, not the subject's own, so reading it would hand
platform authority to everyone who signed in through an admin-org app. And not a
machine — a `client_credentials` token carries no membership set at all, which is
how `PlatformSudo` tells a machine from a person.

An `admin` **role** on an ordinary org is a different authority and grants no
part of this: `_superusers` reaches schema, settings, backups and logs for the
whole process, and one process serves many orgs' Bases.

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

## Admin UI is served at the ROOT

base.hanzo.ai IS the admin, so it lives at the address people type. `/v1` is a
longer pattern and wins every request that belongs to the API.

There used to be a `BASE_ADMIN_UI_PATH` knob (default `/_/`) plus a second knob
to redirect `/` to wherever the first one pointed. Both are gone, and so is the
path they existed to reconcile. It cost more than it looked: three things had to
agree — the Go mount, the SPA's build-time Vite `base`, and the redirect URI
registered with IAM — and each was configured separately, so any asset written
as a root path missed the mount and fell to the SPA fallback. That fallback
answers **200 with HTML**, which is why the broken admin logo showed up in no
status code, no console error and no network log; the browser was simply handed
a web page where it asked for an image.

The SPA's callback is derived from the same base (`import.meta.env.BASE_URL`), so
the address it is served at and the redirect it asks for cannot disagree.

The admin is still gated by `BASE_ENABLE_ADMIN_UI=1` (off by default — production
services are headless `/v1` APIs); the `/v1` data plane is always on.

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
`plugins/org/org_db.go` shard on its own tier) builds on this selector.

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
`/api/iam` — `/v1` is Base's one external prefix.

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

Out of scope (boot against an external Hanzo IAM at `IAM_ENDPOINT` if
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

## Admin UI (ui-react) — on @hanzo/ui + @hanzo/gui, 8.x

The admin (`ui-react/`, React 19 + TanStack Router, embedded via `embed.go`
`//go:embed all:dist`, built with `pnpm --dir ui-react build`) runs the Hanzo 8.x
stack: **`@hanzo/ui` components on `@hanzo/gui`**, the cross-platform substrate,
with a true-black token sheet. **No Tailwind, no Radix, no shadcn** — the
`tailwind.config.cjs`, `postcss.config.cjs`, `src/lib/cn.ts` and the ten
vendored `src/components/ui/*` primitives are all gone.

The old note here recorded the opposite verdict (pin `@hanzo/ui@^5.7.1`, adopt
vendored shadcn, wire Tailwind v3) and named packages by their pre-8.x names.
Both are obsolete: `hanzogui` is now **`@hanzo/gui`**, `hanzogui-loader` is
**`@hanzogui/loader`**, and the 8.x line IS a clean Vite consumer.

### The two layers, and where the line falls
- **Overlays are components.** Dialog and DropdownMenu come from `@hanzo/ui`,
  because they carry a11y, focus management and placement that CSS cannot.
- **Everything else is CSS.** `src/index.css` imports `@hanzo/design/styles.css`
  for tokens and the self-hosted Geist/Geist Mono faces, then defines ~30 class
  names for the things this admin has (`.shell`, `.panel`, `.btn`, `.field`,
  `.table`, `.chip`). It replaced ~220 utility classes pasted across 30 files and
  four duplicated style constants. Nothing invents a colour, size or radius —
  every value is a token, so a brand fork retunes the admin for free.
- Vite needs exactly two settings for gui: alias `react-native` →
  `react-native-web`, and `.web.*` first in `resolve.extensions`. `src/gui-env.d.ts`
  registers the v5 config so gui's shorthand style props type-check (the v5
  config sets `onlyShorthandStyleProps`, so the shorthands ARE the API).
- `.dark` pinned on `<html>`; router `basepath` bound to `import.meta.env.BASE_URL`.

### A green build does not prove a visual change (read before editing UI)
`@hanzo/gui` **drops a prop it does not recognise in silence** — no error, no type
failure, just an unstyled element — and CSS resolves an undefined `var(--x)` to
nothing. Both layers fail quietly, so `pnpm build` going green proves nothing
about what renders. `ui-react/smoke.mjs` (`pnpm --dir ui-react smoke`, needs
playwright) is the check that catches silence: it serves the committed `dist/`
at the root exactly as the Go server does, stubs `/v1`, and gates on every route
painting, every `var(--token)` resolving, zero utility-class residue, every
control computing real colours and radii, the overlays opening, and the confirm
dialog measuring the width `DialogContent maxW` asks for. Measure the dialog
**after** the enter animation — gui applies the content's style classes a frame
late, and reading the box early reports ~viewport width, which looks exactly like
the dropped-prop failure it is meant to catch.

### API layer
- One `/v1` fetch layer (`src/lib/api.ts`) + `BaseClient` object facade
  (`src/lib/base-client.ts`) exposing `base.collection(x).method()` / `settings` /
  `authStore`. Realtime = `/v1/realtime` SSE. The old `"/base"` SDK import is gone.

### Auth is IAM-native
- This fork **retires local `_superusers` password auth**: no `superuser` CLI, no
  password field, `auth-with-password` unbound → 404 for every collection
  (`apis/middlewares_test.go`).
- `src/routes/login.tsx` signs in through **IAM OAuth2 PKCE** via the `@hanzo/iam`
  SPA SDK (`src/lib/iam.ts`), completing at `/auth/callback`; the IAM access-token
  JWT rides as the Base `/v1` bearer and Base validates it against IAM's JWKS.
  HIP-0111. The client-side `authWithPassword` helpers that still called the
  retired endpoint are deleted — nothing in the admin can reach it.
- For local testing without IAM, mint a superuser token via
  `record.NewAuthToken()` and set `localStorage.base_auth_token`.

### Build notes
- `dist/` is committed so `go build` is hermetic — CI compiles the binary with no
  Node toolchain. Rebuild it in the same commit as any `src/` change.
- TypeScript **7.0.2** (the native Go compiler; `tsc` and `tsgo` are the same
  binary). Do NOT add `@typescript/native-preview` — it is 7.0.0-dev, behind
  stable.
- `scripts/sync-admin-ui.sh` is gone. It synced a bundle built externally in
  `gui/apps/admin-base`; the SPA is built in this repo now, so the sync path was
  a second way to do one thing.

### Next
1. `collections_.$id` schema editor is raw inputs + react-hook-form on the class
   sheet; a real field-type editor is the open piece.
2. Grid depth: rich relation picker (fetch related presentable), column
   show/hide + width, saved views/filters (system collections exist), CSV export.
