#!/bin/bash
# E2E bootstrap test: verify hazmat can develop itself.
#
# Assumes hazmat is already init'd. Builds from source, then runs the full
# development workflow inside containment: build, vet, test, lint, CGO,
# TLA+ model checking.
#
# This is NOT a lifecycle test (see e2e.sh for that). This answers:
# "Can an agent inside hazmat containment compile, test, and verify
# the hazmat codebase end-to-end?"
#
# Usage:
#   bash scripts/e2e-bootstrap.sh
#   bash scripts/e2e-bootstrap.sh --skip-tla   # skip TLC (no Java/tla2tools)

set -euo pipefail

SKIP_TLA="${1:-}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HAZMAT="$REPO_ROOT/hazmat/hazmat"
PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { FAIL=$((FAIL + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[31m✗\033[0m %s\n" "$1"; }
phase() { printf "\n\033[1m── %s ──\033[0m\n\n" "$1"; }

if ! id agent >/dev/null 2>&1; then
    echo "error: agent user does not exist — run 'hazmat init' first" >&2
    exit 1
fi

# ── Build ───────────────────────────────────────────────────────────────────

phase "Build (host)"
cd "$REPO_ROOT/hazmat"
make all 2>&1
pass "hazmat + hazmat-launch built from source"
cd "$REPO_ROOT"

# Helper: run a command inside hazmat containment against the hazmat repo.
# Suppresses the session banner (stderr) so only command output is captured.
contained() {
    "$HAZMAT" exec -C "$REPO_ROOT" -- "$@" 2>/dev/null
}

# Like contained but allows stderr through (for commands that need it).
contained_verbose() {
    "$HAZMAT" exec -C "$REPO_ROOT" -- "$@"
}

# ── Toolchain resolution ────────────────────────────────────────────────────

phase "Toolchain resolution"

# The first exec triggers integration resolution (including Homebrew ACL
# repair). Subsequent execs reuse the resolved config.
OUTPUT=$(contained go version) \
    && pass "go version: $OUTPUT" \
    || fail "go not accessible in containment"

OUTPUT=$(contained go env GOROOT) \
    && pass "GOROOT: $OUTPUT" \
    || fail "GOROOT not resolvable in containment"

OUTPUT=$(contained go env GOMODCACHE) \
    && pass "GOMODCACHE: $OUTPUT" \
    || fail "GOMODCACHE not resolvable in containment"

# ── Go vet ──────────────────────────────────────────────────────────────────

phase "Go vet"

contained_verbose bash -c "cd hazmat && go vet ./..." \
    && pass "go vet ./..." \
    || fail "go vet ./..."

# ── Unit tests ──────────────────────────────────────────────────────────────

phase "Unit tests"

contained_verbose bash -c "cd hazmat && go test ./..." \
    && pass "go test ./..." \
    || fail "go test ./..."

# ── Build (pure Go, inside containment) ─────────────────────────────────────

phase "Build (contained)"

contained_verbose bash -c "cd hazmat && go build -trimpath -o /dev/null ." \
    && pass "go build hazmat (pure Go)" \
    || fail "go build hazmat"

# ── CGO build ───────────────────────────────────────────────────────────────

phase "CGO build"

contained_verbose bash -c "cd hazmat && CGO_ENABLED=1 go build -trimpath -o /dev/null ./cmd/hazmat-launch" \
    && pass "CGO_ENABLED=1 go build hazmat-launch" \
    || fail "CGO build hazmat-launch (needs CommandLineTools SDK)"

# ── Lint ────────────────────────────────────────────────────────────────────

phase "Lint"

OUTPUT=$(contained golangci-lint version 2>&1 || true)
if echo "$OUTPUT" | grep -q "golangci-lint"; then
    pass "golangci-lint: $OUTPUT"
else
    fail "golangci-lint not accessible in containment"
fi

contained_verbose bash -c "cd hazmat && golangci-lint run ./..." \
    && pass "golangci-lint run ./..." \
    || fail "golangci-lint run ./..."

# ── TLA+ model checking ────────────────────────────────────────────────────

phase "TLA+ model checking"

if [ "$SKIP_TLA" = "--skip-tla" ]; then
    printf "  \033[33m!\033[0m skipped (--skip-tla)\n"
else
    # Java may not be in the default agent PATH. The tla-java integration
    # resolves JAVA_HOME, so check both PATH and JAVA_HOME/bin.
    OUTPUT=$(contained bash -c '
        if command -v java >/dev/null 2>&1; then
            java -version 2>&1
        elif [ -n "$JAVA_HOME" ] && [ -x "$JAVA_HOME/bin/java" ]; then
            "$JAVA_HOME/bin/java" -version 2>&1
        else
            echo "NOT_FOUND"
        fi
    ' || true)
    if echo "$OUTPUT" | grep -qi "openjdk\|java\|jdk" && ! echo "$OUTPUT" | grep -q "NOT_FOUND"; then
        pass "java available: $(echo "$OUTPUT" | head -1)"
    else
        fail "java not found in containment (check JAVA_HOME and PATH)"
    fi

    TLA2TOOLS_JAR="${TLA2TOOLS_JAR:-$HOME/workspace/tla2tools.jar}"
    if [ ! -f "$TLA2TOOLS_JAR" ]; then
        printf "  \033[33m!\033[0m tla2tools.jar not found at %s — skipping TLC specs\n" "$TLA2TOOLS_JAR"
    else
        for spec in MC_SeatbeltPolicy MC_SetupRollback MC_BackupSafety MC_Migration; do
            cfg="tla/${spec}.cfg"
            if [ ! -f "$REPO_ROOT/$cfg" ]; then
                printf "  \033[33m!\033[0m %s: spec not found, skipping\n" "$spec"
                continue
            fi
            # Use 'bash run_tlc.sh' to avoid shebang exec restrictions under seatbelt.
            # Pass TLA2TOOLS_JAR explicitly since the agent's HOME differs.
            # Use -metadir in /tmp to avoid permission issues on tla/states/.
            contained bash -c "cd tla && TLA2TOOLS_JAR='$TLA2TOOLS_JAR' bash run_tlc.sh -workers auto -metadir /private/tmp/tlc-${spec} -config ${spec}.cfg ${spec}.tla" \
                && pass "TLC $spec" \
                || fail "TLC $spec"
        done
    fi
fi

# ── CLI smoke tests ─────────────────────────────────────────────────────────

phase "CLI smoke tests"

# Run CLI subcommands via go run (skipping CGO with -tags nocgo where possible).
for subcmd in "bootstrap --help" "integration list" "config set --help"; do
    contained bash -c "cd hazmat && CGO_ENABLED=0 go run -tags nocgo . $subcmd" >/dev/null 2>&1 \
        && pass "hazmat $subcmd" \
        || fail "hazmat $subcmd"
done

# ── Credential isolation ────────────────────────────────────────────────────

phase "Credential isolation (sanity)"

for dir in .ssh .aws .gnupg ".config/gh"; do
    full="$HOME/$dir"
    [ -d "$full" ] || continue
    contained_verbose ls "$full" >/dev/null 2>&1 \
        && fail "BREACH: agent read ~/$dir" \
        || pass "agent cannot read ~/$dir"
done

# ── Summary ─────────────────────────────────────────────────────────────────

printf "\n"
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
if [ "$FAIL" -eq 0 ]; then
    printf "\033[32m  All %d tests passed.\033[0m\n" "$TOTAL"
else
    printf "\033[31m  %d/%d tests failed.\033[0m\n" "$FAIL" "$TOTAL"
fi
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

exit "$FAIL"
