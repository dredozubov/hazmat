#!/bin/bash
#
# Non-destructive regression checks for Hazmat test-entrypoint safety rails.
#
# Verifies that:
#   - scripts/e2e.sh refuses to run without an explicit destructive ack
#   - make e2e refuses to run without E2E_ACK=1
#   - live Codex app-server smoke refuses to run without its explicit ack
#   - live Codex desktop attach smoke refuses to run without its explicit ack
#   - live Claude onboarding smoke refuses to run without its explicit ack
#   - live Claude Workflow export smoke refuses to run without its explicit ack
#   - live session-home activation smoke refuses to run without its explicit ack
#   - live cache integration smoke refuses to run without its explicit ack
#   - live OpenHands recipe smoke refuses to run without its explicit ack
#   - live README proof-stack smoke refuses to run without its explicit ack
#   - native harness smoke refuses live mode without its explicit ack
#   - debug trace entrypoints refuse sudo-adjacent live modes without explicit ack
#   - Linux-in-Apple-Container smoke refuses live mode without its explicit ack
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

    if grep -Fq -- "$expected" <<<"$output"; then
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

    if grep -Fq -- "$expected" <<<"$output"; then
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
        if ! grep -Fq -- "$expected" <<<"$output"; then
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

assert_bash_help_contains_all() {
    local label="$1"
    local script="$2"
    shift 2

    local output=""
    local status=0
    set +e
    output=$(bash "$script" --help 2>&1)
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        fail "$label: --help failed with status $status"
        printf '%s\n' "$output" >&2
        return
    fi

    local missing=""
    for expected in "$@"; do
        if ! grep -Fq -- "$expected" <<<"$output"; then
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
        if ! grep -Fq -- "$expected" <<<"$text"; then
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

assert_file_not_contains_any() {
    local label="$1"
    local path="$2"
    shift 2

    if [ ! -r "$path" ]; then
        fail "$label: $path is not readable"
        return
    fi

    local text=""
    text="$(cat "$path")"
    local found=""
    for unexpected in "$@"; do
        if grep -Fq -- "$unexpected" <<<"$text"; then
            if [ -z "$found" ]; then
                found="$unexpected"
            else
                found="$found, $unexpected"
            fi
        fi
    done
    if [ -n "$found" ]; then
        fail "$label: found forbidden text $found"
        return
    fi

    pass "$label"
}

assert_command_contains_all_and_not() {
    local label="$1"
    local forbidden="$2"
    shift 2

    local expected=()
    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
        expected+=("$1")
        shift
    done
    if [ "$#" -eq 0 ]; then
        fail "$label: missing -- command separator"
        return
    fi
    shift

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

    local missing=""
    for want in "${expected[@]}"; do
        if ! grep -Fq -- "$want" <<<"$output"; then
            if [ -z "$missing" ]; then
                missing="$want"
            else
                missing="$missing, $want"
            fi
        fi
    done
    if [ -n "$missing" ]; then
        fail "$label: missing $missing"
        printf '%s\n' "$output" >&2
        return
    fi

    if grep -Fq -- "$forbidden" <<<"$output"; then
        fail "$label: found forbidden text $forbidden"
        printf '%s\n' "$output" >&2
        return
    fi

    pass "$label"
}

assert_file_order() {
    local label="$1"
    local path="$2"
    local before="$3"
    local after="$4"

    if [ ! -r "$path" ]; then
        fail "$label: $path is not readable"
        return
    fi

    local before_line=""
    local after_line=""
    before_line="$(grep -n -F -- "$before" "$path" | head -n 1 | cut -d: -f1)"
    after_line="$(grep -n -F -- "$after" "$path" | head -n 1 | cut -d: -f1)"
    if [ -z "$before_line" ] || [ -z "$after_line" ]; then
        fail "$label: missing order markers"
        return
    fi
    if [ "$before_line" -lt "$after_line" ]; then
        pass "$label"
    else
        fail "$label: expected '$before' before '$after'"
    fi
}

phase "Agent closeout guidance"

assert_file_contains_all \
    "AGENTS.md gates git push closeout" \
    "$REPO_ROOT/AGENTS.md" \
    'Do not run `git push` unless' \
    'Approval needed for exact command: `git push`.' \
    'Do not run bd dolt pull/push in this repo'

assert_file_not_contains_any \
    "AGENTS.md avoids unconditional push mandate" \
    "$REPO_ROOT/AGENTS.md" \
    'NEVER stop before pushing' \
    'NEVER say "ready to push when you are"' \
    'Work is NOT complete until `git push` succeeds'

assert_file_contains_all \
    "Ralph prompt gates git push closeout" \
    "$REPO_ROOT/scripts/ralph/prompt.md" \
    'Hazmat has no Dolt remote; do not run bd dolt pull/push.' \
    'Remote sync is approval-gated' \
    'Approval needed for exact command: `git push`.'

assert_file_not_contains_any \
    "Ralph prompt avoids Dolt and unconditional push" \
    "$REPO_ROOT/scripts/ralph/prompt.md" \
    '   bd dolt pull' \
    '   bd dolt push' \
    'Work is NOT complete until `git push` succeeds'

phase "Destructive guards"

assert_fails_with \
    "scripts/e2e.sh requires destructive ack" \
    "scripts/e2e.sh is destructive to the local Hazmat setup." \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --quick

assert_help_contains_all \
    "scripts/e2e-vm.sh documents restartable VM steps" \
    "$REPO_ROOT/scripts/e2e-vm.sh" \
    "--step STEP" \
    "pull" \
    "download" \
    "base" \
    "HAZMAT_E2E_VM_PROVIDER=tart" \
    "Compatibility alias for pull; no IPSW is downloaded." \
    "Before the first base provisioning, pull the prebuilt image once:" \
    "bash scripts/e2e-vm.sh --step download --quick"

assert_fails_with \
    "scripts/e2e-vm.sh rejects unknown VM step before live work" \
    "unknown VM step 'bogus'" \
    bash "$REPO_ROOT/scripts/e2e-vm.sh" --step bogus

assert_help_contains_all \
    "scripts/pre-release-local.sh documents optional VM gate" \
    "$REPO_ROOT/scripts/pre-release-local.sh" \
    "--vm" \
    "--vm-full" \
    "prebuilt macOS base VM" \
    "HAZMAT_E2E_VM_PROVIDER=tart" \
    "bash scripts/e2e-vm.sh --step download --quick"

assert_fails_with \
    "scripts/e2e.sh rejects unknown VM provider" \
    "unsupported HAZMAT_E2E_VM_PROVIDER=nope" \
    env -u CI HAZMAT_E2E_VM_PROVIDER=nope bash "$REPO_ROOT/scripts/e2e.sh" --vm --vm-step pull

assert_fails_with \
    "scripts/e2e.sh tart provider checks tart before live work" \
    "tart not found" \
    env -u CI PATH="/usr/bin:/bin" HAZMAT_E2E_VM_PROVIDER=tart bash "$REPO_ROOT/scripts/e2e.sh" --vm --vm-step pull

assert_fails_with \
    "scripts/e2e.sh rejects VM keep outside VM mode" \
    "--keep is only valid with --vm" \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --keep

assert_fails_with \
    "scripts/e2e.sh rejects VM base reset outside VM mode" \
    "--reset-vm-base is only valid with --vm" \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --reset-vm-base

assert_fails_with \
    "scripts/e2e.sh rejects VM step outside VM mode" \
    "--vm-step is only valid with --vm" \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --vm-step base

assert_fails_with \
    "scripts/e2e.sh rejects unknown VM step" \
    "unknown --vm-step value: nope" \
    env -u CI bash "$REPO_ROOT/scripts/e2e.sh" --vm --vm-step nope

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
    "Claude onboarding smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-claude" \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" --run

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
    "README proof-stack smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-hazmat-exec" \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh" --run

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
    "Linux Apple Container smoke requires live ack" \
    "refusing live run without --i-understand-this-runs-apple-container-linux-tests" \
    bash "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" --run

assert_fails_with \
    "Linux Apple Container go-test requires live ack" \
    "refusing live go-test without --i-understand-this-runs-apple-container-linux-tests" \
    bash "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" --go-test

assert_fails_with \
    "Linux Apple Container dev shell requires live ack" \
    "refusing live run without --i-understand-this-runs-apple-container-linux-dev" \
    bash "$REPO_ROOT/scripts/linux-apple-container-dev.sh" --shell

assert_fails_with \
    "Linux Apple Container dev command requires live ack" \
    "refusing live run without --i-understand-this-runs-apple-container-linux-dev" \
    bash "$REPO_ROOT/scripts/linux-apple-container-dev.sh" --run -- go test ./platform/linux

assert_fails_with \
    "privileged install ownership check requires live ack" \
    "refusing live run without --i-understand-this-checks-privileged-install-ownership" \
    bash "$REPO_ROOT/scripts/check-privileged-install-ownership.sh" --run

assert_fails_with \
    "privileged install ownership rollback check requires live ack" \
    "refusing rollback residue check without --i-understand-this-checks-privileged-install-ownership" \
    bash "$REPO_ROOT/scripts/check-privileged-install-ownership.sh" --after-rollback

assert_fails_with \
    "make linux-apple-container-test requires APPLE_CONTAINER_ACK=1" \
    "Refusing to run live Apple Container Linux tests." \
    make -C "$REPO_ROOT/hazmat" linux-apple-container-test

assert_fails_with \
    "make linux-apple-container-dev requires APPLE_CONTAINER_ACK=1" \
    "Refusing to run live Apple Container Linux dev shell." \
    make -C "$REPO_ROOT/hazmat" linux-apple-container-dev

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
    "Claude onboarding smoke defaults to disclosure" \
    "claude-onboarding-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh"

assert_succeeds_with \
    "Claude onboarding smoke discloses prompt detection" \
    "auth, onboarding, or visual" \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh"

assert_succeeds_with \
    "Claude Workflow export smoke defaults to disclosure" \
    "claude-workflow-export-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh"

assert_succeeds_with \
    "Claude Workflow export smoke discloses default prompt fallback" \
    "default or override prompt" \
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
    "cache integration smoke discloses Ollama daemon prerequisite" \
    "Requires an already-running host Ollama daemon" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target ollama

assert_succeeds_with \
    "OpenHands recipe smoke defaults to disclosure" \
    "openhands-recipe-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh"

assert_succeeds_with \
    "OpenHands recipe smoke discloses process-mode boundary" \
    "does not treat OpenHands process mode as the" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh"

assert_succeeds_with \
    "README proof-stack smoke defaults to disclosure" \
    "readme-proof-stack-smoke: dry run only" \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh"

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
    "Linux Apple Container smoke defaults to disclosure" \
    "linux-apple-container-smoke: disclosure-only" \
    bash "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh"

assert_succeeds_with \
    "Linux Apple Container dev defaults to disclosure" \
    "linux-apple-container-dev: disclosure-only" \
    bash "$REPO_ROOT/scripts/linux-apple-container-dev.sh"

assert_succeeds_with \
    "Linux VM matrix transcript defaults to disclosure" \
    "linux-vm-matrix-transcript: disclosure-only" \
    sh "$REPO_ROOT/scripts/check-linux-vm-matrix-transcript.sh"

assert_succeeds_with \
    "Apple Container spike defaults to disclosure" \
    "spike-apple-container: disclosure-only" \
    bash "$REPO_ROOT/scripts/spike-apple-container.sh"

phase "Fixture and refusal UX guards"

assert_succeeds_with \
    "Claude onboarding smoke checks fixtures" \
    "claude-onboarding-smoke: fixtures ok" \
    env HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT=/bin/echo \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude onboarding smoke rejects missing Hazmat binary" \
    "/missing-hazmat is missing or not executable" \
    env HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT=/missing-hazmat \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude onboarding smoke rejects nonnumeric timeout" \
    "HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT must be a positive integer" \
    env HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT=fast \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" --check-fixtures

assert_file_contains_all \
    "Claude onboarding smoke detects prompt-shaped failures" \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" \
    "run_with_timeout" \
    "HAZMAT_CLAUDE_ONBOARDING_SMOKE_OK" \
    "output looks like Claude showed an auth or onboarding prompt" \
    "pty.openpty" \
    "interactive TUI output looks like Claude showed an auth/onboarding prompt" \
    "select.*(style|theme)" \
    "visual style"

assert_succeeds_with \
    "Claude Workflow export smoke has a default fixture" \
    "claude-workflow-export-smoke: fixtures ok" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_succeeds_with \
    "Claude Workflow export smoke has an embedded prompt fallback" \
    "claude-workflow-export-smoke: fixtures ok" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_DEFAULT_PROMPT_FILE="$REPO_ROOT/scripts/fixtures/missing-claude-workflow-prompt.txt" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude Workflow export smoke rejects missing prompt override" \
    "missing-claude-workflow-prompt.txt is not a readable regular file" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE="$REPO_ROOT/scripts/fixtures/missing-claude-workflow-prompt.txt" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude Workflow export smoke rejects directory prompt override" \
    "scripts/fixtures is not a readable regular file" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE="$REPO_ROOT/scripts/fixtures" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude Workflow export smoke rejects directory default prompt" \
    "scripts/fixtures is not a readable regular file" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_DEFAULT_PROMPT_FILE="$REPO_ROOT/scripts/fixtures" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude Workflow export smoke checks CLI version fixture" \
    "/usr/bin/false --version failed; verify the host Claude CLI installation" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=/usr/bin/false \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_fails_with \
    "Claude Workflow export smoke rejects relative Claude binary path" \
    "./claude must be an absolute path or command name" \
    env HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT=/bin/echo HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE=./claude \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" --check-fixtures

assert_file_contains_all \
    "Claude Workflow export smoke scans escaped agent paths" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" \
    "slash-escaped" \
    "unicode-slash-lower" \
    "unicode-slash-upper" \
    "sessions-index.json" \
    "exported session still references" \
    "matching exported files:" \
    "head -n 12"

assert_file_contains_all \
    "Codex desktop running-process refusal is bounded" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    "Codex App appears to be running (" \
    "Matching process sample:" \
    "... truncated " \
    "additional matching process(es)"

assert_file_order \
    "Codex desktop checks running app before sudo probe" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    'running="$(running_codex_processes || :)"' \
    'sudo -n -u "$AGENT_USER"'

assert_file_contains_all \
    "Codex app-server missing CLI guidance uses harness lifecycle" \
    "$REPO_ROOT/scripts/check-codex-app-server-smoke.sh" \
    "Codex CLI is not installed" \
    "run hazmat harness update codex"

assert_file_contains_all \
    "Codex desktop missing CLI guidance uses harness lifecycle" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    "Codex CLI is not installed" \
    "run hazmat harness update codex"

for smoke_script in \
    "$REPO_ROOT/scripts/check-codex-app-server-smoke.sh" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    "$REPO_ROOT/scripts/e2e-bootstrap.sh" \
    "$REPO_ROOT/scripts/e2e-harness-smoke-native.sh"
do
    assert_file_contains_all \
        "$(basename "$smoke_script") distinguishes fresh setup from drift repair" \
        "$smoke_script" \
        "fresh host:" \
        "hazmat init" \
        "setup drift:" \
        "hazmat doctor --fix" \
        "hazmat doctor --dry-run"
done

assert_file_contains_all \
    "session-home smoke explains activation blockers" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    "hazmat explain --json" \
    "extract-json-object.awk" \
    "plan-only session_home detail" \
    "activation stopped before the toolchain matrix" \
    "activation_blockers above" \
    "if no Blocking paths or session_home details were printed" \
    'HAZMAT_SESSION_HOME_SMOKE_HAZMAT="$PWD/hazmat/hazmat"' \
    "or reinstall Hazmat so the current blocker-detail" \
    "Do not rerun hazmat init."

assert_file_contains_all \
    "session-home smoke pairs repo binary with repo launch helper" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    'DEFAULT_LAUNCH_HELPER="$REPO_ROOT/hazmat/hazmat-launch"' \
    'LAUNCH_HELPER="${HAZMAT_SESSION_HOME_SMOKE_LAUNCH_HELPER:-$DEFAULT_LAUNCH_HELPER}"' \
    'HAZMAT_LAUNCH_HELPER="$LAUNCH_HELPER"'

assert_command_contains_all_and_not \
    "session-home JSON extractor preserves nested blocker detail" \
    '"following": {' \
    '"session_home": {' \
    '"activation_blockers": [' \
    '"path": ".npm"' \
    '"adapter": "toolchain-cache"' \
    -- \
    awk -v key=session_home -f "$REPO_ROOT/scripts/lib/extract-json-object.awk" "$REPO_ROOT/scripts/fixtures/session-home-explain.json"

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
    "--self-test-proxy" \
    "sudo -n" \
    "Agents must ask before running --check-prereqs or --run" \
    "--self-test-proxy is" \
    "local and does not launch Codex or run Hazmat"

assert_succeeds_with \
    "Codex desktop attach proxy self-test avoids raw params" \
    "codex-desktop-attach-smoke: proxy self-test ok" \
    "$REPO_ROOT/scripts/check-codex-desktop-attach-smoke.sh" --self-test-proxy

assert_help_contains_all \
    "session-home smoke documents sudo-adjacent prereqs" \
    "$REPO_ROOT/scripts/check-session-home-activation-smoke.sh" \
    "--check-prereqs" \
    "--skip-if-missing-prereqs" \
    "sudo -n" \
    "Agents must ask before running --check-prereqs, --skip-if-missing-prereqs, or --run"

assert_help_contains_all \
    "Linux Apple Container smoke documents approval-gated prereqs" \
    "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" \
    "--check-packages" \
    "--compile-tests" \
    "--check-prereqs" \
    "--go-test" \
    "--skip-if-missing-prereqs" \
    "container system status" \
    "Default: ./..." \
    "Agents must ask for explicit approval before running" \
    "--check-prereqs, --skip-if-missing-prereqs, --run, or --go-test"

assert_bash_help_contains_all \
    "Linux Apple Container dev documents approval-gated prereqs" \
    "$REPO_ROOT/scripts/linux-apple-container-dev.sh" \
    "--check-prereqs" \
    "--shell" \
    "--run" \
    "--skip-if-missing-prereqs" \
    "container system status" \
    "writable copy of the repository" \
    "Agents must" \
    "ask for explicit approval before running --check-prereqs, --skip-if-missing-prereqs," \
    "--check-prereqs, --skip-if-missing-prereqs," \
    "--shell, or --run"

assert_help_contains_all \
    "Linux VM matrix transcript documents non-live scope" \
    "$REPO_ROOT/scripts/check-linux-vm-matrix-transcript.sh" \
    "--mode current-user|agent-user" \
    "--skip-preflight" \
    "--output FILE" \
    "It does not" \
    "hazmat init, hazmat doctor --fix, rollback, sudo, or helper-backed launch"

assert_help_contains_all \
    "privileged install ownership check documents sudo-adjacent prereqs" \
    "$REPO_ROOT/scripts/check-privileged-install-ownership.sh" \
    "--check-prereqs" \
    "--skip-if-missing-prereqs" \
    "--after-rollback" \
    "sudo -n" \
    "Agents must ask before running --check-prereqs, --skip-if-missing-prereqs," \
    "--run, or --after-rollback."

assert_succeeds_with \
    "Linux Apple Container smoke default packages list for linux" \
    "linux-apple-container-smoke: packages ok" \
    bash "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" --check-packages

assert_succeeds_with \
    "Linux Apple Container smoke skips packages without tests" \
    "linux-apple-container-smoke: skip hazmat/cmd/hazmat; no compiled test binary" \
    env HAZMAT_LINUX_APPLE_CONTAINER_PACKAGES=./cmd/hazmat \
    bash "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" --compile-tests

assert_succeeds_with \
    "Linux VM current-user transcript keeps experimental claim" \
    "Support claim: experimental" \
    sh "$REPO_ROOT/scripts/check-linux-vm-matrix-transcript.sh" --mode current-user --run --skip-preflight

assert_succeeds_with \
    "Linux VM agent-user transcript keeps setup-required claim" \
    "Support claim: setup-required" \
    sh "$REPO_ROOT/scripts/check-linux-vm-matrix-transcript.sh" --mode agent-user --run --skip-preflight

assert_file_contains_all \
    "Linux Apple Container go-test uses writable container workspace" \
    "$REPO_ROOT/scripts/check-linux-apple-container-smoke.sh" \
    '--user "$guest_user"' \
    '--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly"' \
    "target=/private/tmp" \
    "--warning=no-file-changed" \
    "--exclude ./tla/states" \
    "--exclude ./spike-apple-container-results" \
    "GOWORK=off" \
    "GOCACHE=/work/gocache" \
    'GOMODCACHE="$gomodcache"' \
    "GOFLAGS=\"-mod=readonly"

assert_file_contains_all \
    "Linux Apple Container dev uses disposable writable workspace" \
    "$REPO_ROOT/scripts/linux-apple-container-dev.sh" \
    '--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly"' \
    '--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER_DEV/work,target=/work"' \
    "target=/private/tmp" \
    "--exclude ./tla/states" \
    "cd /work/src/hazmat" \
    "GOWORK=off" \
    "GOCACHE=/work/gocache" \
    'GOMODCACHE="$gomodcache"' \
    "GOFLAGS=\"-mod=readonly"

assert_help_contains_all \
    "Claude onboarding smoke documents fixture consent" \
    "$REPO_ROOT/scripts/check-claude-onboarding-smoke.sh" \
    "--check-fixtures" \
    "--skip-if-missing-fixtures" \
    "Fixture checks inspect local Hazmat setup" \
    "Agents must ask for explicit approval before" \
    "running --check-fixtures, --skip-if-missing-fixtures, or --run"

assert_help_contains_all \
    "Claude Workflow export smoke documents fixture consent" \
    "$REPO_ROOT/scripts/check-claude-workflow-export-smoke.sh" \
    "--check-fixtures" \
    "--skip-if-missing-fixtures" \
    "Fixture checks inspect local Hazmat/Claude tool setup" \
    "Agents must ask for explicit approval before running" \
    "--check-fixtures, --skip-if-missing-fixtures, or --run"

assert_help_contains_all \
    "cache integration smoke documents fixture consent" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" \
    "--check-fixtures" \
    "--skip-if-missing-fixtures" \
    "Fixture checks inspect local tool/cache setup" \
    "Agents must ask for explicit approval before" \
    "running --check-fixtures, --skip-if-missing-fixtures, or --run"

assert_file_contains_all \
    "cache integration smoke qualifies target fixture failures" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" \
    'add_missing_fixture "[$target] $*"' \
    'add_missing_target_fixture "$1" "python3 cannot import transformers"' \
    'add_missing_target_fixture "$1" "set HAZMAT_HF_SMOKE_MODEL' \
    'add_missing_target_fixture "$1" "no usable local Hugging Face model config or cached snapshot config' \
    'require_target_command "$1" "$OLLAMA_BIN"' \
    'add_missing_target_fixture "$1" "$OLLAMA_BIN list failed; start the Ollama daemon or check OLLAMA_HOST"' \
    'add_missing_target_fixture "$1" "python3 cannot import torch"' \
    'add_missing_target_fixture "$1" "set HAZMAT_TORCH_HUB_REPO' \
    'add_missing_target_fixture "$1" "no cached torch.hub repo matching' \
    'add_missing_target_fixture "$1" "set HAZMAT_TORCH_HUB_MODEL' \
    'add_missing_target_fixture "$1" "cached torch.hub repo'

assert_fails_with \
    "cache integration smoke checks Ollama daemon fixture" \
    "/bin/sh list failed; start the Ollama daemon or check OLLAMA_HOST" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HAZMAT_OLLAMA_SMOKE_BIN=/bin/sh \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target ollama --check-fixtures

assert_fails_with \
    "cache integration smoke rejects relative Ollama binary path" \
    "[ollama] ./ollama must be an absolute path or command name" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HAZMAT_OLLAMA_SMOKE_BIN=./ollama \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target ollama --check-fixtures

assert_succeeds_with \
    "cache integration smoke accepts fake Ollama fixture" \
    "cache-integration-smoke: fixtures ok" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HAZMAT_OLLAMA_SMOKE_BIN="$REPO_ROOT/scripts/fixtures/fake-ollama" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target ollama --check-fixtures

assert_fails_with \
    "cache integration smoke checks Hugging Face cache fixture" \
    "no usable local Hugging Face model config or cached snapshot config for sentence-transformers/all-MiniLM-L6-v2" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HOME="$REPO_ROOT/scripts/fixtures/missing-home" HAZMAT_HF_SMOKE_MODEL=sentence-transformers/all-MiniLM-L6-v2 \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target huggingface --check-fixtures

assert_fails_with \
    "cache integration smoke rejects Hugging Face file path fixture" \
    "no usable local Hugging Face model config or cached snapshot config for $REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HAZMAT_HF_SMOKE_MODEL="$REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target huggingface --check-fixtures

assert_fails_with \
    "cache integration smoke rejects Hugging Face directory without config" \
    "no usable local Hugging Face model config or cached snapshot config for $REPO_ROOT/scripts/fixtures/huggingface-model-without-config" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HAZMAT_HF_SMOKE_MODEL="$REPO_ROOT/scripts/fixtures/huggingface-model-without-config" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target huggingface --check-fixtures

assert_succeeds_with \
    "cache integration smoke accepts fake Hugging Face fixture" \
    "cache-integration-smoke: fixtures ok" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo PATH="$REPO_ROOT/scripts/fixtures/fake-bin:$PATH" HAZMAT_HF_SMOKE_MODEL="$REPO_ROOT/scripts/fixtures/huggingface-model-with-config" \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target huggingface --check-fixtures

assert_fails_with \
    "cache integration smoke checks torch-hub cache fixture" \
    "no cached torch.hub repo matching pytorch/vision" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo HOME="$REPO_ROOT/scripts/fixtures/missing-home" HAZMAT_TORCH_HUB_REPO=pytorch/vision HAZMAT_TORCH_HUB_MODEL=resnet18 \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target torch-hub --check-fixtures

assert_fails_with \
    "cache integration smoke checks torch-hub callable fixture" \
    "cached torch.hub repo pytorch/vision does not expose callable resnet18 in hubconf.py" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo TORCH_HOME="$REPO_ROOT/scripts/fixtures/torch-home-empty-callable" HAZMAT_TORCH_HUB_REPO=pytorch/vision HAZMAT_TORCH_HUB_MODEL=resnet18 \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target torch-hub --check-fixtures

assert_succeeds_with \
    "cache integration smoke accepts fake torch-hub fixture" \
    "cache-integration-smoke: fixtures ok" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo PATH="$REPO_ROOT/scripts/fixtures/fake-bin:$PATH" TORCH_HOME="$REPO_ROOT/scripts/fixtures/torch-home-callable" HAZMAT_TORCH_HUB_REPO=pytorch/vision HAZMAT_TORCH_HUB_MODEL=resnet18 \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target torch-hub --check-fixtures

assert_succeeds_with \
    "cache integration smoke accepts all fake fixtures" \
    "cache-integration-smoke: fixtures ok" \
    env HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT=/bin/echo PATH="$REPO_ROOT/scripts/fixtures/fake-bin:$PATH" HAZMAT_OLLAMA_SMOKE_BIN="$REPO_ROOT/scripts/fixtures/fake-ollama" HAZMAT_HF_SMOKE_MODEL="$REPO_ROOT/scripts/fixtures/huggingface-model-with-config" TORCH_HOME="$REPO_ROOT/scripts/fixtures/torch-home-callable" HAZMAT_TORCH_HUB_REPO=pytorch/vision HAZMAT_TORCH_HUB_MODEL=resnet18 \
    "$REPO_ROOT/scripts/check-cache-integration-smoke.sh" --target all --check-fixtures

assert_help_contains_all \
    "OpenHands recipe smoke documents fixture consent" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" \
    "--check-fixtures" \
    "--skip-if-missing-fixtures" \
    "Fixture checks inspect host-side OpenHands/Hazmat tool setup only" \
    "agent PATH and policy are proved by the approved live run" \
    "Agents must ask for explicit" \
    "running --check-fixtures, --skip-if-missing-fixtures, or --run"

assert_fails_with \
    "OpenHands recipe smoke qualifies explicit binary path fixture failures" \
    "missing-openhands is missing or not executable" \
    env HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT=/bin/echo HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN="$REPO_ROOT/scripts/fixtures/missing-openhands" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" --check-fixtures

assert_fails_with \
    "OpenHands recipe smoke rejects relative binary path" \
    "./openhands must be an absolute path or command name" \
    env HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT=/bin/echo HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN=./openhands \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" --check-fixtures

assert_fails_with \
    "OpenHands recipe smoke checks CLI help fixture" \
    "/usr/bin/false --help failed; verify the OpenHands CLI installation" \
    env HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT=/bin/echo HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN=/usr/bin/false \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" --check-fixtures

assert_succeeds_with \
    "OpenHands recipe smoke accepts fake CLI fixture" \
    "openhands-recipe-smoke: fixtures ok" \
    env HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT=/bin/echo HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN="$REPO_ROOT/scripts/fixtures/fake-openhands" \
    "$REPO_ROOT/scripts/check-openhands-recipe-smoke.sh" --check-fixtures

assert_help_contains_all \
    "README proof-stack smoke documents fixture consent" \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh" \
    "--check-fixtures" \
    "--skip-if-missing-fixtures" \
    "Fixture checks inspect local Hazmat setup and the selected host secret fixture" \
    "Agents must ask" \
    "--skip-if-missing-fixtures, or --run"

assert_fails_with \
    "README proof-stack smoke validates output dir fixture" \
    "claude-workflow-export-prompt.txt exists but is not a directory" \
    env HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT=/bin/echo HAZMAT_PROOF_STACK_SECRET_PATH="$REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt" \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh" --output-dir "$REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt" --check-fixtures

assert_fails_with \
    "README proof-stack smoke rejects relative Hazmat binary path" \
    "./hazmat/hazmat must be an absolute Hazmat binary path" \
    env HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT=./hazmat/hazmat HAZMAT_PROOF_STACK_SECRET_PATH="$REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt" \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh" --check-fixtures

assert_fails_with \
    "README proof-stack smoke rejects relative secret fixture" \
    "scripts/fixtures/claude-workflow-export-prompt.txt must be an absolute host secret fixture path" \
    env HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT=/bin/echo HAZMAT_PROOF_STACK_SECRET_PATH=scripts/fixtures/claude-workflow-export-prompt.txt \
    "$REPO_ROOT/scripts/check-readme-proof-stack-smoke.sh" --check-fixtures

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
