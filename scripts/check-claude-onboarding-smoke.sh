#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
TIMEOUT_SECONDS="${HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT:-180}"
SENTINEL="${HAZMAT_CLAUDE_ONBOARDING_SMOKE_SENTINEL:-HAZMAT_CLAUDE_ONBOARDING_SMOKE_OK}"
PROMPT="${HAZMAT_CLAUDE_ONBOARDING_SMOKE_PROMPT:-Reply with exactly $SENTINEL and nothing else.}"
MODE="disclose"
ACK=0
MISSING_FIXTURES=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-claude-onboarding-smoke.sh [options]

Guarded live smoke wrapper for repeated Claude auth/onboarding prompts.

By default, this script prints the exact live command and exits without running
Hazmat or Claude. Live mode requires:
  --run --i-understand-this-runs-hazmat-claude

Options:
  --check-fixtures                Check host-side fixture prerequisites only.
  --skip-if-missing-fixtures      Exit 0 when fixture prerequisites are absent.
  --run                           Run the live Claude print-mode smoke.
  --i-understand-this-runs-hazmat-claude
                                  Required acknowledgement for --run.
  -h, --help                      Show this help.

Environment:
  HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT
                                  Hazmat binary to run. Defaults to the checkout
                                  binary at hazmat/hazmat.
  HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT
                                  Timeout in seconds. Default: 180.
  HAZMAT_CLAUDE_ONBOARDING_SMOKE_SENTINEL
                                  Expected exact response marker.
  HAZMAT_CLAUDE_ONBOARDING_SMOKE_PROMPT
                                  Prompt sent to Claude print mode.

Fixture checks inspect local Hazmat setup. The live run is sudo-adjacent because
it invokes hazmat claude, which may materialize, observe, or repair host Claude
credential/onboarding state. Agents must ask for explicit approval before
running --check-fixtures, --skip-if-missing-fixtures, or --run.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--check-fixtures)
			MODE="check"
			;;
		--skip-if-missing-fixtures)
			MODE="skip"
			;;
		--run)
			MODE="run"
			;;
		--i-understand-this-runs-hazmat-claude)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "claude-onboarding-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

add_missing_fixture() {
	if [ -z "$MISSING_FIXTURES" ]; then
		MISSING_FIXTURES="- $*"
	else
		MISSING_FIXTURES="$MISSING_FIXTURES
- $*"
	fi
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		add_missing_fixture "$1 is not on PATH"
	fi
}

check_fixtures() {
	MISSING_FIXTURES=""

	if [ ! -x "$HAZMAT" ]; then
		add_missing_fixture "$HAZMAT is missing or not executable; run make first or set HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT"
	fi
	require_command mktemp
	require_command mkdir
	require_command chmod
	require_command sleep
	require_command kill
	require_command grep
	require_command sed
	require_command head

	case "$TIMEOUT_SECONDS" in
		''|*[!0-9]*)
			add_missing_fixture "HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT must be a positive integer"
			;;
		0)
			add_missing_fixture "HAZMAT_CLAUDE_ONBOARDING_SMOKE_TIMEOUT must be greater than zero"
			;;
	esac

	if [ -n "$MISSING_FIXTURES" ]; then
		return 1
	fi
	return 0
}

print_missing_fixtures() {
	echo "claude-onboarding-smoke: missing fixtures:" >&2
	printf '%s\n' "$MISSING_FIXTURES" >&2
}

print_disclosure() {
	cat <<EOF
claude-onboarding-smoke: dry run only

This script validates the live hazmat claude startup path against the repeated
auth/onboarding regression. It starts a contained Claude print-mode run in a
scratch project, waits for a bounded response, fails if the output looks like an
auth or onboarding prompt, and requires the expected sentinel response:

  $SENTINEL

Live mode is sudo-adjacent and requires explicit approval:

  scripts/check-claude-onboarding-smoke.sh --run --i-understand-this-runs-hazmat-claude

Live smoke shape:
  hazmat claude --no-backup -C <scratch-project> -p "$PROMPT"

Use the installed binary explicitly when validating an installed build:
  HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT=/path/to/hazmat scripts/check-claude-onboarding-smoke.sh --run --i-understand-this-runs-hazmat-claude

Fixture check:
  scripts/check-claude-onboarding-smoke.sh --check-fixtures

Agents must ask before running --check-fixtures, --skip-if-missing-fixtures, or --run.
EOF
}

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

