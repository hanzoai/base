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
| `graph/` | a Base's records as typed GraphQL: the schema rendered from its collections, every field answered by a request on its own router as the caller (see "The graph") |
| `modules/` | named collection sets an org installs into its Base (`POST /v1/modules/{name}`, `core.GrowCollections`); `modules/crm` is the CRM (`crm.json`), blank-imported by the binary |
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

**The fixture is migrated.** Every test app is a copy of `tests/data`, and a copy
that lacks a migration runs it before its test begins. Twelve missing migrations
cost each test app about four seconds, which is what pushed `core` and `apis` past
the runner's ten minutes (26 s and 23 s once the fixture held them). A new
migration is added to the fixture with `go run ./tests/fixture`;
`tests.TestTheFixtureIsMigrated` fails, naming it, until it is.

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
`authz.Claims.Sudo()` holds (a person whose own org — the first entry of
`orgs` — is IAM's `admin` org; a membership of `admin` is not it), otherwise
`users`. Never read authority from the `owner` claim, which IAM sets to the org
of the application a token was minted through. With `plugins/org` registered,
only IAM tokens count. Without it, a `users` token Base signed earlier still
verifies, but nothing issues a first one. Superuser tokens Base signed stopped
verifying at `1789100100_rotate_superuser_tokens.go`, and `auth-refresh` renews
no superuser token IAM did not issue.

**An org Base under a master key is never plaintext.** `plugins/org` reads the
key from KMS beneath `IAMOrg` in `Register`, refuses to start when it cannot
tell whether one exists or SQLCipher is not linked, and its keyed connector
refuses a plaintext file.

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

**An org admin changes its own Base's schema, and nothing wider.**
`RequireSchemaAuth` admits a superuser or an org admin (`IsOrgAdmin`: the
credential administers the org the request acts in); `rotate` stays a
superuser's. An org admin reaches ordinary base collections only
(`SchemaManageable`: not auth, not view, not system, no `_` name), judged as each
is and as the request would leave it, never `deleteMissing`, and every change is
logged. Any member reads the org's own schema (`RequireSchemaReadAuth`; Base's
own tables are hidden from one), because a client draws records by it. The CRM
module installs through the same door (`POST /v1/modules/crm`).

**Rules read the caller's org authority.** `@request.auth.admin` and
`@request.auth.role` (owner, admin or member) come off the verified token onto
the unsaved record IAM is mirrored into, and an unsaved auth record's own fields
are read off the record rather than joined against a row that does not exist
(`processRequestAuthField`); before that `@request.auth.org_id` matched nothing.

**A Base opened for one org answers that org.** A host that pools Bases sets
`StoreKeyTenant`; a token acting in any other org is refused there.

**Composite values are one JSON column.** currency (`amountMicros`,
`currencyCode`), emails, phones, links, address, fullName and actor are
`CompositeField`s: NULL when empty, parts reached by name in filters, sorts and
aggregates (`emails.primaryEmail`), a path naming no part refused. Emails,
phones and links normalize what is typed, and their primary part can be unique
(a validation query: SQLite admits one writer, PostgreSQL could race two). Every
field embeds `FieldMeta` (label, description, icon, position); a select carries
`options` (label, colour from `TagColors`, position) for values of `values` only;
a base collection carries `label`, `plural`, `icon`, `description`, `history`.
Editor HTML is sanitized before it is written (bluemonday UGC).

**The writer is stated, never sent.** An actor field is stamped from
`core.ActorFrom(ctx)`: record CRUD states the person (`apis.WithRequestActor`), a
host states a program (`core.WithActor`), and nothing in a body sets one.

**History is written in the record's transaction.** A base collection with
`history` writes a `_history` row per create, update and delete (who, when,
changed fields from and to), merging one writer's edits within ten minutes;
`GET /v1/collections/{c}/records/{id}/history` answers under the record's view
rule, without the changes of a field hidden or removed since (a superuser reads
the row whole).

**A where filter is the filter language, written as data.** `core.CompileWhere`
turns `{"and"|"or"|"not"|field, op, value}` into Base's filter with bound
parameters: per-type operators, relative dates in a time zone, `@me`,
back-relations (`list_members_via_person.list` is "in list") and
`inSegment`/`notInSegment` over the `segments` collection. `?where=` on the
records list and `GET /v1/collections/{c}/aggregate?ops=&group=` (count, sums,
averages, empties, uniques, grouped lanes) both run under the list rule. An
aggregate reads columns directly, so for a caller who is not a superuser it
refuses a hidden field and an auth collection's email (`core.EmailGated`, the
rule the filter resolver applies by ANDing `emailVisibility`).

**Grow never drops.** `core.GrowCollections` creates what is missing and adds
fields, select values and indexes by name, keeping every field, rule, option and
index an org changed; it is how modules and a host's imports reach a Base. A
document may say which earlier rules its own replace (`replaces`): a collection
still holding one of those exactly takes the new rule, and a rule an org wrote is
kept. The `_views*` system collections are retired (`1790000200`); saved views
are the CRM's ordinary `views` collection, the org's (`visibility` org or empty)
and a person's own (`private`, which only its maker lists, reads, changes or
deletes, an org admin included). IAM preferences hold only which view a person
opens first.

