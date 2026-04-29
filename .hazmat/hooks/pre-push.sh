#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
GITLEAKS_CONFIG="$SCRIPT_DIR/gitleaks.toml"

cd "$REPO_ROOT"

echo "pre-push: git hook wiring..."
for hook in pre-commit pre-push; do
	hook_path="$REPO_ROOT/.beads/hooks/$hook"
	if [ ! -x "$hook_path" ]; then
		echo "pre-push: expected executable hook shim at $hook_path" >&2
		exit 1
	fi
	if ! grep -q "BEGIN HAZMAT REPO GATE" "$hook_path"; then
		echo "pre-push: $hook_path is missing the Hazmat repo gate chain" >&2
		exit 1
	fi
	if ! grep -q "scripts/$hook" "$hook_path"; then
		echo "pre-push: $hook_path does not call scripts/$hook" >&2
		exit 1
	fi
done

sh "$REPO_ROOT/scripts/check-secret-patterns.sh" repo
sh "$REPO_ROOT/scripts/check-credential-regressions.sh" repo

echo "pre-push: gitleaks scan..."
gitleaks detect --redact -v --no-banner --config "$GITLEAKS_CONFIG"

cd "$REPO_ROOT/hazmat"

echo "pre-push: go vet..."
go vet ./...

echo "pre-push: go test..."
go test ./...

TMPDIR_LINUX_COMPILE="$(mktemp -d)"
cleanup() {
	rm -rf "$TMPDIR_LINUX_COMPILE"
}
trap cleanup EXIT INT TERM HUP

echo "pre-push: linux compile-only..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./... -o "$TMPDIR_LINUX_COMPILE"

echo "pre-push: golangci-lint..."
golangci-lint run ./...

run_smoke() {
	label="$1"
	shift
	echo "pre-push: cli smoke $label..."
	go run . "$@" >/dev/null
}

echo "pre-push: CLI smoke tests..."
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
run_smoke "migrate --help" migrate --help
run_smoke "migrate credentials --help" migrate credentials --help
run_smoke "config set --help" config set --help
run_smoke "config import claude --dry-run" config import claude --dry-run
run_smoke "config import opencode --help" config import opencode --help
run_smoke "config ssh set --help" config ssh set --help
run_smoke "config ssh show --help" config ssh show --help
run_smoke "config ssh test --help" config ssh test --help
run_smoke "config ssh unset --help" config ssh unset --help

echo "pre-push: all checks passed"
