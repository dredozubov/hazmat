#!/bin/bash
#
# Non-destructive regression checks for Hazmat test-entrypoint safety rails.
#
# Verifies that:
#   - scripts/e2e.sh refuses to run without an explicit destructive ack
#   - make e2e refuses to run without E2E_ACK=1
#   - live Codex app-server smoke refuses to run without its explicit ack
#   - live Codex desktop attach smoke refuses to run without its explicit ack
#   - live Claude Workflow export smoke refuses to run without its explicit ack
#   - live session-home activation smoke refuses to run without its explicit ack
#   - live cache integration smoke refuses to run without its explicit ack
#   - live OpenHands recipe smoke refuses to run without its explicit ack
#   - native harness smoke refuses live mode without its explicit ack
#   - debug trace entrypoints refuse sudo-adjacent live modes without explicit ack
#   - Apple Container spike refuses live mode without its explicit ack
#   - release script refuses hazmat claude and push-capable paths without ack
#   - guarded live wrappers default to disclosure-only output
#   - release installer refuses unsupported platforms before download/install
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

    if printf '%s' "$output" | grep -Fq -- "$expected"; then
        pass "$label"
    else
        fail "$label: expected output containing '$expected'"
        printf '%s\n' "$output" >&2
    fi
}

assert_succeeds_with() {
    local label="$1"
    local expected="$2"
    shift 2

    local output=""
    local status=0
    set +e
    output=$("$@" 2>&1)
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        fail "$label: command failed with status $status"
        printf '%s\n' "$output" >&2
        return
    fi

    if printf '%s' "$output" | grep -Fq -- "$expected"; then
        pass "$label"
    else
        fail "$label: expected output containing '$expected'"
        printf '%s\n' "$output" >&2
    fi
}

assert_help_contains_all() {
    local label="$1"
    local command="$2"
    shift 2

    local output=""
    local status=0
    set +e
    output=$("$command" --help 2>&1)
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        fail "$label: --help failed with status $status"
        printf '%s\n' "$output" >&2
        return
    fi

    local missing=""
    for expected in "$@"; do
        if ! printf '%s' "$output" | grep -Fq -- "$expected"; then
            if [ -z "$missing" ]; then
                missing="$expected"
            else
                missing="$missing, $expected"
            fi
        fi
    done
    if [ -n "$missing" ]; then
        fail "$label: --help missing $missing"
        printf '%s\n' "$output" >&2
        return
    fi

    pass "$label"
}

