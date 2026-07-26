#!/bin/sh

set -eu

RUN=0
ACK=0
PHASE=""
ENDPOINT=""
CONFIG_FILE=""
AUTHORITY_FILE=""
AUTHORITY_SHA256=""
CLIENT_SEED_FILE=""
CHECKPOINT_ROOT=""
HANDOFF_FILE=""
EXPECTED_ERROR_CLASS=""
PREBUILT_TEST_BINARY=""

usage() {
	cat <<'EOF'
Usage:
  scripts/check-planescape-configured-provider-live.sh
  scripts/check-planescape-configured-provider-live.sh \
    --run \
    --i-understand-this-contacts-a-live-planescape-provider \
    --phase PHASE \
    --endpoint NUMERIC_IP:PORT \
    --config-file ABSOLUTE_PRIVATE_FILE \
    --authority-file ABSOLUTE_PRIVATE_FILE \
    --authority-sha256 sha256:LOWERCASE_HEX \
    --client-seed-file ABSOLUTE_PRIVATE_FILE \
    --checkpoint-root ABSOLUTE_PRIVATE_DIRECTORY \
    [--handoff-file ABSOLUTE_PRIVATE_FILE] \
    [--expected-error-class CLASS] \
    [--prebuilt-test-binary ABSOLUTE_OWNER_ONLY_EXECUTABLE]

Phases:
  lifecycle       Run configured Tool -> Pause -> Freeze -> Closeout.
  restart-prime   Run configured Tool -> Pause and create --handoff-file.
  restart-replay  Reconnect after a coordinator-managed provider restart,
                  replay Tool and Pause exactly, then Freeze -> Closeout.
  unavailable     Require configured-provider class unavailable.
  denial          Require --expected-error-class invalid, unsupported, or conflict.

The coordinator starts, restarts, and stops the external Planescape endpoint.
This harness never controls Tart or Planescape and never creates plan authority.
Without --prebuilt-test-binary, the harness builds the Go test from source.
The prebuilt binary must be a current-user-owned, single-link 0700 regular file.
When the harness runs as root, a root-owned single-link 0500 file is also valid.
Live diagnostics contain only a phase and a stable status/reason.
EOF
}

fail() {
	printf '%s\n' "hazmat-planescape-live-acceptance: status=fail reason=$1" >&2
	exit 2
}

take_value() {
	if [ "$#" -lt 2 ] || [ -z "$2" ]; then
		fail "missing-input"
	fi
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			RUN=1
			;;
		--i-understand-this-contacts-a-live-planescape-provider)
			ACK=1
			;;
		--phase)
			take_value "$@"
			shift
			PHASE="$1"
			;;
		--endpoint)
			take_value "$@"
			shift
			ENDPOINT="$1"
			;;
		--config-file)
			take_value "$@"
			shift
			CONFIG_FILE="$1"
			;;
		--authority-file)
			take_value "$@"
			shift
			AUTHORITY_FILE="$1"
			;;
		--authority-sha256)
			take_value "$@"
			shift
			AUTHORITY_SHA256="$1"
			;;
		--client-seed-file)
			take_value "$@"
			shift
			CLIENT_SEED_FILE="$1"
			;;
		--checkpoint-root)
			take_value "$@"
			shift
			CHECKPOINT_ROOT="$1"
			;;
		--handoff-file)
			take_value "$@"
			shift
			HANDOFF_FILE="$1"
			;;
		--expected-error-class)
			take_value "$@"
			shift
			EXPECTED_ERROR_CLASS="$1"
			;;
		--prebuilt-test-binary)
			take_value "$@"
			shift
			PREBUILT_TEST_BINARY="$1"
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			fail "unknown-argument"
			;;
	esac
	shift
done

if [ "$RUN" -ne 1 ]; then
	cat <<'EOF'
hazmat-planescape-live-acceptance: disclosure-only

Live mode contacts an already-running protected Planescape endpoint through
Hazmat's configured product path. Run with --help for the exact phase contract.
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	fail "missing-acknowledgement"
fi

case "$PHASE" in
	lifecycle|restart-prime|restart-replay|unavailable|denial)
		;;
	"")
		fail "missing-phase"
		;;
	*)
		fail "invalid-phase"
		;;
esac

for required in \
	"$ENDPOINT" \
	"$CONFIG_FILE" \
	"$AUTHORITY_FILE" \
	"$AUTHORITY_SHA256" \
	"$CLIENT_SEED_FILE" \
	"$CHECKPOINT_ROOT"
do
	if [ -z "$required" ]; then
		fail "missing-input"
	fi
done

for private_path in \
	"$CONFIG_FILE" \
	"$AUTHORITY_FILE" \
	"$CLIENT_SEED_FILE" \
	"$CHECKPOINT_ROOT"
