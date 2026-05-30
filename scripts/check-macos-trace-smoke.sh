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

echo "macos-trace-smoke: running codex no-syscalls trace bundle..."
(
	cd "$REPO_ROOT/hazmat"
	go run . trace codex \
		--out "$TMPDIR_MACOS_TRACE" \
		--name smoke \
		--no-syscalls \
		--no-transcript \
		-- --help >/tmp/hazmat-macos-trace-smoke.out
)

bundle="$(find "$TMPDIR_MACOS_TRACE" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
test -n "$bundle"
grep -q "\"backend\": \"darwin\"" "$bundle/manifest.json"
grep -q "\"harness\": \"codex\"" "$bundle/manifest.json"
grep -q "\"exit_code\": 0" "$bundle/manifest.json"
grep -q "\"syscalls\": false" "$bundle/manifest.json"
grep -q "\"transcript\": false" "$bundle/manifest.json"
test -f "$bundle/harness.json"
test -f "$bundle/command.txt"
test -f "$bundle/before-ps.txt"
test -f "$bundle/after-ps.txt"
test -f "$bundle/indicators.md"

echo "macos-trace-smoke: bundle shape ok: $bundle"
