# Base Network

Seamless, PQ-native, consensus-replicated HA for every Base app.
One mechanism. One env flag. Replica count `N ∈ {1,2,3,…}` works
identically on a laptop (`docker compose`) and on k8s (via
`~/work/liquidity/operator`). No FUSE, no LiteFS, no Consul.

Durability and replication come from `luxfi/consensus/protocol/quasar`
— the same DAG consensus that runs Lux validators — applied to SQLite
WAL frames, one DAG per shard. Archive tier is optional cold storage
on `~/work/hanzo/storage` (`hanzoai/s3`) or GCS for PITR.

## Problem

Base apps (ATS, BD, TA, IAM, KMS, AML, …) each carry SQLite files —
often per-user or per-org — and need HA with real durability, no
split-brain, and uniform ops across dev / test / main.

Pre-existing options fall short:

- **Raw StatefulSet + RWO PVC, replicas > 1** → split-brain, silent
  DB divergence (current mainnet ATS/TA state).
- **LiteFS + Consul** → three moving parts (FUSE, LTX, Consul) and a
  single-primary election we don't need; we already have quasar.
- **Litestream + single writer** → backup, not HA.
- **Postgres** → violates Base's "embedded SQLite by default".

## Goal

- Any Base app opts in with one env: `BASE_NETWORK=quasar`.
- `BASE_REPLICATION=1` works (standalone, zero consensus overhead,
  still ships to archive).
- `BASE_REPLICATION=N` works (full quasar quorum, k = ⌊N/2⌋+1).
- `docker compose up` = true HA locally.
- Operator emits the env surface + HPA/KEDA from one CR field.

## Design

### Data model

- A Base app owns many **shards**. Shard key = `user_id` | `org_id` |
  `<custom header>`.
