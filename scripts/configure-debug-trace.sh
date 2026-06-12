#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
TARGET_GOOS="${HAZMAT_TRACE_TARGET_GOOS:-}"
ACK_DARWIN_DTRACE=0
if [ -z "$TARGET_GOOS" ]; then
	if command -v go >/dev/null 2>&1; then
		TARGET_GOOS="$(go env GOOS)"
	else
		case "$(uname -s)" in
			Darwin) TARGET_GOOS=darwin ;;
			Linux) TARGET_GOOS=linux ;;
			*) TARGET_GOOS=unsupported ;;
		esac
	fi
fi

while [ "$#" -gt 0 ]; do
	case "$1" in
		--i-understand-this-runs-sudo-dtrace-probes)
			ACK_DARWIN_DTRACE=1
			;;
		--target=*)
			TARGET_GOOS="${1#--target=}"
			;;
		--target)
			shift
			if [ "$#" -eq 0 ]; then
				echo "configure-debug-trace: --target requires an argument" >&2
				exit 2
			fi
			TARGET_GOOS="$1"
			;;
		--help|-h)
			cat <<'EOF'
Usage: scripts/configure-debug-trace.sh [--target darwin|linux] [--i-understand-this-runs-sudo-dtrace-probes]

Checks the host prerequisites required before compiling or running the
hazmat_debug trace command. A successful check means developers can build with:

  go build -tags hazmat_debug ./hazmat/cmd/hazmat

On Darwin this runs sudo-adjacent DTrace/dtruss prerequisite probes. Agents must
ask for explicit approval before using --i-understand-this-runs-sudo-dtrace-probes.
EOF
			exit 0
			;;
		*)
			echo "configure-debug-trace: unknown argument: $1" >&2
			exit 2
			;;
	esac
	shift
done

require_executable() {
	label="$1"
	path="$2"
	if [ ! -x "$path" ] || [ -d "$path" ]; then
		echo "configure-debug-trace: missing executable $label at $path" >&2
		exit 1
	fi
}

require_any_executable() {
	label="$1"
	shift
	for path in "$@"; do
		if [ -x "$path" ] && [ ! -d "$path" ]; then
			printf '%s\n' "$path"
			return 0
		fi
	done
	echo "configure-debug-trace: missing executable $label in candidates: $*" >&2
	exit 1
}

require_readable_path() {
	label="$1"
	path="$2"
	if [ ! -r "$path" ]; then
		echo "configure-debug-trace: unreadable $label at $path" >&2
		exit 1
	fi
}

run_required() {
	label="$1"
	shift
	if ! "$@" >/dev/null 2>&1; then
		echo "configure-debug-trace: required check failed: $label" >&2
		echo "configure-debug-trace: command: $*" >&2
		exit 1
	fi
}

run_darwin_dtruss_probe() {
	tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-dtruss-probe.XXXXXX")"
	helper="$tmpdir/hazmat-dtruss-probe"
	src="$tmpdir/main.go"
	cat >"$src" <<'EOF'
package main

func main() {}
EOF
	if ! go build -o "$helper" "$src" >/dev/null 2>&1; then
		rm -rf "$tmpdir"
		echo "configure-debug-trace: failed to build temporary DTrace probe helper" >&2
		exit 1
	fi
	if ! /usr/bin/sudo -n /usr/bin/dtruss "$helper" >"$tmpdir/dtruss.out" 2>&1; then
		sed 's/^/configure-debug-trace: dtruss: /' "$tmpdir/dtruss.out" >&2
		rm -rf "$tmpdir"
		echo "configure-debug-trace: required check failed: DTrace via dtruss" >&2
		echo "configure-debug-trace: command: /usr/bin/sudo -n /usr/bin/dtruss <temporary helper>" >&2
		exit 1
	fi
	rm -rf "$tmpdir"
}

case "$TARGET_GOOS" in
	darwin)
		if [ "$ACK_DARWIN_DTRACE" -ne 1 ]; then
			cat >&2 <<'EOF'
configure-debug-trace: refusing Darwin DTrace prerequisite probes without --i-understand-this-runs-sudo-dtrace-probes

This check runs sudo-adjacent commands including:
  /usr/bin/sudo -n -v
  /usr/bin/sudo -n /usr/bin/dtruss <temporary helper>

Ask for explicit approval before running this exact command.
EOF
			exit 2
		fi
		require_executable sudo /usr/bin/sudo
		require_executable uname /usr/bin/uname
		require_executable sw_vers /usr/bin/sw_vers
		require_executable csrutil /usr/bin/csrutil
		require_executable which /usr/bin/which
		require_executable ps /bin/ps
		require_executable ls /bin/ls
		require_executable script /usr/bin/script
		require_executable log /usr/bin/log
		require_executable dtruss /usr/bin/dtruss
		require_executable fs_usage /usr/bin/fs_usage
		require_executable opensnoop /usr/bin/opensnoop
		run_required "non-interactive sudo" /usr/bin/sudo -n -v
		run_darwin_dtruss_probe
		;;
	linux)
		strace_path="$(require_any_executable strace /usr/bin/strace /bin/strace /usr/local/bin/strace)"
		ps_path="$(require_any_executable ps /usr/bin/ps /bin/ps)"
		journalctl_path="$(require_any_executable journalctl /usr/bin/journalctl /bin/journalctl)"
		dmesg_path="$(require_any_executable dmesg /usr/bin/dmesg /bin/dmesg)"
		ls_path="$(require_any_executable ls /bin/ls /usr/bin/ls)"
		stat_path="$(require_any_executable stat /usr/bin/stat /bin/stat)"
		require_executable uname /usr/bin/uname
		require_executable script /usr/bin/script
		require_readable_path "/proc" /proc
		require_readable_path "/proc/self/status" /proc/self/status
		run_required "journalctl readable output" "$journalctl_path" --no-pager -n 1
		run_required "dmesg readable output" "$dmesg_path" --ctime --color=never
		: "$strace_path" "$ps_path" "$ls_path" "$stat_path"
		;;
	*)
		echo "configure-debug-trace: hazmat trace is not implemented for target $TARGET_GOOS" >&2
		exit 1
		;;
esac

echo "configure-debug-trace: prerequisites ok for $TARGET_GOOS"
cat >"$REPO_ROOT/hazmat/trace_debug_configured.go" <<'EOF'
// Code generated by scripts/configure-debug-trace.sh; DO NOT EDIT.
//go:build hazmat_debug

package main

const traceDebugConfigured = true
EOF
echo "configure-debug-trace: build with: go build -tags hazmat_debug ./hazmat/cmd/hazmat"
