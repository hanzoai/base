# Configuration

Every command, flag and environment variable the `base` binary reads, taken
from the code. A Go program that embeds Base gets the root flags from
`base.New`, the `serve` flags, the core variables, and the variables of the
plugins it registers.

## Commands

| Command | What it does |
|---|---|
| `base serve [domain...]` | Runs the server. Naming domains turns on HTTPS with certificates from Let's Encrypt. |
| `base migrate up\|down [n]\|create <name>\|collections\|history-sync` | Applies, reverts or writes migrations. See [hooks](hooks.md#migrations). |
| `base update` | Replaces the binary with the latest GitHub release. No current release carries binaries, so it stops with `missing asset`. |
| `base cli <command>` | An HTTP client for a running Base: `collection`, `record`, `whoami`, `login`, `crons`, `daemon`, `status`, `rpc`, `config`, `self`, `cluster`, `operator`. |

## Root flags

These apply to every command.

| Flag | Default | Meaning |
|---|---|---|
| `--dir` | `$DATA_DIR`, else `base_/data` beside the path the binary was started by | Data directory. `./base` uses `./base_/data`; a `base` found on `PATH` uses `base_/data` in the working directory. |
| `--dev` | `false`, or `true` under `go run` | Log SQL statements and debug output. |
| `--encryptionEnv` | none | Name of an environment variable whose 32-character value encrypts stored settings. |
| `--queryTimeout` | `30` | Seconds before a `SELECT` is cancelled. |
| `--zap` | `$ZAP_ADDR`, else the HTTP host on `$ZAP_PORT` | ZAP listen address. See [listeners](listeners.md#zap). |
| `--mdns` | `$ZAP_MDNS`, else off | Advertise ZAP over mDNS and connect to the peers found. Never while ZAP listens on loopback. |
| `--no-mdns` | off | Keep mDNS off even when `--mdns` or `ZAP_MDNS` asks for it. |
| `--hooksDir` | `hooks` beside the data directory | JavaScript hooks. |
| `--hooksWatch` | `true` | Restart when a file under the hooks directory changes. Not on Windows. |
| `--hooksPool` | `15` | Interpreters kept for running hook handlers. |
| `--migrationsDir` | `migrations` beside the data directory | JavaScript migrations. |
| `--automigrate` | `true` | Write a migration when a superuser changes a collection over `/v1`. |
| `--publicDir` | `public` beside the binary | Parsed, but serves nothing: Base registers its own `GET /{path...}` route (the admin UI, or a `404` when the admin is off) before the binary checks for one. |
| `--indexFallback` | `true` | Parsed, but has no effect, for the same reason. |
| `-v`, `--version` | | Print the version. |

## serve flags

| Flag | Default | Meaning |
|---|---|---|
| `--http` | `127.0.0.1:8090`, or `0.0.0.0:80` when domains are named | HTTP address. While HTTPS is on, it only redirects and answers ACME challenges. |
| `--https` | none, or `0.0.0.0:443` when domains are named | HTTPS address. |
| `--origins` | `*` | Allowed CORS origins. |

## update flags

| Flag | Default | Meaning |
|---|---|---|
| `--backup` | `true` | Back up the data directory after replacing the binary. |

## cli flags

| Flag | Default | Meaning |
|---|---|---|
| `--url` | `$BASE_URL`, else `http://127.0.0.1:8090` | Base to call. |
| `--token` | `$BASE_TOKEN`, else the file `~/.config/base/token` (`$XDG_CONFIG_HOME/base/token` when set) | Token to send. Get it from IAM. |
| `--base` | none | Org to act in, sent as `X-Org-Id`. |
| `--format` | `table` on a terminal, else `json` | `table`, `json` or `yaml`. |
| `--mainnet` (`-m`), `--testnet` (`-t`), `--devnet` (`-d`), `--dev` | `$APP_ENV`, then `$BASE_ENV`, else local | Environment for `cluster`, `operator` and `status`. |

## Environment

A boolean accepts what Go's `strconv.ParseBool` accepts: `1`, `t`, `true`,
`TRUE` and their false counterparts. The exceptions are `S3_ENABLED` (`true` in
any case), `CLOUD_SQL_ENABLED` (exactly `true`) and `WAITLIST_OPEN` (`1`, `true`
or `yes` in any case).

### Core

| Variable | Default | Meaning |
|---|---|---|
| `DATA_DIR` | | Data directory when `--dir` is not given. |
| `BASE_API_PREFIX` | `/v1` | Where the API is mounted. `/v1/iam` and `/healthz` stay where they are. |
| `BASE_ENABLE_ADMIN_UI` | off | Serve the admin UI at `/`. It signs in through IAM. |
| `BASE_FRAME_ANCESTORS` | `'self'` | CSP `frame-ancestors` for the admin UI. |
| `IAM_DISPLAY_NAME` | empty | Display name of the `iam` provider in `auth-methods`. |
| `NATS_URL` | none | Publish record create, update and delete events to NATS. |
| `S3_ENABLED` | off | Store files in S3 rather than the data directory. |
| `S3_ENDPOINT`, `S3_BUCKET_NAME` (or `S3_BUCKET`), `S3_REGION`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_FORCE_PATH_STYLE` | | The S3 connection, read when `S3_ENABLED` is set. |
| `THUMBS_MAX_WORKERS` | CPUs + 2 | Thumbnails generated at once. |
| `THUMBS_MAX_WAIT` | `60` | Seconds a thumbnail request waits for a worker. |
| `FILES_DELETE_MAX_WORKERS` | `2000` | File deletions run at once. |
| `BASE_PRIVATE_MAX_TAG_BYTES` | `1048576` | Largest value in `/v1/private`, the per-user encrypted blob store. |
| `BASE_PRIVATE_MAX_USER_BYTES` | `104857600` | Total `/v1/private` bytes per user. |
| `BASE_GOJAVM_POOL_SIZE` | `8` | Interpreters for `/v1/functions`, which is how many functions run at once. |
| `TASKS_EMBED` | off | Run the Tasks engine inside Base. See [listeners](listeners.md#embedded-tasks). |
| `TASKS_EMBED_ADDRESS` | `<data dir>/tasks.sock` | Where the embedded engine listens. |
| `TASKS_NAMESPACE` | `default` | Namespace of the embedded engine. |
| `TASKS_ZAP` | none | A Tasks server to use over ZAP. |
| `TASKS_URL` | none | A Tasks server to use over HTTP. With no Tasks server, scheduled jobs run on timers inside the process. |

### ZAP

| Variable | Default | Meaning |
|---|---|---|
| `ZAP_ADDR` | none | ZAP listen address. `--zap` wins over it. |
| `ZAP_PORT` | `9999` | Port used on the HTTP host when no address is given. |
| `ZAP_MDNS` | off | The same as `--mdns`. |
| `ZAP_DISABLED` | off | Do not start ZAP. |

### IAM, orgs and KMS

The binary registers `plugins/org` when `IAM_ENDPOINT` or `IAM_URL` is set.

| Variable | Default | Meaning |
|---|---|---|
| `IAM_ENDPOINT` (or `IAM_URL`) | none | IAM origin. Turns on IAM sign-in and a Base per org. |
| `IAM_ADDRESS` | none | Where Base reaches the IAM service to check tokens and keys, when that is not `IAM_ENDPOINT`. A bare `host:port` is ZAP. |
| `IAM_CLIENT_ID`, `IAM_CLIENT_SECRET` | none | The IAM application Base presents when it calls IAM itself, and to KMS over HTTPS. |
| `KMS_ENDPOINT` (or `KMS_URL`) | none | Secrets store: `host:port`, `zap://host:port`, `https://...`, or `zap+mdns://_kms._tcp` to discover it. Contacted on first use. |
| `LUX_MNEMONIC` (or `MNEMONIC`) | none | Seed of the identity Base signs KMS requests with. Required for KMS over ZAP. |
| `KMS_ENV` | `prod` | Environment of the secrets Base reads. |
| `KMS_SERVICE_PATH` | `hanzo/base` | Derivation path of that identity. |
| `KMS_NODE_ID` | `hanzo-base` | Node name the KMS client presents. |
| `IDV_ENDPOINT` | none | Upstream for `/v1/idv/*`. Unset, `/v1/idv/status` answers `{"enabled":false}` and the rest `503`. |

### Replication

| Variable | Default | Meaning |
|---|---|---|
| `BASE_NETWORK` | `standalone` | `quasar` replicates writes to peers. |
| `BASE_PEERS` | none | Peers as comma-separated `host:port`. Replication listens only when this names one. |
| `BASE_SHARD_KEY` | none | Required with `quasar`: a field of the verified identity such as `user_id` or `org_id`, or `header:<Name>`. |
| `BASE_REPLICATION` | `1` | Members that hold each shard. |
| `BASE_NODE_ID` | `$HOSTNAME` | This member's id. Required with `quasar`. |
| `BASE_NODE_ROLE` | `validator` | `validator` or `archive`. |
| `BASE_ARCHIVE` | `off` | `s3://bucket/prefix` to archive frames. |
| `BASE_LISTEN_P2P` | `:9999` | Peer listener, bound exactly as written. The port must be a number. |
| `BASE_LISTEN_HTTP` | `:8090` | Read, but opens nothing. |
| `BASE_PEER_HTTP_ENDPOINTS` | none | Comma-separated `peer=url` pairs: where to send a write that a peer owns. |
| `BASE_PEER_HTTP_PORT` | `8090` | HTTP port assumed for a peer missing from that list. |
| `AWS_REGION`, `AWS_ENDPOINT_URL`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` | | Credentials for an `s3://` archive. |
| `BASE_ENCRYPT`, `BASE_TLS_CA`, `BASE_TLS_SERVER_CERT`, `BASE_TLS_SERVER_KEY`, `BASE_TLS_ALLOWED_SANS` | | Refused. Setting any of them stops startup with an explanation. |

### Plugins in the binary

The waitlist plugin is always registered and serves `/v1/waitlist/*`.

| Variable | Default | Meaning |
|---|---|---|
| `TURNSTILE_SECRET_KEY` | none | Cloudflare Turnstile secret for checking joins. |
| `WAITLIST_ADMIN_SECRET` | none | Secret for `boost` and `export`. |
| `WAITLIST_AWARD_SECRET` | none | Secret for `award`. |
| `WAITLIST_DEFAULT_SLUGS` | none | Comma-separated waitlists to create at startup. |
| `WAITLIST_ACCESS_CAPACITY` | `0` | Grant access to the top N entries by rank. |
| `WAITLIST_OPEN` | off | Give everyone access. |
| `POINTS_REFERRAL`, `POINTS_SHARE`, `POINTS_INVITE_SENT`, `POINTS_INVITE_CONVERTED`, `POINTS_SOCIAL`, `POINTS_HANZOD`, `POINTS_SIGNUP` | `10`, `2`, `1`, `5`, `15`, `25`, `0` | Points per event. |

The other three are off unless enabled.

| Variable | Default | Meaning |
|---|---|---|
| `BOOTNODE_ENABLED` | off | Serve the bootnode developer platform. |
| `BOOTNODE_API_KEY_SALT` | none | Salt for issued API keys. Required when `IAM_URL` is not a local host. |
| `BOOTNODE_ALLOWED_ORGS` | `hanzo`, `zoo`, `lux`, `pars` | IAM orgs allowed to sign in. |
| `BOOTNODE_K8S_NAMESPACE` | `bootnode` | Namespace for the resources it applies. |
| `IAM_URL`, `IAM_CLIENT_ID`, `IAM_CLIENT_SECRET` | | The IAM application for its OAuth2 code exchange. |
| `FRONTEND_URL` | `http://localhost:3001` | Redirect base when a request names none. |
| `COMMERCE_URL`, `COMMERCE_API_KEY` | none | Billing client. An empty key turns billing off. |
| `KUBE_APISERVER`, `KUBE_TOKEN`, `KUBE_INSECURE_SKIP_TLS_VERIFY` | | Kubernetes API, when not running in a cluster. |
| `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT` | | Kubernetes API from inside a cluster. |
| `CALENDAR_ENABLED` | off | Serve the booking API at `/v1/calendar`. |
| `CALENDAR_SEED_HANDLE` | none | Create a bookable host with this handle at startup. |
| `CALENDAR_SEED_OWNER`, `CALENDAR_SEED_TZ`, `CALENDAR_SEED_TITLE`, `CALENDAR_SEED_LOCATION` | the handle, `America/Los_Angeles`, `Intro`, `https://meet.hanzo.ai/<handle>` | That host's details. |
| `CLOUD_SQL_ENABLED` | off | Serve `/v1/cloud-sql` and `/v1/meta`. |
| `CLOUD_SQL_META_URL`, `CLOUD_SQL_COMPUTE_HOST`, `CLOUD_SQL_PG_USER`, `CLOUD_SQL_PG_PASS` | none | The managed PostgreSQL service it drives. |

### Plugins you link yourself

The binary does not include these. A program that registers one reads its
variables.

| Plugin | Variables |
|---|---|
| `plugins/ha` | `BASE_LOCAL_TARGET`, `BASE_PEERS`, `BASE_STATIC_WRITER`, `BASE_NODE_ID` |
| `plugins/replicate` | `REPLICATE_S3_ENDPOINT`, `REPLICATE_S3_PATH` |
| `plugins/tasks` | `TASKS_ENABLED` (or `HANZO_TASKS_ENABLED`), `TASKS_ADDRESS` (or `HANZO_TASKS_ADDRESS`), `TASKS_NAMESPACE` (or `HANZO_TASKS_NAMESPACE`), `TASKS_QUEUE`, `TASKS_WORKER` |
| `plugins/pyvm`, `plugins/v8vm`, `plugins/wasmvm`, `plugins/starkvm` | `BASE_PYVM_POOL_SIZE`, `BASE_V8VM_POOL_SIZE`, `BASE_WASMVM_POOL_SIZE`, `BASE_STARKVM_POOL_SIZE` |

### Read by a dependency

| Variable | Meaning |
|---|---|
| `HANZO_SQLITE_RAMFS_DIR` | Where the pure-Go encrypted SQLite codec keeps its temporary plaintext copy, when this directory is RAM-backed (on macOS, a `tmpfs` mount). Otherwise the codec uses `/dev/shm`, and failing that the OS temp directory. The copy is deleted on close. |
| `HOSTNAME` | Default replication node id. |

### The cli command

| Variable | Meaning |
|---|---|
| `BASE_URL`, `BASE_TOKEN` | Defaults for `--url` and `--token`. |
| `XDG_CONFIG_HOME` | Where the token file and `base/config.json` live. |
| `APP_ENV`, `BASE_ENV` | Default environment when no environment flag is given. |
