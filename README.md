<p align="center">
    <a href="https://hanzo.io" target="_blank" rel="noopener">
        <img src="https://i.imgur.com/5qimnm5.png" alt="Base Base - open source backend in 1 file" />
    </a>
</p>

<p align="center">
    <a href="https://github.com/hanzoai/base/actions/workflows/release.yaml" target="_blank" rel="noopener"><img src="https://github.com/hanzoai/base/actions/workflows/release.yaml/badge.svg" alt="build" /></a>
    <a href="https://github.com/hanzoai/base/releases" target="_blank" rel="noopener"><img src="https://img.shields.io/github/release/hanzoai/base.svg" alt="Latest releases" /></a>
    <a href="https://pkg.go.dev/github.com/hanzoai/base" target="_blank" rel="noopener"><img src="https://godoc.org/github.com/hanzoai/base?status.svg" alt="Go package documentation" /></a>
</p>

[Base](https://hanzo.io) is an open source Go backend that includes:

- embedded in-memory, SQL and vector databases with **realtime subscriptions**
- built-in **files and users management**
- convenient **Admin dashboard UI**
- and simple **GraphQL** && **REST-ish API**
- native **Analytics and LLM observability**
- deeply with integrated **[Base AI Platform](https://hanzo.ai)** for
  hyperscalability day one.

**Use [Base App](https://hanzo.app) to rapidly iterate and build new apps!

> [!WARNING]
> Please keep in mind that Base Base is still under active development
> and therefore full backward compatibility is not guaranteed before reaching v1.0.0.

## API SDK clients

The easiest way to interact with the Base Web APIs is to use one of the official SDK clients:

- **JavaScript - [base/js-sdk](https://github.com/hanzoai/js-sdk)** (_Browser, Node.js, React Native_)
- **Dart - [base/dart-sdk](https://github.com/hanzoai/dart-sdk)** (_Web, Mobile, Desktop, CLI_)

You could also check the recommendations in https://docs.hanzo.ai/how-to-use/.


## Overview

### Use as standalone app

You could download the prebuilt executable for your platform from the [Releases page](https://github.com/hanzoai/base/releases).
Once downloaded, extract the archive and run `./base serve` in the extracted directory.

The prebuilt executables are based on the [`examples/base/main.go` file](https://github.com/hanzoai/base/blob/master/examples/base/main.go) and comes with the JS VM plugin enabled by default which allows to extend Base with JavaScript (_for more details please refer to [Extend with JavaScript](https://docs.hanzo.ai/js-overview/)_).

### Use as a Go framework/toolkit

Base Base is distributed as a regular Go library package which allows you to build
your own custom app specific business logic and still have a single portable executable at the end.

Here is a minimal example:

0. [Install Go 1.23+](https://go.dev/doc/install) (_if you haven't already_)

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

0. [Install Go 1.23+](https://go.dev/doc/install) (_if you haven't already_)
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
linux   ppc64le
linux   riscv64
linux   s390x
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

## Security

If you discover a security vulnerability within Base, please send an e-mail to **support at hanzo.io**.

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
