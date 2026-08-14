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
- `core/` — app, dialect, the SQLite/server split (`BaseAppConfig.DataDSN`)
- `plugins/org/` — Hanzo IAM (mandatory, one way), per-org Bases at `/v1/bases`
- `cmd/` — CLI (`AddCLISubcommands`), serve, login

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

## Program: swap Supabase for one Go binary

The direction, decided 2026-08-11: Base serves the Supabase wire, so a Supabase
user changes a hostname and their client works. Supabase is roughly seven
services — PostgREST, GoTrue, Realtime, Storage, Edge Functions, Kong, and a
Postgres you must run. Base is one binary and a file. Same API, same policies,
none of the fleet. That is what makes "cheaper" a fact rather than a claim.

| Supabase | Base |
|---|---|
| PostgREST | `/rest/v1/{table}` — `apis/rest.go` |
| GoTrue | Hanzo IAM, the one auth path |
| Realtime | `/v1/realtime` SSE + ZAP |
| Storage | `/v1/files` + object storage |
| Edge Functions (Deno) | `/v1/functions` in-process via `extruntime`; sandboxed execution is `hanzoai/runtime` |
| Postgres (required) | SQLite by default, Postgres optional via `orm/dialect` |
| RLS (in the engine) | enforced in Base's query path; see below |

Shipped: the read path. `/rest/v1/{table}` with select, order, limit/offset,
`Prefer: count=exact` → `Content-Range`, a bare array rather than the
`{items,page,…}` envelope, and predicates that bind as data rather than render
into text. It is a second RENDERING of `recordsList`, never a second read — one
collection lookup, one rule, one field resolver. The test that matters asserts a
collection with no list rule refuses on both doors.

`/rest/v1` is a NEW mount, not a rename: supabase-js hard-codes that path, so
nothing that speaks `/v1/collections` breaks. The 23 Go repos embedding Base
consume Go types, not URLs.

### RLS — one enforcement path, in Go

Postgres enforces RLS in the engine. SQLite has none, so Base enforces policies
itself. **Do not delegate to native Postgres RLS on the sql tier**: Base rules
reference `@request.body.*`, `@request.headers.*`, `@request.method` and
`:changed`, none of which survive translation into a policy expression; nothing
pins a connection per request, so `SET LOCAL` is unsafe, and the count and rows
queries deliberately run concurrently. Base already works hard to make `=`/`!=`
null-safe identically on both engines — delegating would throw away exactly that.
One enforcement path in Go. Native RLS is worth enabling only as a backstop
against connections that are not Base.

Rules ARE predicates, ANDed into the query before pagination and before the
count; a joined collection contributes its own list rule; `?expand=` applies the
related collection's view rule. Base is RLS-on-by-default, which is better than
parity.

**An absent identity matches no row.** `@request.auth.*` with nobody signed in
resolves to a comparison that excludes everything
(`core/record_field_resolver_runner.go:201-222`), which is what `owner =
@request.auth.id` means and what Postgres answers, where `owner = NULL` is NULL
and NULL is not true. It is stated on the built comparison rather than on the
identifier, so it settles every operator at once — `!=` against an absent
identity excludes everything too, exactly as `owner <> NULL` does.

**UPDATE has a post-image check.** `shadowRow` (`apis/record_crud.go:229-271`)
stands a one-row CTE in for the record as it WOULD be after the write, and the
update rule compiles against that shadow exactly as it would against the
collection (`apis/record_crud.go:532-556`). Postgres calls this `WITH CHECK` and
reuses `USING` for it when a policy omits one, so a ported policy means this
whether or not it says so. The rule decides which row may be touched AND what it
is allowed to look like afterwards; those are two questions and both are asked.
`createRule` has had this all along.

### Per-tenant storage — the request reaches the org's Base

An org's Base is a Base: `{DataDir}/orgs/{org}/data.db`, bootstrapped on first
use, with its own collections, settings and logs. `loadAuthToken` resolves the
org from the verified token and points `RequestEvent.App` at that Base before any
handler runs, so every read and write in `/v1` lands there with no handler
knowing anything about tenancy. `apis.Bases` is the one lookup from org to Base;
`plugins/org` registers it, and a deployment that does not has exactly one Base.

The org is `authz.Claims` home-first membership, never the `owner` claim — IAM
stamps `owner` with the org of the APPLICATION a token was minted through, so
reading it put every tenant of one application on one file. `X-Org-Id` and the
rest of the identity headers are deleted on ingress by `tools/claims`: inbound
they are a client asserting an identity. The header is read once first, as the
org a request SAYS it means, and honored only where the token already carries it
— naming any other org is 403, because an empty list reads as an empty Base.

