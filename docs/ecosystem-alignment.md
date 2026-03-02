# Hanzo Base: Ecosystem Alignment Guide

## Question

How to align Hanzo Base (hanzoai/base, PocketBase fork) with the conventions
used across hanzoai/orm, hanzoai/commerce, hanzoai/dbx, hanzoai/sqlite,
hanzoai/gateway, and luxfi/cache -- so Base feels native to the Hanzo
ecosystem rather than a PocketBase derivative.

## Method

Source-level analysis of 7 repos. Every claim cites file:line. No speculation.

Repos analyzed:
- hanzoai/orm (~/work/hanzo/orm/) -- 31 files, Go ORM with generics
- hanzoai/commerce (~/work/hanzo/commerce/) -- 100+ model packages, Go service
- hanzoai/base (~/work/hanzo/base/) -- PocketBase fork, Go
- hanzoai/dbx (~/work/hanzo/dbx/) -- Low-level DB abstraction, Go
- hanzoai/sqlite (~/work/hanzo/sqlite/) -- Encrypted SQLite driver, Go
- hanzoai/gateway (~/work/hanzo/gateway/) -- KrakenD-based API gateway, Go
- luxfi/cache (~/work/lux/cache/) -- Generic cache interfaces, Go

---

## Findings

### A. Naming Conventions

#### A1. Entity Timestamp Fields

CONFLICT: Base uses `created`/`updated`. Everything else uses `createdAt`/`updatedAt`.

| Repo | Go Field | JSON Tag | DB Column | Source |
|------|----------|----------|-----------|--------|
| hanzoai/orm | `CreatedAt time.Time` | `json:"createdAt"` | (JSON blob) | orm/model.go:33 |
| hanzoai/orm | `UpdatedAt time.Time` | `json:"updatedAt"` | (JSON blob) | orm/model.go:34 |
| hanzoai/commerce | `CreatedAt time.Time` | `json:"createdAt"` | (JSON blob) | commerce/models/mixin/model.go:103 |
| hanzoai/commerce | `UpdatedAt time.Time` | `json:"updatedAt"` | (JSON blob) | commerce/models/mixin/model.go:104 |
| hanzoai/base | `Created types.DateTime` | `json:"created"` | `created` | base/core/collection_model.go:372 |
| hanzoai/base | `Updated types.DateTime` | `json:"updated"` | `updated` | base/core/collection_model.go:373 |

The ORM/Commerce pattern (`createdAt`/`updatedAt`) is the ecosystem standard.
Base's `created`/`updated` is PocketBase heritage.

#### A2. Soft Delete

MISSING: Base has no soft delete.

| Repo | Has Soft Delete | Field | Source |
|------|----------------|-------|--------|
| hanzoai/orm | Yes | `Deleted bool` / `json:"deleted"` | orm/model.go:35 |
| hanzoai/commerce | Yes (inherits ORM) | `Deleted bool` / `json:"deleted"` | commerce/models/mixin/model.go:104 |
| hanzoai/base | No | N/A | base/core/db_model.go:16-22 |

ORM's Delete() does `UPDATE SET deleted = 1` (orm/db/sqlite.go:275-278).
Queries filter `deleted = 0` by default (orm/db/sqlite.go:221,860).
Base's Delete removes the row entirely.

#### A3. JSON Serialization Convention

All repos use camelCase for JSON tags. Consistent.

| Repo | Example | Source |
|------|---------|--------|
| ORM | `json:"createdAt"`, `json:"userId"` | orm/model.go:33 |
| Commerce | `json:"storeId"`, `json:"paymentStatus"` | commerce/models/order/order.go:88-100 |
| Base | `json:"deleteRule"`, `json:"listRule"` | base/core/collection_model.go:360-361 |
| dbx | DB columns: snake_case via DefaultFieldMapFunc | dbx/struct.go:69-71 |

Exception: Base record auto-fields use lowercase without `At` suffix (`created` not `createdAt`).

#### A4. Collection/Table/Kind Naming

