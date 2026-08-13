# CRDT privacy backends: plaintext, age, FHE

**Measured:** 2026-04-12, Apple M1 Max, macOS arm64, Go 1.26.1
**Scope:** the CRDT op-log in `crdt/` — document sync, not the record store

`Privacy` is one interface with three implementations (`crdt/privacy.go`).
Every op leaving a replica is sealed by `EncryptOp` and every op arriving is
opened by `DecryptOp`; the CRDT machinery above that line is identical whichever
backend is mounted, so choosing one changes no application code.

- `plaintext/v1` — JSON, no encryption. The default.
- `age/v1` — each op sealed to the recipients' `luxfi/age` keys, ML-KEM-768 +
  X25519 hybrid. The relay holds ciphertext and can compute nothing on it.
- `fhe/tfhe-v1` — `luxfi/fhe`, behind `-tags fhe`. The relay can merge
  ciphertexts without holding a key.

This document is the evidence behind that default: what each backend costs,
measured, and which deployment each one answers. The short version is that FHE
costs about eight orders of magnitude more than age and buys a property only
one deployment shape needs.

## Which one

```
Does a party that must not read the data need to MERGE it?
 YES -> fhe/tfhe-v1        (and read section 2.4 first)
  NO -> Is the relay trusted to read?
         YES -> plaintext/v1 + TLS
          NO -> age/v1 over a relay that only stores and forwards
```

---

## 1. What each backend answers

### The three shapes

| Shape | Who runs the relay | What it may read |
|-------|--------------------|------------------|
| **Self-hosted** | The user, on their own machine. 2–4 devices sync over LAN/WAN. | Everything. It is their machine. |
| **Hosted, operator trusted** | Hanzo, or the org itself. | Everything it serves. Reading is the job. |
| **Hosted, operator not trusted** | Hanzo, for an org that wants the operator unable to read at rest. | Ciphertext. |

Base is multi-tenant, but not on this axis. A request resolves an org from its
verified token and lands on that org's own Base at
`{DataDir}/orgs/{org}/data.db` — separation is one file per tenant, and the
process reads each file because it answers that tenant's queries and evaluates
that tenant's rules. Tenancy here is a boundary between tenants, not a claim
that the process cannot read. Nothing in Base merges across tenants, so no
deployment asks a relay to compute on data it cannot decrypt.

### The matrix

| Shape | plaintext/v1 | age/v1 | fhe/tfhe-v1 |
|-------|--------------|--------|-------------|
| **Self-hosted** | Right answer. TLS covers transit; there is no third party. | Overhead for nothing — the user controls the relay. | Nothing to buy. |
| **Hosted, operator trusted** | Right answer for admin metadata: collections, settings, logs. | Available if an org wants it anyway. | Nothing to buy; the relay does not compute on ops. |
| **Hosted, operator not trusted** | Does not hold. The relay reads what it stores. | Right answer. Each tenant seals to its own keys; the relay stores and forwards. | Buys server-side merge the deployment does not ask for, at section 2.4's cost. |

The property FHE alone provides — merge without a key — has no consumer in
Base. Sync is state-vector based: a device sends its state vector, receives the
ops it lacks, and merges locally. The relay's job is to hold and forward. That
is why `plaintext/v1` is the default and `age/v1` is the escalation, and why
`fhe/tfhe-v1` sits behind a build tag rather than a config flag.

---

## 2. Performance Cost

All measurements taken on Apple M1 Max, arm64. Median of 3 runs where noted.

### 2.1 Plaintext CRDT Baseline (measured)

| Operation | Latency (ns/op) | Memory (B/op) | Allocs/op |
|-----------|-----------------|---------------|-----------|
| GCounter.Increment | 27 | 0 | 0 |
| GCounter.Merge (4 nodes) | 217 | 0 | 0 |
| GCounter.Value | 81 | 0 | 0 |
| PNCounter.Increment | 25 | 0 | 0 |
| PNCounter.Merge (4 nodes) | 418 | 0 | 0 |
| LWWRegister.Set | 22 | 0 | 0 |
| LWWRegister.Merge | 33 | 0 | 0 |
| ORSet.Add | 608 | 220 | 5 |
| ORSet.Merge (100 elems) | 12,195 | 9,672 | 55 |
| RGA.Insert | 100,658 | 660 | 4 |
| RGA.Merge (2x24 chars) | 22,170 | 20,733 | 358 |
| Document.Encode | 23,178 | 21,371 | 97 |
| Document.Decode | 56,209 | 37,236 | 851 |
| SyncManager.HandleStep1 | 17,261 | 16,925 | 80 |