The reserved `admin` org's Base is the process's own, since `_superusers`,
schema, settings, backups and logs are process-wide. A token with no membership
at all is a machine and is refused rather than handed that Base; a machine that
needs a Base uses an IAM key, which carries a real membership.

**The rule belongs to the ADDRESS, not to the subtree.** Three sentences govern
everything under `/v1/bases`, and all three are bound on the ROUTER and read the
path (`actsInNamedOrg`, `publishableReachesNoBase`, `namesItsOwnUser`):

- the org a path names is the org the request acts in, which `loadAuthToken` or
  the key middleware already resolved from the verified credential into
  `apis.RequestEventKeyOrg`;
- a publishable key reaches none of it, refused by KIND and never by method;
- the user a path names is the caller, unless the credential carries the org
  rather than a person — an org admin's token, the org's own `sk-` key, or a
  platform operator.

Refusal is **403 and never 404** — a 404 is an answer about the data, so it tells
a caller its token reached that org.

Why the ROUTER and not the subtree: `tools/router` inherits middleware down the
GROUP tree and not down the URL, so a route registered off the router at the
identical address inherits nothing. Two live mechanisms register exactly that
way — `/v1/bases` itself, and `jsvm`'s `routerAdd`, which takes a path string
from an extension. It is the address that carries the data, so it has to be the
address that carries the rule, and stating it once on the router is what makes a
handler that forgets it impossible to write.

Membership is the token's to state. There is no local `_orgs` or `_org_members`
collection and no local `checkOrgAccess`: a second answer to "who is in this
org" is one that can disagree with the credential the request arrived on, and
the one it arrived on wins.

**Publishable means public, not read-only.** A `pk-` key is the one that ships
inside a web page and travels in `?key=`, so reaching a route with it needs no
Authorization header at all. It is refused across `/v1/bases` by KIND and never
by method — every read there is a GET, so a method check alone settles nothing.
The method floor stays (a `pk-` key writes nothing anywhere) and what it may
READ is settled per address.

**A provider name is not a key to the pod environment.** `provider` is a path
segment the caller writes, so `OrgService.GetCreds` reads KMS or nothing. There
is no environment fallback: the deployment's own secrets, the ones the KMSSecret
CRDs inject, are not addressable by naming a provider an org has not
configured.

Note also what these routes are NOT: `_org_configs` and `_org_customers` are
shared tables on the platform Base keyed by an `org_id` column, so for them the
column is the whole boundary, and the rule above is what defends it. Everything
in `/v1/collections` is defended by the file instead.

`/v1/compliance/*` names no org in its address, so none of the above reaches it
and it has to ask. A compliance application id is the vendor's, and a bare string
in a URL says nothing about who created it, so it is recorded on the caller's
`_org_customers` row (`compliance_application_id`) when the application is
created and checked on every read. `/screen` and `/payment/validate` have no
owned subject at all — the subject is a person the org is considering — so what
they are scoped to is the org doing the asking, and each call spends a screening
on the deployment's vendor account. It is registered only where
`ComplianceEndpoint` is configured, which is nowhere: no deployment sets it, and
nothing in `examples/base/main.go` reads an env var for it, so the whole surface
is unreachable as shipped.

Capacity, still open: open Bases are held in a map with no eviction, so 2000 orgs
is 4000 SQLite handles, and the cold open holds a process-wide write lock across
a full migration run — measured at ~50ms of stall on every other tenant's
request. `orm/db.Namespaces` is the primitive to adopt when that binds.

Hooks bound on the platform app do not fire on a tenant's Base, so what every
Base must do is stated where a Base is built rather than where the router is.
`core.AppBindings` is that list, read by `core.NewBaseApp` next to
`registerBaseHooks` and `registerNATSHooks` — the hook counterpart of
`AppMigrations`, and the reason JS *migrations* already applied per tenant.
Realtime is its one member: `apis` registers `bindRealtimeEvents` from an init,
and `bindRealtimeApi` registers the endpoints and nothing else, because
binding in both places sends a subscriber the same record twice. A subscriber
registers on the broker of the Base its request resolved to, so the broadcast has
to happen on that same Base; two orgs are two brokers, which is what keeps a
topic name they both use from being one topic. The delete broadcast checks its
rule against a Base named separately from the one the record was written to, and
that is now the tenant's own — a rule is a query, and the org's collection exists
on the org's Base and nowhere else.

