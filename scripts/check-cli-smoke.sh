#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT/hazmat"

run_smoke() {
	label="$1"
	shift
	echo "cli-smoke: $label..."
	go run . "$@" >/dev/null
}

run_smoke "init --help" init --help
run_smoke "bootstrap --help" bootstrap --help
run_smoke "bootstrap claude --help" bootstrap claude --help
run_smoke "bootstrap codex --help" bootstrap codex --help
run_smoke "bootstrap opencode --help" bootstrap opencode --help
run_smoke "codex --help" codex --help
run_smoke "opencode --help" opencode --help
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