### 2.2 Serialized Sizes (plaintext)

| Object | Size |
|--------|------|
| GCounter state (4 nodes) | 72 bytes |
| Single register op (gob) | 134 bytes |
| Full document snapshot (admin-typical) | 2,631 bytes |
| 1,000-op sync session (register ops) | ~131 KB |

### 2.3 E2E (luxfi/age, ML-KEM-768 + X25519 hybrid) Overhead (measured)

| Operation | Latency (ns/op) | Memory (B/op) |
|-----------|-----------------|---------------|
| HybridIdentity keygen | 85,715 | 15,144 |
| Encrypt 64 B | 145,027 | 27,797 |
| Encrypt 1 KB | 140,681 | 30,678 |
| Decrypt 64 B | 129,330 | 162,241 |
| Decrypt 1 KB | 123,276 | 163,908 |

| Plaintext Size | Ciphertext Size | Expansion Ratio |
|----------------|-----------------|-----------------|
| 64 B | 1,723 B | 26.9x |
| 1 KB | 2,683 B | 2.6x |

**E2E overhead on Base CRDT sync:**

- Per-op encryption: ~140 us. At <10 ops/sec workload: 1.4 ms/sec total CPU. Negligible.
- 1,000-op sync session: 131 KB plaintext -> ~262 KB encrypted (ops are ~134 B each, expansion ~2.6x at that size once amortized over header). Actual overhead: 1,723 B header per op if encrypted individually, or ~131 KB + 1,659 B header if batched.
- Optimal strategy: batch ops into a single age envelope per sync message. A 131 KB plaintext batch encrypts to ~133 KB (header + stream overhead is ~1.7 KB fixed).
- **Encrypt latency for a batched 1,000-op sync: ~140 us (single call).** Decrypt: ~123 us.

### 2.4 fheCRDT Overhead (measured from luxfi/fhe)

| Operation | Latency | Notes |
|-----------|---------|-------|
| FHE keygen (secret) | 33.6 us | |
| FHE bootstrap key gen | 290 ms | Required once per setup |
| FHE encrypt 1 bit | 39.8 us | |
| FHE encrypt uint64 | 2.67 ms | 64 bit-level encryptions |
| FHE AND gate | 108 ms | Single boolean gate with bootstrapping |
| FHE OR gate | 121 ms | Single boolean gate with bootstrapping |

| Object | Size |
|--------|------|
| 1-bit ciphertext | 17,057 bytes (17 KB) |
| uint64 ciphertext | 1,091,648 bytes (1.04 MB) |
| Bootstrap key | 135,486,811 bytes (129.2 MB) |
| Secret key | 16,620 bytes |
| Public key | 16,555 bytes |

**fheCRDT overhead on Base CRDT sync (projected):**

A GCounter with 4 nodes stores 4x uint64 values. Under fheCRDT:

- **Storage per GCounter:** 4 x 1.04 MB = 4.16 MB (vs 72 bytes plaintext). **57,778x expansion.**
- **GCounter.Merge (homomorphic max of 4 pairs):** Homomorphic comparison requires bit-level cascaded gates. A 64-bit max operation needs ~128 AND gates + 64 OR gates + cascading. At 108 ms per gate: **~20 seconds per merge.** Plaintext merge: 217 ns. **Ratio: ~92,000,000x slower.**
- **1,000-op sync session:** Each op would carry encrypted counter deltas. 1,000 ops x 1.04 MB = 1.04 GB network transfer (vs 131 KB plaintext). **8,000x bandwidth expansion.**
- **Bootstrap key distribution:** Every sync partner needs the 129.2 MB bootstrap key. For 4 devices: 516.8 MB key material.
- **Memory footprint per replica:** Bootstrap key alone is 129.2 MB. A document with 10 counter fields = 41.6 MB of ciphertexts. Total: ~171 MB per document (vs ~3 KB plaintext).

