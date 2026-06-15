#!/bin/bash
# Run E2E tests inside an isolated Lume macOS VM.
#
# Usage:
#   bash scripts/e2e-vm.sh                         # full test with network probes
#   bash scripts/e2e-vm.sh --quick                 # skip live network probes
#   bash scripts/e2e-vm.sh --step download --quick # cache IPSW once
#   bash scripts/e2e-vm.sh --step install --quick  # install base OS only
#   bash scripts/e2e-vm.sh --step setup --quick    # retry Setup Assistant only
#   bash scripts/e2e-vm.sh --step base --quick     # finish base provisioning
#   bash scripts/e2e-vm.sh --keep                  # keep test VM after the run
#
# Prerequisites:
#   - Apple Silicon Mac
#   - Lume installed: brew install lume
#
# First run downloads an IPSW into a persistent cache, creates a base VM from
# that file, then runs Setup Assistant as a separate resumable step. Subsequent
# runs clone the base (~seconds) and destroy the clone after testing.
#
# Base VM: hazmat-e2e-base (persistent, reused across runs)
# Test VM: hazmat-e2e-<pid> (ephemeral, destroyed after each run)

set -euo pipefail

BASE_VM="hazmat-e2e-base"
TEST_VM="hazmat-e2e-$$"
VM_USER="lume"
VM_PASS="lume"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE_SETUP_MARKER="$HOME/.lume/$BASE_VM/.hazmat-e2e-setup-ready"
BASE_READY_MARKER="$HOME/.lume/$BASE_VM/.hazmat-e2e-base-ready"
CACHE_ROOT="${XDG_CACHE_HOME:-$HOME/.cache}/hazmat/e2e-vm"
IPSW_CACHE_DIR="${HAZMAT_E2E_IPSW_CACHE_DIR:-$CACHE_ROOT/ipsw}"
IPSW_LATEST_PATH_FILE="$IPSW_CACHE_DIR/latest.path"
IPSW_LATEST_URL_FILE="$IPSW_CACHE_DIR/latest.url"

QUICK=""
KEEP=""
RESET_BASE=""
STEP="all"
VM_IP=""
BASE_RUN_PID=""
TEST_VM_CREATED=""

usage() {
    cat <<EOF
Usage:
  bash scripts/e2e-vm.sh [--quick] [--keep] [--reset-vm-base]
  bash scripts/e2e-vm.sh --step download [--quick]
  bash scripts/e2e-vm.sh --step install [--quick]
  bash scripts/e2e-vm.sh --step setup [--quick]
  bash scripts/e2e-vm.sh --step base [--quick]

Runs the destructive Hazmat lifecycle test in a disposable Lume VM.

Options:
  --quick             Pass --quick to scripts/e2e.sh inside the guest.
  --keep              Keep the cloned test VM after the run.
  --reset-vm-base     Delete hazmat-e2e-base before recreating it.
  --step STEP         Run one step: download, install, setup, base, all.
  --vm-step STEP      Alias for --step.
  -h, --help          Show this help.

Steps:
  download  Download and cache the latest supported IPSW once.
  install   Create hazmat-e2e-base from cached IPSW only.
  setup     Run Lume Setup Assistant automation on the existing base VM.
  base      Ensure the base VM is installed, setup, and provisioned with Go.
  all       Ensure the base VM, clone a test VM, and run scripts/e2e.sh.

Cache:
  ${HAZMAT_E2E_IPSW_CACHE_DIR:-$IPSW_CACHE_DIR}
  Set HAZMAT_E2E_IPSW=/path/to/file.ipsw to use a manually managed IPSW.

Before the first install, run:
  bash scripts/e2e-vm.sh --step download --quick

If Setup Assistant automation fails, rerun:
  bash scripts/e2e-vm.sh --step setup --quick

Only use --reset-vm-base when you intentionally want to discard the base VM.
It does not delete the cached IPSW.
EOF
}

die() {
    echo "Error: $*" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --quick)
            QUICK="--quick"
            ;;
        --keep)
            KEEP="1"
            ;;
        --reset-vm-base)
            RESET_BASE="1"
            ;;
        --step|--vm-step)
            [ "$#" -ge 2 ] || die "$1 requires a step name"
            shift
            STEP="$1"
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