redact_output() {
	sed -E \
		-e 's/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/<email>/g' \
		-e 's/sk-ant-[A-Za-z0-9_-]+/<redacted>/g' \
		-e 's/(sessionKey|accessToken|refreshToken|oauthAccount)[^[:space:]]*/\1=<redacted>/g' |
		head -n 80
}

print_bounded_output() {
	output_file="$1"
	if [ -s "$output_file" ]; then
		echo "claude-onboarding-smoke: bounded output follows:" >&2
		redact_output <"$output_file" >&2
	else
		echo "claude-onboarding-smoke: command produced no output" >&2
	fi
}

output_has_onboarding_prompt() {
	output_file="$1"
	grep -E -i \
		'(/login|log in|login|sign in|authenticate|authentication required|not authenticated|please authenticate|onboarding|welcome to claude code|visual style|select.*style|choose.*style|theme|press enter|open.*browser|claude[.]ai/login|anthropic console)' \
		"$output_file" >/dev/null 2>&1
}

run_with_timeout() {
	timeout_seconds="$1"
	output_file="$2"
	timeout_marker="$3"
	shift 3

	"$@" >"$output_file" 2>&1 &
	cmd_pid=$!
	(
		sleep "$timeout_seconds"
		if kill -0 "$cmd_pid" 2>/dev/null; then
			: >"$timeout_marker"
			kill "$cmd_pid" 2>/dev/null || true
			sleep 2
			kill -KILL "$cmd_pid" 2>/dev/null || true
		fi
	) &
	watchdog_pid=$!

	set +e
	wait "$cmd_pid"
	status=$?
	set -e

	kill "$watchdog_pid" 2>/dev/null || true
	wait "$watchdog_pid" 2>/dev/null || true

	if [ -f "$timeout_marker" ]; then
		return 124
	fi
	return "$status"
}

run_smoke() {
	SCRATCH="$(mktemp -d /tmp/hazmat-claude-onboarding-smoke.XXXXXX)"
	PROJECT="$SCRATCH/project"
	OUTPUT="$SCRATCH/output.txt"
	TIMEOUT_MARKER="$SCRATCH/timed-out"
	mkdir -p "$PROJECT"
	chmod 755 "$SCRATCH" "$PROJECT"

	set +e
	run_with_timeout "$TIMEOUT_SECONDS" "$OUTPUT" "$TIMEOUT_MARKER" \
		"$HAZMAT" claude \
		--no-backup \
		-C "$PROJECT" \
		-p "$PROMPT"
	status=$?
	set -e

	if [ "$status" -eq 124 ]; then
		echo "claude-onboarding-smoke: timed out after ${TIMEOUT_SECONDS}s waiting for Claude print-mode response" >&2
		echo "claude-onboarding-smoke: this is consistent with an interactive auth/onboarding prompt" >&2
		print_bounded_output "$OUTPUT"
		exit 1
	fi

	if output_has_onboarding_prompt "$OUTPUT"; then
		echo "claude-onboarding-smoke: output looks like Claude showed an auth or onboarding prompt" >&2
		print_bounded_output "$OUTPUT"
		exit 1
	fi

	if [ "$status" -ne 0 ]; then
		echo "claude-onboarding-smoke: hazmat claude exited with status $status" >&2
		print_bounded_output "$OUTPUT"
		exit 1
	fi

	if ! grep -F "$SENTINEL" "$OUTPUT" >/dev/null 2>&1; then
		echo "claude-onboarding-smoke: expected sentinel response not found: $SENTINEL" >&2
		print_bounded_output "$OUTPUT"
		exit 1
	fi

	echo "claude-onboarding-smoke: print-mode startup ok"
}

case "$MODE" in
	disclose)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_fixtures; then
			echo "claude-onboarding-smoke: fixtures ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ]; then
			echo "claude-onboarding-smoke: skipped because fixtures are missing" >&2
			print_missing_fixtures
			exit 0
		fi
		print_missing_fixtures
		exit 2
		;;
	run)
		if [ "$ACK" != "1" ]; then
			echo "claude-onboarding-smoke: refusing live run without --i-understand-this-runs-hazmat-claude" >&2
			exit 2
		fi
		if ! check_fixtures; then
			print_missing_fixtures
			exit 2
		fi
		run_smoke
		;;
esac
