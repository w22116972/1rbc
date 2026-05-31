#!/bin/bash
set -euo pipefail

mkdir -p target/go
export GOCACHE="${GOCACHE:-$PWD/.gocache}"

if [ "$(go env GOARCH)" = "amd64" ] && [ -z "${GOAMD64:-}" ]; then
  export GOAMD64=v3
fi

go build -trimpath -ldflags="-s -w" -o target/go/calculate_average_golang_fast ./src/main/go-fast
