#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
ACK=0
RUN=0

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-agent-user-live-smoke.sh
  scripts/check-linux-agent-user-live-smoke.sh --run --i-understand-this-runs-linux-agent-user-live-smoke

Default mode is disclosure-only. Live mode must run inside a disposable prepared
Linux VM after Linux agent-user setup resources already exist. It invokes the
modeled root helper with sudo -n to exercise agent-user/root-helper launch
behavior. It does not run hazmat init, hazmat doctor --fix, rollback,
destructive rollback, or current-user fallback.

Options:
  --run                                                 Run the prepared-host live smoke.
  --i-understand-this-runs-linux-agent-user-live-smoke  Required acknowledgement for --run.
  -h, --help                                           Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			RUN=1
			;;
		--i-understand-this-runs-linux-agent-user-live-smoke)
			ACK=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-agent-user-live-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$RUN" -ne 1 ]; then
	cat <<'EOF'
linux-agent-user-live-smoke: disclosure-only

This command can collect prepared-host A4-A8/A11 agent-user runtime rows for:
  docs/linux-agent-user-vm-lifecycle-matrix.md
  sandboxing-xuar.4.5

It is not setup, doctor, rollback, or destructive rollback evidence. Live mode
requires a disposable Linux VM where agent-user setup resources are already
present:
  scripts/check-linux-agent-user-live-smoke.sh --run --i-understand-this-runs-linux-agent-user-live-smoke
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	echo "linux-agent-user-live-smoke: refusing live run without --i-understand-this-runs-linux-agent-user-live-smoke" >&2
	exit 2
fi

if [ "$(uname -s)" != "Linux" ]; then
	echo "linux-agent-user-live-smoke: refusing live run outside Linux" >&2
	exit 2
fi

scratch_root="$REPO_ROOT/hazmat/.hazmat-linux-agent-user-smoke"
mkdir -p "$scratch_root"
trap 'rm -rf "$scratch_root"' EXIT HUP INT TERM

echo "Linux agent-user prepared-host live smoke"
echo "Date: $(date -u +%Y-%m-%d)"
echo "Commit: $(git -C "$REPO_ROOT" rev-parse HEAD)"
echo "Runner: ${HAZMAT_LINUX_VM_RUNNER:-manual-vm}"
echo "Root helper: ${HAZMAT_LINUX_AGENT_USER_ROOT_HELPER:-/usr/local/libexec/hazmat-launch}"
echo
echo "Command:"
echo "1. HAZMAT_LINUX_AGENT_USER_VM_SMOKE=1 HAZMAT_EXPERIMENTAL_LINUX_AGENT_USER=1 HAZMAT_LINUX_AGENT_USER_SMOKE_ROOT=$scratch_root go test ./internal/runtime/linux -run '^TestLinuxAgentUserPreparedHostLiveSmokeMatrix$' -count=1 -v"
echo

(
	cd "$REPO_ROOT/hazmat"
	HAZMAT_LINUX_AGENT_USER_VM_SMOKE=1 \
	HAZMAT_EXPERIMENTAL_LINUX_AGENT_USER=1 \
	HAZMAT_LINUX_AGENT_USER_SMOKE_ROOT="$scratch_root" \
	go test ./internal/runtime/linux -run '^TestLinuxAgentUserPreparedHostLiveSmokeMatrix$' -count=1 -v
)

echo
echo "Remaining gaps: A1-A3, A9, and A10 still require setup/doctor/rollback/destructive rollback transcripts."
echo "Support claim: setup-required"