| Repo | Convention | Example | Source |
|------|-----------|---------|--------|
| ORM | singular kebab-case | `"order"`, `"payment-intent"` | orm/model.go:20, commerce/models/order/order.go:40 |
| Commerce | singular kebab-case (same as ORM) | `"order"` | commerce/models/order/order.go:69 |
| Base | user-defined, `_` prefix for system | `"_superusers"`, `"_externalAuths"` | base/core/record_model_superusers.go:12 |
| dbx | snake_case tables | `DefaultFieldMapFunc`: CamelCase to snake_case | dbx/struct.go:69 |

ORM/Commerce: singular kebab-case kind strings.
Base: arbitrary user-defined collection names.
dbx: snake_case for SQL column mapping.

---

### B. API Conventions

#### B1. Route Prefixes

| Repo | External Prefix | Internal Prefix | Source |
|------|----------------|-----------------|--------|
| Commerce | `/v1/commerce/*` | `/api/v1/*` | commerce/commerce.go:694-710 (rewrite), :727 (group) |
| Base | `/api/*` (configurable) | `/api/*` | base/apis/base.go:38-42 |
| Gateway | host+path routing | N/A | gateway/router_engine.go:29-33 |

Commerce canonical external path: `/v1/<service>/*`.
Base default: `/api/*`.
CLAUDE.md mandate: `/v1/` not `/api/`.

#### B2. Error Format

| Repo | Shape | Source |
|------|-------|--------|
| Commerce | `{"error": {"type": "api-error", "message": "..."}}` | commerce/middleware/error.go:22-28 |
| Base | `{"status": int, "message": "...", "data": {...}}` | base/tools/router/error.go:36-42 |

Base's format carries more information (status code, structured data map).

#### B3. Pagination Response

| Repo | Shape | Source |
|------|-------|--------|
| Base | `{"items": [...], "page": N, "perPage": N, "totalItems": N, "totalPages": N}` | base/tools/search/provider.go:55-61 |
| Commerce | Ad hoc per endpoint | No standard envelope |
| ORM | `[]*T, error` (no HTTP) | orm/query.go:93 |

Base has the only standardized pagination format. It should be the ecosystem standard.

#### B4. List Query Parameters

Base: `page`, `perPage`, `sort`, `filter`, `skipTotal` (base/tools/search/provider.go:47-52).
No other repo defines standard list query params.

#### B5. Auth Header

All repos: `Authorization: Bearer <JWT>`. Consistent.

---

### C. Database Conventions

#### C1. Storage Model

ARCHITECTURALLY INCOMPATIBLE approaches:

| Repo | Model | Source |
|------|-------|--------|
| ORM | JSON blobs in `_entities` table | orm/db/sqlite.go:127-136 (single table, JSON data column) |
| Base | SQL table per collection with typed columns | base/core/ (collection schema drives DDL) |
| dbx | Direct SQL with struct mapping | dbx/model_query.go:70 |

ORM stores all entities as `{id, kind, parent_id, data JSON}` in one table.
Queries use `json_extract(data, '$.field')` (orm/db/sqlite.go:879).

Base creates a real SQL table per collection with proper columns.
Queries use direct column access.

Base's approach is strictly superior for:
- Index efficiency (real columns vs JSON extract)
- Schema enforcement (column types vs JSON validation)
- Query planning (SQLite optimizer understands columns, not JSON paths)

Recommendation: Keep Base's SQL-per-collection. Build an ORM adapter that maps to it.

#### C2. Multi-Tenant Isolation

| Repo | Method | Source |
|------|--------|--------|
| ORM | Namespace on key + TenantID in config | orm/db/sqlite.go:26 (TenantID), orm/model.go:57 (Namespace) |
| Commerce | Per-org SQLite database files | commerce/commerce.go:591 (`app.DB.Org("system")`) |
| Base | Single DB per instance | No multi-tenancy |
| hanzoai/sqlite | Per-principal CEK via HKDF-SHA256 | sqlite/cek.go:36-52 |

Commerce creates separate SQLite files per org.
hanzoai/sqlite derives per-principal encryption keys.
Base has no awareness of orgs or tenants.

#### C3. Filter Syntax

| Layer | Syntax | Example | Source |
|-------|--------|---------|--------|
| ORM (application) | `Filter("Field=", value)` | `Query().Filter("Name=", "foo")` | orm/db/query.go:26-34 |
| Base (HTTP API) | String DSL | `filter=(field='value' && field2>100)` | base/tools/search/provider.go:51 |
| dbx (SQL) | HashExp | `dbx.HashExp{"field": value}` | dbx/expression.go |

