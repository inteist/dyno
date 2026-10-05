default: check

build:
    go build -o bin/token-rate ./cmd/token-rate

install:
    go install ./cmd/token-rate

test:
    go test -race ./...

cover:
    go test -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -n 1

lint:
    go vet ./...
    go tool staticcheck ./...

fmt:
    gofmt -s -w .

fmt-check:
    test -z "$(gofmt -s -l .)"

check: fmt-check lint test

golden:
    UPDATE_GOLDEN=1 go test ./...

fixtures:
    ./scripts/capture-fixtures.sh

agent-install:
    ./scripts/install-agent.sh

agent-uninstall:
    ./scripts/install-agent.sh --uninstall
