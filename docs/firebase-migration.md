    # Firebase to Web5
    ## Migration paper

    ## Surface mapping

    | Firebase | Web5 / Lux / Base equivalent |
    |---|---|
    | Auth | DID + passkey-friendly device identity + capability sessions |
    | Firestore / Realtime DB | Encrypted SQLite vault + CRDT log |
    | Cloud Storage | Encrypted blob providers |
    | Cloud Functions | Worker/event layer on vault, sync, or anchor events |
    | Security Rules | Capability policy + chain receipts |
    | Hosting lock-in | Provider portability + local-first execution |

    ## Migration strategy

    1. Add a local vault and make the client authoritative for immediate writes.
    2. Introduce CRDT sync and snapshot/export.
    3. Replace auth with device-rooted keys, capabilities, and optional credentials.
    4. Move recovery and key wrapping into the trust layer.
    5. Add provider choice and anchor receipts.
    6. Add threshold and confidential features only where product value is obvious.

    ## The critical product rule

    Do not make developers think they are adopting a blockchain database.
    The migration should feel like adopting a better backend/runtime.

    ## The developer promise

    - fast local reads
    - offline support
    - portable auth and recovery
    - replaceable providers
    - collaborative sync
    - auditable trust boundaries

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