`jsvm` hooks are deliberately NOT in that list. An extension body is arbitrary,
`$os` binds `exec`/`readFile`/`writeFile`, and whether an extension follows a
request onto a tenant's Base is a question about what an extension may reach —
putting it in the registry would move the question rather than answer it.
`OrgService` is out for the same kind of reason: its methods take an org as an
argument rather than reading one from the request, so it belongs on the process's
own Base, where an operator's extension runs, and `Register` sets it there.

### A stream is opened with a grant

`EventSource` sends no headers, so the credential a caller already holds cannot
travel on the request that opens its stream — and an unauthenticated GET resolves
to no org, registers on the process's Base, and is not there when the `POST
/v1/realtime` that names its id resolves to the org's. A grant travels instead.
`POST /v1/realtime/token` mints one on an ordinary authenticated request; `GET
/v1/realtime?token=` spends it, once, inside half a minute. The string is random
and stands for nothing; what it stands FOR is held in the process
(`apis/realtime_grant.go`) — the Base the minting request resolved to and the
identity it resolved as — so a stream is opened with exactly the authorization of
the request that asked for it, and both halves meet on one broker.

The obvious alternative is the caller's own token in `?token=`, which is what
most SSE APIs do. It spends the wrong thing: an IAM token opens every service in
the estate for as long as it lives, and a query is read by every proxy, ingress
and access log between the browser and here, most of them outside Base. A grant
is worth one stream, on one Base, for thirty seconds — and `logRequest` redacts
`token` alongside `key`, so `_logs` holds neither. A cookie would also work and is
the wrong shape: Base is a token API, and a cookie is a second way to
authenticate. The grant is read by a middleware bound at the one address it
opens, so what it reaches is a property of the route rather than of a string
travelling in a URL; a request carrying none is an anonymous subscriber on the
process's Base, which is what an anonymous caller reads anyway.

The admin does its half in `ui-react/src/lib/api.ts`: mint, open, and reopen with
a fresh grant when the stream drops, because the browser's own retry replays one
that is spent. The handshake event is `CONNECT` on both sides — the client
listened for `PB_CONNECT`, so no `clientId` ever arrived and no subscription was
ever submitted, and both halves had to be right before anything reached a page.

### The process answers what is asked of the process

Anything that governs the process rather than a tenant's data reads the Base the
process serves from, named on the request by the router's event factory and
reached through `core.RequestEvent.Deployment()`: rate-limit settings and
counters, the cleanup cron, `RealIP`, the activity log, and the batch limits.
One policy, one set of counters.

Reading them off `e.App` would ask the tenant instead, since `loadAuthToken` at
priority -1020 has already pointed the request at its tenant's Base by the time
the limiter runs at -1000. Copying the posture onto each tenant Base at bootstrap
is the alternative and is worse: N copies that drift, and per-tenant counters
make a limit of 2 mean 2 per org.

`RealIP` reads the deployment for the same reason and one of its own: which
proxies are believed is the deployment's answer, and only a superuser writes
settings while a tenant's Base has no superuser, so a tenant's `TrustedProxy` is
empty by construction and can never be anything else.

A log row carries no credential and names who acted. `logRequest` redacts any
credential in the query before writing, which matters because a key may arrive in
`?key=` — the shape that needs no Authorization header — and `_logs` is kept for
`Logs.MaxDays`, served by `GET /v1/logs`, and included in every backup. A keyed
request sets `e.Auth`, so a service key's actions are attributed to it.

### Functions — a record that reads as whoever invoked it

The two halves that existed separately are joined: `gojavm` knew how to invoke
by name and bound no host, `jsvm` bound every host and could not invoke, and
`extruntime` was the interface between them, used by nothing but its own tests.

A function is a record in `_functions`, so it is the tenant's own file and the
collection's rules are the rules — `/v1/functions` and the collection refuse in
the same words because there is one rule behind both. The source is the
server's: seeing that a function exists does not return what it says.

**The host is `list` and `one`, and that is all of it.** Not `$app`, not a
process, not a file, not the network. `jsvm`'s `$os` exposes `exec`, `readFile`,
`writeFile` and `exit` — right for an operator's hook file, wrong for something
a tenant may author — and whatever v1 binds is supported forever, so the list is
what a function needs and nothing anticipated.

It reads as its caller: the collection from the Base the credential already
resolved, the rule from the caller's own identity. The invocation payload does
NOT answer a rule — `@request.body` is about a write, and letting a body speak
for it would let a caller satisfy a read rule by naming a field they sent.

### Sandboxes are Hanzo Runtime's, and a function starts one rather than being one

`hanzoai/runtime` is the estate's sandbox: sub-90ms creation, isolated execution
for AI-generated code, File/Git/LSP/Execute, with a generated Go client at
`libs/api-client-go` (module `github.com/hanzoai/apiclient`). **Base must not
grow a second one.**

The two are opposite by design and that is the point. A function binds two reads
and is stopped if it overruns, which is exactly why it is safe to run in-process
at request latency. A coding agent or a research pass needs the four things a
function refuses — network, a writable filesystem, process execution, and
minutes rather than a request — so it belongs on the far side of an isolation
boundary that is a kernel, not a JS engine.

Starting is safe to bind because it hands the function none of that authority.
The function says what to run; the sandbox runs it in isolation; the result
arrives back as an ordinary authenticated write, through the same collection
rules as any other caller. A bot is that with a trigger and an automation is
that with a schedule.

What a workload's own identity should be — a scoped key minted for a sandbox
with its lifetime tied to the run — is deliberately separate, because getting a
workload's authority wrong is the failure that matters. Passing the caller's own
token into a sandbox is the thing not to do.

### Postgres tier — a real dialect that has never been run

The dialect is `hanzoai/orm/dialect`, not the `core/dialect_postgres.go` this
file used to name; 15 methods, ~40 call sites, migrations parameterized through
it. There is no Postgres in CI and no test opens a connection. Known blockers:
`core/validators/db.go` matches SQLite's `"unique constraint failed"`, so every
duplicate is a 500 instead of a 400 field error; `db_retry.go` retries only
SQLite lock messages; and `CreateBackup` archives `DataDir`, which on Postgres
contains no data.

## Program: Base as the universal backend (roadmap)

The north star: **anyone can stand up any modern backend on Base** — a CRM, a
CMS, an app platform, or a small internal tool — and it just works out of the
box on embedded SQLite, then scales per-instance without a rewrite. AI-native,
with flows/automations as first-class. Reaches parity with best-in-class CRM and
headless-CMS products; their code is reference only, the brand is Hanzo only.

### Where a Base keeps its data — embedded here, placed by whoever hosts it

A Base is embedded SQLite. That is what `base serve` runs, what a local
developer gets, and what every tenant, org and user shard is. There is no
setting for it and nothing to misconfigure: run the binary and it works.

Base can also open a server instead, and the `/v1` data plane is the same
either way — the same collections, records, rules, filters, realtime and files
through the same handlers. `Config.DataDSN`/`AuxDSN` name one. Empty is
embedded.

**The choice is the host's, not Base's.** Whether an instance belongs on a
server, and which one, is a question about a deployment, so it is asked by
whoever places the instance rather than read from this process's environment.
Cloud embeds Base as a library and pools a Base per tenant, so it answers per
tenant — and running a server is what a host does: provisioning it, upgrading
it, backing it up, keeping it alive. None of that is Base's job, and Base
having an opinion about it is what made the OSS binary look like it needed one.

`BASE_DB_TIER`, `BASE_DB_URL` and `ResolveStorageTier` are gone for that
reason. So is a `datastore` tier constant, which Base declared publicly and
never implemented — a promise with nothing behind it. OLAP is a different
access pattern and a different product; it belongs where it is built, not as a
reserved word here.

Engine differences are the dialect's (`hanzoai/orm/dialect`), which is estate
code rather than Base's. **No Postgres in CI and no test opens a connection**,
so that path is verified by hand and nothing stops it regressing.

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
| zap | plugins/zap/ | ZAP transport (8.7us latency) — base's fully-wrapped HTTP handler on the `forward` terminal, and nothing else. There are no native message types beside it: resolving an org from a ZAP envelope is authentication, and there is exactly one of those in the estate. |
| org | plugins/org/ | Per-org Bases: orgs read from IAM, per-org encrypted SQLite, and per-org secrets from KMS over native ZAP (github.com/luxfi/kms) |
| bootnode | plugins/bootnode/ | Blockchain dev platform (Go port of Python bootnode): /v1 multi-network OAuth, bn_ project keys, teams, network/node/key provisioning via bootno.de/v1 CRDs (dependency-free kube REST client, no client-go). Reuses iam + platform per-org SQLite isolation. Opt-in via BOOTNODE_ENABLED=true |
| commerce | plugins/commerce/ | Typed client for Hanzo Commerce HTTP API (Square billing). Client interface; bootnode depends on it, never the reverse |
| functions | plugins/functions/ | OpenFaaS gateway proxy — needs Kubernetes. Deploys a container IMAGE by reference; stores no code, binds no hooks. NOT event workers, whatever this table said before. |
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

`~/.config/base/config.json` (respects `$XDG_CONFIG_HOME`). Holds the default
env, the default org and the token path — nothing else. A service URL is
computed, not stored: `EnvURLs(env, service, localPort)` over
`Env.DomainSuffix()`, which is the one way the CLI names a host. Unknown keys
in an existing file are dropped on the next write.

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
booting without `IAM_ENDPOINT` errors at startup.

Base never hosts identity, so there is only one arrangement and only one
knob. `IAM_ENDPOINT` names a Hanzo IAM and `/v1/iam/*` is a transparent
reverse proxy to it. What answers there is IAM's business: federation,
MFA, social, magic links, multi-tenant orgs.

Where IAM runs is a deployment choice, not a Base mode. It is usually
another service (`https://hanzo.id`, or a tenant's). It can equally be
`iam.Embed()` inside a fused daemon, which Base still reaches through
`IAM_ENDPOINT` — being in the same process changes the address, not the
contract, and Base implements no part of OIDC either way.

The `@hanzo/iam/browser` SDK does a PKCE redirect against whatever that
endpoint is, with no client-side branching.

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

## One Base per org, one implementation

An org's Base is `{DataDir}/orgs/{org}/data.db`, opened the first time a request
arrives carrying that org (`plugins/org`). Isolation is physical — a different
org is a different file, so no query can read across two. There is no create
verb: using an org opens its Base. The file was `org.db`, which was one name too
many; the directory already says whose it is, and the second name made a Base
look like something else.

Isolation is also cryptographic. The Base opens under the org's own key —
`OrgDEK`, derived per org, so one org's file is unreadable with another's key and
a leaked key is worth one tenant rather than all of them. `sqlite.OpenDB` is the
whole mechanism and it means the same thing under both builds: SQLCipher when
Base is linked with cgo, the pure-Go codec envelope otherwise. The two are
byte-compatible, so a file written by one opens under the other — which matters
because CI ships `CGO_ENABLED=0` and an operator may not. An empty DEK is dev
mode, opening plaintext; a master key of the wrong length is an error rather than
a silent downgrade, because a key that quietly becomes no key is how data ends up
in the clear while the deployment believes otherwise.

`/v1/bases` reports what is on disk for each org on the caller's token, and
membership is the token's to state. A local copy of "who is in this org" is a
second answer that can disagree with the credential the request arrived on, and
the one it arrived on wins.

There was a second, complete implementation of all this in `core`: a `Bases`
registry with its own encryption, lazy open, and concurrent/nonconcurrent pools,
gated behind `MULTI_BASE`/`MASTER_KEY`, reachable through three `core.App`
interface methods. Same directory, different filename (`data.db` vs `org.db`),
different key derivation, different id validator. Nothing called it — its only
callers were its own tests — and no deployment has ever set either env var. It
is gone.

## Base sends no auth mail

There is no verification or email-change mail, no template for either on a
collection, and no `mails` package. The endpoints those mails pointed at were
deleted in the IAM-native rip — `bindRecordAuthApi` keeps `auth-methods` and
`auth-refresh` and nothing else — so the templates described flows that answer
404 for every deployment, and their buttons addressed a route in an admin SPA
that no longer exists.

What went with them, because each existed only to serve those two mails:
`core.EmailTemplate` and its two collection fields, the `$mails` JSVM namespace
(whose type declarations advertised five helpers, three of which were removed in
that same rip and never bound), the `OnMailerRecordVerificationSend` and
`OnMailerRecordEmailChangeSend` hooks — which nothing could fire — and
`core.MailerRecordEvent`.

Sending mail is untouched: `$app.newMailClient().send()` is the general sender an
app's own hook uses, SMTP settings stay, and `POST /v1/settings/test/email` still
proves they work — it sends a plain message now rather than rendering a template
for a flow that is gone, which is the question an operator actually has.

## A token secret is minted where it is signed with

The secret on a collection's `authToken` / `fileToken` is half the key Base signs
those JWTs with (`record.TokenKey()` is the other half), so it is the server's to
choose. `Collection.MarshalJSON` has always dropped it on the way out; the way in
now says the same thing. `UnmarshalJSON` keeps the secrets already on the model
and takes none from the document, so every door that binds a body — create,
update, `PUT /v1/collections/import` — carries a duration and a rule and never a
key. A new collection gets its secrets from the factory.

Replacing one is therefore a request that carries nothing:
`POST /v1/collections/{collection}/rotate` mints a fresh secret for every token
the collection issues, in one act, because half a rotation is not a thing anyone
wants. Verification reads the secret off the collection the save reloads into the
cache, so every token signed with the old one is refused from the next request on
(`TestRotatingTokenSecretsRefusesTheTokensSignedBefore`).

Base issues two of the four: `auth`, from the refresh endpoint and the sign-in
helper, and `file`, from `POST /v1/files/token`. `verificationToken` and
`emailChangeToken` are still fields — they are in the stored options and they
validate — but nothing in the product mints or reads one; they went quiet with
the mail above. The admin offers the two that are real.

## The admin stopped managing passwords it does not have

`_superusers` carries email, created and updated — there is no password field in
`core/` at all, and `resolveJWKSToken` mirrors a verified IAM identity into an
unsaved record rather than a row. So a settings page that created a superuser
with a password wrote an email-only row that could never sign in, and one that
changed a password sent two fields `SetIfFieldExists` drops and reported success.
It is gone. Who reaches the admin is IAM's `admin` org membership, decided by
`PlatformSudo()`, and Base has nothing to add to that.

## Mount prefix (`BASE_API_PREFIX`)

One knob for where the app's data plane lives. Default `/v1`. For
multi-app deployments where a gateway routes by path, set
`BASE_API_PREFIX=/v1/<app>` (e.g. `/v1/base`, `/v1/team`).

The SPA client must match: `VITE_API_PREFIX` (in `gui/apps/admin-*/vite.config.ts`
`define` block) is the client-side counterpart. Both are configured at
deploy together.

**IAM is always a fixed sibling at `/v1/iam`** regardless of
`BASE_API_PREFIX`. In production a gateway typically routes `/v1/iam/*`
to the central IAM service; otherwise Base proxies it to whatever
`IAM_ENDPOINT` names. Apps do NOT mount their own IAM at `/v1/<app>/iam`.

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

## The admin is one UI with three places to be (`BASE_FRAME_ANCESTORS`)

Standalone at base.hanzo.ai, and as a section inside a Hanzo surface that offers
Base as one of its products. One bundle, one deployment, three mounts — not a
component every host recompiles.

**Why a frame and not a package.** The admin addresses `/v1` with relative paths
and carries the bearer in this origin's `localStorage` (`src/lib/api.ts`), so it
is only ever correct against the Base it was served by. Handed out as a
component, each host would need CORS, an absolute origin and its own copy of that
transport — three implementations of the one thing — and a re-pin and a rebuild
per host every time the admin changes. A frame is the same bytes the Go binary
already embeds, and every host is current the moment the pin moves.

**Who may frame it** is a CSP source list, `BASE_FRAME_ANCESTORS`, default
`'self'`. It is stated in exactly one place, the admin's `frame-ancestors`
(`apis/serve.go`), because the admin is the only surface a frame can be aimed at
to any effect. `X-Frame-Options` is gone rather than kept alongside: it can say
"nobody" or "same origin" and cannot name a host, so it cannot express an
allow-list, and a second weaker copy of a policy is one that drifts. The ingress
already does exactly this in front of hanzo.id.

**Auth does not change, and nothing is handed across the boundary.** The framed
admin runs its own PKCE against IAM like always; a signed-in user has a live
hanzo.id session, so the authorize endpoint 302s straight back with a code and
renders nothing. That is what "shares the host's session" means here — shared at
the IdP, which is where sessions live. No token crosses an origin, no host injects
a credential, no second login. It is the mechanism console.hanzo.ai already uses
to embed studio.hanzo.ai. Two consequences follow and both are honest limits:
IAM must name Base in its OWN `frame-ancestors` for the inner leg to load, and a
host on a different site (hanzo.app is a different registrable domain from
hanzo.ai) is subject to third-party cookie policy — when the silent leg cannot
complete, the frame says so and offers the standalone address, rather than
fabricating a signed-in surface.

**What the frame drops** is the outer chrome only — the brand and the signed-in
account, because the host already answers both — via `src/lib/embed.ts`, which
reads `window.self !== window.top` rather than taking a flag. Where the thing is
is a fact the browser holds; a build flag or query parameter is a second answer
that can be wrong. The browser itself is identical in all three places, because
it is the same admin.

## Both backends run the same data plane

Given a DSN, Base opens PostgreSQL and serves the whole `/v1` data plane on it:
migrations, collection DDL, record CRUD, paging, sorting, filtering, the
per-hour log rollup. Proved by running it — a collection created over the API
lands as a real table with `text`/`numeric`/`jsonb` columns and its declared
index, and records list, page, sort, filter, update and delete through the same
handlers SQLite serves. Realtime, files, auth records, crons, batch and
`/rest/v1` were exercised the same way, against PostgreSQL 18.3.

Three things a live run settled, each of them a place where SQLite's shape had
been assumed:

- **Settings are encrypted at rest on both engines.** The ciphertext is base64
  and `_params.value` is `jsonb`, so it is stored as a JSON string. A row
  written by an older binary is read as-is and rewritten on the first settings
  write; after that an older binary refuses to start rather than misreading it.
  Forward-only, and loud in the direction that is not.
- **The exec mode is `describe_exec`.** A pooled connection holds a prepared
  plan that a schema change invalidates, so without it every collection edit is
  followed by a burst of failures on whichever connections are stale. It costs a
  parse per execution, worst on a local socket and pipelined into the same round
  trip over a network. `exec` and `simple_protocol` are not alternatives: Base
  binds JSON as `[]byte` in many places, which those modes send as `bytea`.
- **The write pool is single-connection only where the engine requires it.** That
  is SQLite's rule; the cap is asked of each connection's own driver, so the aux
  database answers for itself. Lifting it makes lock-order deadlocks reachable in
  one process, where a single connection had been serialising them away — which
  is why retrying a serialization failure is what makes concurrent writes correct
  rather than merely possible.

Nothing in Base branches on the engine. It asks `app.Dialect()`, which resolves
from the driver the data DB was opened with, for the pieces that differ:

| what differs | reached through |
|---|---|
| JSON accessors over a column that may hold a bare scalar | `Each`, `Length`, `Extract`, `Array`, `Last`, wrapped for dbx bracketing in `tools/dbutils/json.go` |
| the schema's own objects | `Catalog()` (type, name, tbl_name, sql) and `Columns()` (tbl_name, cid, name, type, notnull, dflt_value, pk) — every catalog read in `core/db_table.go` |
| generated values in DDL | `Now()`, `Random(n)`, `Json()`, `Bytes()` |
| identifier quoting for hand-built index DDL | `Quote()`, used by `dbutils.Index.Build` |
| what the engine has no equivalent of | `Row()`, `Checkpoint()`, `Format()`, each empty where the feature is absent, so the caller falls back or refuses |
| what the schema needs before it exists | `Prelude()` — Postgres gets a nondeterministic ICU collation named `nocase`, so `COLLATE NOCASE` means the same thing on both. It is for sorting and equality; pattern matching goes through `Like()`, because PG17 refuses a nondeterministic collation for LIKE |
| how a comparison is spelled against a typed column | `Like()`, `Bool()`, `Number()` — see below |

The dialect lives in `hanzoai/orm/dialect`, not here. That is the point: the
thing this repo must not grow is a second hand-rolled dialect, and the estate's
relational package is where one belongs. `core.SQLDialect` — 345 lines, 22
methods, zero callers — was deleted for being exactly that, and it made the
codebase read as though the abstraction existed, which is the most likely reason
nobody noticed the tier could not work.

`orm/engine` was not it. That package is an xorm-compatible bean ORM: Session,
struct tags, reflection over `TableMeta`. Base's collections are defined at
runtime, so there are no structs to reflect over, and Base's relational plane is
`hanzoai/dbx` — which `orm/query` re-exports as identity aliases, so Base is
already on orm's SQL surface under another import path. The one thing `engine`
has that Base needs, `?` to `$N`, dbx's `PgsqlBuilder` already does. What was
missing was the spelling of the four functions, and none of it was exported.

Portable spellings that replaced an engine-specific one outright, with no
dialect involved: the hourly log bucket is `substr(created, 1, 13) || ':00:00'`
rather than a calendar function, because an instant is stored as text in a fixed
layout and an index can be built on the substring; equality is
`IS NOT DISTINCT FROM` rather than SQLite's `IS`; every outer join states a
condition, because only some engines let one be omitted; an `int64` column is
`BIGINT`, since `INTEGER` is 32 bits outside SQLite.

Comparing a JSON sub-path to a **number** works on both engines. A value read
out of a JSON document is text wherever the engine types its columns, so the
filter layer reads it back as a number where it is compared to one —
`dialect.Number`, a guarded cast on Postgres, so a value that is not a numeral
fails to match rather than raising. That is the answer SQLite already gives for
the same data, and an engine difference should not become an error.

Three spellings are the dialect's, all for the same reason: the schema is
generated through the dialect, so a column is a real `boolean`, `numeric` or
`jsonb`, and comparing it needs the engine's own words rather than SQLite's.
`Like` (`LIKE`/`ILIKE` — only SQLite's LIKE folds case, and the nocase collation
is not the fix, since PG17 refuses it for LIKE outright), `Bool` (`1` / `'true'`,
quoted so one literal serves both a boolean column and a JSON reading), and
`Number`. The cast is applied only where a value came out of JSON, marked at the
two extraction sites, so a real numeric column and the `geoDistance` result are
left alone.

What still differs is the question the filter language does not answer: a JSON
**number** compared to a **text** literal. `meta.n = "3"` matches on Postgres,
where the reading is text, and does not on SQLite, where `json_extract` hands
back an integer. Write `meta.n = 3` for a number. Postgres also refuses `~`
against a numeric column, which SQLite answers by stringifying the value.

Also SQLite-only by design, and not on the sql path: the per-tenant `store/`
databases, the WAL/PITR replication in `network/`, and `hack/pitr-restore.go`.
The filter language's `strftime()` is refused on an engine that has no such
function rather than approximated — its format string and its modifiers are
SQLite's, down to the spelling of a month.

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

## Base hosts no identity, so there is no embedded IAM

This section used to describe an in-process OIDC provider reached by setting
`IAM_MODE=embedded`: a login form at `/v1/iam/oauth/authorize`, an `_iam_users`
collection holding bcrypt passwords, an `iam-user` CLI, `EMBEDDED_IAM_ROOT_EMAIL`
to seed the first account, a `platformEmbeddedAuth` middleware validating tokens
against a local signing key at `${DataDir}/iam.key`.

None of it exists. `IAM_MODE`, `iam_embedded.go`, `_iam_users`,
`EMBEDDED_IAM_ROOT_EMAIL`, `IAM_USER_PASSWORD`, the `iam-user` command and
`platformEmbeddedAuth` are absent from the tree — not one of them is findable.
It was worse than merely out of date: it documented Base storing
passwords and running a login flow, which is the one thing this repo is not
allowed to do and which `Register()` refuses at boot in as many words.

What is true is the section above. `IAM_ENDPOINT` is required, `/v1/iam/*`
proxies to it, and where IAM runs — a separate service or `iam.Embed()` in a
fused daemon — changes the address rather than the contract.

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
control computing real colours and radii, the overlays opening, the confirm
dialog measuring the width `DialogContent maxW` asks for, and — `fit` — that the
nav is a 14rem column at 1280, a full-width strip at 390, and a strip with no
brand and no account inside a frame.

Measure the NAV, not the document: a 14rem column on a 390px viewport leaves
166px and the table runs off the side of it, but `.shell__main` owns that
overflow and scrolls it, so `document.scrollWidth` reports nothing wrong and the
defect walks straight through a check written against the page. Measure the
dialog
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

### One session predicate, and one renewal

Every guarded route asks the same question through `~/lib/guard`:
`requireSession` for the admin at large, `requireSuperuser` for `/settings`,
which is stated on the layout and inherited rather than restated per page. The
question itself is `api.session()` — is there a bearer the server will still
take, renewing one that has run down before answering. Route guards used to
write their own `beforeLoad`, and most of them asked whether a token STRING
existed, which an expired session satisfies.

Renewal is the refresh grant (`iam.refreshAccessToken`), taken on demand at the
moment a session is needed rather than on a timer: a timer fires in every open
tab at the same instant, and it has to be re-armed against sleep, throttling and
clock drift. `request()` renews before sending rather than after being refused,
so the 401 recovery on the record form is the backstop it was built to be
instead of the normal path.

**Exactly one refresh may leave this browser.** IAM rotates the refresh token on
every use and revokes the whole family the first time it sees one twice, with no
grace window (`internal/oidc/refresh.go`) — so two tabs renewing at once do not
race for a token, they sign each other out. `navigator.locks` is held per origin
and the re-check inside it makes the loser a no-op, because by then the winner
has written the fresh session to the storage both tabs share. That sharing is
also why the SDK's `storage` is `localStorage`: with `sessionStorage` only the
tab that signed in holds a refresh token, and the recovery this admin offers
signs in on a NEW tab.

Renewal only reaches as far as the refresh token lives, and IAM's
`refreshTTL` falls back to the ACCESS lifetime when an application declares no
`refreshExpireInHours`. An app left that way renews fine for a user who keeps
working and not at all for one who steps away past the hour, so the `hanzo-base`
application should declare a refresh lifetime that outlives its access lifetime.

### A form never sends back what the server would not send it

`Settings.MarshalJSON` and `Collection.MarshalJSON` blank every secret and
`omitempty` drops the key, so `smtp.password`, `s3.secret` and each OAuth2
provider's `clientSecret` are ABSENT from a GET. A form that round-trips one is
not re-sending a placeholder, it is sending the empty box it was drawn with —
and `PATCH` merges onto the stored settings, so that erases the secret. An empty
box means "leave it": the field is omitted, and the label says so.

The same asymmetry runs the other way. Go drops a JSON key it has no field for
without a word (`SetIfFieldExists` for records, `BindBody` for everything else —
`DisallowUnknownFields` appears nowhere), so a control that submits a field no
struct receives reports success and does nothing. Check a form's field names
against the struct that receives them, not against what the page used to do.

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
