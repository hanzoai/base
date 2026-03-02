    # Building Blocks and Open Questions

    ## Stable building blocks

    These now look reasonably stable:
    - local-first data ownership as an app principle
    - SQLite as a local durable store
    - CRDT-based merge patterns
    - DID / VC identity foundations
    - passkeys for device-bound sign-in
    - ML-KEM and ML-DSA as PQ standards
    - FROST as a threshold-signing standard
    - IPFS/Filecoin-style separations between content addressing and incentivized persistence
    - appchain / specialized-L1 deployment models

    ## Frontier building blocks

    These are becoming viable but still need careful product boundaries:
    - threshold FHE / conditional reveal
    - CKKS-backed confidential analytics or inference
    - encrypted provider markets
    - provider portability with strong receipts
    - AI agent memory vaults

    ## The hard unsolved problems

    1. Private indexing and search over encrypted local-first state
    2. Selective sharing inside encrypted relational / document structures
    3. Revocation without device or team chaos
    4. Metadata leakage in sync topologies
    5. Human-friendly threshold UX
    6. Reputation systems for providers without recreating surveillance
    7. Cross-app composition without recentralizing identity or data
    8. Practical encrypted inference that feels like a product, not a demo

    ## Recommendation for Lux / Hanzo

    Stay conservative on the default path:
    - local vault
    - symmetric crypto
    - PQ wrapping
    - capability policy
    - threshold signing
    - threshold reveal only where truly needed

    Be ambitious at the edges:
    - private agent memory
    - org vaults with threshold governance
    - encrypted workflows and recovery
    - portable cloud/app operators
    - selective confidential compute

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
