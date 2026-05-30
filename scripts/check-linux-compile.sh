#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
TMPDIR_LINUX_COMPILE="$(mktemp -d)"
TRACE_DEBUG_MARKER="$REPO_ROOT/hazmat/trace_debug_configured.go"
TRACE_DEBUG_MARKER_BACKUP="$TMPDIR_LINUX_COMPILE/trace_debug_configured.go"

cleanup() {
	if [ -f "$TRACE_DEBUG_MARKER_BACKUP" ]; then
		mv -f "$TRACE_DEBUG_MARKER_BACKUP" "$TRACE_DEBUG_MARKER"
	fi
	rm -rf "$TMPDIR_LINUX_COMPILE"
}
trap cleanup EXIT INT TERM HUP

if [ -f "$TRACE_DEBUG_MARKER" ]; then
	mv -f "$TRACE_DEBUG_MARKER" "$TRACE_DEBUG_MARKER_BACKUP"
fi

echo "linux-compile: GOOS=linux GOARCH=amd64 compile-only probe..."
(
	cd "$REPO_ROOT/hazmat"
	echo "linux-compile: compiling release/default build..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go list ./... | while IFS= read -r pkg; do
		out="$TMPDIR_LINUX_COMPILE/$(printf '%s' "$pkg" | tr '/.' '__').test"
		GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c "$pkg" -o "$out"
	done
	echo "linux-compile: raw hazmat_debug build should require configure-debug-trace..."
	if GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags hazmat_debug . -o "$TMPDIR_LINUX_COMPILE/raw-debug.test" \
		>"$TMPDIR_LINUX_COMPILE/raw-debug.out" 2>"$TMPDIR_LINUX_COMPILE/raw-debug.err"; then
		echo "linux-compile: raw hazmat_debug build unexpectedly succeeded without configure-debug-trace" >&2
		exit 1
	fi
	grep -q "traceDebugConfigured" "$TMPDIR_LINUX_COMPILE/raw-debug.err"
)

echo "linux-compile: release build compiles and raw debug build is gated"
