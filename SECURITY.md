# Security Policy

## Reporting a vulnerability

Email security@hanzo.ai with details. Encrypt with our PGP key (fingerprint TBD).

We respond within 48 hours. Critical issues receive same-day acknowledgment.

## Scope

This policy covers code in this repository. For the broader Hanzo platform threat model, see [hanzoai/HIPs](https://github.com/hanzoai/HIPs).

## Sandbox boundary

`base` enforces tenant isolation at the storage layer: each org gets its own per-tenant data file (`data/{orgSlug}.db`) with a per-org HKDF-derived DEK from KMS, and the WAL is shipped to age-encrypted object storage via `hanzoai/replicate`. User-supplied per-record validators, computed fields, and access rules execute exclusively inside the HIP-0105 in-process runtimes (goja, wazero, pyvm, starkvm) — never in the host Go process directly.

For runtime sandbox guarantees, see HIP-0105 (in-process extension runtimes).
