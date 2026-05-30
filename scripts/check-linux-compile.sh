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
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go list ./... | while IFS= read -r pkg; do
		out="$TMPDIR_LINUX_COMPILE/$(printf '%s' "$pkg" | tr '/.' '__').test"
		GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c "$pkg" -o "$out"
	done
)

echo "linux-compile: Linux build-tagged packages compile"