do
	case "$private_path" in
		/*)
			;;
		*)
			fail "invalid-private-path"
			;;
	esac
done

for existing_file in \
	"$CONFIG_FILE" \
	"$AUTHORITY_FILE" \
	"$CLIENT_SEED_FILE"
do
	if [ ! -f "$existing_file" ] || [ -L "$existing_file" ]; then
		fail "unsafe-private-file"
	fi
done

if [ "${#AUTHORITY_SHA256}" -ne 71 ]; then
	fail "invalid-authority-sha256"
fi
case "$AUTHORITY_SHA256" in
	sha256:*[!0-9a-f]*)
		fail "invalid-authority-sha256"
		;;
	sha256:*)
		;;
	*)
		fail "invalid-authority-sha256"
		;;
esac

case "$PHASE" in
	restart-prime)
		if [ -z "$HANDOFF_FILE" ]; then
			fail "missing-handoff"
		fi
		case "$HANDOFF_FILE" in
			/*)
				;;
			*)
				fail "invalid-private-path"
				;;
		esac
		if [ -e "$HANDOFF_FILE" ] || [ -L "$HANDOFF_FILE" ]; then
			fail "unsafe-handoff"
		fi
		;;
	restart-replay)
		if [ -z "$HANDOFF_FILE" ]; then
			fail "missing-handoff"
		fi
		case "$HANDOFF_FILE" in
			/*)
				;;
			*)
				fail "invalid-private-path"
				;;
		esac
		if [ ! -f "$HANDOFF_FILE" ] || [ -L "$HANDOFF_FILE" ]; then
			fail "unsafe-handoff"
		fi
		;;
	*)
		if [ -n "$HANDOFF_FILE" ]; then
			fail "unexpected-handoff"
		fi
		;;
esac

case "$PHASE" in
	denial)
		case "$EXPECTED_ERROR_CLASS" in
			invalid|unsupported|conflict)
				;;
			"")
				fail "missing-error-class"
				;;
			*)
				fail "invalid-error-class"
				;;
		esac
		;;
	*)
		if [ -n "$EXPECTED_ERROR_CLASS" ]; then
			fail "unexpected-error-class"
		fi
		;;
esac

prebuilt_test_binary_metadata() {
	if metadata="$(stat -f '%u:%Lp:%l' "$PREBUILT_TEST_BINARY" 2>/dev/null)"; then
		printf '%s\n' "$metadata"
		return 0
	fi
	stat -c '%u:%a:%h' "$PREBUILT_TEST_BINARY" 2>/dev/null
}

prebuilt_test_binary_metadata_is_safe() {
	policy_uid="$1"
	policy_metadata="$2"
	if [ "$policy_metadata" = "$policy_uid:700:1" ]; then
		return 0
	fi
	[ "$policy_uid" = "0" ] &&
		[ "$policy_metadata" = "0:500:1" ]
}

if [ -n "$PREBUILT_TEST_BINARY" ]; then
	case "$PREBUILT_TEST_BINARY" in
		/*)
			;;
		*)
			fail "invalid-test-binary-path"
			;;
	esac
	if [ ! -f "$PREBUILT_TEST_BINARY" ] ||
		[ -L "$PREBUILT_TEST_BINARY" ] ||
		[ ! -x "$PREBUILT_TEST_BINARY" ]; then
		fail "unsafe-test-binary"
	fi
	CURRENT_UID="$(id -u 2>/dev/null)" ||
		fail "harness-unavailable"
	PREBUILT_TEST_BINARY_METADATA="$(prebuilt_test_binary_metadata)" ||
		fail "unsafe-test-binary"
	if ! prebuilt_test_binary_metadata_is_safe \
		"$CURRENT_UID" \
		"$PREBUILT_TEST_BINARY_METADATA"; then
		fail "unsafe-test-binary"
	fi
else
	SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)" ||
		fail "harness-unavailable"
	REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." 2>/dev/null && pwd)" ||
		fail "harness-unavailable"
fi

umask 077
RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-planescape-live.XXXXXX" 2>/dev/null)" ||
	fail "harness-unavailable"
trap 'rm -rf -- "$RUN_DIR"' EXIT HUP INT TERM
GO_OUTPUT="$RUN_DIR/go-test.output"

run_with_live_environment() {
	HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE=1 \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_PHASE="$PHASE" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_ENDPOINT="$ENDPOINT" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CONFIG_FILE="$CONFIG_FILE" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE="$AUTHORITY_FILE" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_SHA256="$AUTHORITY_SHA256" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CLIENT_SEED_FILE="$CLIENT_SEED_FILE" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CHECKPOINT_ROOT="$CHECKPOINT_ROOT" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE="$HANDOFF_FILE" \
		HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_EXPECTED_ERROR_CLASS="$EXPECTED_ERROR_CLASS" \
		"$@"
}

run_live_acceptance() {
	if [ -n "$PREBUILT_TEST_BINARY" ]; then
		run_with_live_environment \
			"$PREBUILT_TEST_BINARY" \
			-test.count=1 \
			-test.timeout=180s \
			-test.v \
			-test.run '^TestConfiguredPlanescapeProviderLiveAcceptance$'
		return
	fi

	(
		cd "$REPO_ROOT/hazmat" &&
			run_with_live_environment \
				go test -count=1 -timeout=180s \
				-run '^TestConfiguredPlanescapeProviderLiveAcceptance$' .
	)
}

if run_live_acceptance >"$GO_OUTPUT" 2>&1; then
	if [ -n "$PREBUILT_TEST_BINARY" ] &&
		! grep -Fqx \
			'=== RUN   TestConfiguredPlanescapeProviderLiveAcceptance' \
			"$GO_OUTPUT"; then
		printf '%s\n' \
			"hazmat-planescape-live-acceptance: phase=$PHASE status=fail reason=acceptance" >&2
		exit 1
	fi
	printf '%s\n' \
		"hazmat-planescape-live-acceptance: phase=$PHASE status=pass"
	exit 0
fi

printf '%s\n' \
	"hazmat-planescape-live-acceptance: phase=$PHASE status=fail reason=acceptance" >&2
exit 1
