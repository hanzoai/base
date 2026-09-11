# Network listeners

Every socket a Base process can listen on: what opens it, where it binds, and
how to change that. A `serve` with no flags and no environment opens two, both
on loopback:

```sh
./base serve
lsof -nP -a -p <pid> -iTCP -sTCP:LISTEN
```

```
COMMAND   PID USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
base    62716    z   18u  IPv4 0x570514825a9f5e0d      0t0  TCP 127.0.0.1:8090 (LISTEN)
base    62716    z   19u  IPv4 0x26859a4d8573f408      0t0  TCP 127.0.0.1:9999 (LISTEN)
```

| Listener | Opens when | Binds by default | Change with |
|---|---|---|---|
| HTTP | `serve` | `127.0.0.1:8090`; `0.0.0.0:80` when domains are named | `--http` |
| HTTPS | `--https`, or domains named | `0.0.0.0:443` when domains are named | `--https` |
| ZAP | `serve`, unless `ZAP_DISABLED=true` | the HTTP host, port `9999` | `--zap`, `ZAP_ADDR`, `ZAP_PORT` |
| mDNS | `--mdns` or `ZAP_MDNS=true`, and ZAP is not on loopback | UDP `5353` on every multicast interface | `--no-mdns` keeps it off |
| Replication | `BASE_NETWORK=quasar` and at least one entry in `BASE_PEERS` | `:9999`, every interface | `BASE_LISTEN_P2P` |
| Embedded Tasks | `TASKS_EMBED=true` | unix socket `<data dir>/tasks.sock` | `TASKS_EMBED_ADDRESS` |
| KMS discovery | `KMS_ENDPOINT=zap+mdns://...`, at the first secret read | a port the OS picks, on every interface, plus mDNS | a `host:port` endpoint, which dials instead |

## HTTP and HTTPS

`serve` listens on `--http`, which is `127.0.0.1:8090` unless you say otherwise.
Name domains (`base serve example.com`) and the defaults become `0.0.0.0:80`
and `0.0.0.0:443`, with certificates from Let's Encrypt cached in
`<data dir>/.autocert_cache`. `--https <address>` turns HTTPS on without naming
domains; the certificate is then for the host in that address. While HTTPS is
on, the HTTP address only redirects to HTTPS and answers ACME challenges.

The API is under `/v1` (`BASE_API_PREFIX` moves it) and `GET /healthz` answers
at the root. With replication on, `GET /-/base/members` also answers, with no
credential required. The admin UI is served at `/` only when
`BASE_ENABLE_ADMIN_UI=1`.

## ZAP

ZAP is a binary protocol. The ZAP listener serves the same handler as HTTP: the
same routes, middleware, IAM checks and rules. It adds a port, not a way around
any of them.

- The address is `--zap`, else `ZAP_ADDR`, else the HTTP listener's host on
  `ZAP_PORT` (default `9999`). `--http 127.0.0.1:8090` gives `127.0.0.1:9999`;
  `--http :8090` gives `:9999`, which is every interface. With HTTPS on, the
  host comes from the HTTPS address.
- An address that starts with `/`, `./`, `../` or `@` is a unix socket.
- If the address cannot be read or cannot be bound, Base logs an error and
  keeps serving HTTP.
- The container image runs `serve --http=0.0.0.0:8090`, so inside a container
  ZAP listens on `0.0.0.0:9999`. Publish only the ports you mean to.
- `ZAP_DISABLED=true` turns it off.

## mDNS

mDNS is off unless `--mdns` or `ZAP_MDNS=true` asks for it, and it stays off
when ZAP listens on loopback or a unix socket. `--no-mdns` keeps it off
whatever else is set. When it runs, Base joins the multicast groups
`224.0.0.251` and `ff02::fb` on UDP port 5353 on every multicast-capable
interface, advertises `_hanzo-base._tcp` under the host name, and connects to
the peers it finds.

## Replication

`BASE_NETWORK=quasar` replicates SQLite writes to the peers in `BASE_PEERS`.
With no peers it opens nothing.

- The peer listener binds `BASE_LISTEN_P2P` exactly as written. The default,
  `:9999`, is every interface; `127.0.0.1:9999` is loopback. The port must be a
  number: `BASE_LISTEN_P2P=127.0.0.1` stops startup with
  `missing port in address`.
- Startup also stops without `BASE_SHARD_KEY`, or without `BASE_NODE_ID` when
  `$HOSTNAME` is unset.
- Peers present no certificate. Whatever can reach this port can submit frames
  for the shards this node owns, so `BASE_PEERS` and your network policy are the
  whole trust boundary. [NETWORK.md](NETWORK.md) has the design.
- ZAP defaults to port 9999 as well. Replication starts first, so when both land
  on one address, ZAP is the one that logs `address already in use` and stays
  off. Give one of them another port.
- `BASE_LISTEN_HTTP` is read but opens nothing.

## Embedded Tasks

`TASKS_EMBED=true` runs the Tasks engine inside Base and listens for it at
`TASKS_EMBED_ADDRESS`, by default `<data dir>/tasks.sock`. An address that starts
with `/`, `./`, `../` or `@` is a unix socket; anything else is `host:port`. When
the engine cannot listen, Base logs nothing and runs scheduled jobs on timers
inside the process. That happens when:

- the address is a relative path without `./`. The default is one whenever the
  data directory is relative, as the default `base_/data` is, and it is then
  read as `host:port`;
- the socket's directory does not exist yet. Base starts the engine before it
  creates the data directory, so the default address fails on the first start
  with a new data directory;
- the path is too long for a unix socket. Keep it well under 100 bytes.

A short absolute `TASKS_EMBED_ADDRESS` in a directory that already exists avoids
each of these.

## KMS

The KMS client does nothing until Base first reads a secret. A `host:port` or
`zap://host:port` endpoint then dials that address and listens on nothing, and
an `https://` endpoint uses HTTPS. `zap+mdns://_kms._tcp` is the exception: to
find KMS, it starts a ZAP node that listens on a port the OS picks, on every
interface, and runs mDNS.

## Connections Base makes but never accepts

- IAM: HTTPS to `IAM_ENDPOINT` for signing keys and for `/v1/iam/*`, and
  `IAM_ADDRESS` when set.
- Tasks: `TASKS_ZAP` over ZAP or `TASKS_URL` over HTTP. The client dials and
  does not listen.
- NATS: `NATS_URL`, to publish record events.
- S3 for files (`S3_*`) and for the replication archive (`BASE_ARCHIVE`), SMTP
  when mail is configured, and whatever a hook calls with `$http.send`.
- Replication: the peers in `BASE_PEERS`, and the HTTP address of the peer that
  owns a write.

## Checking a running Base

`lsof -nP -a -p <pid> -i` lists every socket a process holds, listening or not.
