default: check

build:
    go build -o bin/dyno ./cmd/dyno

install:
    go install ./cmd/dyno

# Print the version that a build of the current tree reports.
version: build
    ./bin/dyno version

# Run the checks, then tag HEAD, for example `just release 0.2.0`. Does not push.
release $version: check
    ./scripts/release.sh "$version"

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

# Offline helper safety tests require Python 3; no live endpoint is contacted.
script-test:
    python3 -B -I -m unittest discover -s scripts -p 'test_capture*.py'

check: fmt-check lint test

golden:
    UPDATE_GOLDEN=1 go test ./...

fixtures:
    ./scripts/capture-fixtures.sh

agent-install:
    ./scripts/install-agent.sh

agent-uninstall:
    ./scripts/install-agent.sh --uninstall
