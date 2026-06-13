#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
SECRET_PATH="${HAZMAT_PROOF_STACK_SECRET_PATH:-$HOME/.ssh/id_ed25519}"
MODE="disclose"
ACK=0
OUTPUT_DIR=""
MISSING_FIXTURES=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-readme-proof-stack-smoke.sh [options]

Guarded live smoke wrapper for README proof-stack snippets.

By default, this script prints the exact live command and exits without running
Hazmat. Live mode requires:
  --run --i-understand-this-runs-hazmat-exec

Options:
  --check-fixtures                Check host-side fixture prerequisites only.
  --skip-if-missing-fixtures      Exit 0 when fixture prerequisites are absent.
  --run                           Run the live proof-stack smoke.
  --output-dir DIR                Write sanitized session and diff snippets to DIR.
  --i-understand-this-runs-hazmat-exec
                                  Required acknowledgement for --run.
  -h, --help                      Show this help.

Environment:
  HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT  Hazmat binary to run.
  HAZMAT_PROOF_STACK_SECRET_PATH          Host secret fixture path to prove
                                          unreadable from the contained session.
                                          Default: $HOME/.ssh/id_ed25519

Fixture checks inspect local Hazmat setup and the selected host secret fixture.
The live run is sudo-adjacent because it invokes hazmat exec. Agents must ask
for explicit approval before running --check-fixtures,
--skip-if-missing-fixtures, or --run.
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
		--output-dir)
			if [ "$#" -lt 2 ]; then
				echo "readme-proof-stack-smoke: --output-dir requires a value" >&2
				exit 2
			fi
			OUTPUT_DIR="$2"
			shift
			;;
		--output-dir=*)
			OUTPUT_DIR="${1#--output-dir=}"
			;;
		--i-understand-this-runs-hazmat-exec)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "readme-proof-stack-smoke: unknown argument: $1" >&2
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

validate_output_dir_fixture() {
	if [ -z "$OUTPUT_DIR" ]; then
		return
	fi

	if [ -e "$OUTPUT_DIR" ] && [ ! -d "$OUTPUT_DIR" ]; then
		add_missing_fixture "$OUTPUT_DIR exists but is not a directory"
		return
	fi

	parent="$OUTPUT_DIR"
	while [ ! -e "$parent" ]; do
		next_parent="$(dirname "$parent")"
		if [ "$next_parent" = "$parent" ]; then
			break
		fi
		parent="$next_parent"
	done

	if [ ! -d "$parent" ]; then
		add_missing_fixture "$OUTPUT_DIR has no existing directory ancestor"
	elif [ ! -w "$parent" ]; then
		add_missing_fixture "$OUTPUT_DIR cannot be created because $parent is not writable"
	fi
}