Three syntaxes for three layers. Not conflicting -- they serve different purposes.

---

### D. Hook/Middleware Systems

#### D1. Three Hook Architectures

**ORM: Interface-based (compile-time)**
```go
// orm/hooks.go:7-36
type BeforeCreator interface { BeforeCreate() error }
type AfterCreator interface { AfterCreate() error }
type BeforeUpdater[T any] interface { BeforeUpdate(prev *T) error }
type AfterUpdater[T any] interface { AfterUpdate(prev *T) error }
type BeforeDeleter interface { BeforeDelete() error }
type AfterDeleter interface { AfterDelete() error }
```

**Commerce: Event-based per-kind (runtime)**
```go
// commerce/hooks/hooks.go:270-283
app.Hooks.OnModelCreate("order").Bind(&hooks.Handler{
    ID: "validateInventory",
    Func: func(e *hooks.ModelEvent) error { return e.Next() },
})
```

**Base: Granular event-based with phases (runtime)**
```go
// base/core/record_model.go:56-142
app.OnModelCreate().Bind(...)
app.OnModelCreateExecute().Bind(...)
app.OnModelAfterCreateSuccess().Bind(...)
app.OnModelAfterCreateError().Bind(...)
```

ORM hooks: 6 interfaces, simple, type-safe.
Commerce hooks: Registry with per-kind maps, Handler struct with ID/Priority/Func.
Base hooks: Most granular (pre/execute/success/error phases), but most complex.

Recommendation: ORM interfaces for model code (type-safe). Base hooks for cross-cutting
concerns (audit, analytics, inter-service events).

---

### E. Realtime/Events

#### E1. SSE Event Naming

CONFLICT: Server renamed, SDK not.

| Component | Event Name | Source |
|-----------|-----------|--------|
| Base server (Go) | `HZ_CONNECT` | base/apis/realtime.go:83 |
| Base JS SDK | `PB_CONNECT` | base/sdk/base-js/src/core/realtime.ts:163 |

Server was updated. SDK was not.

#### E2. Inter-Service Events

| Repo | Transport | Source |
|------|-----------|--------|
| Commerce | NATS/JetStream via events.Publisher | commerce/commerce.go:647-651 |
| Base | SSE (client-facing) only | base/apis/realtime.go |

Base lacks inter-service event publishing. Commerce uses NATS.

---

### F. Auth

#### F1. Auth Systems

| Repo | System | Source |
|------|--------|--------|
| Base | Built-in auth collections (type="auth") | base/core/collection_model.go:25 |
| Base | OIDC for superusers only | base/apis/superuser_auth_oidc.go:53-56 |
| Commerce | Hanzo IAM via JWKS middleware | commerce/commerce.go:667-679 |
| Gateway | IAM JWT validation | gateway/auth_middleware.go |

Base has TWO auth systems running in parallel:
1. Built-in auth collections (PocketBase: email/password, OAuth2, OTP per collection)
2. OIDC for `_superusers` collection (partial Hanzo IAM integration)

Ecosystem standard: Hanzo IAM exclusively (Commerce, Gateway both use it).

---

## Analysis

### What Already Aligns

1. **JSON casing**: camelCase everywhere (consistent)
2. **Auth header**: `Authorization: Bearer` (consistent)
3. **Admin UI**: Disabled by default via `BASE_ENABLE_ADMIN_UI` (correct)
4. **Health endpoint**: `/healthz` (correct)
5. **SSE server event**: `HZ_CONNECT` (renamed from PB_CONNECT)
6. **Error format**: Base's `{status, message, data}` is more informative than Commerce's
7. **Pagination**: Base's `{items, page, perPage, totalItems, totalPages}` is the best
8. **Storage model**: Base's SQL-per-collection is superior to ORM's JSON blobs

### What Conflicts

