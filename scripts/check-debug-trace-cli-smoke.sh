#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT/hazmat"

if [ ! -f trace_debug_configured.go ]; then
	echo "debug-trace-cli-smoke: skipped: run scripts/configure-debug-trace.sh first"
	exit 0
fi

run_smoke() {
	label="$1"
	shift
	echo "debug-trace-cli-smoke: $label..."
	go run -tags hazmat_debug . "$@" >/dev/null
}

run_smoke "trace --help" trace --help
run_smoke "trace claude --help" trace claude --help
run_smoke "trace codex --help" trace codex --help
run_smoke "trace opencode --help" trace opencode --help
run_smoke "trace gemini --help" trace gemini --help
run_smoke "trace hermes --help" trace hermes --help
run_smoke "trace qwen --help" trace qwen --help
run_smoke "trace cursor-agent --help" trace cursor-agent --help

echo "debug-trace-cli-smoke: all checks passed"