assert_file_contains_all() {
    local label="$1"
    local path="$2"
    shift 2

    if [ ! -r "$path" ]; then
        fail "$label: $path is not readable"
        return
    fi

    local text=""
    text="$(cat "$path")"
    local missing=""
    for expected in "$@"; do
        if ! printf '%s' "$text" | grep -Fq -- "$expected"; then
            if [ -z "$missing" ]; then
                missing="$expected"
            else
                missing="$missing, $expected"
            fi
        fi
    done
    if [ -n "$missing" ]; then
        fail "$label: missing $missing"
        return
    fi

    pass "$label"
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

assert_fails_with \
    "Codex app-server smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-codex-app-server" \
    "$REPO_ROOT/scripts/check-codex-app-server-smoke.sh" --run

assert_fails_with \
    "Codex desktop attach smoke requires live ack" \
    "--run requires --i-understand-this-may-launch-codex-app" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" --run

assert_fails_with \
    "Claude Workflow export smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-claude-and-host-claude" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --run

assert_fails_with \
    "session-home activation smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-exec" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" --run

assert_fails_with \
    "cache integration smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-exec" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target ollama --run

assert_fails_with \
    "OpenHands recipe smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-exec" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" --run

assert_fails_with \
    "native harness smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-native-hazmat-smoke" \
    bash "$REPO_ROOT/scripts/e2e-harness-smoke-native.sh" --run

assert_fails_with \
    "Darwin trace prerequisite check requires DTrace ack" \
    "refusing Darwin DTrace prerequisite probes without --i-understand-this-runs-sudo-dtrace-probes" \
    bash "$REPO_ROOT/scripts/configure-debug-trace.sh" --target darwin

assert_fails_with \
    "macOS trace smoke requires DTrace ack" \
    "refusing live run without --i-understand-this-runs-sudo-dtrace-probes" \
    "$REPO_ROOT/scripts/check-macos-trace-smoke.sh" --run

assert_fails_with \
    "Linux trace smoke requires privileged Docker ack" \
    "refusing live run without --i-understand-this-runs-privileged-docker" \
    "$REPO_ROOT/scripts/check-linux-trace-smoke.sh" --run

assert_fails_with \
    "Apple Container spike requires live ack" \
    "refusing live run without --i-understand-this-runs-apple-container-spike" \
    bash "$REPO_ROOT/scripts/spike-apple-container.sh" --run

assert_fails_with \
    "release script requires hazmat claude ack" \
    "refusing to run without --i-understand-this-runs-hazmat-claude" \
    bash "$REPO_ROOT/scripts/release.sh" --dry

assert_fails_with \
    "release script requires non-dry push ack" \
    "refusing non-dry release without --i-understand-this-may-push-release" \
    bash "$REPO_ROOT/scripts/release.sh" --i-understand-this-runs-hazmat-claude

phase "Disclosure defaults"

assert_succeeds_with \
    "Codex app-server smoke defaults to disclosure" \
    "codex-app-server-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-codex-app-server-smoke.sh"

assert_succeeds_with \
    "Codex desktop attach smoke defaults to disclosure" \
    "Codex desktop attach smoke host-state disclosure" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh"

assert_succeeds_with \
    "Claude Workflow export smoke defaults to disclosure" \
    "claude-workflow-export-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh"

assert_succeeds_with \
    "session-home activation smoke defaults to disclosure" \
    "session-home-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh"

assert_succeeds_with \
    "cache integration smoke defaults to disclosure" \
    "cache-integration-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh"

assert_succeeds_with \
    "OpenHands recipe smoke defaults to disclosure" \
    "openhands-recipe-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh"

assert_succeeds_with \
    "native harness smoke defaults to disclosure" \
    "native-harness-smoke: disclosure-only" \
    bash "$REPO_ROOT/scripts/e2e-harness-smoke-native.sh"

assert_succeeds_with \
    "macOS trace smoke defaults to disclosure" \
    "macos-trace-smoke: disclosure-only" \
    "$REPO_ROOT/scripts/check-macos-trace-smoke.sh"

assert_succeeds_with \
    "Linux trace smoke defaults to disclosure" \
    "linux-trace-smoke: disclosure-only" \
    "$REPO_ROOT/scripts/check-linux-trace-smoke.sh"

assert_succeeds_with \
    "Apple Container spike defaults to disclosure" \
    "spike-apple-container: disclosure-only" \
    bash "$REPO_ROOT/scripts/spike-apple-container.sh"

phase "Fixture and refusal UX guards"

assert_succeeds_with \
    "Claude Workflow export smoke has a default fixture" \
    "claude-workflow-export-smoke: fixtures ok" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_file_contains_all \
    "Codex desktop running-process refusal is bounded" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    "Codex App appears to be running (" \
    "Matching process sample:" \
    "... truncated " \
    "additional matching process(es)"

assert_file_contains_all \
    "session-home smoke explains activation blockers" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    "activation stopped before the toolchain matrix" \
    "inspect the listed Blocking paths above" \
    "Do not rerun hazmat init."

phase "Sudo-adjacent prereq disclosures"

assert_help_contains_all \
    "Codex app-server smoke documents sudo-adjacent prereqs" \
    "$REPO_ROOT/scripts/check-codex-app-server-smoke.sh" \
    "--check-prereqs" \
    "--skip-if-missing-prereqs" \
    "sudo -n" \
    "Agents must ask before running --check-prereqs, --skip-if-missing-prereqs, or --run"

assert_help_contains_all \
    "Codex desktop attach smoke documents sudo-adjacent prereqs" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    "--check-prereqs" \
    "sudo -n" \
    "Agents must ask before running either command"

assert_help_contains_all \
    "session-home smoke documents sudo-adjacent prereqs" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    "--check-prereqs" \
    "--skip-if-missing-prereqs" \
    "sudo -n" \
    "Agents must ask before running --check-prereqs, --skip-if-missing-prereqs, or --run"

phase "Platform guards"

assert_fails_with \
    "install.sh refuses linux release artifacts" \
    "release/install artifacts are only published for darwin" \
    bash "$REPO_ROOT/scripts/install.sh" --platform linux --version 0.0.0

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
