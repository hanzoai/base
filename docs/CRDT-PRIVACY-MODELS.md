# CRDT Privacy Model Analysis: Plaintext vs E2E (age) vs fheCRDT

**Date:** 2026-04-12
**Author:** Research brief for Base team
**Hardware:** Apple M1 Max, macOS arm64, Go 1.26.1
**Scope:** Hanzo Base admin/sync CRDT layer (`hanzo/base/crdt/`)

---

## Decision Flowchart

```
Is Base self-hosted (single user, single process)?
 YES -> Plaintext CRDT + TLS (luxfi/zap or standard TLS)
  NO -> Is the server trusted to read all data?
         YES -> Plaintext CRDT + TLS
          NO -> Does the server need to merge/aggregate data it cannot decrypt?
                 YES -> fheCRDT (not viable today; see cost analysis)
                  NO -> E2E (luxfi/age) encrypted ops over dumb relay
```

Result: for every realistic Base deployment today, either Plaintext or E2E wins.
fheCRDT is only theoretically needed for a deployment shape Base does not serve.

---

## 1. Threat Model

### Deployment Shapes Base Actually Faces

| Shape | Description | Frequency |
|-------|-------------|-----------|
| **Self-hosted** | Single user runs Base on own machine. 2-4 device sync via LAN/WAN. | Primary use case |
| **Single-tenant SaaS** | One org per Base process. Server operated by org or by Hanzo on their behalf. | Secondary |
| **Multi-tenant SaaS** | Multiple orgs share a Base process. Server operated by Hanzo. | Not supported today. Base is single-tenant per process by design. |

### Threat Model Matrix

| Deployment Shape | Plaintext + TLS | E2E (age) | fheCRDT |
|------------------|-----------------|-----------|---------|
| **Self-hosted** | **Acceptable.** Server is the user's own machine. TLS protects transit. No third party sees data. | Unnecessary overhead. User already controls the server. | Absurd. 10,000x overhead for zero benefit. |
| **Single-tenant SaaS** | **Acceptable if org trusts operator.** Hanzo (or operator) can read admin records. For most admin metadata (collections, settings, logs) this is fine. | **Required if org does NOT trust operator** with data at rest. Prevents operator from reading synced content. Server acts as dumb relay. | Unnecessary. No cross-tenant aggregation needed. Server does not compute on data. |
| **Multi-tenant SaaS** | **Unacceptable.** Server sees all tenants' data. One breach exposes everyone. | **Acceptable.** Each tenant encrypts to their own keys. Server relays opaque blobs. Cannot merge across tenants, but that is not a requirement for admin data. | **Only model that allows server-side merge across tenants.** But Base does not need server-side cross-tenant merge for admin records. |

### Conclusion on Threat Models

Base is single-tenant per process. The multi-tenant SaaS shape does not exist in production. Even if it did, admin records (collections, users, settings, logs) do not need cross-tenant homomorphic aggregation. The server never needs to compute on data it cannot decrypt.

fheCRDT solves a problem Base does not have.

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

## 3. Integration Cost

### 3.1 Plaintext (current state, no changes)

| Dimension | Cost |
|-----------|------|
| Lines of code | 0 (already implemented) |
| New deps | 0 |
| Binary size delta | 0 |
| CI surface | 0 |
| Developer UX | None. Works today. |

### 3.2 E2E (luxfi/age)

| Dimension | Cost |
|-----------|------|
| Lines of code | ~200-300 lines. Wrap `SyncManager.HandleSync` to decrypt incoming, encrypt outgoing. Add key derivation from passphrase (age scrypt) or device keypair (age hybrid). |
| New deps | `github.com/luxfi/age` (~6 transitive deps, all pure Go). Optionally `github.com/luxfi/zap` for PQ-TLS transport (already used elsewhere). |
| Binary size delta | ~2-3 MB (ML-KEM implementation + ChaCha20-Poly1305 stream). |
| CI surface | Add encrypt/decrypt round-trip tests to existing CRDT test suite. ~50 lines of test code. |
| Developer UX | **Option A (recommended):** Derive per-document key from user passphrase via age scrypt. User provides passphrase at Base startup. No key file management. **Option B:** age hybrid keypair stored in Base data directory. User manages key backup. **Option C:** KMS-backed. Key stored in `kms.hanzo.ai`, fetched at startup. Best for SaaS. |

**Integration pattern:**

```
SyncManager.HandleSync(clientID, raw []byte)
  -> age.Decrypt(raw, identity)     // ~130 us
  -> existing CRDT merge logic      // ~20 us
  -> age.Encrypt(response, recipient) // ~140 us
  -> return encrypted response
```

The server becomes a dumb relay. It stores and forwards age-encrypted blobs. It cannot merge, diff, or inspect CRDT state. This means:
- `SyncManager.Diff()` must run on the client, not the server.
- The server stores opaque snapshots and relays updates.
- Client-to-client sync still works (both have the key).
- Server-to-server sync for multi-replica requires shared key (single-tenant is fine).

### 3.3 fheCRDT (luxfi/fhe)

