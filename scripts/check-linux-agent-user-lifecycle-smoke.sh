#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
APP_DIR="$REPO_ROOT/hazmat"
ACK=0
RUN=0
TMP_ROOT=""
CLEANUP_NEEDED=0

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-agent-user-lifecycle-smoke.sh
  scripts/check-linux-agent-user-lifecycle-smoke.sh --run --i-understand-this-runs-linux-agent-user-lifecycle-smoke

Default mode is disclosure-only. Live mode must run inside a disposable Linux
VM or disposable CI runner. It builds hazmat-launch, runs the modeled Linux
agent-user setup graph, runs prepared-host root-helper launch smokes, and then
runs default plus destructive rollback. It uses sudo, creates and deletes the
agent user/group/home, installs/removes /usr/local/libexec/hazmat-launch,
writes/removes /etc/sudoers.d/agent, and touches Linux setup policy paths.

Options:
  --run                                                       Run the live lifecycle smoke.
  --i-understand-this-runs-linux-agent-user-lifecycle-smoke   Required acknowledgement for --run.
  -h, --help                                                 Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			RUN=1
			;;
		--i-understand-this-runs-linux-agent-user-lifecycle-smoke)
			ACK=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-agent-user-lifecycle-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$RUN" -ne 1 ]; then
	cat <<'EOF'
linux-agent-user-lifecycle-smoke: disclosure-only

This command can collect Ubuntu A1-A11 lifecycle evidence for:
  docs/linux-agent-user-vm-lifecycle-matrix.md
  sandboxing-xuar.4.5

Live mode is approval-gated and must run in a disposable Linux VM or disposable
CI runner:
  scripts/check-linux-agent-user-lifecycle-smoke.sh --run --i-understand-this-runs-linux-agent-user-lifecycle-smoke
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	echo "linux-agent-user-lifecycle-smoke: refusing live run without --i-understand-this-runs-linux-agent-user-lifecycle-smoke" >&2
	exit 2
fi

if [ "$(uname -s)" != "Linux" ]; then
	echo "linux-agent-user-lifecycle-smoke: refusing live run outside Linux" >&2
	exit 2
fi

cleanup() {
	if [ "$CLEANUP_NEEDED" -ne 1 ]; then
		return
	fi
	CLEANUP_NEEDED=0
	echo
	echo "Cleanup:"
	(
		cd "$APP_DIR"
		HAZMAT_LINUX_AGENT_USER_CLEANUP_VM_SMOKE=1 \
		HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE=1 \
		HAZMAT_LINUX_ROOT_HELPER_SOURCE="$TMP_ROOT/hazmat-launch" \
			go test . -run '^TestLinuxAgentUserDestructiveCleanupLiveSmoke$' -count=1 -v
	) || true
}

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-linux-agent-user-lifecycle.XXXXXX")"
trap 'cleanup; rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

echo "Linux agent-user lifecycle live smoke"
echo "Date: $(date -u +%Y-%m-%d)"
echo "Commit: $(git -C "$REPO_ROOT" rev-parse HEAD)"
echo "Runner: ${HAZMAT_LINUX_VM_RUNNER:-manual-vm}"
echo "Root helper source: $TMP_ROOT/hazmat-launch"
echo
echo "Build:"
echo "1. go build -o $TMP_ROOT/hazmat-launch ./cmd/hazmat-launch"
(
	cd "$APP_DIR"
	go build -o "$TMP_ROOT/hazmat-launch" ./cmd/hazmat-launch
)

CLEANUP_NEEDED=1

echo
echo "Commands:"
echo "1. HAZMAT_LINUX_AGENT_USER_SETUP_VM_SMOKE=1 HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE=1 HAZMAT_LINUX_ROOT_HELPER_SOURCE=$TMP_ROOT/hazmat-launch go test . -run '^TestLinuxAgentUserSetupLiveSmoke$' -count=1 -v"
echo "2. scripts/check-linux-agent-user-live-smoke.sh --run --i-understand-this-runs-linux-agent-user-live-smoke"
echo "3. HAZMAT_LINUX_AGENT_USER_ROLLBACK_VM_SMOKE=1 HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE=1 HAZMAT_LINUX_ROOT_HELPER_SOURCE=$TMP_ROOT/hazmat-launch go test . -run '^TestLinuxAgentUserRollbackLiveSmoke$' -count=1 -v"
echo

(
	cd "$APP_DIR"
	HAZMAT_LINUX_AGENT_USER_SETUP_VM_SMOKE=1 \
	HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE=1 \
	HAZMAT_LINUX_ROOT_HELPER_SOURCE="$TMP_ROOT/hazmat-launch" \
		go test . -run '^TestLinuxAgentUserSetupLiveSmoke$' -count=1 -v
)

HAZMAT_LINUX_AGENT_USER_ROOT_HELPER=/usr/local/libexec/hazmat-launch \
	"$REPO_ROOT/scripts/check-linux-agent-user-live-smoke.sh" \
		--run \
		--i-understand-this-runs-linux-agent-user-live-smoke

(
	cd "$APP_DIR"
	HAZMAT_LINUX_AGENT_USER_ROLLBACK_VM_SMOKE=1 \
	HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE=1 \
	HAZMAT_LINUX_ROOT_HELPER_SOURCE="$TMP_ROOT/hazmat-launch" \
		go test . -run '^TestLinuxAgentUserRollbackLiveSmoke$' -count=1 -v
)

CLEANUP_NEEDED=0

echo
echo "Support claim: setup-required"
