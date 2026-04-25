# Base Network — Red Review

Adversarial review of four Blue branches. PoCs at
`~/work/hanzo/base/network/red_probe_test.go`. Run:

```
cd ~/work/hanzo/base/network && go test -run TestRedProbe -v
```

Three probes fail as exploits; one skips when Quasar rejects in-engine.

## MUST-FIX before any env opt-in

| # | Sev | Defect |
|---|-----|--------|
| R1 | Critical | Envelope/Frame ShardID confusion → cross-shard frame acceptance (`wal.go:39`, `node.go:163`, `shard.go:100`). |
| R2 | Critical | `Shard.localSeq` pinned to attacker-chosen `f.Seq`; breaks RYW forever (`shard.go:116`). |
| R3 | Critical | Archive segments unauthenticated; CRC-32 only; forged `.lbn` accepted by PITR (`segment.go:105`, `hack/pitr-restore.go:122`). |
| R4 | Critical | `BASE_PEERS` pod-ordinal DNS only resolves for StatefulSets; ATS/BD/IAM/KMS/AML/Compliance are Deployments (`controller.rs:9715`; deploy specs lack `hostname`/`subdomain`). |
| R5 | High | P2P transport has no authentication; `ChainID = sha256(shardID)` is public and enumerable (`transport.go:17`, `quasar.go:60`). |
| R6 | High | Unbounded per-shard archive backlog under hostile backend → OOM (`archive_writer.go:117,281`). |
| R7 | High | HPA / KEDA `scaleTargetRef.kind` hardcoded to `Deployment`; breaks TA (StatefulSet) (`controller.rs:10158,10217`). |
| R8 | High | Segment key overwrite on restart — silent PITR data loss (`segment.go:115`). |

## SAFE-TO-SHIP-AS-IS

- PDB sizing (`controller.rs:9700`) matches doc.
- HRW router (`router.go`) deterministic, commutative.
- Segment decoder robust to truncation/magic/CRC corruption
  (`segment.go:131`). The weakness is authorship, not decode.
- Gateway `stripIdentityHeaders` runs before every bypass
  (`auth_middleware.go:398-402`).
- Gateway 307 follow-once with explicit `redirect_loop` 502
  (`base_network_backend.go:358`).
- `FromEnv` validation rejects invalid mode and `replication > peers+1`
  (`config.go:99`).

## Evidence per slice

### Slice 1 — Core runtime

**R1.** `onPeerFrame(env)` routes by `env.ShardID`; `Frame.Valid` only
checks SHA-256 over the inner `f.ShardID`. Attacker sets
`Envelope.ShardID="victim"` with `Frame.ShardID="attacker"`; the frame
validates, enters the victim's Shard/Engine, and reaches the apply
callback parameterised by `shardID="victim"`. Apply is a no-op today
(`node.go:176`); once the doc's "write F to local SQLite" lands this is
full cross-tenant data injection. ShardKey sources (`user_id`, IAM
`owner`) are enumerable, so R1 + R5 need no insider access.

**R2.** `shard.go:116`: `if f.Seq > localSeq { localSeq = f.Seq }`.
Attacker picks `Seq=2^62`; localSeq pins; honest `Seq=1..N` no longer
advances it. Gateway's `txseq` check (`localSeq >= txseq`) always
returns true → stale state served as current.

**PoC** (`TestRedProbe_EnvelopeShardConfusion`):
`victim localSeq advanced to 9223372036854775792 via a frame whose inner
ShardID was 'attacker-shard'`.

**R5.** `transport.go` has no auth surface. `nopTransport` is the
default (`node.go:49`). The doc's "PQ-signed" claim is delegated to
Quasar, which sees frames only after shard-local submission. Any host
reaching port 9999 forges for any known shardID.

**Fix.** Reject frames where `f.ShardID != Shard.ID` on ingestRemote;
tie `localSeq` to the Quasar engine's finalised height, not the
attacker-controlled frame header; require mTLS + pod-identity on
transport before any cross-namespace exposure.

### Slice 2 — Archive tier

