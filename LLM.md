# LLM.md: hanzoai/base

A map of this repository for coding agents. How to use Base is in `README.md`
and `docs/`. This file says where things are, how to build and test, and what
must stay true. `CLAUDE.md` is a symlink to it.

## What Base is

One Go module, `github.com/hanzoai/base`, whose binary is `examples/base`:
collections and records over a `/v1` REST API, realtime, files, collection
rules and JavaScript hooks, stored in SQLite through `github.com/hanzoai/sqlite`.
Identity is Hanzo IAM and nothing else. With `IAM_ENDPOINT` set, every org gets
its own Base at `<data dir>/orgs/<org>/data.db`.

## Where things are

| Path | What it holds |
|---|---|
| `base.go` | `base.New`, the root flags (`--dir`, `--dev`, `--encryptionEnv`, `--queryTimeout`, `--zap`, `--mdns`, `--no-mdns`), ZAP registration |
| `examples/base/main.go` | the binary: jsvm, migratecmd, waitlist (always on), ghupdate, org (when `IAM_ENDPOINT` or `IAM_URL` is set), bootnode, calendar, cloudsql |
| `cmd/` | `serve.go`, and the `cli` HTTP client in `cli.go` and `cmd/cli/` |
| `apis/` | routes and middleware. `base.go` mounts the groups under `BASE_API_PREFIX`; `serve.go` opens the listeners and serves the admin UI; `middlewares.go` turns a credential into an identity and an org; `credential.go` reads credentials; `record_crud.go` applies rules; also `rest.go`, `realtime*.go`, `functions.go`, `private.go`, `public.go` |
| `core/` | the app, collections, records, the rule resolver (`record_field_resolver*.go`), migrations, settings, Tasks wiring (`base.go`), the replication hook (`base_network.go`) |
| `migrations/` | system migrations, in Go |
| `plugins/jsvm` | `.base.js` hooks and JS migrations on goja; `internal/types` holds the generated `types.d.ts` |
| `plugins/org` | IAM client and `/v1/iam` proxy, per-org Bases, KMS client, `/v1/bases`, `/v1/idv` proxy, publishable keys |
| `plugins/zap` | the ZAP listener, which serves the HTTP handler |
| `plugins/extruntime`, `plugins/gojavm` | the runtime interface and the goja runtime behind `/v1/functions` |
| `plugins/{migratecmd,ghupdate,waitlist,bootnode,calendar,cloudsql,commerce}` | what their names say |
| `plugins/{ha,replicate,tasks,scheduler,vault,pyvm,v8vm,wasmvm,starkvm,extbench}` | plugins the binary does not link |
| `network/` | quasar replication (`BASE_NETWORK=quasar`) and its `s3://` archive |
| `store/` | at-rest encryption for per-base SQLite: keyrings and encrypted replicas |
| `crdt/` | CRDT documents, privacy backends, chain anchors |
| `iam/` | IAM client types, aliased for the plugins |
| `tools/` | router, hook chains, the filter language (`search`), filesystem, cron over the Tasks client (`tasks`), identity headers (`claims`), security |
| `ui-react/` | the admin SPA; `dist/` is committed and embedded |
| `tests/` | the test app and API scenarios; `tests/engine` runs against PostgreSQL |
| `sdk/go`, `sdk/private` | the Go client interface and the private-store TypeScript client. The JS and Dart clients live in hanzo-js/base and hanzo-dart/base |
| `hack/` | `pitr-restore.go` and a compose smoke script |
| `docs/` | user docs (`versions`, `rules`, `hooks`, `config`, `listeners`, `threat-model`) and design notes |

## Build and test

```sh
cd examples/base && CGO_ENABLED=0 go build && ./base serve --http 127.0.0.1:8090
GOWORK=off CGO_ENABLED=0 GOEXPERIMENT=jsonv2 go vet ./...
GOWORK=off CGO_ENABLED=0 GOEXPERIMENT=jsonv2 go test -count=1 ./...
golangci-lint run -c ./golangci.yml ./...
pnpm --dir ui-react install && pnpm --dir ui-react build && pnpm --dir ui-react smoke
```

CI (`hanzo.yml`) runs the Go gates with those three variables, runs
`tests/engine` against PostgreSQL 18, and keeps the skips in
`network/attack_vectors_test.go` apart from the passes. Do not use `make build`
or `make ui`: they run pnpm in `ui/`, which does not exist.

**macOS.** The whole suite passes with the test command above, including
`TestTasksEmbed`, `TestOrgBaseIsEncryptedAtRest`, `TestOAuth2ConfigValidate` and
`TestOAuth2ProviderConfigValidate`. The first two failed on a Mac before
`hanzoai/sqlite` v0.5.10, whose pure-Go codec would decrypt only into RAM. It now
prefers RAM (`HANZO_SQLITE_RAMFS_DIR` when it names a `tmpfs`, then `/dev/shm`)
and falls back to the OS temp directory.

## What must stay true

