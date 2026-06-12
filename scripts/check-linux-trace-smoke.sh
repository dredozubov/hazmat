#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
IMAGE="${HAZMAT_LINUX_TRACE_SMOKE_IMAGE:-golang:1.25}"
GOARCH_VALUE="${HAZMAT_LINUX_TRACE_GOARCH:-$(go env GOHOSTARCH)}"
MODE="disclosure"
ACK_RUN=0
SKIP_IF_MISSING=0

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-trace-smoke.sh
  scripts/check-linux-trace-smoke.sh --run --i-understand-this-runs-privileged-docker [--skip-if-missing-prereqs]

Default mode is disclosure-only. Live mode runs a privileged Docker container
for the Linux trace collector smoke. Agents must ask for explicit approval
before running --run.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			MODE="run"
			;;
		--i-understand-this-runs-privileged-docker)
			ACK_RUN=1
			;;
		--skip-if-missing-prereqs)
			SKIP_IF_MISSING=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-trace-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$MODE" != "run" ]; then
	cat <<EOF
linux-trace-smoke: disclosure-only

This live smoke uses Docker with --privileged, bind-mounts the repository
read-only, installs trace tooling inside the container when needed, and runs the
Linux hazmat_debug trace collector.

To run it, ask for explicit approval for this exact command:

  scripts/check-linux-trace-smoke.sh --run --i-understand-this-runs-privileged-docker
EOF
	exit 0
fi

if [ "$ACK_RUN" -ne 1 ]; then
	echo "linux-trace-smoke: refusing live run without --i-understand-this-runs-privileged-docker" >&2
	exit 2
fi

skip_or_fail() {
	msg="$1"
	if [ "$SKIP_IF_MISSING" -eq 1 ]; then
		echo "linux-trace-smoke: skipped: $msg"
		exit 0
	fi
	echo "linux-trace-smoke: $msg" >&2
	exit 1
}

if ! command -v docker >/dev/null 2>&1; then
	skip_or_fail "docker command not found"
fi
if ! docker info >/dev/null 2>&1; then
	skip_or_fail "docker daemon is not reachable"
fi
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
	skip_or_fail "docker image $IMAGE is not present locally"
fi

TMPDIR_LINUX_TRACE="$(mktemp -d)"
cleanup() {
	rm -rf "$TMPDIR_LINUX_TRACE"
}
trap cleanup EXIT INT TERM HUP

mkdir -p "$TMPDIR_LINUX_TRACE/out"

echo "linux-trace-smoke: configuring and running strict debug trace in $IMAGE..."
docker run --rm --privileged \
	-v "$REPO_ROOT:/src:ro" \
	-v "$TMPDIR_LINUX_TRACE:/work" \
	"$IMAGE" \
		sh -eu -c '
			if command -v apk >/dev/null 2>&1; then
				apk add --no-cache strace procps coreutils util-linux >/dev/null
			fi
			if command -v apt-get >/dev/null 2>&1; then
				apt-get update >/dev/null
				DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends strace procps util-linux systemd >/dev/null
			fi
			if [ ! -x /usr/bin/uname ] && command -v uname >/dev/null 2>&1; then
				ln -sf "$(command -v uname)" /usr/bin/uname
			fi
			mkdir -p /work/src
		cp -R /src/. /work/src/
		cd /work/src
		GOOS=linux GOARCH='"$GOARCH_VALUE"' CGO_ENABLED=0 scripts/configure-debug-trace.sh
		cd /work/src/hazmat
		GOOS=linux GOARCH='"$GOARCH_VALUE"' CGO_ENABLED=0 go build -tags hazmat_debug -o /work/hazmat ./cmd/hazmat
		rm -rf /work/out/*
		script -q -c "/work/hazmat trace codex --out /work/out --name docker-smoke -- --help" /work/wrapper.typescript >/work/trace.out
		bundle="$(find /work/out -mindepth 1 -maxdepth 1 -type d | head -n 1)"
		test -n "$bundle"
		grep -q "\"backend\": \"linux\"" "$bundle/manifest.json"
		grep -q "\"harness\": \"codex\"" "$bundle/manifest.json"
		grep -q "\"exit_code\": 0" "$bundle/manifest.json"
		grep -q "\"syscalls\": true" "$bundle/manifest.json"
		grep -q "\"transcript\": true" "$bundle/manifest.json"
		test -f "$bundle/harness.json"
		test -f "$bundle/command.txt"
		test -f "$bundle/before-ps.txt"
			test -f "$bundle/after-ps.txt"
			test -f "$bundle/indicators.md"
			ls "$bundle"/strace.log* >/dev/null 2>&1
			test -f "$bundle/journal.log"
			test -f "$bundle/dmesg.log"
			test ! -f "$bundle/trace-errors.log"
			echo "$bundle" >/work/bundle-path.txt
		'

echo "linux-trace-smoke: bundle shape ok: $(cat "$TMPDIR_LINUX_TRACE/bundle-path.txt")"
