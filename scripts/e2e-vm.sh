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
  bash scripts/e2e-vm.sh --step pull [--quick]
  bash scripts/e2e-vm.sh --step download [--quick]
  bash scripts/e2e-vm.sh --step base [--quick]

Runs the destructive Hazmat lifecycle test in a disposable macOS VM.
Defaults to Lume; set HAZMAT_E2E_VM_PROVIDER=tart to use Tart/Cirrus images.

Options:
  --quick             Pass --quick to scripts/e2e.sh inside the guest.
  --keep              Keep the cloned test VM after the run.
  --reset-vm-base     Delete hazmat-e2e-base before pull/provision.
  --step STEP         Run one step: pull, download, base, prepare, guest, all.
  --vm-step STEP      Alias for --step.
  -h, --help          Show this help.

Steps:
  pull        Pull the maintained prebuilt image into hazmat-e2e-base.
  download    Compatibility alias for pull; no IPSW is downloaded.
  base        Provision an existing pulled base VM with Go.
  prepare     Clone and boot a disposable test VM, then keep it for guest reruns.
  guest       Run scripts/e2e.sh inside an existing prepared test VM.
  all         Provision the base VM, clone a test VM, and run scripts/e2e.sh.

Before the first base provisioning, pull the prebuilt image once:
  bash scripts/e2e-vm.sh --step download --quick

For Tart:
  HAZMAT_E2E_VM_PROVIDER=tart bash scripts/e2e-vm.sh --step download --quick

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
                all|pull|download|base|prepare|clone|guest)
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
                all|pull|download|base|prepare|clone|guest)
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