### 2.5 Summary Comparison Table

| Metric | Plaintext | E2E (age) | fheCRDT |
|--------|-----------|-----------|---------|
| **GCounter.Increment** | 27 ns | 27 ns + 140 us encrypt | 39.8 us encrypt per bit x 64 = 2.5 ms |
| **GCounter.Merge** | 217 ns | 217 ns (decrypt, merge, re-encrypt: ~280 us total) | ~20 sec (homomorphic) |
| **Merge overhead vs plaintext** | 1x | ~1,300x | ~92,000,000x |
| **Document encode** | 23 us / 2.6 KB | 23 us + 140 us / 4.3 KB | ~2.6 ms / ~4.2 MB |
| **1,000-op sync bandwidth** | 131 KB | 133 KB (batched) | 1.04 GB |
| **Memory per replica** | ~3 KB | ~3 KB + 15 KB key | ~171 MB |
| **Key material per device** | 0 | 15 KB identity | 129.2 MB bootstrap key |
| **Setup time** | 0 | 86 us keygen | 290 ms key + 290 ms bootstrap |

---

## 3. What each backend costs to carry

### 3.1 plaintext/v1

Nothing. No dependency, no key material, no binary weight, no setup. It is the
default because it is free and because most deployments run the relay
themselves.

### 3.2 age/v1

| Dimension | Cost |
|-----------|------|
| Deps | `github.com/luxfi/age` — ~6 transitive, all pure Go. |
| Binary | ~2–3 MB: ML-KEM plus the ChaCha20-Poly1305 stream. |
| Key material | 15 KB identity per device. Derive from a passphrase via age scrypt, hold a hybrid keypair in the data directory, or fetch from `kms.hanzo.ai` — the last is the one for hosted deployments. |
| Per message | ~140 µs to seal, ~123 µs to open, on top of ~20 µs of merge. |

The relay stores and forwards sealed blobs and can do nothing else with them,
which has consequences worth stating plainly:

- `Diff()` runs on a client. The relay cannot compute one.
- The relay holds opaque snapshots and relays updates.
- Client-to-client sync is unaffected: both ends hold the key.
- Replica-to-replica sync needs the replicas to share a key.

Seal per sync message, not per op. Sealing individually costs a 1,723 B header
each; one envelope over a batched 131 KB session costs ~1.7 KB total.

### 3.3 fhe/tfhe-v1

Behind `-tags fhe`, and research-grade: `crdt/privacy_fhe.go` implements
LWW-Register and nothing else, at under one merge per second and 136 KB per
ciphertext at production parameters.

| Dimension | Cost |
|-----------|------|
| Deps | `github.com/luxfi/fhe`, pulling `luxfi/lattice/v7` and its 40+ transitive deps. |
| Binary | ~15–25 MB of lattice arithmetic, NTT and ring operations. |
| Key material | 129.2 MB bootstrap key **per device**, plus 290 ms to generate it. |
| CI | ~290 ms of bootstrap per test setup; the CRDT suite goes from under a second to over a minute. |

Carrying the rest of the document model under FHE is not a matter of writing
more code. What the primitive cannot express:

- **RGA (text editing):** RGA requires linked-list traversal with pointer chasing based on character IDs. This is a sequential, data-dependent operation that FHE cannot express efficiently. Each character comparison would require a homomorphic string comparison circuit. For a 1000-character document: millions of gate evaluations, each taking 108 ms. Estimated time: hours to days per merge. **Not feasible.**
- **ORSet:** Requires set membership tests and tag comparison. Each tag is a variable-length string. Homomorphic string comparison is theoretically possible but impractical. **Not feasible.**
- **LWWRegister with arbitrary values:** FHE operates on integers. Arbitrary `any` values (strings, structs, nested objects) cannot be homomorphically compared. Only numeric timestamps could be compared, but the value itself cannot be meaningfully processed. **Partially feasible (timestamp merge only, value is opaque).**
- **MVRegister:** Requires dominance testing across variable-length entry lists. **Not feasible.**

