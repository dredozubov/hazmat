#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
IMAGE="${HAZMAT_LINUX_TRACE_SMOKE_IMAGE:-alpine:3.22}"
GOARCH_VALUE="${HAZMAT_LINUX_TRACE_GOARCH:-$(go env GOHOSTARCH)}"
SKIP_IF_MISSING=0

if [ "${1:-}" = "--skip-if-missing-prereqs" ]; then
	SKIP_IF_MISSING=1
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

echo "linux-trace-smoke: building linux/$GOARCH_VALUE hazmat binary..."
(
	cd "$REPO_ROOT/hazmat"
	GOOS=linux GOARCH="$GOARCH_VALUE" CGO_ENABLED=0 go build -o "$TMPDIR_LINUX_TRACE/hazmat" .
)

echo "linux-trace-smoke: running trace bundle smoke in $IMAGE..."
docker run --rm \
	-v "$TMPDIR_LINUX_TRACE:/work" \
	"$IMAGE" \
	sh -eu -c '
		if command -v apk >/dev/null 2>&1; then
			apk add --no-cache strace procps coreutils >/dev/null 2>&1 || true
		fi
		rm -rf /work/out/*
		/work/hazmat trace codex --out /work/out --name docker-smoke --no-transcript -- --help >/work/trace.out
		bundle="$(find /work/out -mindepth 1 -maxdepth 1 -type d | head -n 1)"
		test -n "$bundle"
		grep -q "\"backend\": \"linux\"" "$bundle/manifest.json"
		grep -q "\"harness\": \"codex\"" "$bundle/manifest.json"
		grep -q "\"exit_code\": 0" "$bundle/manifest.json"
		test -f "$bundle/harness.json"
		test -f "$bundle/command.txt"
		test -f "$bundle/before-ps.txt"
		test -f "$bundle/after-ps.txt"
		test -f "$bundle/indicators.md"
		if ls "$bundle"/strace.log* >/dev/null 2>&1; then
			:
		elif grep -q strace "$bundle/trace-errors.log" >/dev/null 2>&1; then
			:
		else
			echo "missing strace output or degraded strace evidence" >&2
			exit 1
		fi
		echo "$bundle" >/work/bundle-path.txt
	'

echo "linux-trace-smoke: bundle shape ok: $(cat "$TMPDIR_LINUX_TRACE/bundle-path.txt")"