1. **Timestamp field names**: `created`/`updated` vs `createdAt`/`updatedAt`
2. **API prefix**: `/api` vs `/v1`
3. **No soft delete**: ORM/Commerce have it, Base does not
4. **No multi-tenancy**: Commerce has per-org DBs, Base has nothing
5. **No encryption**: hanzoai/sqlite has CEK, Base does not integrate it
6. **Auth**: Base has its own auth system alongside IAM
7. **JS SDK**: Still references `PB_CONNECT`
8. **No inter-service events**: Commerce has NATS publisher, Base does not

### What Base Does Better

1. **Storage efficiency**: SQL tables with typed columns beat JSON blobs
2. **Query performance**: Column indexes vs json_extract
3. **Migration system**: Proper SQL DDL migrations
4. **Pagination**: Standardized response format with skipTotal optimization
5. **Filter DSL**: String-based filter syntax for HTTP APIs
6. **Hook granularity**: Pre/execute/success/error phases

---

## Recommendation

Align Base to the ecosystem in 5 phases. Each phase is independently deployable.
Backward compatibility via env vars in phases 1-2. Breaking changes in phase 3+.

### Phase 1: Non-Breaking Aliases

**Changes:**
- Accept both `created`/`createdAt` and `updated`/`updatedAt` in JSON input
- Output `createdAt`/`updatedAt` by default
- Env var `BASE_LEGACY_FIELD_NAMES=1` outputs `created`/`updated` for migration
- Change default `BASE_API_PREFIX` from `/api` to `/v1`
- Add `Deleted bool` to BaseModel with soft delete by default

**Files to modify:**
- `base/core/db_model.go:16-22` -- Add CreatedAt, UpdatedAt, Deleted to BaseModel
- `base/core/collection_model.go:372-373` -- Rename Created/Updated fields
- `base/core/field_autodate.go` -- Default names `createdAt`/`updatedAt` for new collections
- `base/apis/base.go:39` -- Default apiPrefix to `/v1`
- `base/tools/search/provider.go` -- Support both old and new field names in sort/filter

### Phase 2: IAM Integration

**Changes:**
- Port Commerce IAM middleware into Base as built-in
- When `BASE_IAM_ISSUER` is set, all API routes validate IAM JWTs
- JWT claims propagated: `sub` for auth identity, `owner` for org namespace
- Deprecate built-in auth collections when IAM is active (return 410 on auth endpoints)

**Files to modify:**
- New file: `base/core/iam_middleware.go` -- Port from commerce/middleware/iammiddleware/
- `base/apis/base.go` -- Wire IAM middleware when configured
- `base/apis/record_crud.go` -- Check IAM claims for authorization

### Phase 3: Multi-Tenancy + Encryption

**Changes:**
- Per-org SQLite databases: `{dataDir}/{orgId}/data.db`
- Org ID from JWT `owner` claim or `X-Hanzo-Org-Id` header
- Integrate `hanzoai/sqlite` for per-principal CEK encryption
- Master key from `BASE_MASTER_KEY` env or KMS fetch

**Files to modify:**
- `base/core/app.go` -- Add org-scoped DB routing
- New file: `base/core/tenant.go` -- Per-org database manager
- `base/core/db_connect.go` -- Use hanzoai/sqlite for encrypted opens

### Phase 4: ORM Bridge

**Changes:**
- Create `orm/db/base.go` adapter implementing `orm.DB` via Base's dbx queries
- Map ORM lifecycle interfaces to Base hook events
- Commerce models can run against Base storage

**Files to modify:**
- New file: `orm/db/base.go` -- ORM DB adapter for Base's dbx
- New file: `orm/db/base_query.go` -- Query translation (ORM filter to dbx)

### Phase 5: SDK + Cleanup

**Changes:**
- Fix JS SDK: `PB_CONNECT` to `HZ_CONNECT`
- Search-replace remaining `PB_`, `pb_`, `pocketbase`, `PocketBase` references
- Add NATS publisher for inter-service events (like Commerce)

**Files to modify:**
- `base/sdk/base-js/src/core/realtime.ts:163` -- PB_CONNECT to HZ_CONNECT
- Global search-replace across SDK and Go source

---

## Architecture Diagram (Target State)

