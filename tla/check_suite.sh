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

  if [ -n "${TLC_LOG_DIR:-}" ]; then
    mkdir -p "${TLC_LOG_DIR}"
    log_path="${TLC_LOG_DIR%/}/${spec}.log"
    if bash ./run_tlc.sh "$@" -config "${spec}.cfg" "${spec}.tla" >"${log_path}" 2>&1; then
      cat "${log_path}"
    else
      status="$?"
      cat "${log_path}"
      return "$status"
    fi
  else
    bash ./run_tlc.sh "$@" -config "${spec}.cfg" "${spec}.tla"
  fi
}

# This is the promoted proof suite. When adding or removing a promoted spec,
# update promoted_specs.tsv plus EXPECTED_PROMOTED_SPEC_COUNT in
# proof_audit.sh and proof_ownership_check.sh in the same change.
run_spec MC_SetupRollback yes
run_spec MC_SeatbeltPolicy no
run_spec MC_BackupSafety yes
run_spec MC_Migration yes
run_spec MC_Tier3LaunchContainment no
run_spec MC_TierPolicyEquivalence no
run_spec MC_SessionPermissionRepairs no
run_spec MC_HarnessLifecycle no
run_spec MC_LaunchFDIsolation no
run_spec MC_LinuxNativeLaunch no
run_spec MC_GitSSHRouting no
run_spec MC_GitHookApproval no
run_spec MC_SecretStoreRecovery no
run_spec MC_CredentialCapabilityLifecycle no