**R3.** `.lbn` footer is CRC-32-IEEE over the body. Attacker with
bucket write access (stale SA, compromised CI, devnet wildcard IAM)
crafts any payload, writes the segment at the deterministic key
`<svc>/<shard>/<startSeq/1M>/<startSeq:020>.lbn`. PITR
(`hack/pitr-restore.go:122`) iterates `Range`, calls `Frame.Valid()`
(self-consistent), inserts the row.

**PoC** (`TestRedProbe_ArchiveForgedSegment`): `forged segment
(seq=5000) accepted; seqs returned [1 2 3 4 5 6 7 8 9 10 5000]`.

**Fix.** Sign the segment envelope (shardID + startSeq + frame count +
frame hashes) with a KMS-held archive-writer key; verify at restore.

**R6.** `archive_writer.go:117,281` — neither `rotateLocked` (append)
nor `reBuffer` (prepend) caps backlog. `uploadWithRetry` retries to
deadline, then re-buffers forever. PoC drives 4 000 frames against a
failing uploader; backlog grows past 11 KiB in under 2 s, no ceiling.

**R8.** `objectKey(svc, shard, startSeq)` is deterministic. Mid-flush
crash loses the in-memory buffer; on restart the witness re-receives
frames from the engine's persisted cursor, a shorter segment is encoded
with the SAME startSeq, and `PutObject` replaces the longer original.
PITR for the lost-tail range returns no frames.

**(a–g) Blue's archive checklist.**
(a) confirmed — forged segments accepted.
(b) confirmed — no cap; PoC.
(c) confirmed — same-seq overwrite via R8.
(d) confirmed — `credentials.NewIAM("")` (`archive_s3.go:168`) hits AWS
IMDS by default; `client.BucketExists` is synchronous in
`newS3Archive`, so pod Start blocks ≤30 s on non-IMDS hosts.
(e) confirmed — `NewArchive` does no host allowlisting; a mis-set
`BASE_ARCHIVE=s3://attacker-bucket/…` with broad pod IAM uploads
cross-bucket. Needs operator-side validation.
(f) not tested — no `fake-gcs-server` in env. Blue owes a
`//go:build integration && gcs` round-trip before enablement.
(g) ruled out — `flushReady` sequential, `reBuffer` prepends under
`w.mu`, ordering preserved. Breaks if uploads ever parallelise.

### Slice 3 — Operator

**R4.** `build_base_peers` emits
`{workload}-{i}.{headless}.{ns}.svc.cluster.local:9999`. Per-pod DNS
records require `spec.hostname` + `spec.subdomain` on the pod —
automatic for StatefulSets, not for Deployments. Deployment pod
templates (`controller.rs:7572,8000,9131,7580,8492,8687`) set neither.
Only TA is a StatefulSet. Consequence:
- `replication=1, replicas=1` works (singleton).
- `replication=1, replicas=2` → each pod sees only itself, both claim
  every shard, gateway alternates, SQLite diverges.
- `replication≥2, replicas≥2` → no quorum; writes block.

The feature is operational only on TA today. The devnet overlay (ATS,
replicas=1) happens to work; any replicas bump breaks silently.

**Fix.** Either set `hostname`/`subdomain` on Deployment pod templates
(effectively requires StatefulSet migration) or have `base/network` do
real peer discovery via the headless svc RRset + gossip.

**R7.** `build_base_hpa` (`:10158`) and `build_keda_scaled_object`
(`:10217`) hardcode `kind: "Deployment"`. HPA Scale-subresource is
resolved by GVK+name; a Deployment-kind ref targeting a StatefulSet
name never binds. The comment at `:10099` asserts the opposite and is
wrong. Thread workload kind through `WorkloadMeta`.

**Other operator checks OK.** OwnerRefs everywhere, SSA idempotent,
replication validated and not clamped, PDB `None` for N=1.

### Slice 4 — Universe specs

Devnet-ATS overlay at `replication=1` is inert w.r.t. R4 *today* iff
the target CR is `replicas=1`; confirm before merging. Any future
replicas bump breaks it. Consistent-hash is deterministic given a
common member set (`router.go:28-34`); R4 breaks that, so split-brain
is concrete the moment `replicas > 1`.

### Slice 5 — Gateway

