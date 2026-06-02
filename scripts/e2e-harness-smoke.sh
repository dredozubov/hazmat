#!/bin/sh
# Hermetic all-harness smoke tests for Hazmat launch plumbing.
#
# This runner avoids sudo, hazmat init, and /Users/agent. It runs a
# build-tagged Go smoke fixture that redirects host and agent state under a
# temporary root while exercising the real harness command entrypoints.

set -eu

usage() {
    cat <<EOF
Usage:
  bash scripts/e2e-harness-smoke.sh [--skip-build]
  bash scripts/e2e-harness-smoke.sh --list-harnesses

Runs hermetic smoke coverage for:
  - Claude Code, Codex, OpenCode, Gemini, Hermes, Qwen, and Cursor Agent foreground launch paths
  - provider/env delivery for harnesses that consume provider env grants
  - file-backed auth materialization, harvest, and cleanup where applicable

This default smoke does not require sudo, hazmat init, an agent account, or
/Users/agent writes. Use scripts/e2e-harness-smoke-native.sh for the optional
prepared-host launch-helper and seatbelt smoke.
EOF
}

SKIP_BUILD=""
LIST_HARNESSES=""
SMOKE_HARNESSES="claude codex opencode gemini hermes qwen cursor-agent"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --skip-build)
            SKIP_BUILD="1"
            ;;
        --list-harnesses)
            LIST_HARNESSES="1"
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

if [ -n "$LIST_HARNESSES" ]; then
    for harness in $SMOKE_HARNESSES; do
        printf '%s\n' "$harness"
    done
    exit 0
fi

TMPROOT="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-hermetic-harness-smoke.XXXXXX")"
cleanup() {
    status=$?
    if [ "${HAZMAT_SMOKE_KEEP:-}" != "1" ]; then
        rm -rf "$TMPROOT"
    else
        echo "harness-smoke: kept fixture root: $TMPROOT" >&2
    fi
    exit "$status"
}
trap cleanup EXIT INT TERM HUP

mkdir -p "$TMPROOT/host-home" "$TMPROOT/tmp" "$TMPROOT/cache" "$TMPROOT/go-build-cache" "$TMPROOT/forbidden-bin"
cat > "$TMPROOT/forbidden-bin/sudo" <<EOF
#!/bin/sh
echo "sudo \$*" >> "$TMPROOT/sudo-invoked"
exit 97
EOF
chmod 0755 "$TMPROOT/forbidden-bin/sudo"

GOMODCACHE_CURRENT="$(go env GOMODCACHE 2>/dev/null || true)"

export HOME="$TMPROOT/host-home"
export TMPDIR="$TMPROOT/tmp"
export XDG_CACHE_HOME="$TMPROOT/cache"
export GOCACHE="$TMPROOT/go-build-cache"
export GOTELEMETRY=off
export HAZMAT_SMOKE_FORBIDDEN_BIN="$TMPROOT/forbidden-bin"
export PATH="$TMPROOT/forbidden-bin:$PATH"
if [ -n "$GOMODCACHE_CURRENT" ]; then
    export GOMODCACHE="$GOMODCACHE_CURRENT"
fi

if [ -n "$SKIP_BUILD" ]; then
    echo "harness-smoke: --skip-build accepted; go test still builds the smoke fixture package" >&2
fi

cd "$REPO_ROOT/hazmat"
go test -tags hazmat_smoke_fixture -run '^TestHermeticHarnessSmoke$' -count=1 .