| Dimension | Cost |
|-----------|------|
| Lines of code | ~1,500-2,500 lines. Rewrite every CRDT type to operate on `fhe.Ciphertext` instead of `uint64`/`any`. Implement homomorphic max (for GCounter merge), homomorphic comparison (for LWW timestamp), homomorphic set operations. RGA (text) is not feasible under FHE. |
| New deps | `github.com/luxfi/fhe` (pulls in `luxfi/lattice/v7` -- heavy lattice crypto). `github.com/luxfi/lattice/v7` has 40+ transitive deps. |
| Binary size delta | ~15-25 MB (lattice arithmetic, NTT, ring operations). |
| CI surface | Every CRDT type needs FHE-specific tests. Bootstrapping takes 290 ms per test setup. Test suite runtime would increase from <1 sec to >60 sec. |
| Developer UX | **Severe.** Every Base user must: (a) generate FHE keys (~290 ms), (b) distribute 129.2 MB bootstrap key to all devices, (c) accept that text editing (RGA) is not supported under FHE, (d) accept 20-second merge latency for counters. |

**What cannot be implemented under fheCRDT:**

- **RGA (text editing):** RGA requires linked-list traversal with pointer chasing based on character IDs. This is a sequential, data-dependent operation that FHE cannot express efficiently. Each character comparison would require a homomorphic string comparison circuit. For a 1000-character document: millions of gate evaluations, each taking 108 ms. Estimated time: hours to days per merge. **Not feasible.**
- **ORSet:** Requires set membership tests and tag comparison. Each tag is a variable-length string. Homomorphic string comparison is theoretically possible but impractical. **Not feasible.**
- **LWWRegister with arbitrary values:** FHE operates on integers. Arbitrary `any` values (strings, structs, nested objects) cannot be homomorphically compared. Only numeric timestamps could be compared, but the value itself cannot be meaningfully processed. **Partially feasible (timestamp merge only, value is opaque).**
- **MVRegister:** Requires dominance testing across variable-length entry lists. **Not feasible.**

Only `GCounter` and `PNCounter` (integer-only CRDTs) can be meaningfully implemented under FHE. This covers 2 of 5 CRDT types in the Base document model.

---

## 4. Analysis

### The core question: does Base need server-side computation on encrypted data?

No. Base admin records sync between a user's 2-4 devices. The sync protocol is state-vector based: each device sends its state vector, receives missing ops, and merges locally. The server's role is relay and storage, not computation.

E2E encryption makes the server a dumb pipe. This is exactly what it should be for the SaaS deployment shape where the operator should not see admin data.

fheCRDT would allow the server to merge on behalf of offline clients. But:
1. Base has <10 ops/sec per tenant. Devices sync directly.
2. Merge must happen anyway on the client for RGA/ORSet/MVRegister.
3. The cost (92M x slower, 57K x larger, 129 MB keys) is catastrophic.
4. 3 of 5 CRDT types cannot be implemented under FHE at all.

### When would fheCRDT make sense?

fheCRDT becomes relevant when:
- The server must aggregate across many clients that never sync directly (e.g., anonymous voting tallies, privacy-preserving analytics).
- The data is purely numeric (counters, sums, max).
- Latency tolerance is seconds, not milliseconds.
- The number of merge operations is small (10s, not 1000s).

None of these conditions match Base admin data sync.

---

## 5. Recommendation

**For self-hosted Base:** Stay plaintext. Add no encryption layer. TLS on the wire (already present) is sufficient. Zero integration cost, zero performance cost.

**For single-tenant SaaS Base (future):** Add optional E2E encryption using `luxfi/age` with hybrid ML-KEM-768 + X25519. ~200 lines of integration code. ~140 us per sync message. ~2 KB bandwidth overhead per batch. Key derived from passphrase (scrypt) or stored in KMS. Ship it as a `--encrypt-sync` flag on Base startup.

**Do not implement fheCRDT for Base.** The cost is 8 orders of magnitude higher than E2E for a threat model Base does not face. The luxfi/fhe library is well-built for its intended purpose (confidential EVM execution), but CRDT admin sync is not that purpose.

### Implementation Priority

1. **Now:** Nothing. Plaintext is correct for the current deployment shape.
2. **When SaaS ships:** Add `luxfi/age` E2E as opt-in. Budget: 1-2 days integration.
3. **Never (for Base):** fheCRDT.

---

## Sources

All performance numbers measured directly on this hardware during this analysis. Source code references:

1. `hanzo/base/crdt/types.go` -- GCounter, PNCounter, LWWRegister, ORSet, MVRegister implementations
2. `hanzo/base/crdt/sync.go` -- SyncManager, state-vector protocol
3. `hanzo/base/crdt/document.go` -- Document container, Encode/Decode, Diff
4. `hanzo/base/crdt/text.go` -- RGA (Replicated Growable Array) for collaborative text
5. `luxfi/fhe/fhe.go` -- TFHE parameters, key generation, bootstrap key
6. `luxfi/fhe/encryptor.go` -- Bit/integer encryption
7. `luxfi/fhe/evaluator.go` -- Boolean gate evaluation (AND, OR, XOR with bootstrapping)
8. `luxfi/fhe/integer_ops.go` -- Homomorphic comparison, bitwise ops
9. `luxfi/fhe/serialization.go` -- Ciphertext/key serialization sizes
10. `luxfi/age/pq.go` -- ML-KEM-768 + X25519 hybrid encryption (HybridRecipient/HybridIdentity)
11. `luxfi/age/age.go` -- age Encrypt/Decrypt, stream cipher (ChaCha20-Poly1305)
12. `hanzo/base/base.go` -- Base app launcher, single-tenant per process architecture