case "$STEP" in
    download|install|setup|base|all)
        ;;
    *)
        die "unknown VM step '$STEP' (expected download, install, setup, base, or all)"
        ;;
esac

cleanup() {
    if [ -n "${BASE_RUN_PID:-}" ]; then
        lume stop "$BASE_VM" 2>/dev/null || true
        wait "$BASE_RUN_PID" 2>/dev/null || true
        BASE_RUN_PID=""
    fi

    if [ -z "$TEST_VM_CREATED" ]; then
        return
    fi

    if [ -n "$KEEP" ]; then
        echo ""
        echo "VM $TEST_VM kept alive for debugging."
        if [ -n "$VM_IP" ]; then
            echo "  SSH:     ssh $VM_USER@$VM_IP"
        fi
        echo "  Destroy: lume stop $TEST_VM && lume delete $TEST_VM --force"
        return
    fi

    echo "Cleaning up VM $TEST_VM..."
    lume stop "$TEST_VM" 2>/dev/null || true
    lume delete "$TEST_VM" --force 2>/dev/null || true
}
trap cleanup EXIT

if ! command -v lume >/dev/null 2>&1; then
    die "lume not found. Install with: brew install lume"
fi

host_major_version() {
    sw_vers -productVersion | cut -d. -f1
}

base_preset() {
    local host_version
    host_version="$(host_major_version)"
    case "$host_version" in
        26) echo "tahoe" ;;
        15) echo "sequoia" ;;
        *)  echo "tahoe" ;;
    esac
}

lume_vm_exists() {
    local vm="$1"
    lume get "$vm" >/dev/null 2>&1
}

latest_ipsw_url() {
    local url
    url="$(lume ipsw | awk '/^https:\/\// { found=$0 } END { print found }')"
    [ -n "$url" ] || die "could not resolve latest IPSW URL from 'lume ipsw'"
    echo "$url"
}

download_ipsw() {
    mkdir -p "$IPSW_CACHE_DIR"

    local url filename path partial
    url="$(latest_ipsw_url)"
    filename="${url##*/}"
    case "$filename" in
        *.ipsw)
            ;;
        *)
            die "resolved IPSW URL does not end in .ipsw: $url"
            ;;
    esac

    path="$IPSW_CACHE_DIR/$filename"
    partial="$path.part"

    if [ -s "$path" ]; then
        echo "Cached IPSW already exists: $path"
    else
        echo "Downloading IPSW to persistent cache:"
        echo "  $path"
        if ! curl --fail --location --continue-at - --output "$partial" "$url"; then
            echo "Resumable IPSW download failed; retrying from scratch..." >&2
            rm -f "$partial"
            curl --fail --location --output "$partial" "$url"
        fi
        mv -f "$partial" "$path"
        echo "Cached IPSW downloaded: $path"
    fi

    printf '%s\n' "$url" > "$IPSW_LATEST_URL_FILE"
    printf '%s\n' "$path" > "$IPSW_LATEST_PATH_FILE"
}

cached_ipsw_path() {
    if [ -n "${HAZMAT_E2E_IPSW:-}" ]; then
        [ -f "$HAZMAT_E2E_IPSW" ] || die "HAZMAT_E2E_IPSW does not point to a file: $HAZMAT_E2E_IPSW"
        echo "$HAZMAT_E2E_IPSW"
        return
    fi

    if [ -f "$IPSW_LATEST_PATH_FILE" ]; then
        local path
        path="$(sed -n '1p' "$IPSW_LATEST_PATH_FILE")"
        if [ -n "$path" ] && [ -f "$path" ]; then
            echo "$path"
            return
        fi
    fi

    if [ -d "$IPSW_CACHE_DIR" ]; then
        local fallback
        fallback="$(find "$IPSW_CACHE_DIR" -maxdepth 1 -type f -name '*.ipsw' -print | sort | tail -1 || true)"
        if [ -n "$fallback" ]; then
            echo "$fallback"
            return
        fi
    fi

    return 1
}

