#!/bin/sh

set -eu

cd "$(dirname "$0")"

run_spec() {
  spec="$1"
  liveness="$2"

  printf '==> %s\n' "$spec"

  set -- -workers "${TLC_WORKERS:-auto}"
  if [ "$liveness" = "yes" ]; then
    set -- "$@" -lncheck final
  fi

  if [ -n "${TLC_METADIR_ROOT:-}" ]; then
    mkdir -p "${TLC_METADIR_ROOT}"
    metadir="$(mktemp -d "${TLC_METADIR_ROOT%/}/${spec}.XXXXXX")"
    set -- "$@" -metadir "$metadir"
  fi

  bash ./run_tlc.sh "$@" -config "${spec}.cfg" "${spec}.tla"
}

run_spec MC_SetupRollback yes
run_spec MC_SeatbeltPolicy no
run_spec MC_BackupSafety yes
run_spec MC_Migration yes
run_spec MC_Tier3LaunchContainment no
run_spec MC_TierPolicyEquivalence no
run_spec MC_SessionPermissionRepairs no
run_spec MC_HarnessLifecycle no
