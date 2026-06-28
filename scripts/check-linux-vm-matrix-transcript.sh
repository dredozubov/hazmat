#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
APP_DIR="$REPO_ROOT/hazmat"
SCRIPT_REL="scripts/check-linux-vm-matrix-transcript.sh"
MODE="current-user"
RUN=0
SKIP_PREFLIGHT=0
OUTPUT=""
TMP_OUT="${TMPDIR:-/tmp}/hazmat-linux-vm-matrix-cmd.out.$$"
TMP_ERR="${TMPDIR:-/tmp}/hazmat-linux-vm-matrix-cmd.err.$$"

trap 'rm -f "$TMP_OUT" "$TMP_ERR"' EXIT HUP INT TERM

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-vm-matrix-transcript.sh
  scripts/check-linux-vm-matrix-transcript.sh --mode current-user --run [--skip-preflight] [--output FILE]
  scripts/check-linux-vm-matrix-transcript.sh --mode agent-user --run [--skip-preflight] [--output FILE]

Default mode is disclosure-only. --run emits a non-mutating transcript scaffold
for the Linux VM matrices. It captures commit, distro/kernel facts, capability
facts, passive setup-resource facts, and non-live preflight results. It does not
run hazmat init, hazmat doctor --fix, rollback, sudo, or helper-backed launch.

Options:
  --mode current-user|agent-user  Select transcript matrix. Default: current-user.
  --run                           Emit the transcript.
  --skip-preflight                Do not run Go/build preflight commands.
  --output FILE                   Write transcript to FILE instead of stdout.
  -h, --help                      Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--mode)
			shift
			if [ "$#" -eq 0 ]; then
				echo "linux-vm-matrix-transcript: --mode requires a value" >&2
				exit 2
			fi
			MODE="$1"
			;;
		--run)
			RUN=1
			;;
		--skip-preflight)
			SKIP_PREFLIGHT=1
			;;
		--output)
			shift
			if [ "$#" -eq 0 ]; then
				echo "linux-vm-matrix-transcript: --output requires a value" >&2
				exit 2
			fi
			OUTPUT="$1"
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-vm-matrix-transcript: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

case "$MODE" in
	current-user|agent-user)
		;;
	*)
		echo "linux-vm-matrix-transcript: --mode must be current-user or agent-user" >&2
		exit 2
		;;
esac

if [ "$RUN" -ne 1 ]; then
	cat <<EOF
linux-vm-matrix-transcript: disclosure-only

This command emits non-mutating transcript scaffolding for:
  docs/linux-current-user-vm-smoke-matrix.md
  docs/linux-agent-user-vm-lifecycle-matrix.md

It is useful inside disposable Linux VMs before an approval-gated live matrix
run, but it is not itself support evidence for S1-S7 or A1-A11. Ask for exact
approval before any follow-up command that runs hazmat init, hazmat doctor --fix,
rollback, sudo, or helper-backed launch.

Examples:
  scripts/check-linux-vm-matrix-transcript.sh --mode current-user --run
  scripts/check-linux-vm-matrix-transcript.sh --mode agent-user --run --output /tmp/hazmat-linux-agent-user-transcript.txt
EOF
	exit 0
fi

if [ -n "$OUTPUT" ]; then
	case "$OUTPUT" in
		/*)
			output_parent="$(dirname "$OUTPUT")"
			if [ ! -d "$output_parent" ]; then
				echo "linux-vm-matrix-transcript: output parent does not exist: $output_parent" >&2
				exit 2
			fi
			exec >"$OUTPUT"
			;;
		*)
			echo "linux-vm-matrix-transcript: --output must be absolute" >&2
			exit 2
			;;
	esac
fi

command_status() {
	label="$1"
	shift
	printf '%s: ' "$label"
	if "$@" >"$TMP_OUT" 2>"$TMP_ERR"; then
		echo "pass"
	else
		status=$?
		echo "fail exit=$status"
		if [ -s "$TMP_ERR" ]; then
			sed 's/^/  stderr: /' "$TMP_ERR"
		fi
	fi
	rm -f "$TMP_OUT" "$TMP_ERR"
}

file_state() {
	path="$1"
	if [ -e "$path" ]; then
		if [ -r "$path" ]; then
			printf 'present readable'
		else
			printf 'present unreadable'
		fi
	else
		printf 'absent'
	fi
}

read_first_existing() {
	for path in "$@"; do
		if [ -r "$path" ]; then
			sed -n '1,12p' "$path"
			return
		fi
	done
	echo "unavailable"
}

feature_file_state() {
	path="$1"
	if [ -r "$path" ]; then
		value="$(tr '\n' ' ' <"$path" | sed 's/[[:space:]][[:space:]]*/ /g; s/^ //; s/ $//')"
		printf 'available source=%s value=%s' "$path" "${value:-empty}"
	elif [ -e "$path" ]; then
		printf 'present-unreadable source=%s' "$path"
	else
		printf 'unavailable source=%s' "$path"
	fi
}

namespace_state() {
	path="$1"
	if [ -e "$path" ]; then
		printf 'available source=%s' "$path"
	else
		printf 'unavailable source=%s' "$path"
	fi
}

run_preflight() {
	if [ "$SKIP_PREFLIGHT" -eq 1 ]; then
		echo "Preflight:"
		echo "- skipped by --skip-preflight"
		return
	fi
	echo "Preflight:"
	command_status "- go test platform/containment/runtime linux packages" env APP_DIR="$APP_DIR" sh -c 'cd "$APP_DIR" && go test ./platform/linux ./containment/linux ./internal/runtime/linux'
	command_status "- scripts/check-linux-compile.sh" env REPO_ROOT="$REPO_ROOT" sh -c 'cd "$REPO_ROOT" && scripts/check-linux-compile.sh'
}

