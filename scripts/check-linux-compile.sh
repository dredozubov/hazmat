#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
TMPDIR_LINUX_COMPILE="$(mktemp -d)"

cleanup() {
	rm -rf "$TMPDIR_LINUX_COMPILE"
}
trap cleanup EXIT INT TERM HUP

echo "linux-compile: GOOS=linux GOARCH=amd64 compile-only probe..."
(
	cd "$REPO_ROOT/hazmat"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./... -o "$TMPDIR_LINUX_COMPILE"
)

echo "linux-compile: unsupported Linux backend compiles"