```
                     Hanzo IAM (OIDC/JWKS)
                            |
                     JWT validation
                            |
              +-------------+-------------+
              |                           |
        Hanzo Gateway              Direct API
     (KrakenD, rate limit)         (dev mode)
              |                           |
              +-------------+-------------+
                            |
                    Hanzo Base /v1/*
                            |
         +------------------+------------------+
         |                  |                  |
    Record CRUD        Realtime SSE       Collection API
         |              (HZ_CONNECT)           |
         |                  |                  |
    +----+----+        NATS Publisher     Schema DDL
    |         |         (inter-svc)       migrations
  Hooks    Validation        |                 |
  (ORM      (ozzo)     +----+----+             |
  ifaces)               |         |             |
    |              Commerce    Other        dbx query
    |              events     services      builder
    |                                          |
    +------------------------------------------+
                            |
                   Per-Org SQLite DB
                   (hanzoai/sqlite)
                            |
                   CEK via HKDF-SHA256
                   (hanzoai/sqlite/cek)
                            |
                   WAL + S3 replication
                   (hanzoai/replicate)
```

---

## Sources

1. orm/model.go:23-36 -- ORM Model struct (CreatedAt, UpdatedAt, Deleted)
2. orm/hooks.go:7-42 -- ORM lifecycle hook interfaces
3. orm/db/sqlite.go:116-148 -- ORM SQLite schema (_entities table)
4. orm/db/sqlite.go:267-279 -- ORM soft delete (UPDATE SET deleted = 1)
5. orm/db/sqlite.go:860 -- ORM query filter (json_extract + deleted = 0)
6. orm/db/query.go:26-34 -- ParseFilterString implementation
7. orm/db/query.go:50-65 -- ToJSONFieldName (camelCase conversion)
8. orm/registry.go:52-83 -- Register[T] with kind string
9. orm/names/mapper.go:20-21 -- SnakeMapper for DB columns
10. commerce/commerce.go:694-710 -- canonicalPathHandler rewrite
11. commerce/commerce.go:727 -- Commerce route group /api/v1
12. commerce/commerce.go:591 -- Per-org SQLite (app.DB.Org)
13. commerce/commerce.go:667-679 -- IAM middleware config
14. commerce/commerce.go:647-651 -- NATS/JetStream publisher
15. commerce/models/mixin/model.go:100-104 -- BaseModel (CreatedAt/UpdatedAt)
16. commerce/models/mixin/orm_bridge.go:32-38 -- Model[T] bridges to ORM
17. commerce/models/order/order.go:40,69 -- Kind registration
18. commerce/models/order/order.go:88-100 -- JSON field naming (camelCase)
19. commerce/hooks/hooks.go:204-310 -- Hook Registry
20. commerce/middleware/error.go:22-28 -- Error format
21. base/core/db_model.go:16-22 -- BaseModel (Id only, no timestamps)
22. base/core/collection_model.go:372-373 -- `created`/`updated` fields
23. base/core/field_autodate.go:34-59 -- AutodateField (OnCreate/OnUpdate)
24. base/core/record_model.go:56-142 -- Base hook system
25. base/core/record_model_superusers.go:12 -- _superusers collection name
26. base/apis/base.go:38-42 -- BASE_API_PREFIX defaults to "/api"
27. base/apis/serve.go:81-95 -- Admin UI disabled by default, root redirect
28. base/apis/realtime.go:83 -- HZ_CONNECT event name (server)
29. base/sdk/base-js/src/core/realtime.ts:163 -- PB_CONNECT (stale, needs fix)
30. base/tools/router/error.go:36-42 -- ApiError struct
31. base/tools/search/provider.go:47-61 -- Pagination (params + Result struct)
32. base/apis/superuser_auth_oidc.go:53-56 -- OIDC config from env
33. dbx/struct.go:47-71 -- DbTag, DefaultFieldMapFunc (snake_case)
34. dbx/model_query.go:10-13 -- TableModel interface
35. sqlite/cek.go:36-52 -- Per-principal CEK derivation via HKDF
36. sqlite/sqlite.go:112-144 -- Encrypted SQLite Open()
37. gateway/router_engine.go:29-33 -- RoutesConfig YAML structure
38. gateway/auth_middleware.go -- JWT validation via JWKS
39. cache/cache.go:8-26 -- Cacher[K,V] generic interface