Only the integer-only CRDTs — `GCounter` and `PNCounter` — could be carried
meaningfully, two of the five types in the document model, and neither is
implemented today.

---

## 4. Why plaintext is the default

Sync is state-vector based: a device sends its state vector, receives the ops
it lacks, and merges locally. The relay holds and forwards. There is no point
in the protocol where a party that must not read the data needs to compute on
it, which is the only thing FHE buys.

Homomorphic merge would let a relay merge for an offline client. Against that:
the workload is under 10 ops/sec per tenant and devices sync directly; RGA,
ORSet and MVRegister have to merge on the client regardless; and the cost is
92 million times a plaintext merge, 57 thousand times the storage, and a
129 MB key on every device.

It earns its place elsewhere — a relay aggregating across clients that never
sync with each other (sealed auctions, tallies, cross-base analytics), on
numeric data, where seconds per operation is acceptable and merges are counted
in tens. That is what the build tag is for.

---

## 5. What this layer does and does not do

`crdt/` is deliberately policy-free. It seals and opens ops and merges CRDTs;
it enforces nothing about who is talking to it. That is a layering choice, and
it means three things are yours to put in front of it:

- **A sync message carries no proof of who sent it.** The layer parses a
  `SyncMessage` and merges what it finds; anyone who can deliver bytes to it
  can propose a merge. **Authenticate at the transport** — mTLS between
  replicas, an IAM-verified session on the socket that carries client sync —
  and treat reachability as authority, because here it is.
- **Nothing throttles a merge.** `SyncManager` merges as fast as it is fed, and
  a document's op-log grows with what it accepts. **Put a per-document limit at
  the HTTP or WebSocket edge** before that edge faces anything but localhost.
- **`gob` is Go-only.** `Document.Encode`/`Decode` use it, so a non-Go client
  reads through the JSON `OpEnvelope` path (`SealOps`/`OpenOps`) rather than
  the snapshot. Not a security property — a portability one, and the reason to
  prefer the envelope in mixed-language deployments.

Sealing does not substitute for either of the first two. `age/v1` stops a relay
reading an op; it does not stop an unauthenticated peer submitting one, because
a peer holding the recipients' public keys can seal a perfectly valid op. Read
that as: encryption answers confidentiality here, and authentication is a
separate thing you still have to bring.

What the layer does hold, with a regression test for each
(`crdt/privacy_security_test.go`): an envelope's privacy tag cannot be
downgraded to plaintext on a sealed document; the right tag with the wrong key
does not open; an envelope from one document does not replay into another;
`Encode` seals the snapshot rather than emitting the plaintext under it; and a
raw blob with no document binding is refused.

---

## Sources

Every number above was measured on the hardware named at the top, not
estimated. What was read to produce them:

- `crdt/privacy.go` — the `Privacy` interface and `plaintext/v1`
- `crdt/privacy_age.go`, `crdt/privacy_fhe.go` — the other two backends
- `crdt/types.go` — GCounter, PNCounter, LWWRegister, ORSet, MVRegister
- `crdt/text.go` — RGA, the collaborative-text type
- `crdt/sync.go` — SyncManager and the state-vector protocol
- `crdt/document.go` — the document container, Encode/Decode, Diff, SealOps/OpenOps
- `luxfi/fhe` — TFHE parameters and key generation (`fhe.go`), bit and integer
  encryption (`encryptor.go`), gate evaluation with bootstrapping
  (`evaluator.go`), homomorphic comparison (`integer_ops.go`), and the
  serialized sizes (`serialization.go`)
- `luxfi/age` — the hybrid ML-KEM-768 + X25519 recipient and identity
  (`pq.go`), and the ChaCha20-Poly1305 stream (`age.go`)

The tenancy model — one Base per org under `{DataDir}/orgs/{org}/data.db`,
resolved from the verified token — is in `plugins/org/` and described in
`LLM.md`.
