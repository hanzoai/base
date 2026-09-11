# Security Policy

## Reporting a vulnerability

Email security@hanzo.ai with details.

We respond within 48 hours. Critical issues receive same-day acknowledgment.

## Scope

This policy covers code in this repository. What Base defends on its own, and
what it leaves to a deployment, is in [docs/threat-model.md](docs/threat-model.md).

## Isolation

With `IAM_ENDPOINT` set, each org's data is its own SQLite file,
`<data dir>/orgs/<org>/data.db`, so a query in one org cannot read another's
rows. The `base` binary opens those files unencrypted: encryption needs a master
key from KMS, which the org plugin reads only when it is given an `IAMOrg`.

JavaScript hooks run inside the Base process with its full authority. They are
not sandboxed.
