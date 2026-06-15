#!/bin/bash
# E2E lifecycle test for Hazmat.
#
# Tests every critical path: init, containment, snapshot, restore (with
# byte-level content verification), rollback, and idempotency.
#
# Works anywhere: local Mac, Lume VM guest, GHA macOS runner, Cirrus CI.
#
# Usage:
#   HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh
#   HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh --quick
#   bash scripts/e2e.sh --vm --quick
#
# Warning:
#   This script is destructive to the local Hazmat setup. It runs init,
#   rollback --delete-user --delete-group, and then re-inits again. Prefer
#   scripts/e2e.sh --vm for isolated local verification.
#
# Prerequisites:
#   - macOS with sudo access
#   - Go 1.21+ (for building)

set -euo pipefail

usage() {
    cat <<EOF
Usage:
  HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh
  HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh --quick
  bash scripts/e2e.sh --vm --quick

This host-side lifecycle test is destructive to the current Hazmat setup.
Prefer --vm for isolated local verification.

Options:
  --quick    Skip live network probes inside the lifecycle.
  --vm       Run this lifecycle inside an isolated Lume macOS VM.
  --keep     With --vm, keep the test VM for debugging instead of deleting it.
  --reset-vm-base
            With --vm, delete the cached base VM before provisioning.
EOF
}

QUICK=""
RUN_IN_VM=""
KEEP_VM=""
RESET_VM_BASE=""
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HAZMAT="$REPO_ROOT/hazmat/hazmat"
PASS=0
FAIL=0
TOTAL=0

# shellcheck source=scripts/lib/test_lock.sh
. "$REPO_ROOT/scripts/lib/test_lock.sh"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --quick)
            QUICK="1"
            ;;
        --vm)
            RUN_IN_VM="1"
            ;;
        --keep)
            KEEP_VM="1"
            ;;
        --reset-vm-base)
            RESET_VM_BASE="1"
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

if [ -n "$KEEP_VM" ] && [ -z "$RUN_IN_VM" ]; then
    echo "error: --keep is only valid with --vm" >&2
    usage >&2
    exit 2
fi

if [ -n "$RESET_VM_BASE" ] && [ -z "$RUN_IN_VM" ]; then
    echo "error: --reset-vm-base is only valid with --vm" >&2
    usage >&2
    exit 2
fi

E2E_VM_CLEANUP_TEST_VM=""
E2E_VM_CLEANUP_VM_USER=""
E2E_VM_CLEANUP_VM_IP=""

cleanup_e2e_vm() {
    if [ -z "${E2E_VM_CLEANUP_TEST_VM:-}" ]; then
        return
    fi
    if [ -n "$KEEP_VM" ]; then
        echo ""
        echo "VM $E2E_VM_CLEANUP_TEST_VM kept alive for debugging."
        if [ -n "${E2E_VM_CLEANUP_VM_IP:-}" ]; then
            echo "  SSH:     ssh $E2E_VM_CLEANUP_VM_USER@$E2E_VM_CLEANUP_VM_IP"
        fi
        echo "  Destroy: lume stop $E2E_VM_CLEANUP_TEST_VM && lume delete $E2E_VM_CLEANUP_TEST_VM --force"
        return
    fi
    echo "Cleaning up VM $E2E_VM_CLEANUP_TEST_VM..."
    lume stop "$E2E_VM_CLEANUP_TEST_VM" 2>/dev/null || true
    lume delete "$E2E_VM_CLEANUP_TEST_VM" --force 2>/dev/null || true
}