- Each shard = one SQLite file on the pod's local PVC (hot cache).
- Each shard is replicated across `BASE_REPLICATION` network members
  (that shard's quasar subset). Assignment = consistent-hash over
  live members.
- A shard's finalized WAL-frame DAG IS its durability tier. Archive
  is a cheap, optional second tier.

### Shard hierarchy + per-shard encryption

Multi-tenant Base apps produce **many** shards. The model is two-level
physical files plus a fixed logical consensus grouping:

- **Physical shard = one SQLite file.** Files exist at two scopes:
  - `platform` — `_platform.db` (rare; e.g. IAM app registry).
  - `org`      — `<org_id>.db` (tenant-wide state).
  - `user`     — `<org_id>/<user_id>.db` (per-customer state).
  A single request may touch multiple scopes in one transaction;
  Base opens the right handle based on the route's declared scope.
- **Virtual shard (vshard) = consensus group.** A fixed-cardinality
  ring (default **256**, power-of-two) is hashed over at connection
  time: `vshard = hash(shardKey) mod 256`. Each vshard has a quasar
  validator subset of size `BASE_REPLICATION`. All physical files
  whose `shardKey` maps to vshard `V` replicate together. This keeps
  consensus metadata O(256) instead of O(customers).
- **Assignment** of physical pods to vshards = consistent-hash over
  live network members. Adding a pod moves ~1/N of the vshards to
  the new pod; physical files on those vshards migrate via quasar
  tail-sync (not a file copy — new pod subscribes to the vshard's
  DAG and materialises from frames).

**Per-file encryption** (SQLCipher, opt-in):

- `BASE_ENCRYPT=sqlcipher` activates per-file page-level encryption.
- Each physical file has its own 256-bit Data Encryption Key (DEK).
- DEKs are wrapped by a per-org Key Encryption Key (KEK) held in
  KMS; KEK is wrapped by a platform KEK-of-KEKs. On `open(shard)`:
  1. If `<shard>.dekwrap` exists on disk: fetch KEK from KMS,
     unwrap DEK.
  2. If not: generate DEK, wrap with KEK, persist `<shard>.dekwrap`.
- DEKs cached in memory, per-shard, with LRU eviction (default
  4096 hot files). Cold files drop their DEK; re-fetch on next
  access costs one KMS round-trip.
- WAL frames leaving the pod carry **ciphertext** pages only —
  quasar replication, gateway transit, and archive storage never
  see plaintext. PQ sig from quasar is over the ciphertext.
- Right-to-be-forgotten = delete the `.dekwrap` (+ quasar tombstone
  for the vshard's frame range). Ciphertext becomes unreadable
  globally, including in archive segments.

**Env additions for this layer:**

| var                      | values                                        | notes |
|--------------------------|-----------------------------------------------|-------|
| `BASE_VSHARDS`           | `256` default, power-of-two                   | fixed at deploy; resharding is a separate tool. |
| `BASE_ENCRYPT`           | `sqlcipher` \| `off` (default)                | opt-in page encryption. |
| `BASE_KMS_URL`           | `https://kms.<env>.satschel.com`              | for KEK fetch/wrap. |
| `BASE_KEK_SCOPE`         | `platform` \| `org` (default)                 | grain of the KEK. |
| `BASE_DEK_CACHE_SIZE`    | `4096` default                                | LRU cap for unwrapped DEKs in RAM. |

### Write path

```
client ──► gateway (base-network://svc)
            │   JWT→shardKey, consistent-hash → writer pod
            ▼
         writer pod
            │   handle request, SQLite commit
            ▼
         sqlite3_wal_hook fires frame F
            │
            ▼
         network.SubmitFrame(shardID, F)
            │   quasar vertex, PQ-signed
            ▼
         quasar finalize (k-of-N ack)
            │
            ├──► apply callback: write F to local SQLite (writer + replicas)
            └──► optional: archive validator appends F to S3/GCS segment
```

No round-trip to a coordinator; no FUSE; no separate leader-election
service. A finalized frame IS the commit.

### Read path

- Any pod in the shard's subset can serve reads.
- Gateway sets a `txseq` cookie with the last-seen finalized seq.
- Target pod checks `localSeq ≥ txseq`:
  - yes → serve.
  - no  → either block briefly on quasar tail (typical < 100 ms) or
    307 to a caught-up peer.

### Membership

- Every Base pod is a quasar validator.
- Peers discovered from `BASE_PEERS` seed list (CSV of DNS),
  gossip thereafter.
- In k8s: headless Service, peers = `pod-0…N-1.svc.cluster.local:9651`.
- In compose: static DNS names.
- Heartbeat TTL 1.5 s. On miss: the pod's shards are reassigned
  across survivors; new assignments sync the DAG from peers before
  accepting writes.

### Replication factor

| N     | quasar mode      | k (acks needed) | failure mode                         |
|-------|------------------|-----------------|--------------------------------------|
| 1     | standalone       | 1               | No HA. Archive = durability.         |
| 2     | pair             | 2               | 1-down blocks writes, reads survive. |
| 3+    | quorum           | ⌊N/2⌋+1         | Byzantine-safe, PQ-signed.           |

Same code path for every N; only the `k` constant changes.

### Archive tier (optional)

A **witness-only** validator runs alongside as `BASE_NODE_ROLE=archive`.
It does not vote. It subscribes to finalized frames and appends
per-shard WAL segments to object storage. Layout:

```
<scheme>://<bucket>/<svc>/<shard>/<seq-prefix>/<segment>.lbn
```

`.lbn` = *Lux Base Network* segment — a compact length-prefixed list
of PQ-signed, quasar-finalized frames. Replay any prefix ⇒ SQLite at
that `txseq`. PITR for free.

Backend is pluggable via `github.com/hanzoai/s3` client surface:

- S3 (`s3://…`) — MinIO-protocol, used by `~/work/hanzo/storage`.
- GCS (`gs://…`) — native client, used for production in GCP-hosted
  clusters.
- off — archive disabled; durability = quasar DAG only.

### Env surface (one shape, every Base app)

| var                         | values                                      | notes |
|-----------------------------|---------------------------------------------|-------|
| `BASE_NETWORK`              | `quasar` \| `standalone` (default)          | standalone = today's behaviour, no network. |
| `BASE_SHARD_KEY`            | `user_id` \| `org_id` \| `<header name>`    | required when `BASE_NETWORK=quasar`. |
| `BASE_REPLICATION`          | `1` \| `2` \| `3` \| …                      | ≤ `BASE_PEERS` count. |
| `BASE_PEERS`                | CSV of `host:port` DNS                      | operator-emitted in k8s; explicit in compose. |
| `BASE_NODE_ROLE`            | `validator` (default) \| `archive`          |  |
| `BASE_ARCHIVE`              | `gs://…` \| `s3://…` \| `off` (default)     |  |
| `BASE_LISTEN_HTTP`          | `:8090` default                             | Base HTTP. |
| `BASE_LISTEN_P2P`           | `:9651` default                             | quasar p2p port. |
| `BASE_SHARD_BACKLOG_MAX`    | bytes (64 MiB default)                      | R6 per-shard archive backlog cap; drop-oldest beyond. |
| `BASE_SHARD_BACKLOG_SEGMENTS` | integer (100 000 default)                 | R6 segment-count cap; first-to-hit with MAX drops. |
| `BASE_TLS_CA`               | path to PEM bundle                          | R5 mTLS CA for peer verification. Unset ⇒ no TLS. |
| `BASE_TLS_SERVER_CERT`      | path to PEM                                 | R5 server cert presented to inbound peers. |
| `BASE_TLS_SERVER_KEY`       | path to PEM                                 | R5 server key. |
| `BASE_TLS_ALLOWED_SANS`     | CSV of DNS SANs                             | R5 SAN allowlist. Typically = `BASE_PEERS` stripped of `:9651`. |

### R1–R8 fix notes

- **R1 (frame/envelope shardID binding)**: inbound envelopes whose inner
  `Frame.ShardID` disagrees with `Envelope.ShardID` are rejected at the
  transport boundary; the apply loop re-checks as defence-in-depth.
- **R2 (localSeq monotonic)**: `Shard.localSeq` advances strictly by
  +1 per finalised frame. Out-of-order frames buffer (cap 1024). An
  attacker-forged high-Seq frame stays in the buffer forever because
  its predecessors never arrive, so the gateway's `txseq` check cannot
  be fooled into reporting "caught up".
- **R3 (archive authorship)**: segments are `LBN2` — Ed25519 signature
  over body || CRC32 || pubkey. Writers refuse to encode without a
  signer; readers refuse to decode without a verifier; `LBN1` hard-
  rejected as a downgrade vector.
- **R4 (StatefulSet by default)**: when `network.enabled: true`, the
  operator emits a StatefulSet (`spec.network.workload: StatefulSet`
  default) so `BASE_PEERS` pod-ordinal DNS resolves. Deployment is
  permitted only at `replicas == 1`.
- **R5 (mTLS on p2p)**: `TLSConfig` wires Ed25519/EC mTLS with SAN
  pinning against the BASE_PEERS list. Transport wire-format (QUIC/
  gRPC) is Phase-2; `TLSConfig.ServerConfig() / ClientConfig()` land
  the crypto surface.
- **R6 (backlog caps)**: per-shard cap 64 MiB / 100 k segments,
  drop-oldest with `base_shard_backlog_drops_total` metric.
- **R7 (HPA workload kind)**: operator threads workload kind into the
  HPA / KEDA `scaleTargetRef.kind` so a StatefulSet-backed service
  autoscales correctly.
- **R8 (nanos-suffixed object keys)**: `objectKey(…, startSeq, nanos)`
  produces unique keys per flush; `Range` dedupes by
  `(startSeq + frameIndex)` when multiple segments overlap.

## Layout

### `~/work/hanzo/base/network/`  (new package)

```
network/
  network.go    // NewNetwork(cfg) → *Network; Start/Stop; public API below.
  config.go     // env parsing, defaults, validation.
  node.go       // Node identity (stable from pod name or $HOSTNAME).
  quasar.go     // thin wrapper over luxfi/consensus/protocol/quasar.
  wal.go        // install sqlite3_wal_hook; frame serialise/PQ-sign.
  apply.go      // on-finalize callback → sqlite_apply().
  shard.go      // Shard struct: assignment, local cache, txseq.
  router.go     // consistent-hash ring; who owns shardID?
  archive.go    // BASE_NODE_ROLE=archive loop → hanzoai/s3 or GCS.
  metrics.go    // Prom: base_shards, base_wal_lag_bytes,
                //       base_hot_shards, base_lease_contentions,
                //       base_archive_lag_bytes, ...
  network_test.go
  wal_test.go
  router_test.go
  integration_test.go  // docker compose N=3 smoke: write, crash a node, read, restore.
```

Public API that Base calls from `core/base.go` on startup:

```go
package network

type Network interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Installed by Base onto the sqlite *Conn; intercepts commits.
    InstallWALHook(conn *sqlite.Conn, shardID string) error

    // Given a shardID, returns the current writer endpoint plus
    // whether it's the local pod. Gateway uses this.
    WriterFor(shardID string) (endpoint string, local bool)

    // Any member in the shard subset. Gateway uses this for reads.
    MembersFor(shardID string) []string

    // Metrics plumbing.
    Metrics() *Metrics
}

func New(cfg Config) (Network, error)
```

Base itself (`core/base.go`) gains:

```go
if net := network.FromEnv(); net.Enabled() {
    b.net = net
    b.db.OnConnect(func(c *sqlite.Conn, shardID string) error {
        return net.InstallWALHook(c, shardID)
    })
}
```

That is all the coupling. Rest of Base code is unchanged.

### `~/work/liquidity/operator/`  (reconciler update)

Add `BaseNetworkSpec` struct reused across Base-ish CRDs:

```rust
pub struct BaseNetworkSpec {
    pub enabled: bool,
    pub shard_key: String,            // user_id | org_id | <hdr>
    pub replication: u8,              // ≤ replicas
    pub archive: Option<String>,      // gs://… | s3://… | off
    pub autoscale: Option<Autoscale>,
}

pub struct Autoscale {
    pub min: u8,
    pub max: u8,
    pub cpu_target: Option<u8>,
    pub memory_target: Option<u8>,
    pub hot_shards_target: Option<u32>,
}
```

Added to: `LiquidBDSpec`, `LiquidATSSpec`, `LiquidTASpec`,
`LiquidIAMSpec`, `LiquidKMSSpec`, `LiquidAMLSpec`.

New `controller.rs` helper:

```rust
reconcile_base_network(api, spec, meta) -> ();
```

emits, per replica-managed workload:

- **Headless Service** (`clusterIP: None`) for pod-ordinal peer DNS.
- **Env** on pods: `BASE_NETWORK`, `BASE_SHARD_KEY`, `BASE_REPLICATION`,
  `BASE_PEERS` (computed from `spec.replicas` + headless svc),
  `BASE_ARCHIVE`, `BASE_NODE_ROLE=validator`.
- Optional archive sidecar Deployment (`BASE_NODE_ROLE=archive`) when
  `archive != off`.
- **HPA** (cpu, memory) and **KEDA ScaledObject** (Prom metrics
  `base_hot_shards`, `base_wal_lag_bytes`) when `autoscale` set.
- **PodDisruptionBudget**: `minAvailable = k` where `k = ⌊N/2⌋+1`
  for N≥3, `N-1` for N=2, `0` for N=1.

CR shape:

```yaml
spec:
  replicas: 3
  network:
    enabled: true
    shardKey: user_id
    replication: 3
    archive: gs://liquidity-base-wal/bd
    autoscale:
      min: 3
      max: 7
      cpuTarget: 70
      memoryTarget: 75
      hotShardsTarget: 500
```

### `~/work/hanzo/gateway/` (new upstream type)

`base-network://<service>` upstream.

- Parses JWT `sub` or configured header → shardID.
- Polls `GET /-/base/members` on the service's headless Service
  every 5 s; caches member list.
- Consistent-hashes shardID → owner pod.
- Writes → owner pod. Reads → random pick from `MembersFor(shardID)`,
  propagating `txseq` cookie.
- On 307 from a not-yet-caught-up replica, retries to another member.
- Passes `X-Base-Shard: <shardID>` upstream for logging.

Config example:

```yaml
upstreams:
  - name: liquid-bd
    type: base-network
    service: liquid-bd.liquidity.svc:8090
    shardKey: user_id
    shardKeySource: jwt.sub
```

### Local dev — docker compose parity

`~/work/hanzo/base/compose.network.yml`:

```yaml
services:
  base-a: &node
    image: ghcr.io/hanzoai/base:dev
    environment:
      BASE_NETWORK: quasar
      BASE_SHARD_KEY: user_id
      BASE_REPLICATION: 3
      BASE_PEERS: base-a:9651,base-b:9651,base-c:9651
      BASE_ARCHIVE: off
    ports: ["18090:8090"]
  base-b:
    <<: *node
    ports: ["18091:8090"]
  base-c:
    <<: *node
    ports: ["18092:8090"]
```

`docker compose -f compose.network.yml up` → real 3-node HA locally.
Kill one container, writes still succeed, reads still succeed, DB
never diverges. Same code path as prod.

## Acceptance tests

1. **Single-node**: `N=1`, `archive=off` → behaves exactly like
   today. No regression in `core/base_test.go`.
2. **Docker compose N=3**: write 1000 rows, kill `base-b`, read all
   1000 from `base-a` and `base-c`. Bring `base-b` back, verify it
   catches up (txseq matches). No row loss, no duplication.
3. **Archive PITR**: write 1000 rows, record txseq, write 100 more.
   Restore to the first txseq from GCS archive; DB has exactly 1000.
4. **k8s operator**: apply `LiquidBD` with `network.replication: 3`.
   Verify: headless svc, 3 pods with BASE_PEERS env, HPA, PDB
   minAvailable=2, archive sidecar when `archive != off`.
5. **Autoscale**: drive `base_hot_shards` > `hotShardsTarget` via
   synthetic load; verify KEDA scales Deployment up, new pod joins
   the network, existing shards rebalance.
6. **Gateway**: two users in different shards produce writes on
   different owner pods; both succeed; read-your-writes respected
   via `txseq` cookie.

## Non-goals

- FUSE. Nothing mounts a fuse filesystem.
- LiteFS. No import, no fork. Pure quasar WAL-hook.
- 3rd-party SQLite processes. This is Base-internal; external apps
  don't get the WAL-hook.
- Cross-app shard sharing. Each Base service has its own network.

## Deprecations

- `~/work/hanzo/base-ha` is superseded. After `base/network` lands
  and ATS/BD/TA/IAM/KMS/AML migrate, archive `base-ha` to the
  deprecations page. The repo stays read-only for history.
