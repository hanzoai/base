    # Blog posts bundle

    ## Post 1: #OwnYourData

    For years, the cloud usually meant one company owned the database,
    one company owned the login system, and one company decided how hard it was to leave.

    The next model is better.

    Keep app state in an encrypted local vault.
    Sync it privately.
    Anchor trust on-chain.
    Make identity portable.

    That is what **#OwnYourData** means.

    ## Post 2: Stop Putting Your App Database On-Chain

    The chain should be the trust kernel:
    identity, key policy, anchors, receipts, provider registry, settlement.

    The database should stay local:
    encrypted, fast, offline, synced, portable.

    ## Post 3: Every User Deserves a Vault

    Each user should have a durable private home for:
    - notes
    - files
    - messages
    - credentials
    - AI memory
    - preferences
    - application state

    Not as a feature bolted onto SaaS, but as the default app architecture.

    ## Post 4: Firebase Was the Prototype. Web5 Is the Successor.

    Firebase proved developers want:
    - auth
    - storage
    - sync
    - functions
    - easy iteration

    Web5 keeps the ergonomics and changes the trust model.

    ## Post 5: Agents Need Wallets, Vaults, and Boundaries

    AI agents need:
    - private memory
    - scoped capabilities
    - spend rules
    - threshold approvals
    - portable execution
    - recoverable identity

    Otherwise they are just scripts with credit cards.

    ## Post 6: The Decentralized Cloud Is a Market, Not a Company

    Sync, storage, recovery, indexing, and gateways should be open provider classes.
    Apps should choose service quality, not surrender ownership.

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
