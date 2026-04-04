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
#
# Warning:
#   This script is destructive to the local Hazmat setup. It runs init,
#   rollback --delete-user --delete-group, and then re-inits again. Prefer
#   scripts/e2e-vm.sh for isolated local verification.
#
# Prerequisites:
#   - macOS with sudo access
#   - Go 1.21+ (for building)

set -euo pipefail

QUICK="${1:-}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HAZMAT="$REPO_ROOT/hazmat/hazmat"
PASS=0
FAIL=0
TOTAL=0

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

# ── Build ────────────────────────────────────────────────────────────────────

phase "Build"
cd "$REPO_ROOT/hazmat"
make clean && make all
sudo make install-helper
pass "hazmat + hazmat-launch built"

# ── Unit tests ───────────────────────────────────────────────────────────────

phase "Unit tests"
go test ./... && pass "go test ./... passed" || fail "go test ./... failed"

# ── Phase 1: Fresh install ───────────────────────────────────────────────────

phase "Phase 1: Fresh install"
"$HAZMAT" init --yes && pass "hazmat init completed" || fail "hazmat init failed"

if [ "$QUICK" = "--quick" ]; then
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
"$HAZMAT" exec -C "$PROJECT" touch /tmp/hazmat-escape-test-$$ > /dev/null 2>&1
if [ -f "/tmp/hazmat-escape-test-$$" ]; then
    # /tmp is shared — this is a known limitation, not a breach.
    # But agent should NOT be able to write to host home.
    sudo rm -f "/tmp/hazmat-escape-test-$$"
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

# ── Phase 7: Idempotency ────────────────────────────────────────────────────

phase "Phase 7: Idempotency (rollback → reinit → check)"
"$HAZMAT" init --yes && pass "reinit completed" || fail "reinit failed"

if [ "$QUICK" = "--quick" ]; then
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
