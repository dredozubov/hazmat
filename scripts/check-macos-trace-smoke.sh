#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
MODE="disclosure"
ACK_RUN=0

usage() {
	cat <<'EOF'
Usage:
  scripts/check-macos-trace-smoke.sh
  scripts/check-macos-trace-smoke.sh --run --i-understand-this-runs-sudo-dtrace-probes

Default mode is disclosure-only. Live mode builds the developer trace binary and
runs a macOS DTrace/dtruss-backed trace smoke. Agents must ask for explicit
approval before running --run.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			MODE="run"
			;;
		--i-understand-this-runs-sudo-dtrace-probes)
			ACK_RUN=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "macos-trace-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$MODE" != "run" ]; then
	cat <<'EOF'
macos-trace-smoke: disclosure-only

This live smoke builds the hazmat_debug binary and runs a DTrace/dtruss-backed
trace command. It may call:
  /usr/bin/sudo -n -v
  /usr/bin/sudo -n /usr/bin/dtruss <temporary helper>

To run it, ask for explicit approval for this exact command:

  scripts/check-macos-trace-smoke.sh --run --i-understand-this-runs-sudo-dtrace-probes
EOF
	exit 0
fi

if [ "$ACK_RUN" -ne 1 ]; then
	echo "macos-trace-smoke: refusing live run without --i-understand-this-runs-sudo-dtrace-probes" >&2
	exit 2
fi

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
	make hazmat-debug TRACE_ACK=1
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
