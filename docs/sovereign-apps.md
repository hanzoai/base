    # The Sovereign App Manifesto

    Software should not require surrender.

    Users should not have to trade ownership for convenience,
    privacy for sync, or portability for collaboration.

    The cloud gave us reach but took away control.
    Blockchains gave us trust minimization but often degraded usability.
    The synthesis is Web5:

    > **Put trust on-chain, keep state local, sync privately, make identity portable.**

    ## The new default

    A sovereign app is:
    - local-first
    - encrypted by default
    - portable across providers
    - offline-capable
    - collaborative without central ownership
    - auditable without exposing private state
    - programmable through cryptographic policy

    ## The architecture

    A modern sovereign app has two planes.

    ### Data plane
    - encrypted SQLite vaults
    - CRDT operation logs
    - local execution
    - optional blob/snapshot storage

    ### Trust plane
    - portable identities
    - key handles and wrapping policy
    - capability grants and revocations
    - threshold approvals and reveals
    - chain anchors and receipts
    - provider registration and settlement

    ## Why now

    This architecture only became realistic after several research and standards waves converged:
    - local-first software crystallized a compelling app model
    - CRDTs became operationally practical
    - DID and VC standards made portable identity legible
    - passkeys normalized device-bound credentials
    - NIST finalized PQ standards
    - FROST gave threshold signing a stable spec
    - FHE libraries became usable enough for selective workloads
    - appchains/L1s made chain specialization economically and technically viable

    ## What this replaces

    The old SaaS stack bundled:
    - your data model
    - your auth model
    - your recovery model
    - your provider dependency

    The sovereign stack unbundles them.

    Users own state.
    Chains own trust.
    Providers sell service quality.
    Developers own product logic.

    ## The promise

    Every user should have a vault.
    Every org should control policy.
    Every agent should have memory and scoped rights.
    Every app should be able to survive a provider change.

    That is not “everything on-chain.”
    That is **trust as a shared utility, state as a sovereign asset.**

    ## Source notes

Primary and standards-oriented sources used throughout these documents:

- Kleppmann et al., *Local-First Software: You Own Your Data, in spite of the Cloud* (2019) — https://martin.kleppmann.com/papers/local-first.pdf
- W3C DID Core 1.0 Recommendation — https://www.w3.org/TR/did-1.0/
- W3C Verifiable Credentials Data Model 2.0 — https://www.w3.org/TR/vc-data-model-2.0/
- NIST FIPS 203 (ML-KEM) — https://csrc.nist.gov/pubs/fips/203/final
- NIST FIPS 204 (ML-DSA) — https://csrc.nist.gov/pubs/fips/204/final
- RFC 9591 FROST — https://www.rfc-editor.org/rfc/rfc9591.pdf
- Zama TFHE-rs docs — https://docs.zama.org/tfhe-rs
- Zama threshold-fhe repository — https://github.com/zama-ai/threshold-fhe
- SQLite one-file database — https://sqlite.org/onefile.html
- SQLite appropriate uses — https://sqlite.org/whentouse.html
- Filecoin/IPFS docs — https://docs.filecoin.io/basics/how-storage-works/filecoin-and-ipfs and https://docs.ipfs.tech/concepts/faq/
- Automerge 3.0 announcement — https://automerge.org/blog/automerge-3/
- Avalanche L1 docs — https://build.avax.network/docs/avalanche-l1s
- FIDO Alliance passkeys pages / adoption research — https://fidoalliance.org/passkeys/ and https://fidoalliance.org/passkey-adoption-doubles-in-2024-more-than-15-billion-online-accounts-can-leverage-passkeys/
