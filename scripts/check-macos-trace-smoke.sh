#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"

if [ "$(go env GOOS)" != "darwin" ]; then
	echo "macos-trace-smoke: skipped: host Go OS is not darwin"
	exit 0
fi

TMPDIR_MACOS_TRACE="$(mktemp -d)"
cleanup() {
	rm -rf "$TMPDIR_MACOS_TRACE"
}
trap cleanup EXIT INT TERM HUP

echo "macos-trace-smoke: configuring debug trace build..."
(
	cd "$REPO_ROOT"
	HAZMAT_DEBUG_BIN="$TMPDIR_MACOS_TRACE/hazmat-debug" \
	HAZMAT_TRACE_CLAUDE_BIN="$TMPDIR_MACOS_TRACE/hazmat-trace-claude" \
	make hazmat-debug
)

echo "macos-trace-smoke: running codex full trace bundle..."
(
	cd "$REPO_ROOT/hazmat"
	/usr/bin/script -q "$TMPDIR_MACOS_TRACE/wrapper.typescript" "$TMPDIR_MACOS_TRACE/hazmat-debug" trace codex \
		--out "$TMPDIR_MACOS_TRACE" \
		--name smoke \
		-- --help >/tmp/hazmat-macos-trace-smoke.out
)

bundle="$(find "$TMPDIR_MACOS_TRACE" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
test -n "$bundle"
grep -q "\"backend\": \"darwin\"" "$bundle/manifest.json"
grep -q "\"harness\": \"codex\"" "$bundle/manifest.json"
grep -q "\"exit_code\": 0" "$bundle/manifest.json"
grep -q "\"syscalls\": true" "$bundle/manifest.json"
grep -q "\"transcript\": true" "$bundle/manifest.json"
test -f "$bundle/harness.json"
test -f "$bundle/command.txt"
test -f "$bundle/before-ps.txt"
test -f "$bundle/after-ps.txt"
test -f "$bundle/terminal.typescript"
test -f "$bundle/dtruss.log"
test -f "$bundle/fs_usage.log"
test -f "$bundle/opensnoop.log"
test -f "$bundle/indicators.md"

echo "macos-trace-smoke: bundle shape ok: $bundle"
