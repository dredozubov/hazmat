#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"

echo "fast-check: secret-pattern scan..."
bash "$REPO_ROOT/scripts/check-secret-patterns.sh"

cd "$REPO_ROOT/hazmat"

echo "fast-check: go vet..."
go vet ./...

echo "fast-check: go test..."
go test ./...

echo "fast-check: linux compile-only..."
bash "$REPO_ROOT/scripts/check-linux-compile.sh"

echo "fast-check: golangci-lint..."
golangci-lint run ./...

echo "fast-check: CLI smoke tests..."
bash "$REPO_ROOT/scripts/check-cli-smoke.sh"

echo "fast-check: all checks passed"
