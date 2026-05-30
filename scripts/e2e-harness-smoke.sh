#!/bin/bash
# Host-side harness smoke tests for real Hazmat launch plumbing.
#
# This is intentionally narrower than scripts/e2e.sh: it assumes Hazmat is
# already initialized, swaps in synthetic agent-owned harness binaries, runs the
# normal launch path, and restores every touched file before exit.

set -euo pipefail

usage() {
    cat <<EOF
Usage:
  bash scripts/e2e-harness-smoke.sh [--skip-build]

Runs non-destructive harness smokes for:
  - Hermes foreground launch with managed HERMES_HOME
  - Claude auth materialization/harvest preserving host-owned auth when the
    runtime credential file is rewritten to a logged-out shape

Prerequisites:
  - macOS host with hazmat already initialized
  - non-interactive sudo for the current user (sudo -n)
EOF
}

SKIP_BUILD=""
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HAZMAT="$REPO_ROOT/hazmat/hazmat"
AGENT_HOME="/Users/agent"
PASS=0
PROJECT=""

# shellcheck source=scripts/lib/test_lock.sh
. "$REPO_ROOT/scripts/lib/test_lock.sh"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --skip-build)
            SKIP_BUILD="1"
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

TMPDIR_SMOKE="$(mktemp -d /tmp/hazmat-harness-smoke.XXXXXX)"
BACKUP_MANIFEST="$TMPDIR_SMOKE/backups.manifest"

