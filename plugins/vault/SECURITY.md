# Base Vault SDK — Security Configuration Guide

## Production Security Settings

### MPC (~/work/lux/mpc)

```yaml
# mpcd consensus mode — no NATS, no Consul, no single point of failure
mode: consensus
threshold: 3          # 3-of-5 signers required
total_nodes: 5        # 5 MPC nodes in signer set
bond: 1000000         # 1M LUX bond per signer (slashable)

# Storage: SQLite default, Postgres for production multi-instance
db: sqlite://mpc.db   # local dev
# db: postgres://...  # production HA

# Key storage: ZapDB with ChaCha20-Poly1305 encryption
zapdb_password: env    # from HSM via ZAPDB_ENCRYPTED_PASSWORD
hsm_provider: aws      # aws | gcp | azure | env | file

# Transport: ZAP p2p (no NATS)
p2p_port: 9999
api_port: 8081

# JWT: distinct issuer/audience per service
jwt_issuer: mpc.lux.network
jwt_audience: mpc-api

# Rate limiting: use RemoteAddr (not X-Forwarded-For)
# Body size: 1MB max per request
```

**Critical settings:**
- `mode: consensus` — never use legacy NATS mode in production
- `threshold >= ceil(total_nodes * 2/3)` — BFT safety
- `hsm_provider` — never store master key on disk
- `zapdb_password` — always from HSM, never env var in production
- JWT tokens carry `iss` and `aud` claims — refresh tokens have separate audience

### KMS (~/work/lux/kms)

```yaml
# KMS runs on Hanzo Base framework
# All routes require superuser auth (e.HasSuperuserAuth())

server:
  addr: ":8080"

mpc:
  # Internal K8s DNS — MPC API in lux-mpc namespace
  url: "http://mpc-api.lux-mpc.svc.cluster.local:8081"
  # MPC_TOKEN must be set — never allow unauthenticated MPC communication

store:
  path: "/data/kms/keys.json"  # key metadata only, never plaintext keys
```

**Critical settings:**
- MPC_TOKEN must be non-empty — fail hard if missing
- All /v1/keys/* routes require superuser auth
- Org ID comes from JWT claims (`getOrgID(ctx)`), never from HTTP headers
- Key metadata is JSON (wallet IDs, public keys) — never stores private keys

### Base Vault SDK (~/work/hanzo/base/plugins/vault)

```go
vault.MustRegister(app, vault.Config{
    Enabled:     true,
    DataDir:     "/data/vaults",      // per-user .db files here
    OrgID:       "my-org",
    MasterKey:   masterKeyFromHSM,    // 32 bytes, NEVER on disk
    ChainRPC:    "http://node:9650/ext/bc/I",  // I-Chain for anchoring
    SyncEnabled: true,
    ZAPPort:     9999,
})
```

**Critical settings:**
- `MasterKey` — 32 bytes from HSM or K-Chain ML-KEM threshold unwrap
- `DataDir` — filesystem permissions 0700, encrypted volume recommended
- Per-user SQLite files are AES-256-GCM encrypted with derived DEKs
- DEK derivation: `HMAC-SHA256(orgKEK, "vault:user:" + userID)` — deterministic, no DB lookup
- CRDT ops carry ciphertext — sync peers cannot read plaintext
- Chain anchors are Merkle roots — chain never stores row data

## Key Hierarchy

```
Root of Trust (pick one):
├── Cloud HSM (AWS KMS / GCP Cloud KMS / Azure Key Vault)
│   └── Decrypt ZAPDB_ENCRYPTED_PASSWORD → Master KEK
├── K-Chain ML-KEM (threshold unwrap via T-Chain signers)
│   └── 3-of-5 signers reconstruct Master KEK
└── Hardware (Zymbit SCM / YubiHSM)
    └── PKCS#11 unwrap → Master KEK

Master KEK (32 bytes, never on disk)
├── Org KEK = HMAC-SHA256(master, "vault:org:" + orgID)
│   ├── User DEK = HMAC-SHA256(orgKEK, "vault:user:" + userID)
│   │   └── AES-256-GCM per entry (random 12-byte nonce)
│   ├── Shared Vault DEK = HMAC-SHA256(orgKEK, "vault:shared:" + vaultID)
│   └── Collection DEK = HMAC-SHA256(userDEK, "collection:" + name)
├── Device Key = HMAC-SHA256(userDEK, "device:" + deviceID)
│   └── Wraps user DEK for local storage on device
└── Recovery Shares = XOR split of user DEK (threshold reconstruction)
```

## Encryption Layers

| Layer | Algorithm | Purpose |
|-------|-----------|---------|
| Vault file | AES-256-GCM | Per-entry encryption in SQLite shard |
| ZapDB | ChaCha20-Poly1305 | MPC key share storage |
| Key wrapping | ML-KEM-768 (FIPS 203) | Post-quantum KEK distribution |
| Transport | ZAP + mTLS | Node-to-node encrypted communication |
| CRDT sync | Ciphertext relay | Peers see encrypted ops, not plaintext |
| Chain anchor | SHA-256 Merkle root | Commitment only, no data |

## Threat Model

| Threat | Mitigation |
|--------|-----------|
| Compromised user device | DEK zeroed on revocation; other users unaffected |
| Compromised sync relay | Relays see ciphertext only; cannot decrypt |
| Compromised single MPC node | Threshold requires 3-of-5; single node is useless |
| Quantum adversary | ML-KEM for key exchange; AES-256 is quantum-resistant |
| Chain state exposure | Chain stores Merkle roots and policy, never row data |
| Insider at cloud HSM | HSM only unwraps; never sees plaintext app data |
| RPC compromise | MaxBackingChangePct caps backing attestation delta at 15% |

## Production Checklist

- [ ] MPC in consensus mode (not legacy NATS)
- [ ] HSM provider configured (not env/file)
- [ ] MPC_TOKEN set (not empty)
- [ ] JWT issuer/audience validated
- [ ] Rate limiter uses RemoteAddr (not XFF)
- [ ] Body size limit enabled (1MB)
- [ ] KMS superuser auth enabled
- [ ] Org ID from JWT claims (not X-Org-ID header)
- [ ] Vault DataDir permissions 0700
- [ ] MasterKey from HSM (not hardcoded)
- [ ] Confirmation depth >= 6 for bridge watchers
- [ ] MaxBackingChangePct <= 15%
- [ ] All /v1/ routes (no /api/ prefix)
- [ ] No tenant-specific URLs in MPC config
- [ ] CRDT ops encrypted before sync
- [ ] Chain anchors are commitments only
```

## Non-Goals

- FHE as default execution (opt-in only for specific compute paths)
- All app data on-chain (chain is trust plane, not data plane)
- Single shared database (one shard per principal)
- Centralized key custody (threshold or self-custody)
