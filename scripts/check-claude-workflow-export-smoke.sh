#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
CLAUDE_BIN="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE:-claude}"
DEFAULT_PROMPT_FILE="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_DEFAULT_PROMPT_FILE:-$REPO_ROOT/scripts/fixtures/claude-workflow-export-prompt.txt}"
PROMPT_FILE_OVERRIDE="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE:-}"
PROMPT_FILE="${PROMPT_FILE_OVERRIDE:-$DEFAULT_PROMPT_FILE}"
RESUME_PROMPT="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_RESUME_PROMPT:-Inspect this resumed session briefly. If prior Workflow artifacts are unavailable, say so without rerunning the whole task.}"
AGENT_PROJECT_ROOT="${HAZMAT_CLAUDE_WORKFLOW_SMOKE_AGENT_PROJECT_ROOT:-/Users/agent/.claude/projects}"
MODE="disclose"
ACK=0
MISSING_FIXTURES=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-claude-workflow-export-smoke.sh [options]

Guarded live smoke wrapper for Claude Workflow/subagent export-resume handoff.

By default, this script prints the exact live command and exits without running
Hazmat or Claude. Live mode requires:
  --run --i-understand-this-runs-hazmat-claude-and-host-claude

Options:
  --check-fixtures                Check host-side fixture prerequisites only.
  --skip-if-missing-fixtures      Exit 0 when fixture prerequisites are absent.
  --run                           Run the live export/resume smoke.
  --i-understand-this-runs-hazmat-claude-and-host-claude
                                  Required acknowledgement for --run.
  -h, --help                      Show this help.

Environment:
  HAZMAT_CLAUDE_WORKFLOW_SMOKE_HAZMAT       Hazmat binary to run.
  HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE       Host Claude executable name/path.
  HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE  Optional prompt file override for
                                            live run. Defaults to
                                            scripts/fixtures/claude-workflow-export-prompt.txt.
                                            It should produce Workflow/subagent
                                            sidecar artifacts in Claude Code.
  HAZMAT_CLAUDE_WORKFLOW_SMOKE_RESUME_PROMPT
                                            Host-side resume prompt.

Fixture checks inspect local Hazmat/Claude tool setup. The live run is
sudo-adjacent because it invokes hazmat claude and also runs host Claude with
--resume. Agents must ask for explicit approval before running
--check-fixtures, --skip-if-missing-fixtures, or --run.
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
		--i-understand-this-runs-hazmat-claude-and-host-claude)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "claude-workflow-export-smoke: unknown argument: $1" >&2
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
	case "$1" in
		*/*)
			if [ ! -x "$1" ]; then
				add_missing_fixture "$1 is missing or not executable"
			fi
			;;
		*)
			if ! command -v "$1" >/dev/null 2>&1; then
				add_missing_fixture "$1 is not on PATH"
			fi
			;;
	esac
}

command_available() {
	case "$1" in
		*/*)
			[ -x "$1" ]
			;;
		*)
			command -v "$1" >/dev/null 2>&1
			;;
	esac
}

default_prompt_text() {
	cat <<'EOF'
Create a small reproducible Workflow/subagent artifact for a Hazmat export smoke.

Use the Task tool twice with two separate subagents:

1. Ask one subagent to inspect this scratch project and report the visible top-level files.
2. Ask another subagent to create a short implementation-risk note for exporting Claude sidecar metadata between users.

After both subagents finish, summarize their results in no more than five sentences. Do not modify files.
EOF
}

prompt_text() {
	if [ -n "$PROMPT_FILE_OVERRIDE" ] || [ -r "$PROMPT_FILE" ]; then
		cat "$PROMPT_FILE"
		return
	fi
	default_prompt_text
}

check_fixtures() {
	MISSING_FIXTURES=""

	if [ ! -x "$HAZMAT" ]; then
		add_missing_fixture "$HAZMAT is missing or not executable; run make first"
	fi
	require_command mktemp
	require_command find
	require_command grep
	require_command head
	require_command "$CLAUDE_BIN"
	if command_available "$CLAUDE_BIN" && ! "$CLAUDE_BIN" --version >/dev/null 2>&1; then
		add_missing_fixture "$CLAUDE_BIN --version failed; verify the host Claude CLI installation"
	fi
	if [ -n "$PROMPT_FILE_OVERRIDE" ] && [ ! -r "$PROMPT_FILE" ]; then
		add_missing_fixture "$PROMPT_FILE is not readable"
	fi

	if [ -n "$MISSING_FIXTURES" ]; then
		return 1
	fi
	return 0
}

print_missing_fixtures() {
	echo "claude-workflow-export-smoke: missing fixtures:" >&2
	printf '%s\n' "$MISSING_FIXTURES" >&2
}