phase() { printf "\n\033[1m── %s ──\033[0m\n\n" "$1"; }
pass() { PASS=$((PASS + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
die() { printf "  \033[31m✗\033[0m %s\n" "$1" >&2; exit 1; }

backup_path() {
    local path="$1"
    local label="$2"
    local backup="$TMPDIR_SMOKE/backup/$label"

    if sudo -n /usr/bin/test -e "$path"; then
        /bin/mkdir -p "$(/usr/bin/dirname "$backup")"
        sudo -n /bin/cp -pR "$path" "$backup"
        printf '%s|%s\n' "$path" "$backup" >> "$BACKUP_MANIFEST"
    else
        printf '%s|\n' "$path" >> "$BACKUP_MANIFEST"
    fi
}

restore_backups() {
    if [ ! -f "$BACKUP_MANIFEST" ]; then
        return
    fi

    while IFS='|' read -r path backup; do
        [ -n "$path" ] || continue
        sudo -n /bin/rm -rf "$path" 2>/dev/null || true
        if [ -n "$backup" ]; then
            sudo -n /bin/mkdir -p "$(/usr/bin/dirname "$path")" 2>/dev/null || true
            sudo -n /bin/cp -pR "$backup" "$path" 2>/dev/null || true
        fi
    done < "$BACKUP_MANIFEST"
}

cleanup() {
    local status=$?
    restore_backups
    if [ -n "$PROJECT" ]; then
        sudo -n /bin/rm -rf "$PROJECT" 2>/dev/null || true
    fi
    /bin/rm -rf "$TMPDIR_SMOKE"
    exit "$status"
}
trap cleanup EXIT INT TERM HUP

install_agent_executable() {
    local src="$1"
    local dest="$2"
    sudo -n /bin/mkdir -p "$(/usr/bin/dirname "$dest")"
    sudo -n /bin/cp -f "$src" "$dest"
    sudo -n /usr/sbin/chown agent:staff "$dest"
    sudo -n /bin/chmod 0755 "$dest"
}

write_host_secret() {
    local path="$1"
    local content="$2"
    /bin/mkdir -p "$(/usr/bin/dirname "$path")"
    /usr/bin/printf '%s\n' "$content" > "$path"
    /bin/chmod 0600 "$path"
}

assert_file_contains() {
    local path="$1"
    local needle="$2"
    local label="$3"
    if /usr/bin/grep -q "$needle" "$path"; then
        pass "$label"
    else
        die "$label: missing $needle in $path"
    fi
}

if [ "$(/usr/bin/uname -s)" != "Darwin" ]; then
    echo "harness-smoke: skipped: native harness launch smoke is macOS-only"
    exit 0
fi

if ! /usr/bin/id agent >/dev/null 2>&1; then
    die "agent user does not exist; run hazmat init first"
fi

if ! sudo -n true >/dev/null 2>&1; then
    die "non-interactive sudo is required; run sudo -v first"
fi

if ! sudo -n -u agent /usr/bin/true >/dev/null 2>&1; then
    die "non-interactive sudo -u agent is required"
fi

acquire_hazmat_test_suite_lock "scripts/e2e-harness-smoke.sh"

phase "Build"
if [ -z "$SKIP_BUILD" ]; then
    (cd "$REPO_ROOT" && make all)
fi
[ -x "$HAZMAT" ] || die "hazmat binary not found at $HAZMAT"
pass "hazmat binary ready"

backup_path "$AGENT_HOME/.local/bin/hermes" "agent-hermes-bin"
backup_path "$AGENT_HOME/.hazmat/hermes" "agent-hermes-state"
backup_path "$AGENT_HOME/.local/bin/claude" "agent-claude-bin"
backup_path "$AGENT_HOME/.claude/.credentials.json" "agent-claude-credentials"
backup_path "$AGENT_HOME/.claude.json" "agent-claude-state"
backup_path "$HOME/.hazmat/secrets/claude/credentials.json" "host-claude-credentials"
backup_path "$HOME/.hazmat/secrets/claude/state.json" "host-claude-state"

PROJECT="$(mktemp -d /tmp/hazmat-harness-project.XXXXXX)"

phase "Hermes foreground launch"
FAKE_HERMES="$TMPDIR_SMOKE/hermes"
cat > "$FAKE_HERMES" <<'EOF'
#!/bin/sh
set -eu
if [ "${HERMES_HOME:-}" != "$HOME/.hazmat/hermes" ]; then
  echo "unexpected HERMES_HOME=${HERMES_HOME:-}" >&2
  exit 41
fi
if [ "${1:-}" = "--version" ]; then
  mkdir -p "$HERMES_HOME"
  echo "hermes fake smoke"
  exit 0
fi
echo "unexpected Hermes args: $*" >&2
exit 42
EOF
install_agent_executable "$FAKE_HERMES" "$AGENT_HOME/.local/bin/hermes"

HERMES_OUT="$TMPDIR_SMOKE/hermes.out"
"$HAZMAT" hermes --no-backup -C "$PROJECT" -- --version > "$HERMES_OUT" 2>&1
assert_file_contains "$HERMES_OUT" "hermes fake smoke" "Hermes fake CLI ran through hazmat hermes"
sudo -n -u agent /usr/bin/test -d "$AGENT_HOME/.hazmat/hermes" \
    && pass "Hermes managed state root exists" \
    || die "Hermes managed state root missing"

phase "Claude auth harvest guard"
write_host_secret "$HOME/.hazmat/secrets/claude/credentials.json" \
    '{"sessionKey":"stored-token","refreshToken":"stored-refresh"}'
write_host_secret "$HOME/.hazmat/secrets/claude/state.json" \
    '{"oauthAccount":{"emailAddress":"smoke@example.com"},"userID":"u-smoke","hasAvailableSubscription":true}'
sudo -n /bin/rm -f "$AGENT_HOME/.claude/.credentials.json" "$AGENT_HOME/.claude.json"

FAKE_CLAUDE="$TMPDIR_SMOKE/claude"
cat > "$FAKE_CLAUDE" <<'EOF'
#!/bin/sh
set -eu
cred="$HOME/.claude/.credentials.json"
state="$HOME/.claude.json"
test -f "$cred" || { echo "missing materialized Claude credentials" >&2; exit 51; }
test -f "$state" || { echo "missing materialized Claude state" >&2; exit 52; }
grep -q "stored-token" "$cred" || { echo "missing stored token" >&2; exit 53; }
grep -q "oauthAccount" "$state" || { echo "missing stored state" >&2; exit 54; }
printf '{}\n' > "$cred"
printf '{"projects":{"hazmat-smoke":true}}\n' > "$state"
echo "FAKE_CLAUDE_OK"
EOF
install_agent_executable "$FAKE_CLAUDE" "$AGENT_HOME/.local/bin/claude"

CLAUDE_OUT="$TMPDIR_SMOKE/claude.out"
"$HAZMAT" claude --no-backup -C "$PROJECT" -p "auth smoke" > "$CLAUDE_OUT" 2>&1
assert_file_contains "$CLAUDE_OUT" "FAKE_CLAUDE_OK" "Claude fake CLI ran through hazmat claude"
assert_file_contains "$HOME/.hazmat/secrets/claude/credentials.json" "stored-token" "Host-owned Claude credentials survived logged-out rewrite"
assert_file_contains "$HOME/.hazmat/secrets/claude/state.json" "oauthAccount" "Host-owned Claude state survived logged-out rewrite"
if sudo -n /usr/bin/test -e "$AGENT_HOME/.claude/.credentials.json"; then
    die "Claude credential residue remained in agent home"
else
    pass "Claude credential residue removed from agent home"
fi

printf "\n\033[32m  All %d harness smoke checks passed.\033[0m\n" "$PASS"