require_cached_ipsw() {
    local path
    if ! path="$(cached_ipsw_path)"; then
        echo "Cached IPSW not found." >&2
        echo "Download it once with:" >&2
        echo "  bash scripts/e2e-vm.sh --step download --quick" >&2
        echo "" >&2
        echo "Or point Hazmat at a manually downloaded IPSW:" >&2
        echo "  HAZMAT_E2E_IPSW=/path/to/file.ipsw bash scripts/e2e-vm.sh --step install --quick" >&2
        exit 1
    fi
    echo "$path"
}

get_vm_ip() {
    local vm="$1"
    local ip
    ip=$(lume get "$vm" -f json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('ip',''))" 2>/dev/null || true)
    if [ -z "$ip" ]; then
        ip=$(lume get "$vm" 2>/dev/null | grep -oE '192\.168\.[0-9]+\.[0-9]+' | head -1 || true)
    fi
    echo "$ip"
}

wait_for_ssh() {
    local vm="$1"
    echo "Waiting for SSH on $vm..."
    for _ in $(seq 1 90); do
        local ip
        ip=$(get_vm_ip "$vm")
        if [ -n "$ip" ] && ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -o BatchMode=yes "$VM_USER@$ip" true 2>/dev/null; then
            echo "SSH ready at $VM_USER@$ip"
            return 0
        fi
        sleep 2
    done
    echo "Error: VM did not become reachable via SSH within 180s" >&2
    return 1
}

vm_ssh_to() {
    local ip="$1"
    shift
    ssh -o StrictHostKeyChecking=no -o BatchMode=yes "$VM_USER@$ip" "$@"
}

reset_base_if_requested() {
    if [ -z "$RESET_BASE" ]; then
        return
    fi

    echo "Resetting base VM $BASE_VM..."
    lume stop "$BASE_VM" 2>/dev/null || true
    lume delete "$BASE_VM" --force 2>/dev/null || true
    rm -f "$BASE_SETUP_MARKER" "$BASE_READY_MARKER"
}

create_base_vm() {
    if lume_vm_exists "$BASE_VM"; then
        echo "Base VM $BASE_VM already exists; skipping OS install."
        return
    fi

    local host_version preset ipsw_path
    host_version="$(host_major_version)"
    preset="$(base_preset)"
    ipsw_path="$(require_cached_ipsw)"

    echo "Creating base VM $BASE_VM (one-time, ~15-20 min)..."
    echo "Host macOS $host_version: Setup Assistant preset will be '$preset'."
    echo "Using cached IPSW: $ipsw_path"
    echo "This step installs macOS only; unattended setup is a separate resumable step."
    lume create "$BASE_VM" \
        --os macOS \
        --ipsw "$ipsw_path" \
        --cpu 4 \
        --memory 8GB \
        --disk-size 50GB \
        --no-display
    rm -f "$BASE_SETUP_MARKER" "$BASE_READY_MARKER"
    echo "Base VM $BASE_VM OS install completed."
}

setup_base_vm() {
    lume_vm_exists "$BASE_VM" || die "base VM $BASE_VM does not exist; run: bash scripts/e2e-vm.sh --step install --quick"

    if [ -f "$BASE_SETUP_MARKER" ]; then
        echo "Base VM $BASE_VM already has Hazmat setup marker."
        return
    fi

    local preset
    preset="$(base_preset)"

    echo "Running Setup Assistant automation for base VM $BASE_VM..."
    echo "If this fails, rerun: bash scripts/e2e-vm.sh --step setup --quick"
    if ! lume setup "$BASE_VM" --mode preset --unattended "$preset" --no-display; then
        echo "" >&2
        echo "Base VM $BASE_VM setup failed." >&2
        echo "The OS install step is separate now, so rerun setup without downloading IPSW again:" >&2
        echo "  bash scripts/e2e-vm.sh --step setup --quick" >&2
        echo "" >&2
        echo "Only rebuild the base if you intentionally want a fresh OS install:" >&2
        echo "  bash scripts/e2e-vm.sh --reset-vm-base --step base --quick" >&2
        exit 1
    fi

    touch "$BASE_SETUP_MARKER"
    rm -f "$BASE_READY_MARKER"
    echo "Base VM $BASE_VM Setup Assistant automation completed."
}