**One auth, IAM's.** Base has no passwords, sign-in routes, OTP or OAuth2 of its
own. `resolveJWKSToken` (`apis/middlewares.go`) verifies a token against IAM's
JWKS and mirrors it into an unsaved record: `_superusers` when
`authz.Claims.Sudo()` holds (membership of IAM's `admin` org), otherwise
`users`. Never read authority from the `owner` claim, which IAM sets to the org
of the application a token was minted through. With `plugins/org` registered,
only IAM tokens count. Without it, tokens Base signed earlier still verify and
`auth-refresh` renews them, but nothing issues a first one.

**The org is the token's.** `orgOf` takes it from the token's memberships.
`X-Org-Id` only selects among them; anything else is `403`, never `404`, since a
404 answers a question about data. `tools/claims` strips `X-Org-Id`,
`X-User-Id` and `X-User-Email` on the way in. `loadAuthToken` points
`RequestEvent.App` at the org's Base before any handler runs, so handlers know
nothing about tenancy.

**Rules are enforced in Go, on every engine.** `nil` is superuser only and `""`
is public. A list rule is ANDed into the query before paging and counting. An
update is checked against the record as it would be after the write
(`shadowRow`). Code that reads records outside the record API checks with
`CanAccessRecord`.

**A publishable key reaches only public routes.** `pk-` keys ship in web pages.
`plugins/org/publishable.go` refuses them on any route not wrapped in
`apis.Public(...)`, and each product marks its own public routes where it
registers them.

**One credential reader.** `apis.Credential` is the only code that reads a token
or key off a request.

**A hook follows its event.** `jsvm` states record and model hooks on every Base
through `core.AppBindings`; `routerAdd` and `cronAdd` register once. Each
handler is compiled from its own source text and cannot see file-level
variables.

**Realtime opens with a grant.** `POST /v1/realtime/token` mints a grant that
`GET /v1/realtime?token=` spends once within 30 seconds
(`apis/realtime_grant.go`). An IAM token never travels in a query string.

**Functions are records.** `_functions` rows run in-process on goja
(`apis/functions.go`). The host binds `list`, `one` and `start`, capped by
`functionMaxRows` (500) and `functionMaxRuns` (8). Nothing installs
`StoreKeySandboxes`, so `start` refuses.

**One SQLite driver.** Import `github.com/hanzoai/sqlite` only. A direct
`modernc.org/sqlite` import registers the `sqlite` driver twice under cgo and
panics at init.

**Engine differences go through `app.Dialect()`** (`hanzoai/orm/dialect`),
never through a branch on the engine's name. `Config.DataDSN` opens
PostgreSQL; empty means embedded SQLite.

**Listeners bind where they are told.** ZAP takes the HTTP host unless given an
address, mDNS is opt-in and never on loopback, `BASE_LISTEN_P2P` binds its host
exactly, and the Tasks and direct KMS clients dial without listening.
`docs/listeners.md` is the list; change it with the code.

**Paths are `/v1`.** No `/api/` segment anywhere. `BASE_API_PREFIX` moves the
data plane and nothing else: `/v1/iam` and `/healthz` stay put. The admin SPA
writes `/v1` into `ui-react/src/lib/api.ts`, so it does not work under another
prefix.

## Admin UI

`ui-react`: React 19, TanStack Router, `@hanzo/ui` components on `@hanzo/gui`,
no Tailwind. Served at `/` only when `BASE_ENABLE_ADMIN_UI=1`; who may frame it
is `BASE_FRAME_ANCESTORS` (default `'self'`). It signs in with IAM PKCE
(`VITE_IAM_URL`, default `https://hanzo.id`; client `hanzo-base`).

- Rebuild `dist/` in the same commit as any `src/` change, and on Linux: a macOS
  build emits a larger bundle from the same source.
- A green build proves little. `@hanzo/gui` drops props it does not know without
  an error; `pnpm smoke` checks what renders.
- Only one tab may refresh the session at a time (`navigator.locks`). IAM
  rotates refresh tokens and revokes the family when one is reused.
- Reads blank secrets and `PATCH` merges, so a form omits a secret field left
  empty rather than sending it.

## Known problems

- `apis/installer.go` tells a Base without IAM to run `superuser upsert`, a command that no longer exists.
- `--publicDir` and `--indexFallback` do nothing: `apis.Serve` registers `GET /{path...}` before the binary's static route checks for one.
- A fresh Base's `users.createRule` is `""`, so anonymous callers can create rows.
- `examples/base` sets no `IAMOrg`, so `plugins/org` never reads the master key and org Bases open unencrypted.
- `TASKS_EMBED` fails and `NewBaseApp` drops the error when the default socket path is relative (`zap.Network` reads `base_/data/tasks.sock` as a TCP address) and on a data directory's first start (the engine binds before the directory exists).
- The start banner prints `/v1/` and a Dashboard address whatever `BASE_API_PREFIX` and `BASE_ENABLE_ADMIN_UI` say.
- No GitHub release after `v0.36.7-hanzo.1` carries binaries, so `base update` fails, and `ghcr.io/hanzoai/base` has no image for `v1.5.92` to `v1.5.96`.
- `go install github.com/hanzoai/base/examples/base@<version>` fails on the `replace` in `go.mod`.
- In `cmd/cli`, `K8sContext()` returns literals such as `gke_mainnet` that name no cluster, and `EnvURLs` builds `example.com` hosts and has no caller.

## Writing in this repo

Paths are `/v1`. Hanzo IAM is the only auth. Zen models are Hanzo's own, and no
upstream model or product is named. Docs are plain and short, and every command
in them has been run.
