ui:
	cd ui && pnpm install && pnpm build

build: ui
	go build -o base ./examples/base/main.go

dev:
	go run ./examples/base/main.go serve --http=127.0.0.1:8090 --dir=./pb_data

lint:
	golangci-lint run -c ./golangci.yml ./...

test:
	go test ./... -v --cover

jstypes:
	go run ./plugins/jsvm/internal/types/types.go

test-report:
	go test ./... -v --cover -coverprofile=coverage.out
	go tool cover -html=coverage.out
