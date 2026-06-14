#!/bin/sh

set -eu

AGENT_USER="${HAZMAT_PRIVILEGED_OWNERSHIP_AGENT_USER:-agent}"
AGENT_HOME="${HAZMAT_PRIVILEGED_OWNERSHIP_AGENT_HOME:-/Users/agent}"
AGENT_GROUP="${HAZMAT_PRIVILEGED_OWNERSHIP_AGENT_GROUP:-staff}"
MODE="disclose"
ACK=0
MISSING_PREREQS=""

usage() {
	cat <<'EOF'
Usage: scripts/check-privileged-install-ownership.sh [options]

Guarded live check for issue-17-class privileged install ownership outcomes.

By default, this script prints the exact live command and exits without reading
host setup state or running sudo-adjacent probes. Live mode requires:
  --run --i-understand-this-checks-privileged-install-ownership

Options:
  --check-prereqs           Check local prerequisites; exit 0 when ready,
                            exit 2 with reasons when the host is not ready.
  --skip-if-missing-prereqs Skip with exit 0 when prerequisites are missing.
  --run                     Verify post-init ownership and agent write probes.
  --after-rollback          Verify rollback left no root-owned setup residue.
  --i-understand-this-checks-privileged-install-ownership
                            Required acknowledgement for --run and
                            --after-rollback.
  -h, --help                Show this help.

This check is sudo-adjacent. Prereq and live modes inspect local agent setup,
use sudo -n, and run mkdir/rmdir probes as the agent user.
Agents must ask before running --check-prereqs, --skip-if-missing-prereqs,
--run, or --after-rollback.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--check-prereqs)
			MODE="check"
			;;
		--skip-if-missing-prereqs)
			MODE="skip"
			;;
		--run)
			MODE="run"
			;;
		--after-rollback)
			MODE="after-rollback"
			;;
		--i-understand-this-checks-privileged-install-ownership)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "privileged-install-ownership: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

add_missing_prereq() {
	if [ -z "$MISSING_PREREQS" ]; then
		MISSING_PREREQS="- $*"
	else
		MISSING_PREREQS="$MISSING_PREREQS
- $*"
	fi
}

ownership_paths() {
	cat <<EOF
$AGENT_HOME/.cache
$AGENT_HOME/.config
$AGENT_HOME/.config/hazmat
$AGENT_HOME/.local
$AGENT_HOME/.local/bin
$AGENT_HOME/.local/lib
$AGENT_HOME/.local/share
$AGENT_HOME/.local/state
$AGENT_HOME/.npm
EOF
}

stat_owner_group() {
	path="$1"
	if stat -f '%Su:%Sg' "$path" >/dev/null 2>&1; then
		stat -f '%Su:%Sg' "$path"
	else
		stat -c '%U:%G' "$path"
	fi
}

check_prereqs() {
	MISSING_PREREQS=""
	if ! id "$AGENT_USER" >/dev/null 2>&1; then
		add_missing_prereq "agent user $AGENT_USER does not exist; run hazmat init first"
	fi
	if ! sudo -n true >/dev/null 2>&1; then
		add_missing_prereq "sudo -n is not available; run from a prepared disposable host"
	fi
	for path in $(ownership_paths); do
		if [ ! -d "$path" ]; then
			add_missing_prereq "$path is missing; run hazmat init first"
		fi
	done
}

report_missing_prereqs() {
	if [ -z "$MISSING_PREREQS" ]; then
		return 1
	fi
	echo "privileged-install-ownership: missing prerequisites:" >&2
	echo "$MISSING_PREREQS" >&2
	return 0
}

run_check() {
	probe_name=".hazmat-ownership-probe-$$"
	for path in $(ownership_paths); do
		want="$AGENT_USER:$AGENT_GROUP"
		got="$(stat_owner_group "$path")"
		if [ "$got" != "$want" ]; then
			echo "privileged-install-ownership: $path owner/group = $got, want $want" >&2
			exit 1
		fi
		probe="$path/$probe_name"
		if ! sudo -n -u "$AGENT_USER" mkdir "$probe"; then
			echo "privileged-install-ownership: agent user cannot create $probe" >&2
			exit 1
		fi
		if ! sudo -n -u "$AGENT_USER" rmdir "$probe"; then
			echo "privileged-install-ownership: agent user cannot remove $probe" >&2
			exit 1
		fi
	done
	echo "privileged-install-ownership: ownership and agent write probes ok"
}

run_after_rollback_check() {
	failed=0
	for path in $(ownership_paths); do
		if [ ! -e "$path" ]; then
			continue
		fi
		got="$(stat_owner_group "$path")"
		case "$got" in
			root:*)
				echo "privileged-install-ownership: rollback left root-owned residue: $path ($got)" >&2
				failed=1
				;;
			*)
				echo "privileged-install-ownership: rollback residue remains but is not root-owned: $path ($got)" >&2
				;;
		esac
	done
	if [ "$failed" -ne 0 ]; then
		exit 1
	fi
	echo "privileged-install-ownership: rollback residue check ok"
}

case "$MODE" in
	disclose)
		usage
		echo
		echo "Exact live command after approval:"
		echo "  scripts/check-privileged-install-ownership.sh --run --i-understand-this-checks-privileged-install-ownership"
		;;
	check)
		check_prereqs
		if report_missing_prereqs; then
			exit 2
		fi
		echo "privileged-install-ownership: prerequisites ok"
		;;
	skip)
		check_prereqs
		if report_missing_prereqs; then
			echo "privileged-install-ownership: skipping because prerequisites are missing"
			exit 0
		fi
		echo "privileged-install-ownership: prerequisites ok"
		;;
	run)
		if [ "$ACK" -ne 1 ]; then
			echo "privileged-install-ownership: refusing live run without --i-understand-this-checks-privileged-install-ownership" >&2
			exit 2
		fi
		check_prereqs
		if report_missing_prereqs; then
			exit 2
		fi
		run_check
		;;
	after-rollback)
		if [ "$ACK" -ne 1 ]; then
			echo "privileged-install-ownership: refusing rollback residue check without --i-understand-this-checks-privileged-install-ownership" >&2
			exit 2
		fi
		run_after_rollback_check
		;;
esac
