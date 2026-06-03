# Hanzo Base KMS Plugin

Zero-knowledge encrypted secret management for [Hanzo Base](https://github.com/hanzoai/base) with FHE query support.

## What It Does

The KMS plugin adds transparent field-level encryption to any Base collection. Configured fields are encrypted before database writes and decrypted after reads, making encryption invisible to the application layer. FHE-encrypted indexes allow equality queries on encrypted data without decryption.

The plugin connects to the [TFHE-KMS MPC cluster](https://github.com/hanzoai/kms) — a distributed key management system using Shamir secret sharing, HPKE key wrapping, and AES-256-GCM encryption. No single node ever holds the complete encryption key.

## Quick Start

```go
package main

import (
    "github.com/hanzoai/base"
    "github.com/hanzoai/base/plugins/kms"
)

func main() {
    app := base.New()

    kms.MustRegister(app, kms.Config{
        Nodes:     []string{"https://kms-mpc-0:9999", "https://kms-mpc-1:9999", "https://kms-mpc-2:9999"},
        OrgSlug:   "my-org",
        Threshold: 2,
        EncryptedCollections: map[string][]string{
            "credentials": {"api_key", "api_secret"},
            "tokens":      {"access_token", "refresh_token"},
        },
        FHESearchable: map[string][]string{
            "credentials": {"name"},
        },
    })

    app.Start()
}
```

## Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `Nodes` | `[]string` | Yes | MPC node ZAP wire addresses |
| `OrgSlug` | `string` | Yes | Organization identifier for key derivation |
| `Threshold` | `int` | Yes | Shamir threshold (t-of-n) |
| `Enabled` | `bool` | No | Enable/disable plugin (default: true) |
| `AutoUnlock` | `bool` | No | Unlock on startup (dev only) |
| `Passphrase` | `string` | No | Bootstrap passphrase (dev only, use KMS_PASSPHRASE env in prod) |
| `EncryptedCollections` | `map[string][]string` | No | Collection -> fields to encrypt |
| `FHESearchable` | `map[string][]string` | No | Collection -> fields with FHE indexes |

## Encrypted Collections

Fields listed in `EncryptedCollections` are transparently encrypted with AES-256-GCM before storage. Each encrypted value is stored with the prefix `enc:v1:` followed by base64-encoded ciphertext.

Encryption uses Additional Authenticated Data (AAD) scoped to `org:collection:record_id`, preventing ciphertext from being transplanted between records.

```go
EncryptedCollections: map[string][]string{
    "credentials": {"api_key", "api_secret"},
    "wallets":     {"private_key", "mnemonic"},
}
```

## FHE Search

Fields listed in `FHESearchable` maintain a deterministic HMAC-SHA256 index in a synthetic `_fhe_{field}` column. This allows equality queries without decrypting the underlying data.

Index derivation:
```
field_key = HMAC-SHA256(CEK, org_slug + ":" + collection + ":" + field)
index     = HMAC-SHA256(field_key, plaintext_value)
```

FHE fields must also appear in `EncryptedCollections`.

```go
// Query encrypted data by name without decrypting:
p := app.Store().Get("kms").(*kms.Plugin)
token, _ := p.ComputeSearchToken("credentials", "name", "my-api-key")
records, _ := app.FindAll("credentials", "_fhe_name = ?", token)
```

## Compliance Modes

The MPC cluster supports regulatory compliance enforcement. When enabled, all secret access is logged to a WORM (Write Once Read Many) hash-chained audit trail.

| Mode | Description | Retention | Requirements |
|------|-------------|-----------|--------------|
| `HIPAA` | Healthcare PHI protection | 6+ years | Break-glass, WORM audit |
| `SEC` | SEC/FINRA ATS/BD/TA | 6+ years (17a-4) | WORM audit, escrow |
| `FINRA` | Broker-dealer examination | 6+ years | WORM audit, escrow |
| `SOX` | Sarbanes-Oxley | 7+ years | WORM audit |
| `GDPR` | EU data protection | Configurable | WORM audit |

Compliance is configured on the MPC node, not the Base plugin. The plugin inherits the compliance posture of its connected cluster.

## API Endpoints

All endpoints require superuser authentication.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/kms/secrets` | Create an encrypted secret |
| `GET` | `/v1/kms/secrets/{key}` | Retrieve and decrypt a secret |
| `DELETE` | `/v1/kms/secrets/{key}` | Delete a secret |
| `GET` | `/v1/kms/secrets` | List all secret names |
| `POST` | `/v1/kms/unlock` | Unlock the cluster (derive CEK from passphrase) |
| `POST` | `/v1/kms/lock` | Lock the cluster (zero CEK from memory) |
| `POST` | `/v1/kms/invite` | Wrap CEK for a new member's HPKE public key |
| `POST` | `/v1/kms/sync` | Trigger CRDT sync across MPC nodes |
| `GET` | `/v1/kms/status` | Health status of all MPC nodes |

## Enterprise Features

Enterprise features are configured on the MPC node (`EnterpriseConfig`):

- **Multi-region replication** — Configure primary/replica regions for geographic redundancy
- **Key rotation policy** — Automatic CEK rotation with configurable interval and advance notification
- **IP allow-lists** — Restrict access by CIDR range
- **MFA enforcement** — Require MFA for secret access
- **Session timeout** — Auto-lock CEK after inactivity
- **Audit sinks** — Fan-out WORM audit entries to GCP Cloud Logging, AWS CloudWatch, Azure Monitor, S3, GCS, or custom webhooks
- **HSM integration** — AWS CloudHSM, PKCS#11, YubiHSM for root key protection
- **KMIP** — Key Management Interoperability Protocol for enterprise key lifecycle

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  Application (Hanzo Base)                                     │
│  ┌─────────────────────┐                                      │
│  │  KMS Plugin          │                                     │
│  │  • encrypt/decrypt   │  REST API (/v1/kms/*)              │
│  │  • FHE index         │                                     │
│  │  • record hooks      │                                     │
│  └────────┬────────────┘                                      │
└───────────┼──────────────────────────────────────────────────┘
            │ ZAP envelope (TLS + HPKE)
            ▼
┌───────────────────────────────────────────────────────────────┐
│  TFHE-KMS MPC Cluster (t-of-n Shamir)                           │
│                                                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│  │ MPC Node │  │ MPC Node │  │ MPC Node │  ...               │
│  │  ZapDB   │  │  ZapDB   │  │  ZapDB   │                    │
│  │  Shard   │  │  Shard   │  │  Shard   │                    │
│  └──────────┘  └──────────┘  └──────────┘                    │
│       │              │              │                          │
│       └──────────────┼──────────────┘                         │
│              FHE CRDT Sync                                    │
│                                                               │
│  Compliance: WORM Audit │ Escrow │ Break-Glass │ Retention    │
│  Enterprise: HSM │ KMIP │ Audit Sinks │ Multi-Region          │
└───────────────────────────────────────────────────────────────┘
```

## Chain-Agnostic Design

The KMS is chain-agnostic. Any application — Lux L1/L2, exchange platforms, or standard web services — connects via the standard ZAP envelope and REST APIs. There is no chain-specific configuration.

Use cases across different deployments:
- **Blockchain nodes**: validator keys, staking credentials
- **Exchange platforms**: wallet keys, API keys, trading credentials, compliance records
- **Web applications**: API secrets, database credentials, encryption keys
- **Compliance-regulated services**: ATS/BD/TA records, PHI, financial controls
