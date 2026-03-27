#!/bin/bash
# E2E lifecycle test for Hazmat.
#
# Runs the full init → check → session → rollback → reinit cycle.
# Works anywhere: local Mac, Lume VM guest, GHA macOS runner, Cirrus CI.
#
# Usage:
#   bash scripts/e2e.sh              # from repo root
#   bash scripts/e2e.sh --quick      # skip live network probes
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

# ── Build ────────────────────────────────────────────────────────────────────

phase "Build"
cd "$REPO_ROOT/hazmat"
make clean && make all
pass "hazmat + hazmat-launch built"

# ── Unit tests ───────────────────────────────────────────────────────────────

phase "Unit tests"
go test ./... && pass "go test ./... passed" || fail "go test ./... failed"

# ── Phase 1: Fresh install ───────────────────────────────────────────────────

phase "Phase 1: Fresh install"
sudo "$HAZMAT" init --yes && pass "hazmat init completed" || fail "hazmat init failed"

if [ "$QUICK" = "--quick" ]; then
    "$HAZMAT" init check && pass "hazmat init check passed" || fail "hazmat init check failed"
else
    "$HAZMAT" init check --full && pass "hazmat init check --full passed" || fail "hazmat init check --full failed"
fi

# ── Phase 2: Session + snapshot ──────────────────────────────────────────────

phase "Phase 2: Session + snapshot"

PROJECT=$(mktemp -d)
echo "e2e test content" > "$PROJECT/file.txt"
mkdir -p "$PROJECT/subdir"
echo "nested" > "$PROJECT/subdir/nested.txt"

# exec should work in containment
"$HAZMAT" exec -C "$PROJECT" ls > /dev/null \
    && pass "hazmat exec runs successfully" \
    || fail "hazmat exec failed"

# Snapshot should have been created
cd "$PROJECT"
"$HAZMAT" snapshots 2>&1 | grep -q "pre-session" \
    && pass "pre-session snapshot created automatically" \
    || fail "no pre-session snapshot found"

# --no-backup should skip snapshot
SNAP_BEFORE=$("$HAZMAT" snapshots 2>&1 | grep -c "pre-session" || true)
"$HAZMAT" exec --no-backup -C "$PROJECT" true > /dev/null
SNAP_AFTER=$("$HAZMAT" snapshots 2>&1 | grep -c "pre-session" || true)
[ "$SNAP_BEFORE" = "$SNAP_AFTER" ] \
    && pass "--no-backup skipped snapshot" \
    || fail "--no-backup did not skip snapshot"

# Credential isolation: agent should NOT be able to read host SSH keys
if [ -d "$HOME/.ssh" ]; then
    "$HAZMAT" exec -C "$PROJECT" ls "$HOME/.ssh" > /dev/null 2>&1 \
        && fail "ISOLATION BREACH: agent read host .ssh" \
        || pass "agent cannot read host .ssh"
else
    printf "  - skipped .ssh test (directory doesn't exist)\n"
fi

# Agent should NOT be able to read host AWS credentials
if [ -d "$HOME/.aws" ]; then
    "$HAZMAT" exec -C "$PROJECT" ls "$HOME/.aws" > /dev/null 2>&1 \
        && fail "ISOLATION BREACH: agent read host .aws" \
        || pass "agent cannot read host .aws"
else
    printf "  - skipped .aws test (directory doesn't exist)\n"
fi

cd "$REPO_ROOT"
rm -rf "$PROJECT"

# ── Phase 3: Rollback ───────────────────────────────────────────────────────

phase "Phase 3: Rollback"
sudo "$HAZMAT" init rollback --delete-user --delete-group --yes \
    && pass "hazmat init rollback completed" \
    || fail "hazmat init rollback failed"

# Verify clean state
! id agent > /dev/null 2>&1 \
    && pass "agent user removed" \
    || fail "agent user still exists after rollback"

! test -f /etc/sudoers.d/agent \
    && pass "sudoers file removed" \
    || fail "sudoers file still exists after rollback"

! test -f /etc/pf.anchors/agent \
    && pass "pf anchor file removed" \
    || fail "pf anchor file still exists after rollback"

! grep -q "AI Agent Blocklist" /etc/hosts \
    && pass "DNS blocklist removed from /etc/hosts" \
    || fail "DNS blocklist still in /etc/hosts after rollback"

# ── Phase 4: Idempotency ────────────────────────────────────────────────────

phase "Phase 4: Idempotency (reinit)"
sudo "$HAZMAT" init --yes && pass "second hazmat init completed" || fail "second hazmat init failed"

if [ "$QUICK" = "--quick" ]; then
    "$HAZMAT" init check && pass "second hazmat init check passed" || fail "second hazmat init check failed"
else
    "$HAZMAT" init check --full && pass "second hazmat init check --full passed" || fail "second hazmat init check --full failed"
fi

# ── Phase 5: TLA+ invariant (AgentContained) ────────────────────────────────

phase "Phase 5: Invariant checks"

# Sudoers and pf anchor must coexist
if test -f /etc/sudoers.d/agent && test -f /etc/pf.anchors/agent; then
    pass "AgentContained: sudoers and pf anchor both present"
elif ! test -f /etc/sudoers.d/agent && ! test -f /etc/pf.anchors/agent; then
    pass "AgentContained: neither sudoers nor pf anchor present"
else
    fail "AgentContained VIOLATED: sudoers and pf anchor out of sync"
fi

# ── Final cleanup ────────────────────────────────────────────────────────────

phase "Cleanup"
sudo "$HAZMAT" init rollback --delete-user --delete-group --yes > /dev/null 2>&1 \
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
