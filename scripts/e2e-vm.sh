#!/bin/bash
# Run E2E tests inside an isolated Lume macOS VM.
#
# Usage:
#   bash scripts/e2e-vm.sh              # full test with network probes
#   bash scripts/e2e-vm.sh --quick      # skip live network probes
#   bash scripts/e2e-vm.sh --keep       # don't destroy VM after (for debugging)
#
# Prerequisites:
#   - Apple Silicon Mac
#   - Lume installed: brew install lume
#
# First run creates a base VM from IPSW (~15-20 min, one-time). Subsequent
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
QUICK=""
KEEP=""
VM_IP=""

for arg in "$@"; do
    case "$arg" in
        --quick) QUICK="--quick" ;;
        --keep)  KEEP="1" ;;
    esac
done

cleanup() {
    if [ -n "$KEEP" ]; then
        echo ""
        echo "VM $TEST_VM kept alive for debugging."
        echo "  SSH:     ssh $VM_USER@$VM_IP"
        echo "  Destroy: lume stop $TEST_VM && lume delete $TEST_VM --force"
        return
    fi
    echo "Cleaning up VM $TEST_VM..."
    lume stop "$TEST_VM" 2>/dev/null || true
    lume delete "$TEST_VM" --force 2>/dev/null || true
}
trap cleanup EXIT

# ── Preflight ────────────────────────────────────────────────────────────────

if ! command -v lume &>/dev/null; then
    echo "Error: lume not found. Install with: brew install lume"
    exit 1
fi

# ── Ensure base VM exists ────────────────────────────────────────────────────
# Creates a macOS Sequoia VM with unattended setup (user: lume, pass: lume,
# SSH enabled). This takes ~15-20 min on first run but only happens once.

if lume get "$BASE_VM" &>/dev/null; then
    echo "Base VM $BASE_VM already exists."
else
    # Detect host macOS version to pick the right unattended preset.
    HOST_VERSION=$(sw_vers -productVersion | cut -d. -f1)
    case "$HOST_VERSION" in
        26) PRESET="tahoe" ;;
        15) PRESET="sequoia" ;;
        *)  PRESET="tahoe" ;;
    esac

    echo "Creating base VM $BASE_VM (one-time, ~15-20 min)..."
    echo "Host macOS $HOST_VERSION → using '$PRESET' preset."
    echo "This downloads macOS from Apple and runs unattended Setup Assistant."
    lume create "$BASE_VM" \
        --os macOS \
        --ipsw latest \
        --cpu 4 \
        --memory 8GB \
        --disk-size 50GB \
        --unattended "$PRESET" \
        --no-display
    echo "Base VM $BASE_VM created."

    # Boot the base VM to install Go, then stop it.
    echo "Installing Go in base VM..."
    lume run "$BASE_VM" --no-display &
    BASE_PID=$!

    wait_for_ssh "$BASE_VM"

    BASE_IP=$(get_vm_ip "$BASE_VM")
    vm_ssh_to "$BASE_IP" 'command -v brew &>/dev/null || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
    vm_ssh_to "$BASE_IP" 'eval "$(/opt/homebrew/bin/brew shellenv)" && brew install go'
    # Enable passwordless sudo
    vm_ssh_to "$BASE_IP" "echo '$VM_PASS' | sudo -S sh -c 'echo \"$VM_USER ALL=(ALL) NOPASSWD: ALL\" > /etc/sudoers.d/$VM_USER && chmod 440 /etc/sudoers.d/$VM_USER'"

    lume stop "$BASE_VM"
    wait
    echo "Base VM ready with Go + passwordless sudo."
fi

# ── Helper functions ─────────────────────────────────────────────────────────

get_vm_ip() {
    local vm="$1"
    # Try JSON output first, fall back to text parsing
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
    for i in $(seq 1 90); do
        local ip
        ip=$(get_vm_ip "$vm")
        if [ -n "$ip" ] && ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -o BatchMode=yes "$VM_USER@$ip" true 2>/dev/null; then
            echo "SSH ready at $VM_USER@$ip"
            return 0
        fi
        sleep 2
    done
    echo "Error: VM did not become reachable via SSH within 180s"
    return 1
}

vm_ssh_to() {
    local ip="$1"
    shift
    ssh -o StrictHostKeyChecking=no -o BatchMode=yes "$VM_USER@$ip" "$@"
}

# ── Clone and boot test VM ───────────────────────────────────────────────────

echo "Cloning $BASE_VM → $TEST_VM..."
lume clone "$BASE_VM" "$TEST_VM"

echo "Booting $TEST_VM (headless, shared dir: $REPO_ROOT)..."
lume run "$TEST_VM" --no-display --shared-dir "$REPO_ROOT" &

wait_for_ssh "$TEST_VM"
VM_IP=$(get_vm_ip "$TEST_VM")

# ── Copy repo to VM local disk ───────────────────────────────────────────────
# VirtioFS is fine for reads but Go builds are faster on local disk.

GUEST_REPO="/tmp/hazmat-repo"
echo "Copying repo to VM local disk..."
vm_ssh_to "$VM_IP" "rm -rf $GUEST_REPO && cp -a '/Volumes/My Shared Files' $GUEST_REPO"

# ── Run E2E ──────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════"
echo "  Running E2E tests inside VM ($TEST_VM)"
echo "════════════════════════════════════════════════════════"
echo ""

vm_ssh_to "$VM_IP" "eval \"\$(/opt/homebrew/bin/brew shellenv)\" && cd $GUEST_REPO && bash scripts/e2e.sh $QUICK"
