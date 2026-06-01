#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
GITLEAKS_CONFIG="$SCRIPT_DIR/gitleaks.toml"

cd "$REPO_ROOT"

echo "pre-push: git hook wiring..."
if [ -d "$REPO_ROOT/.beads/hooks" ]; then
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
else
	echo "pre-push: .beads/hooks not present; skipping local hook shim validation"
fi

sh "$REPO_ROOT/scripts/check-secret-patterns.sh" repo
sh "$REPO_ROOT/scripts/check-credential-regressions.sh" repo

echo "pre-push: gitleaks scan..."
gitleaks detect --redact -v --no-banner --config "$GITLEAKS_CONFIG"

cd "$REPO_ROOT/hazmat"

echo "pre-push: go vet..."
go vet ./...

echo "pre-push: go test..."
go test ./...

if [ -f trace_debug_configured.go ]; then
	echo "pre-push: debug trace go test..."
	go test -tags hazmat_debug ./...
else
	echo "pre-push: raw debug trace build is gated..."
	if go test -c -tags hazmat_debug . -o /tmp/hazmat-raw-debug.test >/tmp/hazmat-raw-debug.out 2>/tmp/hazmat-raw-debug.err; then
		echo "pre-push: raw hazmat_debug build unexpectedly succeeded without configure-debug-trace" >&2
		exit 1
	fi
	grep -q "traceDebugConfigured" /tmp/hazmat-raw-debug.err
fi

echo "pre-push: linux compile-only..."
"$REPO_ROOT/scripts/check-linux-compile.sh"

echo "pre-push: macOS trace smoke skipped (debug-only; run scripts/check-macos-trace-smoke.sh manually)"

if [ "${HAZMAT_LINUX_TRACE_SMOKE:-}" = "1" ]; then
	echo "pre-push: linux trace Docker smoke..."
	"$REPO_ROOT/scripts/check-linux-trace-smoke.sh" --skip-if-missing-prereqs
else
	echo "pre-push: linux trace Docker smoke skipped (set HAZMAT_LINUX_TRACE_SMOKE=1 to enable)"
fi

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
run_smoke "bootstrap gemini --help" bootstrap gemini --help
run_smoke "bootstrap hermes --help" bootstrap hermes --help
run_smoke "bootstrap qwen --help" bootstrap qwen --help
run_smoke "harness --help" harness --help
run_smoke "harness status --help" harness status --help
run_smoke "harness status" harness status
run_smoke "harness status --json" harness status --json
run_smoke "harness update --help" harness update --help
run_smoke "harness uninstall --help" harness uninstall --help
for harness in claude codex opencode gemini hermes qwen; do
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
run_smoke "gemini --help" gemini --help
run_smoke "hermes --help" hermes --help
run_smoke "qwen --help" qwen --help
echo "pre-push: cli smoke trace hidden in release build..."
if go run . trace --help >/tmp/hazmat-trace-help.out 2>/tmp/hazmat-trace-help.err; then
	echo "pre-push: trace unexpectedly exists in default build" >&2
	exit 1
fi
"$REPO_ROOT/scripts/check-debug-trace-cli-smoke.sh"
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

if [ "${HAZMAT_CODEX_APP_SERVER_SMOKE:-}" = "1" ]; then
	echo "pre-push: codex app-server smoke..."
	"$REPO_ROOT/scripts/check-codex-app-server-smoke.sh" --skip-if-missing-prereqs
else
	echo "pre-push: codex app-server smoke skipped (set HAZMAT_CODEX_APP_SERVER_SMOKE=1 to enable)"
fi

echo "pre-push: all checks passed"
