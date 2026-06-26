#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT/hazmat"

run_smoke() {
	label="$1"
	shift
	echo "cli-smoke: $label..."
	go run ./cmd/hazmat "$@" >/dev/null
}

run_smoke "init --help" init --help
run_smoke "bootstrap --help" bootstrap --help
run_smoke "bootstrap claude --help" bootstrap claude --help
run_smoke "bootstrap codex --help" bootstrap codex --help
run_smoke "bootstrap opencode --help" bootstrap opencode --help
run_smoke "bootstrap antigravity --help" bootstrap antigravity --help
run_smoke "bootstrap hermes --help" bootstrap hermes --help
run_smoke "bootstrap qwen --help" bootstrap qwen --help
run_smoke "bootstrap cursor-agent --help" bootstrap cursor-agent --help
run_smoke "bootstrap pi --help" bootstrap pi --help
run_smoke "harness --help" harness --help
run_smoke "harness status --help" harness status --help
run_smoke "harness status" harness status
run_smoke "harness status --json" harness status --json
run_smoke "harness update --help" harness update --help
run_smoke "harness uninstall --help" harness uninstall --help
for harness in claude codex opencode antigravity hermes qwen cursor-agent pi; do
	run_smoke "harness status $harness" harness status "$harness"
	run_smoke "harness status $harness --json" harness status "$harness" --json
	run_smoke "harness update $harness --dry-run" --dry-run harness update "$harness"
	run_smoke "harness uninstall $harness --dry-run" --dry-run harness uninstall "$harness"
done
run_smoke "codex --help" codex --help
run_smoke "codex-app-server --help" codex-app-server --help
run_smoke "codex-app-shim --help" codex-app-shim --help
run_smoke "app-server --help" app-server --help
run_smoke "opencode --help" opencode --help
run_smoke "antigravity --help" antigravity --help
run_smoke "hermes --help" hermes --help
run_smoke "qwen --help" qwen --help
run_smoke "cursor-agent --help" cursor-agent --help
run_smoke "pi --help" pi --help
echo "cli-smoke: trace hidden in release build..."
if go run ./cmd/hazmat trace --help >/tmp/hazmat-trace-help.out 2>/tmp/hazmat-trace-help.err; then
	echo "cli-smoke: trace unexpectedly exists in default build" >&2
	exit 1
fi
run_smoke "integration --help" integration --help
run_smoke "integration list" integration list
run_smoke "integration show node" integration show node
run_smoke "integration setup" integration setup
run_smoke "integration scaffold --help" integration scaffold --help
run_smoke "integration validate template" integration validate ../docs/examples/integration-template.yaml
run_smoke "config set --help" config set --help
run_smoke "config import claude --dry-run" config import claude --dry-run
run_smoke "config import opencode --help" config import opencode --help
run_smoke "config ssh set --help" config ssh set --help
run_smoke "config ssh show --help" config ssh show --help
run_smoke "config ssh test --help" config ssh test --help
run_smoke "config ssh unset --help" config ssh unset --help

echo "cli-smoke: all checks passed"
