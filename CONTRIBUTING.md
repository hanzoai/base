# Contributing

Base is one Go module. `examples/base` is the binary, and everything else is
packages it imports.

## Go

You need the Go version in `go.mod` (1.26.8) or newer.

```sh
cd examples/base
go run . serve --http 127.0.0.1:8090
```

Check your change the way CI does, with the settings the published binary is
built with:

```sh
GOWORK=off CGO_ENABLED=0 GOEXPERIMENT=jsonv2 go vet ./...
GOWORK=off CGO_ENABLED=0 GOEXPERIMENT=jsonv2 go test -count=1 ./...
golangci-lint run -c ./golangci.yml ./...
```

On macOS two tests fail for want of RAM-backed storage: `TestTasksEmbed` in
`core` and `TestOrgBaseIsEncryptedAtRest` in `plugins/org`. The encrypted SQLite
codec will only decrypt into RAM, which is `/dev/shm` on Linux, or on macOS a
`tmpfs` mount named by `HANZO_SQLITE_RAMFS_DIR`.

## Admin UI

The admin is a React app in `ui-react/`, built with pnpm. Its build output,
`ui-react/dist`, is committed and embedded in the binary, so building Go needs
no Node toolchain.

```sh
pnpm --dir ui-react install
pnpm --dir ui-react dev      # http://localhost:3000, sends /v1 to localhost:8090
pnpm --dir ui-react build
pnpm --dir ui-react smoke    # loads the built admin in Playwright and checks what renders
```

Commit a rebuilt `dist/` with any change under `ui-react/src`, and build it on
Linux: a macOS build of the same source emits a different, larger bundle, and
the next Linux build undoes it.

## Pull requests

Open an issue before a large change. Keep a pull request to one change, with
its tests, and update `docs/` when behaviour a reader relies on changes.
