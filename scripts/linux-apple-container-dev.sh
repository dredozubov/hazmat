#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
CONTAINER_BIN="${HAZMAT_APPLE_CONTAINER_BIN:-container}"
IMAGE="${HAZMAT_LINUX_APPLE_CONTAINER_IMAGE:-golang:1.25}"
NETWORK="${HAZMAT_LINUX_APPLE_CONTAINER_NETWORK:-none}"
MODE="disclosure"
ACK_RUN=0
SKIP_IF_MISSING=0
MISSING_PREREQS=""
TMPDIR_LINUX_APPLE_CONTAINER_DEV=""

usage() {
	cat <<'EOF'
Usage:
  bash scripts/linux-apple-container-dev.sh
  bash scripts/linux-apple-container-dev.sh --check-prereqs
  bash scripts/linux-apple-container-dev.sh --shell --i-understand-this-runs-apple-container-linux-dev [--skip-if-missing-prereqs]
  bash scripts/linux-apple-container-dev.sh --run --i-understand-this-runs-apple-container-linux-dev -- <command> [args...]

Default mode is disclosure-only. Live modes create a short-lived Apple Container
Linux session with a writable copy of the repository at /work/src/hazmat. The
host checkout is mounted read-only and is not modified by commands run inside
the container. Edit on the host, run Linux checks in the container, and commit
from the host.

Options:
  --check-prereqs                Check host-side Apple Container prerequisites.
  --skip-if-missing-prereqs      Exit 0 when prerequisites are absent.
  --shell                        Open a shell in the writable Linux copy.
  --run                          Run the command after -- in the writable Linux copy.
  --i-understand-this-runs-apple-container-linux-dev
                                  Required acknowledgement for --shell and --run.
  -h, --help                     Show this help.

Environment:
  HAZMAT_APPLE_CONTAINER_BIN            Apple Container executable name or absolute path.
  HAZMAT_LINUX_APPLE_CONTAINER_IMAGE    Guest image. Default: golang:1.25
  HAZMAT_LINUX_APPLE_CONTAINER_NETWORK  container run network. Default: none

Prereq checks inspect local Apple Container setup with `container system status`.
Live modes may create containers and may pull the configured image. Agents must
ask for explicit approval before running --check-prereqs, --skip-if-missing-prereqs,
--shell, or --run.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--check-prereqs)
			MODE="check"
			shift
			;;
		--skip-if-missing-prereqs)
			SKIP_IF_MISSING=1
			if [ "$MODE" = "disclosure" ]; then
				MODE="skip"
			fi
			shift
			;;
		--shell)
			MODE="shell"
			shift
			;;
		--run)
			MODE="run"
			shift
			;;
		--i-understand-this-runs-apple-container-linux-dev)
			ACK_RUN=1
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		--)
			shift
			break
			;;
		*)
			echo "linux-apple-container-dev: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

add_missing_prereq() {
	if [ -z "$MISSING_PREREQS" ]; then
		MISSING_PREREQS="- $*"
	else
		MISSING_PREREQS="$MISSING_PREREQS
- $*"
	fi
}

require_command() {
	case "$1" in
		*/*)
			case "$1" in
				/*)
					;;
				*)
					add_missing_prereq "$1 must be an absolute path or command name"
					return
					;;
			esac
			if [ ! -x "$1" ]; then
				add_missing_prereq "$1 is missing or not executable"
			fi
			;;
		*)
			if ! command -v "$1" >/dev/null 2>&1; then
				add_missing_prereq "$1 is not on PATH"
			fi
			;;
	esac
}

container_cmd() {
	case "$CONTAINER_BIN" in
		/*)
			printf '%s\n' "$CONTAINER_BIN"
			;;
		*/*)
			printf '%s\n' "$CONTAINER_BIN"
			;;
		*)
			command -v "$CONTAINER_BIN" 2>/dev/null || printf '%s\n' "$CONTAINER_BIN"
			;;
	esac
}

check_prereqs() {
	MISSING_PREREQS=""

	require_command go
	require_command mktemp
	require_command "$CONTAINER_BIN"

	if [ "$(uname -s)" != "Darwin" ]; then
		add_missing_prereq "Apple Container requires macOS; current host is $(uname -s)"
	fi
	if [ "$(uname -m)" != "arm64" ]; then
		add_missing_prereq "Apple Container requires Apple silicon; current host arch is $(uname -m)"
	fi

	if command -v "$CONTAINER_BIN" >/dev/null 2>&1 || { [ -x "$CONTAINER_BIN" ] 2>/dev/null; }; then
		if ! "$CONTAINER_BIN" system status --format json >/tmp/hazmat-apple-container-dev-status.$$ 2>/tmp/hazmat-apple-container-dev-status.err.$$; then
			add_missing_prereq "container system status failed; start Apple Container for the invoking user"
		elif ! grep -q '"status"[[:space:]]*:[[:space:]]*"running"' /tmp/hazmat-apple-container-dev-status.$$; then
			add_missing_prereq "container system status did not report running"
		fi
		rm -f /tmp/hazmat-apple-container-dev-status.$$ /tmp/hazmat-apple-container-dev-status.err.$$
	fi

	if [ -n "$MISSING_PREREQS" ]; then
		return 1
	fi
	return 0
}

