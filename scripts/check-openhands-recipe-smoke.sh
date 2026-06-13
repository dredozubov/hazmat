#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
OPENHANDS_BIN="${HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN:-openhands}"
MODE="disclose"
ACK=0
MISSING_FIXTURES=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-openhands-recipe-smoke.sh [options]

Guarded live smoke wrapper for the recipe-only OpenHands path.

By default, this script prints the exact live command and exits without running
Hazmat. Live mode requires:
  --run --i-understand-this-runs-hazmat-exec

Options:
  --check-fixtures                Check host-side fixture prerequisites only.
  --skip-if-missing-fixtures      Exit 0 when fixture prerequisites are absent.
  --run                           Run the live recipe smoke.
  --i-understand-this-runs-hazmat-exec
                                  Required acknowledgement for --run.
  -h, --help                      Show this help.

Environment:
  HAZMAT_OPENHANDS_RECIPE_SMOKE_HAZMAT  Hazmat binary to run.
  HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN     OpenHands executable name or path.

Fixture checks inspect local OpenHands/Hazmat tool setup. The live run is
sudo-adjacent because it invokes hazmat exec. Agents must ask for explicit
approval before running --check-fixtures, --skip-if-missing-fixtures, or --run.
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
		--i-understand-this-runs-hazmat-exec)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "openhands-recipe-smoke: unknown argument: $1" >&2
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
		add_missing_fixture "$HAZMAT is missing or not executable; run make first"
	fi
	require_command mktemp
	require_command "$OPENHANDS_BIN"

	if [ -n "$MISSING_FIXTURES" ]; then
		return 1
	fi
	return 0
}

print_missing_fixtures() {
	echo "openhands-recipe-smoke: missing fixtures:" >&2
	printf '%s\n' "$MISSING_FIXTURES" >&2
}

print_disclosure() {
	cat <<EOF
openhands-recipe-smoke: dry run only

This script validates the recipe-only OpenHands path with a live hazmat exec
session. It does not install OpenHands, import host ~/.openhands, pass a host
Docker socket, configure provider credentials, or claim first-class
hazmat-openhands support.

Live mode is sudo-adjacent and requires explicit approval:

  scripts/check-openhands-recipe-smoke.sh --run --i-understand-this-runs-hazmat-exec

Live smoke shape:
  hazmat exec --network none --no-backup -C <scratch-project> -- $OPENHANDS_BIN --help

Fixture check:
  scripts/check-openhands-recipe-smoke.sh --check-fixtures
EOF
}

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

run_smoke() {
	SCRATCH="$(mktemp -d /tmp/hazmat-openhands-recipe-smoke.XXXXXX)"
	PROJECT="$SCRATCH/project"
	mkdir -p "$PROJECT"
	chmod 755 "$SCRATCH" "$PROJECT"

	"$HAZMAT" exec \
		--docker=none \
		--network none \
		--no-backup \
		-C "$PROJECT" \
		-- "$OPENHANDS_BIN" --help
}

case "$MODE" in
	disclose)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_fixtures; then
			echo "openhands-recipe-smoke: fixtures ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ]; then
			echo "openhands-recipe-smoke: skipped because fixtures are missing" >&2
			print_missing_fixtures
			exit 0
		fi
		print_missing_fixtures
		exit 2
		;;
	run)
		if [ "$ACK" != "1" ]; then
			echo "openhands-recipe-smoke: refusing live run without --i-understand-this-runs-hazmat-exec" >&2
			exit 2
		fi
		if ! check_fixtures; then
			print_missing_fixtures
			exit 2
		fi
		run_smoke
		;;
esac