**Paths are `/v1`.** No `/api/` segment anywhere. `BASE_API_PREFIX` moves the
data plane and nothing else: `/v1/iam` and `/healthz` stay put. The admin SPA
writes `/v1` into `ui-react/src/lib/api.ts`, so it does not work under another
prefix. Outbound calls follow the same rule: the Tasks client posts to
`/v1/tasks/namespaces/default`. `route_prefix_test.go` fails on a `/api/` string
literal anywhere in the module or a quoted `/api` path in `ui-react` or `sdk`.

## The graph

`graph.New(app, router)` is the Base's records as GraphQL, executed by zip's
engine: a host mounts `g.Schema` and `g.Resolve` with `zip.MountGraphFor` (cloud
serves it at `/v1/base/graphql` and composes it into `/v1/graphql` as `base`).
The package has no transport of its own and no second authorization.

- **The schema is the Base's own**, rendered from its collections and rebuilt
  when one changes (keyed by each collection's id and `updated`). Base and view
  collections are types; a collection Base keeps for itself (`_*`, system) and an
  auth collection are not, and a relation to one reads as the id it holds.
  Naming: `people` → `BasePerson`, roots `person(id)`, `people(...)`,
  `peopleGroupBy(field, filter)`, `createPerson`, `updatePerson`,
  `upsertPerson(data, on)`, `deletePerson`; a name that would collide is left out
  (`model.skipped`). Field names are Base's, verbatim. Every type starts with
  `Base`, so a host composes it without renaming.
- **Every field is a request on the Base's router**, made with the caller's
  headers (`zip.Headers`): the records list (`?where=`, `sort`, `expand`), view,
  create, update, delete, `/aggregate` and `/history`. Rules, tenancy, rate limits
  and expand's view checks are therefore the REST path's; `graph_test` holds the
  graph's refusals to REST's own messages.
- **A filter is a where clause.** `BasePersonFilter` is `and`/`or`/`not`,
  `inSegment`/`notInSegment`, each field's own filter and each back-relation
  (`list_members_via_person`), and every operator becomes one leaf of
  `core.CompileWhere`; `@me`, relative dates, `tz` and `weekStart` mean what they
  mean there. `orderBy` is a list of one-field objects (a composite part, or one
  relation deep), with the id appended so the order is total.
- **A cursor is a position.** Base pages by page and perPage, so a window of n
  from an offset is read in pages of n+1, one page or the two it spans; inserts
  before a cursor shift what follows it. `totalCount` is counted only when
  selected, and `aggregate` (on any connection) and `GroupBy` are the aggregate
  route, over the filter and never the page.
- **Bounds.** A page holds at most 100; a selection reaches at most four
  relations, back-relations or histories deep, refused before any request; one
  root field makes at most 50 requests; zip refuses a request past 10,000
  resolutions whole.
- **No subscriptions.** Realtime is the SSE stream, under the same rules.

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

- A composite's unique primary part (an email, a phone, a URL) is held by a SELECT before the write (`checkUniquePart`), not by an index: the part is inside a JSON column and an expression index is spelled per engine. SQLite admits one writer, so it holds there; on a Base placed on PostgreSQL two writers of one address at once can both pass. Decided to keep until a placed Base takes concurrent writes of one key; the fix is a unique expression index from the dialect.
- `migrations/1780500000_connect_messaging_calendar.go` gives `connectedAccount` encrypted token fields, so an OAuth token can sit in a record. Secrets belong in KMS; the collection is retired by another change, and nothing reads it.
- `plugins/jsvm/internal/types/generated/types.d.ts` has not been regenerated for the composite field types: the generator names its aliases at random, so a run rewrites some 36,000 lines.

- `apis/installer.go` tells a Base without IAM to run `superuser upsert`, a command that no longer exists.
- `--publicDir` and `--indexFallback` do nothing: `apis.Serve` registers `GET /{path...}` before the binary's static route checks for one.
- An encrypted org Base needs SQLCipher linked (`CGO_ENABLED=1`, `-tags libsqlite3` and the flags in hanzoai/sqlite's README). The image builds `CGO_ENABLED=0`, whose pure-Go codec gives each handle a private copy of a file, so a Base with a master key refuses to start there.
- `TASKS_EMBED` fails and `NewBaseApp` drops the error when the default socket path is relative (`zap.Network` reads `base_/data/tasks.sock` as a TCP address) and on a data directory's first start (the engine binds before the directory exists).
- The start banner prints `/v1/` and a Dashboard address whatever `BASE_API_PREFIX` and `BASE_ENABLE_ADMIN_UI` say.
- No GitHub release after `v0.36.7-hanzo.1` carries binaries, so `base update` fails, and `ghcr.io/hanzoai/base` has no image for `v1.5.92` to `v1.5.96`.
- `go install github.com/hanzoai/base/examples/base@<version>` fails on the `replace` in `go.mod`.
- In `cmd/cli`, `K8sContext()` returns literals such as `gke_mainnet` that name no cluster, and `EnvURLs` builds `example.com` hosts and has no caller.

## Writing in this repo

Paths are `/v1`. Hanzo IAM is the only auth. Zen models are Hanzo's own, and no
upstream model or product is named. Docs are plain and short, and every command
in them has been run.
