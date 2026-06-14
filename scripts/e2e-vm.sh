#!/bin/bash
# Compatibility wrapper for the VM-backed Hazmat lifecycle test.
#
# The lifecycle and VM orchestration both live in scripts/e2e.sh so host and VM
# modes cannot drift.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

usage() {
    cat <<EOF
Usage:
  bash scripts/e2e-vm.sh [--quick] [--keep] [--reset-vm-base]
  bash scripts/e2e-vm.sh --step download [--quick]
  bash scripts/e2e-vm.sh --step install [--quick]
  bash scripts/e2e-vm.sh --step setup [--quick]
  bash scripts/e2e-vm.sh --step setup-terms [--quick]
  bash scripts/e2e-vm.sh --step base [--quick]

Runs the destructive Hazmat lifecycle test in a disposable Lume VM.

Options:
  --quick             Pass --quick to scripts/e2e.sh inside the guest.
  --keep              Keep the cloned test VM after the run.
  --reset-vm-base     Delete hazmat-e2e-base before recreating it.
  --step STEP         Run one step: download, install, setup, setup-terms, base, prepare, guest, all.
  --vm-step STEP      Alias for --step.
  -h, --help          Show this help.

Steps:
  download    Download and cache the latest supported IPSW once.
  install     Create hazmat-e2e-base from cached IPSW only.
  setup       Run Lume Setup Assistant automation on the existing base VM.
  setup-terms Resume Lume Setup Assistant automation from Terms and Conditions.
  base        Ensure the base VM is installed, setup, and provisioned with Go.
  prepare     Clone and boot a disposable test VM, then keep it for guest reruns.
  guest       Run scripts/e2e.sh inside an existing prepared test VM.
  all         Ensure the base VM, clone a test VM, and run scripts/e2e.sh.

Before the first install, run:
  bash scripts/e2e-vm.sh --step download --quick

If Setup Assistant automation fails, rerun:
  bash scripts/e2e-vm.sh --step setup --quick

This wrapper delegates to scripts/e2e.sh --vm so host and VM lifecycle logic
cannot drift.
EOF
}

args=()
while [ "$#" -gt 0 ]; do
    case "$1" in
        --help|-h)
            usage
            exit 0
            ;;
        --step|--vm-step)
            if [ "$#" -lt 2 ]; then
                echo "error: $1 requires a value" >&2
                usage >&2
                exit 2
            fi
            case "$2" in
                all|download|install|setup|setup-terms|base|prepare|clone|guest)
                    ;;
                *)
                    echo "error: unknown VM step '$2'" >&2
                    usage >&2
                    exit 2
                    ;;
            esac
            args+=(--vm-step "$2")
            shift
            ;;
        --step=*)
            step="${1#--step=}"
            case "$step" in
                all|download|install|setup|setup-terms|base|prepare|clone|guest)
                    ;;
                *)
                    echo "error: unknown VM step '$step'" >&2
                    usage >&2
                    exit 2
                    ;;
            esac
            args+=(--vm-step "$step")
            ;;
        *)
            args+=("$1")
            ;;
    esac
    shift
done

exec bash "$SCRIPT_DIR/e2e.sh" --vm "${args[@]}"
