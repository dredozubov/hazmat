#!/bin/bash
# Local repo-matrix validation entrypoint for Hazmat session integrations.
#
# Usage:
#   bash scripts/e2e-stack-matrix.sh
#   bash scripts/e2e-stack-matrix.sh --detect --id next-js
#   bash scripts/e2e-stack-matrix.sh --smoke --track informational --id apache-maven
#
# Environment overrides:
#   STACKCHECK_MANIFEST
#   STACKCHECK_WORKSPACE_ROOT

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HAZMAT="$REPO_ROOT/hazmat/hazmat"
MODE="contract"
TRACK="required"
WAVE=""
MANIFEST="${STACKCHECK_MANIFEST:-$REPO_ROOT/testdata/stack-matrix/repos.yaml}"
WORKSPACE_ROOT="${STACKCHECK_WORKSPACE_ROOT:-$HOME/workspace/stack-matrix}"
SKIP_BUILD=""
IDS=()

usage() {
    cat <<EOF
Usage: bash scripts/e2e-stack-matrix.sh [options]

Modes:
  --detect           Run detection-only checks
  --contract         Run detect + contract checks (default)
  --smoke            Run detect + contract + contained workflow checks

Selection:
  --track <name>     required, informational, or all (default: required)
  --wave <n>         only run repos from the given wave
  --id <repo-id>     only run the named repo id (repeatable)

Paths:
  --manifest <path>        repo corpus manifest
  --workspace-root <path>  checkout/cache root for pinned repos
  --skip-build             trust the existing Hazmat binary instead of rebuilding

Examples:
  bash scripts/e2e-stack-matrix.sh
  bash scripts/e2e-stack-matrix.sh --detect --id next-js
  bash scripts/e2e-stack-matrix.sh --smoke --track informational --id apache-maven
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --detect)
            MODE="detect"
            ;;
        --contract)
            MODE="contract"
            ;;
        --smoke)
            MODE="smoke"
            ;;
        --track)
            [ "$#" -ge 2 ] || { echo "error: --track requires a value" >&2; exit 1; }
            TRACK="$2"
            shift
            ;;
        --wave)
            [ "$#" -ge 2 ] || { echo "error: --wave requires a value" >&2; exit 1; }
            WAVE="$2"
            shift
            ;;
        --id)
            [ "$#" -ge 2 ] || { echo "error: --id requires a value" >&2; exit 1; }
            IDS+=("$2")
            shift
            ;;
        --manifest)
            [ "$#" -ge 2 ] || { echo "error: --manifest requires a value" >&2; exit 1; }
            MANIFEST="$2"
            shift
            ;;
        --workspace-root)
            [ "$#" -ge 2 ] || { echo "error: --workspace-root requires a value" >&2; exit 1; }
            WORKSPACE_ROOT="$2"
            shift
            ;;
        --skip-build)
            SKIP_BUILD="1"
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

if [ -z "$SKIP_BUILD" ] || [ ! -x "$HAZMAT" ]; then
    echo "Building Hazmat binaries..."
    (cd "$REPO_ROOT/hazmat" && make all)
fi

cmd=(
    "$HAZMAT"
    stackcheck
    "$MODE"
    --manifest "$MANIFEST"
    --workspace-root "$WORKSPACE_ROOT"
    --track "$TRACK"
)

if [ -n "$WAVE" ]; then
    cmd+=(--wave "$WAVE")
fi

for id in "${IDS[@]}"; do
    cmd+=(--id "$id")
done

printf 'hazmat: repo-matrix %s (track=%s)\n' "$MODE" "$TRACK"
printf '  manifest: %s\n' "$MANIFEST"
printf '  workspace: %s\n' "$WORKSPACE_ROOT"
if [ -n "$WAVE" ]; then
    printf '  wave: %s\n' "$WAVE"
fi
if [ "${#IDS[@]}" -gt 0 ]; then
    printf '  ids: %s\n' "${IDS[*]}"
fi
printf '\n'

"${cmd[@]}"