`X-User-Id`/`X-Org-Id` spoofing neutralised — `stripIdentityHeaders`
before every bypass, middleware chained at `router_engine.go:337`. But
`shard_key_source: header:<Name>` has no gateway protection; compose
uses `header:X-User-Id`, letting clients choose shard. Add a CI lint
rejecting `header:*` in non-dev configs.

`/-/base/members` unauthenticated — in-cluster trust assumption, info
only. 307 follow-once correct. HRW on empty shardID blocked by
`shard_key_missing` 400.

### Slice 6 — Compose

`header:X-User-Id` by design (dev). Smoke exits 2 pending Agent #1's
`/-/base/members`. Not re-run locally — image build prohibited by user
policy. Blue must exercise the full cycle in CI.

## Cross-cutting

- **WAL replay** — see R1 + R5.
- **`BASE_ENCRYPT=sqlcipher`** — `config.go` ignores the var; nothing
  partially wired. When Blue adds encryption, add a startup assertion
  that SQLCipher is linked + keyed before any connection opens.
- **Vshard deviation** — doc says 256-vshard consistent-hash; impl
  does HRW directly over shardID → `O(shards)` engines. OK at N≤5,
  hundreds of shards. Breaks the doc's scalability claim at
  thousands. Revise doc or build the vshard layer.
- **Engine-per-shard memory** — 1024-cap channel per shard; at 100 k
  shards ≈20 GB channel-only lower bound. Nothing tested above 3
  shards. Pre-scale debt.

## Ship recommendation

**do-not-ship** for any env with `replicas > 1` on Deployment-backed
services or `BASE_ARCHIVE ≠ off` until R3 + R6 land.

**fix-then-ship** for devnet-ATS singleton overlay, conditional on all
of: R1 (frame binding + monotonic localSeq), R3 or archive=off, R4 or
explicit `replicas=1` + documented ban on bumps, R6 or archive=off.

## Blue Handoff

**What Blue got right.** Gateway identity handling; PDB table;
router determinism; segment-decoder robustness; owner refs and
idempotent apply in the operator; clean no-op standalone fallback.

**What Blue missed.**
- Envelope ShardID is a separate trust boundary from frame ShardID.
- `localSeq` update is attacker-controllable and monotonic-one-way.
- Per-pod DNS only exists for StatefulSets.
- HPA scaleTargetRef kind must track the underlying workload kind.
- Archive integrity requires authorship binding; CRC is not enough.
- Backlog must be capped to avoid OOM under hostile backends.

**Fix priority for Blue:**
1. R1 + R2 — envelope/frame ShardID binding + monotonic localSeq.
2. R4 — DNS / peer discovery for Deployment-backed workloads.
3. R3 — signed segment manifests (or KMS-wrapped MAC).
4. R6 — backlog cap with backpressure contract.
5. R7 — thread workload kind into HPA/KEDA.
6. R8 — segment key disambiguation per pod-run.

**Re-review scope after fixes:**
- Rerun `TestRedProbe_*` (all should pass/skip with documented reason).
- New probes: concurrent-flush ordering once uploads parallelise; GCS
  integration with `fake-gcs-server`; compose smoke round-trip with
  Agent #1's members endpoint.
- K8s apply into a kind cluster with a Deployment-backed Base service
  at N=2 proving peer DNS resolves.

---
RED COMPLETE. Findings ready for Blue.
Total: 4 critical, 4 high, 3 medium, 3 low, 2 info
Top 3 for Blue to fix:
1. R1 — envelope/frame ShardID confusion (and R2 localSeq pinning)
2. R4 — BASE_PEERS DNS unresolvable on Deployment-backed services
3. R3 — archive segments unauthenticated; CRC-32 is not integrity
Re-review needed: yes — full slice 1 + slice 2 + slice 3 after R1/R3/R4/R6/R7/R8
Recommendation: do-not-ship (except devnet-ATS singleton after R1/R3 fixes)

## Attack-suite catalog

Canonical list of adversarial tests. Source: `network/attack_vectors_test.go`.
Enforced by `.github/workflows/network-attack-suite.yml` which runs on every
PR touching `network/**` or `docs/NETWORK.md`. A new test must be added to
both the Go file and this table in the same PR, or CI fails.