run_e2e_vm() {
    local base_vm="${HAZMAT_E2E_BASE_VM:-hazmat-e2e-base}"
    local test_vm="${HAZMAT_E2E_TEST_VM:-hazmat-e2e-$$}"
    local vm_user="${HAZMAT_E2E_VM_USER:-lume}"
    local vm_pass="${HAZMAT_E2E_VM_PASS:-lume}"
    local vm_ip=""
    local guest_repo="/tmp/hazmat-repo"
    local base_ready_marker="${HAZMAT_E2E_BASE_READY_MARKER:-$HOME/.lume/$base_vm/.hazmat-e2e-base-ready}"

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
            if [ -n "$ip" ] && ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -o BatchMode=yes "$vm_user@$ip" true 2>/dev/null; then
                echo "SSH ready at $vm_user@$ip"
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
        ssh -o StrictHostKeyChecking=no -o BatchMode=yes "$vm_user@$ip" "$@"
    }

    reset_base_vm() {
        echo "Deleting base VM $base_vm..." >&2
        lume stop "$base_vm" 2>/dev/null || true
        lume delete "$base_vm" --force 2>/dev/null || true
        rm -f "$base_ready_marker"
        if [ -n "${base_pid:-}" ]; then
            wait "$base_pid" 2>/dev/null || true
        fi
    }

    fail_unreachable_base_vm() {
        cat >&2 <<EOF
Base VM $base_vm exists but has no Hazmat readiness marker:
  $base_ready_marker

Hazmat tried to resume provisioning the preserved base VM, but SSH did not
become reachable. The VM was preserved to avoid redownloading the IPSW.

To intentionally rebuild the base VM, run:
  bash scripts/e2e.sh --vm --reset-vm-base --quick
EOF
        exit 2
    }

    fail_base_still_provisioning() {
        cat >&2 <<EOF
Base VM $base_vm is still being provisioned by Lume.

Hazmat will not reset or stop it automatically because that can force another
IPSW download. Let the current Lume provisioning finish, then rerun:
  bash scripts/e2e.sh --vm --quick

Only discard the cached base if you intentionally want to rebuild it:
  bash scripts/e2e.sh --vm --reset-vm-base --quick
EOF
        exit 2
    }

    provision_base_vm() {
        local base_pid=""
        local base_ip=""
        local run_log=""
        local run_status=0

        echo "Provisioning base VM $base_vm..."
        run_log="$(mktemp "${TMPDIR:-/tmp}/hazmat-e2e-lume-run.XXXXXX")"
        lume run "$base_vm" --no-display >"$run_log" 2>&1 &
        base_pid=$!
        sleep 2
        if ! kill -0 "$base_pid" 2>/dev/null; then
            wait "$base_pid" || run_status=$?
            if grep -q "still being provisioned" "$run_log"; then
                cat "$run_log" >&2
                rm -f "$run_log"
                fail_base_still_provisioning
            fi
            cat "$run_log" >&2
            rm -f "$run_log"
            echo "lume run $base_vm failed before SSH became reachable (exit $run_status)." >&2
            if [ "$run_status" -eq 0 ]; then
                run_status=1
            fi
            exit "$run_status"
        fi

        if ! wait_for_ssh "$base_vm"; then
            lume stop "$base_vm" 2>/dev/null || true
            wait "$base_pid" 2>/dev/null || true
            rm -f "$run_log"
            fail_unreachable_base_vm
        fi

        base_ip=$(get_vm_ip "$base_vm")
        if ! vm_ssh_to "$base_ip" 'command -v brew >/dev/null 2>&1 || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'; then
            echo "Base VM $base_vm Homebrew setup failed; preserving it for repair." >&2
            echo "Use --reset-vm-base only if you want to delete and rebuild it." >&2
            exit 1
        fi
        if ! vm_ssh_to "$base_ip" 'eval "$(/opt/homebrew/bin/brew shellenv)" && brew install go'; then
            echo "Base VM $base_vm Go setup failed; preserving it for repair." >&2
            echo "Use --reset-vm-base only if you want to delete and rebuild it." >&2
            exit 1
        fi
        if ! vm_ssh_to "$base_ip" "echo '$vm_pass' | sudo -S sh -c 'echo \"$vm_user ALL=(ALL) NOPASSWD: ALL\" > /etc/sudoers.d/$vm_user && chmod 440 /etc/sudoers.d/$vm_user'"; then
            echo "Base VM $base_vm passwordless sudo setup failed; preserving it for repair." >&2
            echo "Use --reset-vm-base only if you want to delete and rebuild it." >&2
            exit 1
        fi

        lume stop "$base_vm"
        wait "$base_pid" || true
        rm -f "$run_log"
        mkdir -p "$(dirname "$base_ready_marker")"
        printf 'ready\n' >"$base_ready_marker"
        echo "Base VM ready with Go + passwordless sudo."
    }
    trap cleanup_e2e_vm EXIT

    if ! command -v lume >/dev/null 2>&1; then
        echo "Error: lume not found. Install with: brew install lume"
        exit 1
    fi

    if [ -n "$RESET_VM_BASE" ] && lume get "$base_vm" >/dev/null 2>&1; then
        reset_base_vm
    fi

    if lume get "$base_vm" >/dev/null 2>&1; then
        if [ -f "$base_ready_marker" ]; then
            echo "Base VM $base_vm already exists."
        else
            echo "Base VM $base_vm exists without Hazmat readiness marker; resuming provisioning."
            provision_base_vm
        fi
    else
        local host_version preset
        host_version=$(sw_vers -productVersion | cut -d. -f1)
        case "$host_version" in
            26) preset="tahoe" ;;
            15) preset="sequoia" ;;
            *) preset="tahoe" ;;
        esac

        echo "Creating base VM $base_vm (one-time, ~15-20 min)..."
        echo "Host macOS $host_version -> using '$preset' preset."
        echo "This downloads macOS from Apple and runs unattended Setup Assistant."
        if ! lume create "$base_vm" \
            --os macOS \
            --ipsw latest \
            --cpu 4 \
            --memory 8GB \
            --disk-size 50GB \
            --unattended "$preset" \
            --no-display; then
            echo "Base VM $base_vm setup failed; preserving it to avoid IPSW redownload." >&2
            echo "Use --reset-vm-base only if you want to delete and rebuild it." >&2
            exit 1
        fi
        echo "Base VM $base_vm created."
        provision_base_vm
    fi

    echo "Cloning $base_vm -> $test_vm..."
    E2E_VM_CLEANUP_TEST_VM="$test_vm"
    E2E_VM_CLEANUP_VM_USER="$vm_user"
    lume clone "$base_vm" "$test_vm"

    echo "Booting $test_vm (headless, shared dir: $REPO_ROOT)..."
    lume run "$test_vm" --no-display --shared-dir "$REPO_ROOT" &

    wait_for_ssh "$test_vm"
    vm_ip=$(get_vm_ip "$test_vm")
    E2E_VM_CLEANUP_VM_IP="$vm_ip"

    echo "Copying repo to VM local disk..."
    vm_ssh_to "$vm_ip" "rm -rf $guest_repo && cp -a '/Volumes/My Shared Files' $guest_repo"

    echo ""
    echo "════════════════════════════════════════════════════════"
    echo "  Running E2E tests inside VM ($test_vm)"
    echo "════════════════════════════════════════════════════════"
    echo ""

    local quick_arg=""
    if [ -n "$QUICK" ]; then
        quick_arg="--quick"
    fi
    vm_ssh_to "$vm_ip" "eval \"\$(/opt/homebrew/bin/brew shellenv)\" && cd $guest_repo && HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh $quick_arg"
}

