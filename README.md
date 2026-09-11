<p align="center"><img src=".github/hero.svg" alt="Base" width="880"></p>

# Base

Base is a backend in one Go binary: collections of records behind a REST API at
`/v1`, with realtime updates, files, per-collection access rules and JavaScript
hooks, stored in SQLite. Identity comes from Hanzo IAM, and with IAM on, each
org's data lives in a database file of its own.

## Run it

You need Go 1.26.8 or newer.

```sh
git clone --depth 1 https://github.com/hanzoai/base
cd base/examples/base
CGO_ENABLED=0 go build
```

Describe a collection in a migration. Base applies migrations when it starts.

```sh
mkdir -p base_/migrations
cat > base_/migrations/1_notes.js <<'EOF'
migrate((app) => {
  app.save(new Collection({
    type: "base",
    name: "notes",
    listRule: "",
    viewRule: "",
    createRule: "",
    fields: [{ name: "text", type: "text", required: true }],
  }))
})
EOF
./base serve --http 127.0.0.1:8090
```

```
Server started at http://127.0.0.1:8090
├─ REST API:  http://127.0.0.1:8090/v1/
└─ Dashboard: http://127.0.0.1:8090/
```

From a second terminal, create a record and list the collection:

```sh
curl -s -X POST http://127.0.0.1:8090/v1/collections/notes/records \
  -H 'Content-Type: application/json' -d '{"text":"hello"}'
{"collectionId":"hbc_3395098727","collectionName":"notes","id":"kbx358lmic2a4bw","text":"hello"}

curl -s http://127.0.0.1:8090/v1/collections/notes/records
{"items":[{"collectionId":"hbc_3395098727","collectionName":"notes","id":"kbx358lmic2a4bw","text":"hello"}],"page":1,"perPage":30,"totalItems":1,"totalPages":1}
```

The empty rules let anyone read and write `notes`; [docs/rules.md](docs/rules.md)
shows how to close them. The data is in `base_/data`.

The startup output has two stale lines. The Dashboard address answers only when
`BASE_ENABLE_ADMIN_UI=1` is set, and the hint to run `superuser upsert` names a
command that was removed. Superusers come from IAM.

## Use it as a Go library

In an empty directory, save this as `main.go`:

```go
package main

import (
	"log"

	"github.com/hanzoai/base"
	"github.com/hanzoai/base/core"
)

func main() {
	app := base.New()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET("/hello", func(re *core.RequestEvent) error {
			return re.String(200, "Hello")
		})
		return se.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
```

```sh
go mod init example.com/hello
go mod tidy
CGO_ENABLED=0 go build
./hello serve
```

`GET http://127.0.0.1:8090/hello` then answers `Hello`, next to the whole `/v1`
API.

## Read next

| Page | For |
|---|---|
| [docs/versions.md](docs/versions.md) | which tags serve what, and moving from `/api` and passwords to `/v1` and IAM |
| [docs/rules.md](docs/rules.md) | access rules, identity from IAM, orgs |
| [docs/hooks.md](docs/hooks.md) | JavaScript hooks and migrations |
| [docs/config.md](docs/config.md) | every command, flag and environment variable |
| [docs/listeners.md](docs/listeners.md) | every socket Base can open |
| [docs/threat-model.md](docs/threat-model.md) | what Base defends, and what you configure |
| [examples/hooks-hybrid-search](examples/hooks-hybrid-search) | full-text and vector search written as hooks |

## Clients

- JavaScript and TypeScript: [`@hanzo/base`](https://github.com/hanzo-js/base).
  Give it the origin, as in `new BaseClient('http://127.0.0.1:8090')`; it adds
  `/v1` itself.
- Dart: [hanzo-dart/base](https://github.com/hanzo-dart/base).

## Contributing and security

[CONTRIBUTING.md](CONTRIBUTING.md) covers building and testing. Report
vulnerabilities as [SECURITY.md](SECURITY.md) describes. Base is MIT licensed;
see [LICENSE.md](LICENSE.md).
