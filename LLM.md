# LLM.md - Hanzo Base

## Identity

Hanzo Base is the **local-first application runtime for Lux**.

Not a fork, not a wrapper. The Web5 client runtime.

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
| functions | plugins/functions/ | Event workers (on CRDT ops, chain receipts) |
| jsvm | plugins/jsvm/ | JavaScript plugin VM |

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

Every Base-derived daemon (ats, bd, ta) uses `cmd.AddCLISubcommands(root)` to get:

| Command | Purpose | Lux CLI Equivalent |
|---------|---------|-------------------|
| `cluster init/start/stop/status/leader/replicate/failover` | Manage base-ha HA groups | `lux network start/stop/status` |
| `operator apply/status/describe/upgrade/logs` | Manage Liquidity K8s operator CRDs | `lux chain deploy` |
| `config show/set-env/set-org/init` | CLI config (~/.config/base/config.json) | `lux config` |
| `status` | Daemon health + cluster state | `lux status` |
| `self version/doctor` | Binary management | `lux self` |
| `rpc get/post/patch/delete` | Direct API passthrough | `lux rpc` |

### Network Flags

All commands accept `--mainnet/-m`, `--testnet/-t`, `--devnet/-d`, `--dev`. Exactly one may be set.
Fallback: `$LIQUIDITY_ENV` -> `$BASE_ENV` -> default `local`.

### Config File

`~/.config/base/config.json` (respects `$XDG_CONFIG_HOME`). Contains default env, per-env URLs, default org.

### Cluster (HA)

Local mode (`--dev`): spawns N `base-ha` processes with auto-filled `BASE_PEERS`.
K8s mode (`--mainnet/--testnet/--devnet`): `kubectl scale` against the correct GKE context.
Consensus: `--consensus lux` (default) or `--consensus pubsub`.

### Operator (K8s CRDs)

Wraps kubectl against `liquid.network/v1alpha1` CRDs. Context map:
- devnet: `gke_liquidity-devnet_us-central1_dev`
- testnet: `gke_liquidity-testnet_us-central1_test`
- mainnet: `gke_liquidity-mainnet_us-central1_main`

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
| API prefix | `/api` | `/v1` | CONFLICT |
| Soft delete | Hard delete only | `Deleted bool` flag | MISSING |
| Multi-tenancy | None | Per-org SQLite + CEK | MISSING |
| Auth | Built-in auth collections | Hanzo IAM (OIDC/JWKS) | DONE (EXTERNAL_AUTH_ONLY env var) |
| SSE event name | `CONNECT` | `CONNECT` | OK (server + SDK aligned) |
| Error format | `{status, message, data}` | `{status, message, data}` | OK |
| Pagination | `{items, page, perPage, totalItems, totalPages}` | Same | OK |

Migration path: 5 phases, backward-compatible aliases first.
Full details: research brief produced by scientist agent on 2026-04-10.

## External Auth Mode (2026-04-10)

Set `EXTERNAL_AUTH_ONLY=true` to defer all auth to an external OIDC provider.
Optional env vars: `JWKS_URL` (token validation endpoint), `AUTH_USERS_COLLECTION` (default: "users").

Behavior when active:
- Built-in auth endpoints blocked for non-superuser collections (password, OTP, OAuth2, email change, password reset, verification)
- `auth-methods`, `auth-refresh`, `impersonate` still work
- `_superusers` collection is exempt (admin panel login)
- `loadAuthToken` validates bearer tokens via JWKS, creates ephemeral user records from JWT claims
- Superuser local tokens always accepted as fallback (admin sessions, CLI tools)
- Regular user local tokens rejected

The platform plugin (`plugins/platform/`) sets these same store keys automatically when `IAMEndpoint` is configured. The env vars provide the same behavior for standalone Base deployments without the platform plugin.

Store keys: `StoreKeyExternalAuthOnly`, `StoreKeyJWKSURL`, `StoreKeyAuthUsersCollection`.
