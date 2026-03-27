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
#   - First run pulls ~40 GB macOS image (cached after that)
#
# What it does:
#   1. Clones a vanilla macOS Sequoia VM (CoW, fast)
#   2. Boots it headless with the repo shared via VirtioFS
#   3. SSHs in, installs Go, runs scripts/e2e.sh
#   4. Destroys the clone

set -euo pipefail

VM_NAME="hazmat-e2e-$$"
VM_IMAGE="macos-sequoia-vanilla:latest"
VM_USER="lume"
VM_PASS="lume"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
QUICK=""
KEEP=""

for arg in "$@"; do
    case "$arg" in
        --quick) QUICK="--quick" ;;
        --keep)  KEEP="1" ;;
    esac
done

cleanup() {
    if [ -n "$KEEP" ]; then
        echo "VM $VM_NAME kept alive for debugging."
        echo "  SSH:     ssh $VM_USER@\$(lume get $VM_NAME -f json | jq -r '.ip')"
        echo "  Destroy: lume stop $VM_NAME && lume delete $VM_NAME --force"
        return
    fi
    echo "Cleaning up VM $VM_NAME..."
    lume stop "$VM_NAME" 2>/dev/null || true
    lume delete "$VM_NAME" --force 2>/dev/null || true
}
trap cleanup EXIT

# ── Preflight ────────────────────────────────────────────────────────────────

if ! command -v lume &>/dev/null; then
    echo "Error: lume not found. Install with: brew install lume"
    exit 1
fi

echo "Creating VM $VM_NAME from $VM_IMAGE..."
lume clone "$VM_IMAGE" "$VM_NAME" 2>/dev/null \
    || lume pull "$VM_IMAGE" && lume clone "$VM_IMAGE" "$VM_NAME"

echo "Booting VM (headless, shared dir: $REPO_ROOT)..."
lume run "$VM_NAME" --no-display --shared-dir "$REPO_ROOT" &
VM_PID=$!

# ── Wait for SSH ─────────────────────────────────────────────────────────────

echo "Waiting for SSH..."
for i in $(seq 1 60); do
    VM_IP=$(lume get "$VM_NAME" -f json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('ip',''))" 2>/dev/null || true)
    if [ -n "$VM_IP" ] && ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -o BatchMode=yes "$VM_USER@$VM_IP" true 2>/dev/null; then
        echo "SSH ready at $VM_USER@$VM_IP"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "Error: VM did not become reachable via SSH within 120s"
        exit 1
    fi
    sleep 2
done

# SSH helper
vm_ssh() {
    ssh -o StrictHostKeyChecking=no -o BatchMode=no \
        "$VM_USER@$VM_IP" "$@"
}

vm_ssh_pass() {
    # For commands that need the password piped to sudo
    sshpass -p "$VM_PASS" ssh -o StrictHostKeyChecking=no "$VM_USER@$VM_IP" "$@"
}

# ── Install Go if needed ─────────────────────────────────────────────────────

echo "Checking Go installation in VM..."
if ! vm_ssh "command -v go" &>/dev/null; then
    echo "Installing Go in VM via Homebrew..."
    vm_ssh 'command -v brew &>/dev/null || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
    vm_ssh 'eval "$(/opt/homebrew/bin/brew shellenv)" && brew install go'
fi

# ── Configure passwordless sudo ──────────────────────────────────────────────

echo "Enabling passwordless sudo for test user..."
vm_ssh "echo '$VM_PASS' | sudo -S sh -c 'echo \"$VM_USER ALL=(ALL) NOPASSWD: ALL\" > /etc/sudoers.d/$VM_USER'"

# ── Run E2E tests ────────────────────────────────────────────────────────────

# The repo is mounted at /Volumes/My Shared Files inside the VM.
# Copy it to a local path to avoid VirtioFS performance quirks with Go builds.
GUEST_REPO="/tmp/hazmat-repo"

echo "Copying repo to VM local disk..."
vm_ssh "rm -rf $GUEST_REPO && cp -a '/Volumes/My Shared Files' $GUEST_REPO"

echo ""
echo "════════════════════════════════════════════════════════"
echo "  Running E2E tests inside VM"
echo "════════════════════════════════════════════════════════"
echo ""

# Run the test. eval brew shellenv ensures Go is on PATH.
vm_ssh "eval \"\$(/opt/homebrew/bin/brew shellenv)\" && cd $GUEST_REPO && bash scripts/e2e.sh $QUICK"
EXIT_CODE=$?

exit "$EXIT_CODE"
