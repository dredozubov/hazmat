#!/bin/bash
#
# Non-destructive regression checks for Hazmat test-entrypoint safety rails.
#
# Verifies that:
#   - scripts/e2e.sh refuses to run without an explicit destructive ack
#   - make e2e refuses to run without E2E_ACK=1
#   - host-side test entrypoints fail fast when another host-side test holds
#     the shared lock

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { FAIL=$((FAIL + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[31m✗\033[0m %s\n" "$1"; }
phase() { printf "\n\033[1m── %s ──\033[0m\n\n" "$1"; }

assert_fails_with() {
    local label="$1"
    local expected="$2"
    shift 2

    local output=""
    local status=0
    set +e
    output=$("$@" 2>&1)
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        fail "$label: command unexpectedly succeeded"
        return
    fi

    if printf '%s' "$output" | grep -Fq "$expected"; then
        pass "$label"
    else
        fail "$label: expected output containing '$expected'"
        printf '%s\n' "$output" >&2
    fi
}

phase "Destructive guards"

assert_fails_with \
    "scripts/e2e.sh requires destructive ack" \
    "scripts/e2e.sh is destructive to the local Hazmat setup." \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --quick

assert_fails_with \
    "make e2e requires E2E_ACK=1" \
    "Refusing to run destructive host lifecycle test." \
    make -C "$REPO_ROOT/hazmat" e2e

phase "Shared host lock"

holder_pid=""
cleanup() {
    if [ -n "$holder_pid" ]; then
        kill "$holder_pid" >/dev/null 2>&1 || true
        wait "$holder_pid" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT INT TERM HUP

(
    # shellcheck source=scripts/lib/test_lock.sh
    . "$REPO_ROOT/scripts/lib/test_lock.sh"
    acquire_hazmat_test_suite_lock "scripts/test-entrypoint-guards.sh"
    sleep 30
) &
holder_pid=$!
sleep 1

assert_fails_with \
    "shared lock blocks repo-matrix entrypoint" \
    "another Hazmat host-side test is already running." \
    bash "$REPO_ROOT/scripts/e2e-stack-matrix.sh" --detect --skip-build --id next-js

cleanup
holder_pid=""
trap - EXIT INT TERM HUP

printf "\n"
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
if [ "$FAIL" -eq 0 ]; then
    printf "\033[32m  All %d checks passed.\033[0m\n" "$TOTAL"
else
    printf "\033[31m  %d/%d checks failed.\033[0m\n" "$FAIL" "$TOTAL"
fi
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

exit "$FAIL"
