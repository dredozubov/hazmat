#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
ACK=0
RUN=0

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-current-user-live-smoke.sh
  scripts/check-linux-current-user-live-smoke.sh --run --i-understand-this-runs-linux-current-user-live-smoke

Default mode is disclosure-only. Live mode must run inside a disposable Linux VM.
It invokes the Linux current-user runtime in child test processes, mutating
namespaces, mounts, Landlock, seccomp, and process state inside those children.
It does not run sudo, hazmat init, hazmat doctor --fix, rollback, agent-user
setup, or helper-backed launch.

Options:
  --run                                                   Run the live smoke.
  --i-understand-this-runs-linux-current-user-live-smoke  Required acknowledgement for --run.
  -h, --help                                             Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			RUN=1
			;;
		--i-understand-this-runs-linux-current-user-live-smoke)
			ACK=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-current-user-live-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$RUN" -ne 1 ]; then
	cat <<'EOF'
linux-current-user-live-smoke: disclosure-only

This command can collect S1-S7 current-user live smoke rows for:
  docs/linux-current-user-vm-smoke-matrix.md
  sandboxing-xuar.3.5

Live mode is approval-gated and must run in a disposable Linux VM:
  scripts/check-linux-current-user-live-smoke.sh --run --i-understand-this-runs-linux-current-user-live-smoke
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	echo "linux-current-user-live-smoke: refusing live run without --i-understand-this-runs-linux-current-user-live-smoke" >&2
	exit 2
fi

if [ "$(uname -s)" != "Linux" ]; then
	echo "linux-current-user-live-smoke: refusing live run outside Linux" >&2
	exit 2
fi

echo "Linux current-user live smoke"
echo "Date: $(date -u +%Y-%m-%d)"
echo "Commit: $(git -C "$REPO_ROOT" rev-parse HEAD)"
echo "Runner: ${HAZMAT_LINUX_VM_RUNNER:-manual-vm}"
echo
echo "Command:"
echo "1. HAZMAT_LINUX_CURRENT_USER_VM_SMOKE=1 HAZMAT_EXPERIMENTAL_LINUX_CURRENT_USER=1 go test ./internal/runtime/linux -run '^TestLinuxCurrentUserLiveSmokeMatrix$' -count=1 -v"
echo

(
	cd "$REPO_ROOT/hazmat"
	HAZMAT_LINUX_CURRENT_USER_VM_SMOKE=1 \
	HAZMAT_EXPERIMENTAL_LINUX_CURRENT_USER=1 \
	go test ./internal/runtime/linux -run '^TestLinuxCurrentUserLiveSmokeMatrix$' -count=1 -v
)

echo
echo "Support claim: experimental"
