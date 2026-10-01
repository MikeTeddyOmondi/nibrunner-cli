# nibrunner-cli. `just` with no target lists these.
default:
    @just --list

bin_name := "nibr"

# Build for this machine, into ./bin.
build:
    mkdir -p bin
    go build -o bin/{{bin_name}} .

# Cross-compile a static Linux amd64 binary, for a host actually running nibrunnerd (e.g. db9).
build-linux:
    mkdir -p bin
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/{{bin_name}}-linux-amd64 .

# Run locally, passing args straight through, e.g. `just run host status`.
run *args:
    go run . {{args}}

# go vet and gofmt, the two checks CI should run.
lint:
    go vet ./...
    gofmt -l .

# gofmt -w, or with --check, fail instead of rewriting.
fmt *args:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{args}}" == *--check* ]]; then
        unformatted="$(gofmt -l .)"
        if [[ -n "$unformatted" ]]; then
            echo "$unformatted"
            exit 1
        fi
    else
        gofmt -w .
    fi

test:
    go test ./...

tidy:
    go mod tidy

clean:
    rm -rf bin