print_missing_prereqs() {
	echo "linux-apple-container-dev: missing prerequisites:" >&2
	printf '%s\n' "$MISSING_PREREQS" >&2
}

print_disclosure() {
	cat <<EOF
linux-apple-container-dev: disclosure-only

This starts a short-lived Apple Container Linux development session with a
writable copy of the repository at /work/src/hazmat. The host checkout is
mounted read-only. Changes made inside the container are disposable; edit and
commit from the host.

Open an interactive shell after exact-command approval:
  bash scripts/linux-apple-container-dev.sh --shell --i-understand-this-runs-apple-container-linux-dev

Run a Linux command after exact-command approval:
  bash scripts/linux-apple-container-dev.sh --run --i-understand-this-runs-apple-container-linux-dev -- go test ./platform/linux ./containment/linux

Prereq check:
  bash scripts/linux-apple-container-dev.sh --check-prereqs

Image:
  $IMAGE

Network:
  $NETWORK
EOF
}

cleanup() {
	if [ -n "$TMPDIR_LINUX_APPLE_CONTAINER_DEV" ]; then
		rm -rf "$TMPDIR_LINUX_APPLE_CONTAINER_DEV"
	fi
}
trap cleanup EXIT INT TERM HUP

run_dev() {
	if [ "$ACK_RUN" -ne 1 ]; then
		echo "linux-apple-container-dev: refusing live run without --i-understand-this-runs-apple-container-linux-dev" >&2
		exit 2
	fi
	if ! check_prereqs; then
		if [ "$SKIP_IF_MISSING" -eq 1 ]; then
			echo "linux-apple-container-dev: skipped because prerequisites are missing" >&2
			print_missing_prereqs
			exit 0
		fi
		print_missing_prereqs
		exit 2
	fi

	TMPDIR_LINUX_APPLE_CONTAINER_DEV="$(mktemp -d)"
	mkdir -p "$TMPDIR_LINUX_APPLE_CONTAINER_DEV/work" "$TMPDIR_LINUX_APPLE_CONTAINER_DEV/private-tmp"

	gomodcache="$(cd "$REPO_ROOT/hazmat" && go env GOMODCACHE)"
	container_gomodcache="/work/gomodcache"
	mount_module_cache=0
	if [ -n "$gomodcache" ] && [ -d "$gomodcache" ]; then
		mount_module_cache=1
		container_gomodcache="/go/pkg/mod"
	fi

	container_name="hazmat-linux-dev-$$"
	guest_user="$(id -u):$(id -g)"
	if [ "$MODE" = "shell" ]; then
		set -- sh
	elif [ "$#" -eq 0 ]; then
		echo "linux-apple-container-dev: --run requires a command after --" >&2
		exit 2
	fi

	echo "linux-apple-container-dev: start $container_name in $IMAGE"
	if [ "$mount_module_cache" -eq 1 ]; then
		"$(container_cmd)" run --rm \
			--name "$container_name" \
			--user "$guest_user" \
			--network "$NETWORK" \
			--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER_DEV/work,target=/work" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER_DEV/private-tmp,target=/private/tmp" \
			--mount "type=bind,source=$gomodcache,target=/go/pkg/mod,readonly" \
			--workdir /work \
			"$IMAGE" \
			sh -eu -c "$container_dev_script" sh "$container_gomodcache" "$@"
	else
		"$(container_cmd)" run --rm \
			--name "$container_name" \
			--user "$guest_user" \
			--network "$NETWORK" \
			--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER_DEV/work,target=/work" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER_DEV/private-tmp,target=/private/tmp" \
			--workdir /work \
			"$IMAGE" \
			sh -eu -c "$container_dev_script" sh "$container_gomodcache" "$@"
	fi
}

container_dev_script='
gomodcache="$1"
shift
mkdir -p /work/src /work/gocache /work/tmp /work/home "$gomodcache"
tar -C /hazmat-src \
	--warning=no-file-changed \
	--exclude ./.git \
	--exclude ./.beads \
	--exclude ./spike-apple-container-results \
	--exclude ./tla/states \
	-cf - . | tar -C /work/src -xf -
cd /work/src/hazmat
export CGO_ENABLED=0
export GOWORK=off
export HOME=/work/home
export GOCACHE=/work/gocache
export GOMODCACHE="$gomodcache"
export GOTMPDIR=/work/tmp
export GOFLAGS="-mod=readonly ${GOFLAGS:-}"
exec "$@"
'

case "$MODE" in
	disclosure)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_prereqs; then
			echo "linux-apple-container-dev: prerequisites ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ] || [ "$SKIP_IF_MISSING" -eq 1 ]; then
			echo "linux-apple-container-dev: skipped because prerequisites are missing" >&2
			print_missing_prereqs
			exit 0
		fi
		print_missing_prereqs
		exit 2
		;;
	shell|run)
		run_dev "$@"
		;;
esac
