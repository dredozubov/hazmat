#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
TLA_DIR="${ROOT_DIR}/tla"

tracked="$(git -C "$ROOT_DIR" ls-files -- \
  'tla/*_TTrace_*.tla' \
  'tla/*_TTrace_*.bin' \
  'tla/states/*')"

if [ -n "$tracked" ]; then
  printf '%s\n' "$tracked" >&2
  echo "trace-artifacts: generated TLC trace/state artifacts must not be tracked" >&2
  echo "trace-artifacts: document a reduced reproducer instead of committing raw TLC output" >&2
  exit 1
fi

local_trace_count="$(find "$TLA_DIR" -maxdepth 1 -type f \( -name '*_TTrace_*.tla' -o -name '*_TTrace_*.bin' \) | wc -l | tr -d ' ')"
local_state_count="$(find "$TLA_DIR/states" -type f 2>/dev/null | wc -l | tr -d ' ')"

printf 'trace-artifacts: ok (%s local trace files, %s local state files ignored)\n' \
  "$local_trace_count" \
  "$local_state_count"