print_disclosure() {
	cat <<EOF
claude-workflow-export-smoke: dry run only

This script validates the live Claude Workflow/subagent export-resume handoff.
It starts a contained Claude print-mode run from the default or override prompt,
exports the latest contained session into the host Claude store, checks that the
exported host transcript/sidecar files do not contain stale agent Claude project
paths, and resumes the exported session with host Claude.

Fixture checks inspect local Hazmat/Claude tool setup. Live mode is
sudo-adjacent and requires explicit approval:

  scripts/check-claude-workflow-export-smoke.sh --run --i-understand-this-runs-hazmat-claude-and-host-claude

Live smoke shape:
  hazmat claude --no-backup -C <scratch-project> -p "\$(cat $DEFAULT_PROMPT_FILE)"
  hazmat export claude session -C <scratch-project>
  claude --resume <exported-session-id> --fork-session -p <resume-prompt>

Prompt file:
  $PROMPT_FILE

If HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE is unset and the default fixture is
not present, the script uses an embedded fallback prompt with the same shape.

Fixture check:
  scripts/check-claude-workflow-export-smoke.sh --check-fixtures

Agents must ask before running --check-fixtures, --skip-if-missing-fixtures, or --run.
EOF
}

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

find_exported_session_path() {
	find "$HOME/.claude/projects" -type f -name "$1.jsonl" -print 2>/dev/null | head -n 1
}

agent_project_root_pattern() {
	case "$1" in
		literal)
			printf '%s\n' "$AGENT_PROJECT_ROOT"
			;;
		slash-escaped)
			printf '%s\n' "$AGENT_PROJECT_ROOT" | sed 's:/:\\/:g'
			;;
		unicode-slash-lower)
			printf '%s\n' "$AGENT_PROJECT_ROOT" | sed 's:/:\\u002f:g'
			;;
		unicode-slash-upper)
			printf '%s\n' "$AGENT_PROJECT_ROOT" | sed 's:/:\\u002F:g'
			;;
	esac
}

assert_export_has_no_agent_project_paths() {
	session_id="$1"
	host_transcript="$(find_exported_session_path "$session_id")"
	if [ -z "$host_transcript" ]; then
		echo "claude-workflow-export-smoke: exported host transcript not found for $session_id" >&2
		exit 1
	fi
	host_project_dir="$(dirname "$host_transcript")"
	host_sidecar_dir="$host_project_dir/$session_id"
	if [ ! -d "$host_sidecar_dir" ]; then
		echo "claude-workflow-export-smoke: exported session has no sidecar directory; prompt may not have produced Workflow/subagent artifacts" >&2
		exit 1
	fi

	for encoding in literal slash-escaped unicode-slash-lower unicode-slash-upper; do
		pattern="$(agent_project_root_pattern "$encoding")"
		if grep -R -F "$pattern" "$host_transcript" "$host_sidecar_dir" >/dev/null 2>&1; then
			echo "claude-workflow-export-smoke: exported session still references $AGENT_PROJECT_ROOT ($encoding)" >&2
			echo "claude-workflow-export-smoke: matching exported files:" >&2
			grep -R -l -F "$pattern" "$host_transcript" "$host_sidecar_dir" 2>/dev/null |
				sed 's/^/  - /' |
				head -n 12 >&2
			exit 1
		fi
	done
}

run_smoke() {
	SCRATCH="$(mktemp -d /tmp/hazmat-claude-workflow-export-smoke.XXXXXX)"
	PROJECT="$SCRATCH/project"
	mkdir -p "$PROJECT"
	chmod 755 "$SCRATCH" "$PROJECT"

	"$HAZMAT" claude \
		--no-backup \
		-C "$PROJECT" \
		-p "$(prompt_text)"

	session_id="$("$HAZMAT" export claude session -C "$PROJECT")"
	assert_export_has_no_agent_project_paths "$session_id"

	"$CLAUDE_BIN" \
		--resume "$session_id" \
		--fork-session \
		-p "$RESUME_PROMPT"

	echo "claude-workflow-export-smoke: export/resume smoke ok for $session_id"
}

case "$MODE" in
	disclose)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_fixtures; then
			echo "claude-workflow-export-smoke: fixtures ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ]; then
			echo "claude-workflow-export-smoke: skipped because fixtures are missing" >&2
			print_missing_fixtures
			exit 0
		fi
		print_missing_fixtures
		exit 2
		;;
	run)
		if [ "$ACK" != "1" ]; then
			echo "claude-workflow-export-smoke: refusing live run without --i-understand-this-runs-hazmat-claude-and-host-claude" >&2
			exit 2
		fi
		if ! check_fixtures; then
			print_missing_fixtures
			exit 2
		fi
		run_smoke
		;;
esac
