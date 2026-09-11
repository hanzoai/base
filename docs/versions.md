# Versions and upgrading

## What each tag serves

Tag numbers do not run in a straight line: two lines of work were tagged in the
same range. This is what each range serves, read from its code.

| Tags | Default API prefix | Sign-in |
|---|---|---|
| `v0.36.7-hanzo.1` to `v0.39.9` | `/api` | passwords, `base superuser` |
| `v0.39.10`, `v0.39.11` | `/v1/base` | passwords, `base superuser` |
| `v0.39.12`, `v0.39.13` | `/api` | passwords, `base superuser` |
| `v0.39.14` to `v0.39.17` | `/v1` | Hanzo IAM |
| `v0.40.0` to `v0.52.3` | `/api` | passwords, `base superuser` |
| `v1.0.0` | `/api` | Hanzo IAM |
| `v1.1.0` and later | `/v1` | Hanzo IAM |

From `v0.39.10` on, `BASE_API_PREFIX` overrides the prefix. On a password tag,
superusers and users sign in at
`<prefix>/collections/<collection>/auth-with-password`. IAM tags have no such
route.

The default data directory's name differs between tags too (`hz_data`, `data`,
`base_/data`), and so do the hooks and migrations directories beside it. Current
versions use `base_/data`. Pass `--dir`, `--hooksDir` and `--migrationsDir` when
you upgrade, and the names stop mattering.

## Where to get a version

| Channel | What is there |
|---|---|
| Go module | Tagged versions of `github.com/hanzoai/base`; `go list -m -versions github.com/hanzoai/base` lists them. |
| Source | `git clone --depth 1 --branch <tag> https://github.com/hanzoai/base`, then `CGO_ENABLED=0 go build` in `examples/base`. |
| Container images | [`ghcr.io/hanzoai/base`](https://github.com/hanzoai/base/pkgs/container/base), tagged with the version, with or without the `v`. Not every git tag has an image, so check before you pin. `latest`, `main` and `dev` are old builds. |
| GitHub releases | Binaries only for `v0.36.7-hanzo.1`, which serves `/api`. Later releases have none, so `base update` stops with `missing asset`. |

`go install github.com/hanzoai/base/examples/base@<version>` does not work: the
module's `go.mod` has a `replace` directive, and `go install` refuses those.

## Moving from /api and passwords to /v1 and IAM

A current Base opens an older data directory in place and migrates it when it
starts. Data written by `v0.36.7-hanzo.1` comes through with its collections,
rules and records.

**Paths.** Routes move from `/api/...` to `/v1/...`, and the old paths answer
`404`. The JavaScript client adds the prefix itself, so give it the origin:

```ts
import { BaseClient } from '@hanzo/base'

const base = new BaseClient('http://127.0.0.1:8090')
const page = await base.collection('notes').getList(1, 20)
```

Give it `http://127.0.0.1:8090/v1` instead and every call goes to
`/v1/v1/...` and fails with `404`. If Base is mounted under a longer prefix, set
the same value in both places: `BASE_API_PREFIX` on the server and `prefix` on
the client.

**Sign-in.** Password, OTP and OAuth2 sign-in are gone, and so is
`base superuser`. On first start the migrations drop the `password` column of
`_superusers` and the `_mfas`, `_otps`, `_externalAuths` and `_authOrigins`
tables. People sign in at Hanzo IAM and send its access token, and superusers
are the members of IAM's `admin` org. Set `IAM_ENDPOINT`; [rules.md](rules.md)
covers the rest.

**Tokens from before the upgrade.** On its first start, `v1.5.98` or later
replaces the secrets superuser tokens are signed with, so a superuser token from
an older version, `v0.36.7-hanzo.1` included, gets `401`. `auth-refresh` renews
no superuser token that IAM did not issue. A `users` token Base signed keeps
working until `IAM_ENDPOINT` is set, which makes Base ignore every token IAM did
not sign.

**Ids in rules.** Rules carry over as written, but `@request.auth.id` now comes
from the IAM token (see [rules.md](rules.md#where-identity-comes-from)). A rule
such as `owner = @request.auth.id` matches only records whose `owner` holds the
id derived from that token. Old `users` rows stay, and nobody signs in as them.

**Hooks and migrations.** They load the same way. A hook file that calls a
removed sign-in hook, such as `onRecordAuthWithPasswordRequest`, fails to load,
and the log says so on the first start.

## Upgrading safely

Copy the data directory, run the new binary against the copy with `--dir`, and
check your collections, rules and hooks before you switch. `base update` cannot
do this for you while releases carry no binaries.