print_runner_command() {
	printf '%s --mode %s --run' "$SCRIPT_REL" "$MODE"
	if [ "$SKIP_PREFLIGHT" -eq 1 ]; then
		printf ' --skip-preflight'
	fi
	if [ -n "$OUTPUT" ]; then
		printf ' --output %s' "$OUTPUT"
	fi
	printf '\n'
}

print_common_header() {
	echo "Date: $(date -u +%Y-%m-%d)"
	echo "Commit: $(git -C "$REPO_ROOT" rev-parse HEAD)"
	echo "Runner: ${HAZMAT_LINUX_VM_RUNNER:-manual-vm}"
	echo "Distro:"
	read_first_existing /etc/os-release /usr/lib/os-release | sed 's/^/  /'
	echo "Kernel: $(uname -srvmo 2>/dev/null || uname -a)"
	echo "Arch: $(uname -m)"
}

print_capabilities() {
	echo "Capability report:"
	echo "- user namespace: $(feature_file_state /proc/sys/kernel/unprivileged_userns_clone)"
	echo "- mount namespace: $(namespace_state /proc/self/ns/mnt)"
	echo "- network namespace: $(namespace_state /proc/self/ns/net)"
	echo "- Landlock: $(feature_file_state /sys/kernel/security/landlock/abi)"
	echo "- seccomp: $(feature_file_state /proc/sys/kernel/seccomp/actions_avail)"
	echo "- cgroup v2: $(feature_file_state /sys/fs/cgroup/cgroup.controllers)"
}

print_current_user_transcript() {
	echo "Linux current-user VM smoke transcript"
	print_common_header
	echo "UID mode: invoking uid $(id -u), no dedicated agent user"
	echo "linux.identity: current-user"
	echo "linux.helper_strategy: rootless-userns"
	echo
	print_capabilities
	echo
	run_preflight
	echo
	echo "Commands:"
	printf '1. '
	print_runner_command
	echo
	echo "Scenario results:"
	echo "S1 project write: pending live VM execution; not run by scaffold"
	echo "S2 read-only denial: pending live VM execution; not run by scaffold"
	echo "S3 credential denial: pending live VM execution; not run by scaffold"
	echo "S4 network none: pending live VM execution; not run by scaffold"
	echo "S5 cancellation cleanup: pending live VM execution; not run by scaffold"
	echo "S6 missing primitive: pending live VM execution or typed fixture evidence"
	echo "S7 raw streams: pending live VM execution; not run by scaffold"
	echo
	echo "Remaining gaps: S1-S7 require real Linux current-user runner execution across Ubuntu/Debian/Fedora/Arch."
	echo "Support claim: experimental"
}

print_agent_setup_resources() {
	echo "Setup resources:"
	echo "- linuxAgentUser: $(id agent 2>/dev/null || echo absent)"
	echo "- linuxSharedGroup: $(getent group dev 2>/dev/null || echo absent)"
	echo "- linuxAgentHome: $(file_state /home/agent)"
	echo "- linuxWorkspaceAccess: passive scaffold; verify with live agent-user scenario"
	echo "- linuxLaunchHelper: $(file_state /usr/local/libexec/hazmat-launch)"
	echo "- linuxSudoers: $(file_state /etc/sudoers.d/agent)"
	echo "- linuxCgroupRoot: $(file_state /sys/fs/cgroup/hazmat-agent)"
	echo "- linuxDistroProfile: $(file_state /var/lib/hazmat/linux/distro-profile.json)"
	echo "- linuxToolHome: /home/agent/.cache=$(file_state /home/agent/.cache), /home/agent/.config=$(file_state /home/agent/.config)"
}

print_agent_user_transcript() {
	echo "Linux agent-user VM lifecycle transcript"
	print_common_header
	echo "Invoking UID: $(id -u)"
	echo "Agent UID/GID: $(id -u agent 2>/dev/null || echo absent)/$(id -g agent 2>/dev/null || echo absent)"
	echo "linux.identity: agent-user"
	echo "linux.helper_strategy: root-helper"
	echo
	print_capabilities
	echo "- service manager: $(if command -v systemctl >/dev/null 2>&1; then echo systemctl-present; else echo systemctl-absent; fi)"
	echo
	print_agent_setup_resources
	echo
	run_preflight
	echo
	echo "Commands:"
	printf '1. '
	print_runner_command
	echo
	echo "Scenario results:"
	echo "A1 fresh setup: pending approval-gated live setup; not run by scaffold"
	echo "A2 idempotent setup: pending approval-gated live setup; not run by scaffold"
	echo "A3 drift diagnostics: pending live check/doctor transcript; not run by scaffold"
	echo "A4 helper admission: pending helper-backed launch transcript; not run by scaffold"
	echo "A5 run metadata: pending helper-backed launch transcript; not run by scaffold"
	echo "A6 filesystem policy: pending helper-backed launch transcript; not run by scaffold"
	echo "A7 network policy: pending helper-backed launch transcript; not run by scaffold"
	echo "A8 cancellation cleanup: pending helper-backed launch transcript; not run by scaffold"
	echo "A9 default rollback: pending approval-gated rollback; not run by scaffold"
	echo "A10 destructive rollback: pending explicit destructive rollback approval; not run by scaffold"
	echo "A11 unsupported host: pending typed gap transcript or fixture evidence"
	echo
	echo "Remaining gaps: A1-A11 require disposable Linux host setup/doctor/run-agent/rollback/destructive rollback transcripts."
	echo "Support claim: setup-required"
}

case "$MODE" in
	current-user)
		print_current_user_transcript
		;;
	agent-user)
		print_agent_user_transcript
		;;
esac
