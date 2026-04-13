# base

[Base](https://hanzo.ai/base) is an open source Go backend that includes:

- embedded in-memory, SQL and vector databases with **realtime subscriptions**
- built-in **files and users management**
- convenient **Admin dashboard UI**
- and simple **GraphQL** && **REST-ish API**
- native **Analytics and LLM observability**
- deeply with integrated **[Hanzo AI Platform](https://hanzo.ai)** for
  hyperscalability day one.

**Use [Hanzo App](https://hanzo.app) to rapidly iterate and build new apps!

> [!WARNING]
> Please keep in mind that Base is still under active development
> and therefore full backward compatibility is not guaranteed before reaching v1.0.0.

## API SDK clients

The easiest way to interact with the Base Web APIs is to use one of the official SDK clients:

- **JavaScript - [@hanzoai/js-sdk](https://github.com/hanzoai/js-sdk)** (_Browser, Node.js, React Native_)
- **Dart - [@hanzoai/dart-sdk](https://github.com/hanzoai/dart-sdk)** (_Web, Mobile, Desktop, CLI_)

You could also check the recommendations in https://docs.hanzo.ai/how-to-use/.


## Overview

### Use as standalone app

You could download the prebuilt executable for your platform from the [Releases page](https://github.com/hanzoai/base/releases).
Once downloaded, extract the archive and run `./base serve` in the extracted directory.

The prebuilt executables are based on the [`examples/base/main.go` file](https://github.com/hanzoai/base/blob/master/examples/base/main.go) and comes with a JavaScript plugin enabled by default which allows to extend Base with JavaScript (_for more details please refer to [Extend with JavaScript](https://docs.hanzo.ai/js-overview/)_).

### Use as a Go framework/toolkit

Base Base is distributed as a regular Go library package which allows you to build
your own custom app specific business logic and still have a single portable executable at the end.

Here is a minimal example:

0. [Install Go 1.25+](https://go.dev/doc/install) (_if you haven't already_)

1. Create a new project directory with the following `main.go` file inside it:
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
            // registers new "GET /hello" route
            se.Router.GET("/hello", func(re *core.RequestEvent) error {
                return re.String(200, "Hello world!")
            })

            return se.Next()
        })

        if err := app.Start(); err != nil {
            log.Fatal(err)
        }
    }
    ```

2. To init the dependencies, run `go mod init myapp && go mod tidy`.

3. To start the application, run `go run main.go serve`.

4. To build a statically linked executable, you can run `CGO_ENABLED=0 go build` and then start the created executable with `./myapp serve`.

_For more details please refer to [Extend with Go](https://docs.hanzo.ai/go-overview/)._

### Building and running the repo main.go example

To build the minimal standalone executable, like the prebuilt ones in the releases page, you can simply run `go build` inside the `examples/base` directory:

0. [Install Go 1.25+](https://go.dev/doc/install) (_if you haven't already_)
1. Clone/download the repo
2. Navigate to `examples/base`
3. Run `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build`
   (_https://go.dev/doc/install/source#environment_)
4. Start the created executable by running `./base serve`.

Note that the supported build targets by the pure Go SQLite driver at the moment are:

```
darwin  amd64
darwin  arm64
freebsd amd64
freebsd arm64
linux   386
linux   amd64
linux   arm
linux   arm64
linux   loong64
linux   ppc64le
linux   riscv64
linux   s390x
windows 386
windows amd64
windows arm64
```

### Testing

Base comes with mixed bag of unit and integration tests.
To run them, use the standard `go test` command:

```sh
go test ./...
```

Check also the [Testing guide](http://docs.hanzo.ai/testing) to learn how to write your own custom application tests.

## SQLite Replication

Base uses SQLite per org (encrypted via `hanzoai/sqlite` with per-principal CEK).
The `hanzoai/replicate` sidecar streams WAL changes to S3, encrypted with `luxfi/age`.

K8s sidecar pattern:
- **Init container**: restores the latest snapshot from S3 on startup
- **Sidecar**: continuously replicates WAL to S3 while the service runs

Config: `litestream.yml` with `age-identities` / `age-recipients` fields for
end-to-end encryption of replicated data.

## Security

If you discover a security vulnerability within Base, please send an e-mail to **security at hanzo.ai**.

All reports will be promptly addressed and you'll be credited in the fix release notes.

## Contributing

Base is free and open source project licensed under the [MIT License](LICENSE.md).
You are free to do whatever you want with it, even offering it as a paid service.

You could help continuing its development by:

- [Contribute to the source code](CONTRIBUTING.md)
- [Suggest new features and report issues](https://github.com/hanzoai/base/issues)

PRs for new OAuth2 providers, bug fixes, code optimizations and documentation improvements are more than welcome.

But please refrain creating PRs for _new features_ without previously discussing the implementation details.
Base has a [roadmap](https://github.com/orgs/base/projects/2) and I try to work on issues in specific order and such PRs often come in out of nowhere and skew all initial planning with tedious back-and-forth communication.

Don't get upset if I close your PR, even if it is well executed and tested. This doesn't mean that it will never be merged.
Later we can always refer to it and/or take pieces of your implementation when the time comes to work on the issue (don't worry you'll be credited in the release notes).

## CLI

The `base cli` subcommand provides a complete HTTP client for operating any running Base-backed daemon from the command line. It works against `base`, `atsd`, `brokerd`, `tad`, `bdd`, or any binary that embeds Base.

### Targeting a server

```bash
# Defaults to http://127.0.0.1:8090 if nothing is set
base cli --url http://localhost:8090 collection list

# Or use environment variables
export BASE_URL=http://localhost:8090
export BASE_TOKEN=eyJhbGciOi...
base cli collection list
```

Global flags (apply to all subcommands):

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--url` | `BASE_URL` | `http://127.0.0.1:8090` | Server URL |
| `--token` | `BASE_TOKEN` | `~/.config/base/token` | Auth token |
| `--tenant` | - | - | Sets `X-Org-Id` header |
| `--format` | - | `table` on TTY, `json` otherwise | `table`, `json`, or `yaml` |

### Authentication

```bash
# Login as a regular user
base cli login --email user@example.com --password secret123

# Login as superuser
base cli login --email admin@example.com --password secret123 --superuser

# Check who you are
base cli whoami
```

Token is stored at `~/.config/base/token` (or `$XDG_CONFIG_HOME/base/token`) with mode `0600`.

### Collections

```bash
# List all collections
base cli collection list

# Show a specific collection's schema
base cli collection get users

# Export schema as JSON (always JSON, ignores --format)
base cli collection schema users > schema.json
```

### Records

```bash
# List records with filtering and sorting
base cli record list posts --filter "title~'hello'" --limit 10 --sort "-created"

# Get a single record
base cli record get posts abc123

# Create a record
base cli record create posts '{"title":"Hello","body":"World"}'

# Update a record
base cli record update posts abc123 '{"title":"Updated"}'

# Delete a record
base cli record delete posts abc123
```

### Crons

```bash
# List registered cron schedules
base cli crons list
```

### Using from a downstream daemon

Any Base-backed daemon can expose these subcommands without duplicating code.
For example, in `~/work/liquidity/ats/main.go`:

```go
package main

import (
    "log"

    "github.com/hanzoai/base"
    "github.com/hanzoai/base/cmd"
    "github.com/hanzoai/base/core"
)

func main() {
    app := base.New()

    // ... register ATS-specific hooks and routes ...

    // Register system commands manually for flattened CLI:
    // `ats collection list` instead of `ats cli collection list`
    app.RootCmd.AddCommand(cmd.NewSuperuserCommand(app))
    app.RootCmd.AddCommand(cmd.NewServeCommand(app, true))
    cmd.AddCLISubcommands(app.RootCmd)

    if err := app.Execute(); err != nil {
        log.Fatal(err)
    }
}
```

Then operate the running ATS:

```bash
ats collection list
ats record list trades --filter "status='settled'" --limit 20
ats crons list
ats daemon status
```

### CLI Registration Patterns

**Flattened** (recommended for domain daemons): commands at root level.

```go
app.RootCmd.AddCommand(cmd.NewSuperuserCommand(app))
app.RootCmd.AddCommand(cmd.NewServeCommand(app, true))
cmd.AddCLISubcommands(app.RootCmd)  // collection, record, login, whoami, crons, daemon
app.Execute()
```

**Nested** (default via `app.Start()`): commands under `cli` parent.

```go
app.Start()  // registers serve, superuser, cli (with all subcommands)
// Access via: myapp cli collection list
```

### Daemon Lifecycle

The `daemon` subcommand manages the process lifecycle:

```bash
myapp daemon start              # local: nohup spawn
myapp daemon stop               # local: kill
myapp daemon status             # local: pgrep
myapp daemon logs --follow      # local: tail -f
myapp daemon restart            # local: stop + start

myapp daemon status --env dev   # K8s: kubectl get pods
myapp daemon restart --env test --yes  # K8s: rollout restart
```

K8s actions require `--env` (dev, test, main) and are dry-run by default.
Pass `--yes` to execute.