check_fixtures() {
	MISSING_FIXTURES=""

	if [ ! -x "$HAZMAT" ]; then
		add_missing_fixture "$HAZMAT is missing or not executable; run make first"
	fi
	require_command mktemp
	require_command git

	case "$SECRET_PATH" in
		/*)
			;;
		*)
			add_missing_fixture "$SECRET_PATH must be an absolute host secret fixture path"
			;;
	esac

	if [ ! -e "$SECRET_PATH" ]; then
		add_missing_fixture "$SECRET_PATH does not exist; set HAZMAT_PROOF_STACK_SECRET_PATH to an existing host secret fixture"
	elif [ ! -f "$SECRET_PATH" ]; then
		add_missing_fixture "$SECRET_PATH is not a regular file"
	elif [ ! -r "$SECRET_PATH" ]; then
		add_missing_fixture "$SECRET_PATH is not readable by the invoking user"
	fi
	validate_output_dir_fixture

	if [ -n "$MISSING_FIXTURES" ]; then
		return 1
	fi
	return 0
}

print_missing_fixtures() {
	echo "readme-proof-stack-smoke: missing fixtures:" >&2
	printf '%s\n' "$MISSING_FIXTURES" >&2
}

print_disclosure() {
	cat <<EOF
readme-proof-stack-smoke: dry run only

This script captures the README proof-stack shape with a live hazmat exec
session:
- create a scratch demo project
- write proof.txt inside the contained session
- attempt to read the selected host secret fixture without printing its bytes
- run hazmat diff afterward for recovery evidence

Selected host secret fixture:
  $SECRET_PATH

Live mode and fixture checks are sudo-adjacent and require explicit approval:

  scripts/check-readme-proof-stack-smoke.sh --check-fixtures
  scripts/check-readme-proof-stack-smoke.sh --run --i-understand-this-runs-hazmat-exec

To save sanitized snippets:

  scripts/check-readme-proof-stack-smoke.sh --run --output-dir docs/proofs --i-understand-this-runs-hazmat-exec

Live smoke shape:
  hazmat exec --docker=none --network none -C <scratch-project> -- /bin/sh -eu -s <host-secret-path>
  (cd <scratch-project> && hazmat diff)
EOF
}

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

run_smoke() {
	SCRATCH="$(mktemp -d /tmp/hazmat-readme-proof-stack.XXXXXX)"
	PROJECT="$SCRATCH/project"
	mkdir -p "$PROJECT"
	chmod 755 "$SCRATCH" "$PROJECT"
	git -C "$PROJECT" init -q
	printf '%s\n' '# Hazmat proof demo' >"$PROJECT/README.md"

	set +e
	SESSION_OUTPUT="$("$HAZMAT" exec \
		--docker=none \
		--network none \
		-C "$PROJECT" \
		-- /bin/sh -eu -s "$SECRET_PATH" <<'PROOF_STACK_SESSION' 2>&1
secret_path="$1"

printf '%s\n' 'contained session wrote this file' >proof.txt
test -f proof.txt
printf '%s\n' 'proof-stack: project write ok'

if cat "$secret_path" >/dev/null 2>&1; then
	echo "proof-stack: host secret unexpectedly readable: $secret_path" >&2
	exit 21
fi

printf '%s\n' "proof-stack: host secret unreadable from contained session: $secret_path"
PROOF_STACK_SESSION
	)"
	SESSION_STATUS=$?
	set -e

	printf '%s\n' "$SESSION_OUTPUT"
	if [ "$SESSION_STATUS" -ne 0 ]; then
		return "$SESSION_STATUS"
	fi

	set +e
	DIFF_OUTPUT="$(cd "$PROJECT" && "$HAZMAT" diff 2>&1)"
	DIFF_STATUS=$?
	set -e

	printf '%s\n' "$DIFF_OUTPUT"
	if [ "$DIFF_STATUS" -ne 0 ]; then
		return "$DIFF_STATUS"
	fi

	if [ -n "$OUTPUT_DIR" ]; then
		mkdir -p "$OUTPUT_DIR"
		printf '%s\n' "$SESSION_OUTPUT" >"$OUTPUT_DIR/readme-proof-stack-session.txt"
		printf '%s\n' "$DIFF_OUTPUT" >"$OUTPUT_DIR/readme-proof-stack-diff.txt"
		printf 'readme-proof-stack-smoke: wrote sanitized snippets to %s\n' "$OUTPUT_DIR"
	fi
}

case "$MODE" in
	disclose)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_fixtures; then
			echo "readme-proof-stack-smoke: fixtures ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ]; then
			echo "readme-proof-stack-smoke: skipped because fixtures are missing" >&2
			print_missing_fixtures
			exit 0
		fi
		print_missing_fixtures
		exit 2
		;;
	run)
		if [ "$ACK" != "1" ]; then
			echo "readme-proof-stack-smoke: refusing live run without --i-understand-this-runs-hazmat-exec" >&2
			exit 2
		fi
		if ! check_fixtures; then
			print_missing_fixtures
			exit 2
		fi
		run_smoke
		;;
esac
