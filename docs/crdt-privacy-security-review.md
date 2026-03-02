# Security Review — CRDT Privacy Stack (hanzo/base/crdt)

Review date: 2026-04-12

## Fixed Findings

| # | Severity | Title | Status |
|---|----------|-------|--------|
| 3 | HIGH | EncryptedBridge OOM via oversized ciphertext | FIXED: MaxBlobSize cap (default 256 KiB) |
| 4 | HIGH | Document.Encode() leaks plaintext for age/fhe-backed docs | FIXED: Encode seals via privacy backend; Decode accepts opts |
| 13 | LOW | Cross-doc replay test is advisory (t.Log not t.Fatal) | FIXED: upgraded to t.Fatal regression gate |

## INFO Findings (not fixed, documented)

### INFO-1: EncryptedBridge does not validate SyncMessage authenticity

The EncryptedBridge.HandleEncryptedSync parses a SyncMessage from raw JSON
without verifying message authenticity (e.g., HMAC or signature). An
attacker on the network can craft arbitrary SyncMessages.

Mitigation: transport-layer authentication (mTLS, NATS auth, WebSocket
session tokens). Not a CRDT-layer concern.

### INFO-2: Gob encoding is Go-specific

The Document serialization uses Go's gob encoding, which is not portable
to non-Go clients. This limits interoperability.

Mitigation: for cross-language clients, use the SealOps/OpenOps envelope
protocol (which is JSON-based) rather than Encode/Decode.

### INFO-3: SyncManager does not enforce per-document rate limits

A malicious client can flood the SyncManager with rapid sync messages for
a single document, causing excessive merge operations.

Mitigation: rate limiting at the WebSocket/HTTP handler layer. The CRDT
library is intentionally policy-free.