start_base_vm() {
    echo "Booting base VM $BASE_VM..."
    lume run "$BASE_VM" --no-display &
    BASE_RUN_PID=$!

    sleep 2
    if ! kill -0 "$BASE_RUN_PID" 2>/dev/null; then
        local status=0
        wait "$BASE_RUN_PID" || status=$?
        BASE_RUN_PID=""
        echo "Base VM $BASE_VM did not start." >&2
        echo "If Lume says the VM is still being provisioned, let it finish and rerun:" >&2
        echo "  bash scripts/e2e-vm.sh --step base --quick" >&2
        exit "$status"
    fi
}

stop_base_vm() {
    if [ -z "$BASE_RUN_PID" ]; then
        return
    fi

    lume stop "$BASE_VM" 2>/dev/null || true
    wait "$BASE_RUN_PID" 2>/dev/null || true
    BASE_RUN_PID=""
}

provision_base_vm() {
    lume_vm_exists "$BASE_VM" || die "base VM $BASE_VM does not exist; run: bash scripts/e2e-vm.sh --step install --quick"

    if [ -f "$BASE_READY_MARKER" ]; then
        echo "Base VM $BASE_VM already has Hazmat readiness marker."
        return
    fi

    echo "Installing Go and passwordless sudo in base VM $BASE_VM..."
    start_base_vm

    if ! wait_for_ssh "$BASE_VM"; then
        stop_base_vm
        echo "" >&2
        echo "Base VM $BASE_VM exists but SSH did not become reachable." >&2
        echo "If Setup Assistant was not completed, rerun:" >&2
        echo "  bash scripts/e2e-vm.sh --step setup --quick" >&2
        echo "Then finish base provisioning with:" >&2
        echo "  bash scripts/e2e-vm.sh --step base --quick" >&2
        exit 1
    fi

    local base_ip
    base_ip="$(get_vm_ip "$BASE_VM")"
    vm_ssh_to "$base_ip" 'command -v brew >/dev/null 2>&1 || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
    vm_ssh_to "$base_ip" 'eval "$(/opt/homebrew/bin/brew shellenv)" && brew install go'
    vm_ssh_to "$base_ip" "echo '$VM_PASS' | sudo -S sh -c 'echo \"$VM_USER ALL=(ALL) NOPASSWD: ALL\" > /etc/sudoers.d/$VM_USER && chmod 440 /etc/sudoers.d/$VM_USER'"

    stop_base_vm
    touch "$BASE_READY_MARKER"
    echo "Base VM ready with Go + passwordless sudo."
}

ensure_base_vm() {
    reset_base_if_requested

    if ! lume_vm_exists "$BASE_VM"; then
        create_base_vm
    fi

    setup_base_vm
    provision_base_vm
}

clone_and_prepare_test_vm() {
    ensure_base_vm

    echo "Cloning $BASE_VM -> $TEST_VM..."
    lume clone "$BASE_VM" "$TEST_VM"
    TEST_VM_CREATED="1"

    echo "Booting $TEST_VM (headless, shared dir: $REPO_ROOT)..."
    lume run "$TEST_VM" --no-display --shared-dir "$REPO_ROOT" &

    wait_for_ssh "$TEST_VM"
    VM_IP="$(get_vm_ip "$TEST_VM")"

    local guest_repo="/tmp/hazmat-repo"
    echo "Copying repo to VM local disk..."
    vm_ssh_to "$VM_IP" "rm -rf $guest_repo && cp -a '/Volumes/My Shared Files' $guest_repo"
}

run_guest_e2e() {
    local guest_repo="/tmp/hazmat-repo"
    echo ""
    echo "========================================================"
    echo "  Running E2E tests inside VM ($TEST_VM)"
    echo "========================================================"
    echo ""

    vm_ssh_to "$VM_IP" "eval \"\$(/opt/homebrew/bin/brew shellenv)\" && cd $guest_repo && bash scripts/e2e.sh $QUICK"
}

case "$STEP" in
    download)
        download_ipsw
        ;;
    install)
        reset_base_if_requested
        create_base_vm
        ;;
    setup)
        reset_base_if_requested
        setup_base_vm
        ;;
    base)
        ensure_base_vm
        ;;
    all)
        clone_and_prepare_test_vm
        run_guest_e2e
        ;;
esac