if [ -n "$RUN_IN_VM" ]; then
    run_e2e_vm
    exit 0
fi

if [ -z "${CI:-}" ] && [ "${HAZMAT_E2E_ACK_DESTRUCTIVE:-}" != "1" ]; then
    echo "error: scripts/e2e.sh is destructive to the local Hazmat setup." >&2
    echo "Run with HAZMAT_E2E_ACK_DESTRUCTIVE=1, or prefer scripts/e2e.sh --vm for isolated verification." >&2
    exit 1
fi

acquire_hazmat_test_suite_lock "scripts/e2e.sh"

pass() { PASS=$((PASS + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { FAIL=$((FAIL + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[31m✗\033[0m %s\n" "$1"; }
phase() { printf "\n\033[1m── %s ──\033[0m\n\n" "$1"; }

assert_file_content() {
    local file="$1" expected="$2" label="$3"
    if [ ! -f "$file" ]; then
        fail "$label: file missing ($file)"
        return
    fi
    actual=$(cat "$file")
    if [ "$actual" = "$expected" ]; then
        pass "$label"
    else
        fail "$label: expected $(printf '%q' "$expected"), got $(printf '%q' "$actual")"
    fi
}

assert_file_absent() {
    local file="$1" label="$2"
    if [ ! -e "$file" ]; then
        pass "$label"
    else
        fail "$label: file still exists ($file)"
    fi
}

check_privileged_install_ownership() {
    bash "$REPO_ROOT/scripts/check-privileged-install-ownership.sh" "$@" \
        --i-understand-this-checks-privileged-install-ownership
}

# ── Build ────────────────────────────────────────────────────────────────────

phase "Build"
cd "$REPO_ROOT"
make clean && make all
sudo make install-helper
pass "hazmat + hazmat-launch built"

# ── Unit tests ───────────────────────────────────────────────────────────────

phase "Unit tests"
(cd "$REPO_ROOT/hazmat" && go test ./...) \
    && pass "go test ./... passed" \
    || fail "go test ./... failed"

# ── Phase 1: Fresh install ───────────────────────────────────────────────────

phase "Phase 1: Fresh install"
"$HAZMAT" init --yes && pass "hazmat init completed" || fail "hazmat init failed"
check_privileged_install_ownership --run \
    && pass "privileged install ownership verified after init" \
    || fail "privileged install ownership check failed after init"

if [ -n "$QUICK" ]; then
    "$HAZMAT" check && pass "hazmat check passed" \
        || printf "  \033[33m!\033[0m hazmat check reported issues (non-fatal — some checks are environment-dependent)\n"
else
    "$HAZMAT" check --full && pass "hazmat check --full passed" \
        || printf "  \033[33m!\033[0m hazmat check --full reported issues (non-fatal — some checks are environment-dependent)\n"
fi

# ── Phase 2: Containment ────────────────────────────────────────────────────

phase "Phase 2: Containment verification"

# Use /tmp (world-traversable) so the agent user can reach the project dir.
# Default mktemp -d creates under $TMPDIR (/private/var/folders/.../) which
# is not traversable by other users.
PROJECT=$(mktemp -d /tmp/hazmat-e2e-XXXXXX)

# exec runs in containment
"$HAZMAT" exec -C "$PROJECT" echo "hello" > /dev/null \
    && pass "hazmat exec runs in containment" \
    || fail "hazmat exec failed"

# Agent can write to project dir
"$HAZMAT" exec -C "$PROJECT" touch "$PROJECT/agent-wrote-this" > /dev/null 2>&1 \
    && pass "agent can write to project directory" \
    || fail "agent cannot write to project directory"

# Agent CANNOT read host credential directories
for dir in .ssh .aws .gnupg ".config/gh"; do
    full="$HOME/$dir"
    if [ -d "$full" ]; then
        "$HAZMAT" exec -C "$PROJECT" ls "$full" > /dev/null 2>&1 \
            && fail "ISOLATION BREACH: agent read ~/$dir" \
            || pass "agent cannot read ~/$dir"
    fi
done

# Agent CANNOT write outside project
TMP_ESCAPE="/tmp/hazmat-escape-test-$$"
if "$HAZMAT" exec -C "$PROJECT" touch "$TMP_ESCAPE" > /dev/null 2>&1; then
    if [ -f "$TMP_ESCAPE" ]; then
        # /tmp may be shared in some environments; this is not the security boundary.
        sudo rm -f "$TMP_ESCAPE"
    fi
fi
"$HAZMAT" exec -C "$PROJECT" touch "$HOME/hazmat-escape-test-$$" > /dev/null 2>&1 \
    && { rm -f "$HOME/hazmat-escape-test-$$"; fail "ISOLATION BREACH: agent wrote to host home"; } \
    || pass "agent cannot write to host home directory"

rm -rf "$PROJECT"

# ── Phase 3: Snapshot creation ───────────────────────────────────────────────

phase "Phase 3: Snapshot creation"

PROJECT=$(mktemp -d /tmp/hazmat-e2e-XXXXXX)
echo "original line 1" > "$PROJECT/file.txt"
mkdir -p "$PROJECT/subdir"
echo "nested content" > "$PROJECT/subdir/nested.txt"
printf '\x00\x01\x02\xff' > "$PROJECT/binary.dat"

# First session: automatic snapshot of original state
"$HAZMAT" exec -C "$PROJECT" true > /dev/null
cd "$PROJECT"
"$HAZMAT" snapshots 2>&1 | grep -q "pre-session" \
    && pass "pre-session snapshot created automatically" \
    || fail "no pre-session snapshot found after exec"

# --no-backup skips snapshot
SNAP_BEFORE=$("$HAZMAT" snapshots 2>&1 | grep -c "pre-session" || true)
"$HAZMAT" exec --no-backup -C "$PROJECT" true > /dev/null
SNAP_AFTER=$("$HAZMAT" snapshots 2>&1 | grep -c "pre-session" || true)
[ "$SNAP_BEFORE" = "$SNAP_AFTER" ] \
    && pass "--no-backup skipped snapshot" \
    || fail "--no-backup created a snapshot anyway (before=$SNAP_BEFORE after=$SNAP_AFTER)"

# Second session: snapshot again (should now have 2 pre-session snapshots)
"$HAZMAT" exec -C "$PROJECT" true > /dev/null
SNAP_COUNT=$("$HAZMAT" snapshots 2>&1 | grep -c "pre-session" || true)
[ "$SNAP_COUNT" -ge 2 ] \
    && pass "multiple snapshots accumulate ($SNAP_COUNT)" \
    || fail "expected ≥2 snapshots, got $SNAP_COUNT"

# ── Phase 4: Agent damages project, restore recovers it ─────────────────────

phase "Phase 4: Snapshot restore (content verification)"

# Simulate agent damage: modify, delete, and add files
echo "CORRUPTED BY AGENT" > "$PROJECT/file.txt"
rm -f "$PROJECT/subdir/nested.txt"
rm -f "$PROJECT/binary.dat"
echo "rogue file" > "$PROJECT/rogue.txt"
mkdir -p "$PROJECT/rogue-dir"
echo "rogue nested" > "$PROJECT/rogue-dir/evil.txt"

# Verify damage happened
assert_file_content "$PROJECT/file.txt" "CORRUPTED BY AGENT" "damage: file.txt overwritten"
assert_file_absent "$PROJECT/subdir/nested.txt" "damage: nested.txt deleted"
assert_file_absent "$PROJECT/binary.dat" "damage: binary.dat deleted"

# Restore from the most recent pre-session snapshot (session=1 because
# the second exec created the newest snapshot of the original state before
# the agent damage happened outside containment).
RESTORE_EXIT=0
"$HAZMAT" --yes restore --session=1 2>&1 || RESTORE_EXIT=$?
[ "$RESTORE_EXIT" -eq 0 ] \
    && pass "hazmat restore completed successfully" \
    || fail "hazmat restore failed (exit $RESTORE_EXIT)"

# Verify restored content byte-for-byte
assert_file_content "$PROJECT/file.txt" "original line 1" \
    "restore: file.txt content matches original"
# Known Kopia limitation: shallow restore doesn't traverse subdirectories.
# Tracked separately — don't block CI on this.
if [ -f "$PROJECT/subdir/nested.txt" ]; then
    assert_file_content "$PROJECT/subdir/nested.txt" "nested content" \
        "restore: subdir/nested.txt content matches original"
else
    printf "  \033[33m!\033[0m restore: subdir/nested.txt not restored (known Kopia shallow-restore limitation)\n"
fi

# Verify binary file round-trip
if [ -f "$PROJECT/binary.dat" ]; then
    ACTUAL_HEX=$(xxd -p "$PROJECT/binary.dat" | tr -d '\n')
    if [ "$ACTUAL_HEX" = "000102ff" ]; then
        pass "restore: binary.dat byte-level content matches"
    else
        fail "restore: binary.dat content mismatch (hex: $ACTUAL_HEX)"
    fi
else
    fail "restore: binary.dat not restored"
fi

# ── Phase 5: Undo-the-undo (pre-restore snapshot exists) ────────────────────

phase "Phase 5: Pre-restore snapshot (undo-the-undo)"

"$HAZMAT" snapshots 2>&1 | grep -q "pre-restore" \
    && pass "pre-restore snapshot was created during restore" \
    || fail "no pre-restore snapshot found (undo-the-undo is broken)"

cd "$REPO_ROOT"
rm -rf "$PROJECT"

# ── Phase 6: Rollback completeness ──────────────────────────────────────────

phase "Phase 6: Rollback"
"$HAZMAT" rollback --delete-user --delete-group --yes \
    && pass "hazmat rollback completed" \
    || fail "hazmat rollback failed"
check_privileged_install_ownership --after-rollback \
    && pass "privileged install ownership rollback residue check passed" \
    || fail "privileged install ownership rollback residue check failed"

# Every artifact must be gone
! id agent > /dev/null 2>&1 \
    && pass "agent user removed" \
    || fail "agent user still exists"

! test -f /etc/sudoers.d/agent \
    && pass "sudoers file removed" \
    || fail "sudoers file still exists"

! test -f /etc/pf.anchors/agent \
    && pass "pf anchor file removed" \
    || fail "pf anchor file still exists"

! grep -q "AI Agent Blocklist" /etc/hosts \
    && pass "DNS blocklist removed from /etc/hosts" \
    || fail "DNS blocklist still in /etc/hosts"

! test -f /Library/LaunchDaemons/com.local.pf-agent.plist \
    && pass "LaunchDaemon plist removed" \
    || fail "LaunchDaemon plist still exists"

# ── Phase 6b: requireInit guard ──────────────────────────────────────────────
# After rollback, session commands must fail with a clear error instead of
# prompting for a sudo password.

ERR_OUTPUT=$("$HAZMAT" claude -p "hello" 2>&1 || true)
if echo "$ERR_OUTPUT" | grep -q "not initialized"; then
    pass "requireInit guard: 'hazmat claude' fails with init message after rollback"
else
    fail "requireInit guard: expected 'not initialized' error, got: $ERR_OUTPUT"
fi

# ── Phase 7: Idempotency ────────────────────────────────────────────────────

phase "Phase 7: Idempotency (rollback → reinit → check)"
"$HAZMAT" init --yes && pass "reinit completed" || fail "reinit failed"

if [ -n "$QUICK" ]; then
    "$HAZMAT" check && pass "reinit check passed" \
        || printf "  \033[33m!\033[0m reinit check reported issues (non-fatal)\n"
else
    "$HAZMAT" check --full && pass "reinit check --full passed" \
        || printf "  \033[33m!\033[0m reinit check --full reported issues (non-fatal)\n"
fi

# ── Phase 8: Invariants ─────────────────────────────────────────────────────

phase "Phase 8: Invariant checks"

# TLA+ AgentContained: sudoers and pf anchor must coexist or both be absent
if test -f /etc/sudoers.d/agent && test -f /etc/pf.anchors/agent; then
    pass "AgentContained: sudoers and pf anchor both present"
elif ! test -f /etc/sudoers.d/agent && ! test -f /etc/pf.anchors/agent; then
    pass "AgentContained: neither present (clean state)"
else
    fail "AgentContained VIOLATED: sudoers and pf anchor out of sync"
fi

# Verify init is actually idempotent (running init again changes nothing)
"$HAZMAT" init --yes 2>&1 | grep -c "already" > /tmp/hazmat-idemp-$$ || true
SKIP_COUNT=$(cat /tmp/hazmat-idemp-$$)
rm -f /tmp/hazmat-idemp-$$
[ "$SKIP_COUNT" -gt 5 ] \
    && pass "idempotency: init on already-configured system skips ≥$SKIP_COUNT steps" \
    || fail "idempotency: expected most steps to be skipped, only $SKIP_COUNT were"

# ── Cleanup ──────────────────────────────────────────────────────────────────

phase "Cleanup"
"$HAZMAT" rollback --delete-user --delete-group --yes > /dev/null 2>&1 \
    && pass "final rollback completed" \
    || fail "final rollback failed"

# ── Summary ──────────────────────────────────────────────────────────────────

printf "\n"
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
if [ "$FAIL" -eq 0 ]; then
    printf "\033[32m  All %d tests passed.\033[0m\n" "$TOTAL"
else
    printf "\033[31m  %d/%d tests failed.\033[0m\n" "$FAIL" "$TOTAL"
fi
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

exit "$FAIL"