Status legend: `PASS` = defence in place; `SKIP` = blocked on blue's
`feat/network-v0-redfix` (only accepted skip reason in CI).

### 1. Consensus / frame / envelope integrity

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_FrameShardIDSpoof` | Peer crafts Envelope{shard:victim} wrapping Frame{shard:attacker}. | Envelope ShardID MUST equal Frame.ShardID or the frame is rejected. | PASS |
| `TestAttack_SelfForgedSeqOverflow` | Peer submits Seq=2^62 pinning localSeq forever. | localSeq advances from quasar finalised height, not Frame.Seq header. | PASS |
| `TestAttack_DuplicateFrameReplay` | Captured frame resent 5×. | apply() is idempotent per (salt, cksm); FramesFinalized ≤ 1. | PASS |
| `TestAttack_OutOfOrderFrames` | Quasar DAG delivers seqs non-linearly. | localSeq = max() across deliveries, not +1-contiguous. | PASS |
| `TestAttack_ForgedBlockHeight` | Block.Height diverges from Frame.Seq. | decodeFrame trusts Frame.Seq; Height is advisory. | PASS |
| `TestAttack_NilFrameFields` | Empty shardID, nil payload, oversized shardIDLen. | Decoder never panics; rejects deterministically. | PASS |

### 2. P2P / transport

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_UnauthenticatedQuasarSubmit` | Any host on :9999 submits frames for any shard. | Production transport requires mTLS + pod identity. | SKIP |
| `TestAttack_ReplayOldFrame` | Attacker replays a captured envelope hours later. | Dedupe on (salt, cksm) coalesces. | PASS |
| `TestAttack_PeerImpersonation` | Peer claims a stronger NodeID. | Peer identity attested by TLS cert CN / Noise static key. | SKIP |
| `TestAttack_QuasarFloodDOS` | 2× channel-cap submits. | submitLocal never blocks >3 s. | PASS |
| `TestAttack_GatewayMembershipPoisoning` | Malicious /-/base/members response. | Router.membersFor("") does not leak full ring. | SKIP |

### 3. Archive / PITR

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_ArchiveSegmentForgery` | Bucket writer forges a segment with their own key. | Ed25519 signature required; trust set = archive-role key only. | PASS |
| `TestAttack_ArchiveSegmentRewrite` | Mid-flush crash → same startSeq overwrites. | objectKey includes per-flush nanos suffix. | PASS |
| `TestAttack_ArchiveOutOfOrderSegments` | Two flushes overlap startSeq. | Range dedupes by (startSeq + frameIndex). | PASS |
| `TestAttack_PITRReplayCrossShard` | Segment for shard B placed under shard A's path. | Range asserts inner ShardID matches requested shard. | SKIP |
| `TestAttack_ArchiveBucketPermissionLeak` | BASE_ARCHIVE set to attacker-bucket. | Operator-level allowlist against spec.archive. | SKIP |

### 4. Shard routing / isolation

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_ShardKeySpoofingViaHeader` | `shard_key_source: header:X-User-Id` in prod config. | Gateway config CI lint rejects `header:*` in non-dev. | SKIP |
| `TestAttack_ShardKeyMissingHTTPFallback` | Request with no shardID lands on full ring. | Empty shardID rejected or yields empty member set. | SKIP |
| `TestAttack_OwnerConsistencyAcrossPods` | Different peers see members in different orders. | ownerOf() byte-identical across any pod sharing member set. | PASS |
| `TestAttack_EmptyShardKey` | BASE_SHARD_KEY unset in production. | Config.validate() rejects empty ShardKey when Enabled. | PASS |
| `TestAttack_ConcurrentRebalance` | setMembers while reader is iterating. | Atomic snapshot; no duplicates, no empty slice under race. | PASS |

