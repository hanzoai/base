# Build the way the product ships. core/sqlite_math_required.go REFUSES to
# compile under cgo without -tags sqlite_math_functions — deliberately, because
# that backend's SQL surface lacks the math functions the search layer emits, and
# it names CGO_ENABLED=0 as the other fix. cgo is the toolchain default on a dev
# machine, so without this line build, test AND lint all die at
# "undefined: cgoBuildNeedsSQLiteMathFunctions" — none of them ran here before.
# 0 is what the Dockerfile sets (pure-Go modernc sqlite); ?= keeps it overridable.
export CGO_ENABLED ?= 0

# The one entrypoint build links and dev runs, so the two cannot drift.
MAIN := ./examples/base/main.go

help: ## Show this help.
	@awk 'BEGIN{FS=":.*##";printf "\nUsage: make <target>\n\nTargets:\n"} /^[a-zA-Z_-]+:.*##/{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# The admin SPA is ui-react/, not ui/. This recipe named a directory that does
# not exist in this repo, so it failed at `cd: can't cd to ui` — and `build`
# depended on it, which is why `make build` could not reach the Go compiler.
ui: ## Rebuild the admin SPA into ui-react/dist.
	cd ui-react && pnpm install && pnpm build

# DELIBERATELY NOT `build: ui`. ui-react/dist is COMMITTED — .gitignore ignores
# dist/ everywhere and then un-ignores this one — because it is the //go:embed
# source (ui-react/embed.go) the compiler reads. The Dockerfile links the binary
# straight from that committed dist and never runs pnpm, so making the Go build
# depend on a pnpm install is both a step the shipped build does not take and a
# reason `make build` needs node and the network to compile Go. `build` is now
# the same link the image performs; change the SPA and you run `make ui`
# yourself, which is what the Dockerfile already tells you to do before tagging.
build: ## Build ./base — the binary the image ships.
	go build -o base $(MAIN)

# Base hosts no identity of its own, so it refuses to boot without IAM_ENDPOINT
# (e.g. IAM_ENDPOINT=https://hanzo.id make dev). serve's --dev is on by default
# and its data lands in base_/data, which .gitignore already anchors.
dev: ## Run the server locally (needs IAM_ENDPOINT).
	go run $(MAIN) serve

lint: ## Lint (golangci-lint).
	golangci-lint run -c ./golangci.yml ./...

test: ## Run the tests.
	go test ./... -v --cover

jstypes: ## Regenerate the jsvm TypeScript declarations.
	go run ./plugins/jsvm/internal/types/types.go

test-report: ## Run the tests and open an HTML coverage report.
	go test ./... -v --cover -coverprofile=coverage.out
	go tool cover -html=coverage.out

# Only what this Makefile writes: the binary (build) and the coverage profile
# (test-report). Both are anchored in .gitignore. NOT ui-react/dist — that is
# committed embed source, not output, and `make ui` is what rewrites it.
clean: ## Remove built artifacts.
	rm -f base coverage.out

.PHONY: help ui build dev lint test jstypes test-report clean