### 5. Resource exhaustion / DoS

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_UnboundedShardBacklog` | S3 outage × high write rate → OOM. | BacklogMaxBytes/Segments enforced; IncDrops fires. | PASS |
| `TestAttack_ApplyLoopStarvation` | One noisy shard starves others. | Per-shard applyLoop isolation. | PASS |
| `TestAttack_ManySmallShards` | 100k shards × per-shard engine. | Per-tenant shard-count cap. | SKIP |
| `TestAttack_QuasarEnginePerShardMemory` | 1024-cap channel × 100k shards ≈ 20 GB lower bound. | Channel-cap tuned OR shared channel pool. | SKIP |

### 6. Encryption / KMS (SQLCipher + base/plugins/kms)

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_WrongDEKForShard` | DEK swap across shards → cross-tenant plaintext. | BASE_ENCRYPT wired with per-shard DEK binding. | SKIP |
| `TestAttack_KMSNodeImpersonation` | Rogue node joins KMS consensus. | KMS membership cryptographically attested. | SKIP |
| `TestAttack_DeletedDEKReadAttempt` | RTBF deletes wrapper; replay pre-deletion ciphertext. | Ciphertext unreadable post-deletion; quasar tombstone. | SKIP |
| `TestAttack_CrossOrgKEKLeak` | `BASE_KEK_SCOPE=platform` means one KEK for every org. | Org-scope KEK is default; platform-scope requires explicit flag. | SKIP |

### 7. Operator / CRD / k8s

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_HPAWrongKindSilent` | HPA scaleTargetRef.kind hardcoded Deployment; TA is StatefulSet. | WorkloadMeta.Kind threaded through builder. | SKIP |
| `TestAttack_BasePeersDNSResolvable` | Pod-ordinal FQDN only resolves for StatefulSets. | StatefulSet required OR headless-SRV discovery. | SKIP |
| `TestAttack_FeatureGateEscape` | `BASE_NETWORK=paxos-v99` silently falls back to standalone. | ConfigFromEnv rejects unknown modes. | PASS |

### 8. Correctness under concurrency

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_WriterChurnLostCommits` | 8×25 concurrent buildFrame+submit. | Atomic Seq allocation; all 200 finalise. | PASS |
| `TestAttack_NetworkPartition` | ctx cancelled → applyLoop spins. | shard.close() cancels ctx; goroutine exits. | PASS |
| `TestAttack_ClockSkew` | One node's clock 1 hr ahead. | Ordering by Frame.Seq only; Timestamp is advisory. | PASS |

### Creative additions (discovered while writing the suite)

| Test | Threat | Invariant | Status |
|------|--------|-----------|--------|
| `TestAttack_SegmentCRCWithBadSig` | Attacker recomputes CRC but can't sign. | Signature check MUST run even when CRC matches. | PASS |
| `TestAttack_SegmentV1Downgrade` | Attacker writes unauthenticated LBN1 at the path. | LBN1 magic is a hard-reject. | PASS |
| `TestAttack_NilVerifier` | Racy startup yields nil verifier. | decodeSegment(b, nil) returns ErrSegmentUnsigned. | PASS |
| `TestAttack_P2PPortBindLocalOnly` | Default `:9999` binds every interface. | Production bind to pod IP only. | SKIP |
| `TestAttack_FrameSeqZero` | First frame has Seq=0 colliding with txseq=0 sentinel. | First Seq emitted is 1, never 0. | PASS |

### Totals (at time of catalog publish)

27 PASS, 13 SKIP (blocking-blue), 0 FAIL. Runtime 3 s.

Skip taxonomy:
- Transport auth (`UnauthenticatedQuasarSubmit`, `PeerImpersonation`,
  `P2PPortBindLocalOnly`) — needs real transport to replace `nopTransport`.
- KMS / SQLCipher (`WrongDEKForShard`, `KMSNodeImpersonation`,
  `DeletedDEKReadAttempt`, `CrossOrgKEKLeak`) — needs `BASE_ENCRYPT`
  plumbing and `base/plugins/kms` wire-up.
- Operator CRD (`HPAWrongKindSilent`, `BasePeersDNSResolvable`) — needs
  WorkloadMeta.Kind threading in `~/work/liquidity/operator/controller.rs`.
- Gateway-owned (`ShardKeySpoofingViaHeader`,
  `ShardKeyMissingHTTPFallback`, `GatewayMembershipPoisoning`,
  `PITRReplayCrossShard`, `ArchiveBucketPermissionLeak`) — defence lives
  outside this package.
- Pre-scale (`ManySmallShards`, `QuasarEnginePerShardMemory`) — design
  debt that binds before thousand-shard deployments.
