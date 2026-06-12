#!/bin/bash
# Optional prepared-host harness smoke tests for real Hazmat launch plumbing.
#
# This is intentionally narrower than scripts/e2e.sh: it assumes Hazmat is
# already initialized, swaps in synthetic agent-owned harness binaries, runs the
# normal launch path, and restores every touched file before exit.

set -euo pipefail

usage() {
    cat <<EOF
Usage:
  bash scripts/e2e-harness-smoke-native.sh
  bash scripts/e2e-harness-smoke-native.sh --run --i-understand-this-runs-native-hazmat-smoke [--skip-build]
  bash scripts/e2e-harness-smoke-native.sh --list-harnesses

Runs non-destructive harness smokes for:
  - Claude Code, Codex, OpenCode, Gemini, Hermes, Qwen, and Cursor Agent foreground launch paths
  - provider/env delivery for harnesses that consume provider env grants
  - file-backed auth materialization, harvest, and cleanup where applicable
  - Claude auth materialization/harvest preserving host-owned auth when the
    runtime credential file is rewritten to a logged-out shape

Prepared-host prerequisites:
  - macOS host with hazmat already initialized
  - non-interactive sudo for the current user (sudo -n)

Default mode is disclosure-only. Live mode is sudo-adjacent because it mutates
and restores /Users/agent harness state and invokes native Hazmat launch paths.
Agents must ask for explicit approval before running --run.

For the release-blocking hermetic smoke that does not require those
prerequisites, use:

  bash scripts/e2e-harness-smoke.sh
EOF
}

MODE="disclosure"
ACK_RUN=""
SKIP_BUILD=""
LIST_HARNESSES=""
SMOKE_HARNESSES="claude codex opencode gemini hermes qwen cursor-agent"
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
        --run)
            MODE="run"
            ;;
        --i-understand-this-runs-native-hazmat-smoke)
            ACK_RUN="1"
            ;;
        --skip-build)
            SKIP_BUILD="1"
            ;;
        --list-harnesses)
            LIST_HARNESSES="1"
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

if [ -n "$LIST_HARNESSES" ]; then
    for harness in $SMOKE_HARNESSES; do
        printf '%s\n' "$harness"
    done
    exit 0
fi

if [ "$MODE" != "run" ]; then
    cat <<EOF
native-harness-smoke: disclosure-only

This script validates prepared-host native Hazmat launch plumbing by temporarily
backing up and replacing agent-owned harness binaries/state, seeding host
secret-store fixtures, running native hazmat harness commands, and restoring the
touched paths. It requires hazmat init and non-interactive sudo.

To run the live smoke, ask for explicit approval for this exact command:

  bash scripts/e2e-harness-smoke-native.sh --run --i-understand-this-runs-native-hazmat-smoke
EOF
    exit 0
fi

if [ -z "$ACK_RUN" ]; then
    echo "native-harness-smoke: refusing live run without --i-understand-this-runs-native-hazmat-smoke" >&2
    exit 2
fi

TMPDIR_SMOKE="$(mktemp -d /tmp/hazmat-harness-smoke.XXXXXX)"
BACKUP_MANIFEST="$TMPDIR_SMOKE/backups.manifest"
ABSENT_AGENT_DIR_MANIFEST="$TMPDIR_SMOKE/absent-agent-dirs.manifest"

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

record_absent_agent_dir() {
    local path="$1"
    if ! sudo -n /usr/bin/test -e "$path"; then
        printf '%s\n' "$path" >> "$ABSENT_AGENT_DIR_MANIFEST"
    fi
}

restore_absent_agent_dirs() {
    if [ ! -f "$ABSENT_AGENT_DIR_MANIFEST" ]; then
        return
    fi

    while IFS= read -r path; do
        [ -n "$path" ] || continue
        sudo -n /bin/rm -rf "$path" 2>/dev/null || true
    done < "$ABSENT_AGENT_DIR_MANIFEST"
}

cleanup() {
    local status=$?
    restore_backups
    restore_absent_agent_dirs
    if [ -n "$PROJECT" ]; then
        sudo -n /bin/rm -rf "$PROJECT" 2>/dev/null || true
    fi
    sudo -n /bin/rm -rf "$TMPDIR_SMOKE" 2>/dev/null || /bin/rm -rf "$TMPDIR_SMOKE"
    exit "$status"
}
trap cleanup EXIT INT TERM HUP

install_agent_executable() {
    local src="$1"
    local dest="$2"
    sudo -n -u agent /bin/mkdir -p "$(/usr/bin/dirname "$dest")"
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
    if /usr/bin/grep -Fq "$needle" "$path"; then
        pass "$label"
    else
        die "$label: missing $needle in $path"
    fi
}

assert_agent_file_absent() {
    local path="$1"
    local label="$2"
    if sudo -n /usr/bin/test -e "$path"; then
        die "$label: residue remained at $path"
    fi
    pass "$label"
}

assert_agent_file_contains() {
    local path="$1"
    local needle="$2"
    local label="$3"
    if sudo -n -u agent /usr/bin/grep -Fq "$needle" "$path"; then
        pass "$label"
    else
        die "$label: missing $needle in $path"
    fi
}

remove_agent_paths() {
    for path in "$@"; do
        sudo -n /bin/rm -rf "$path"
    done
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

acquire_hazmat_test_suite_lock "scripts/e2e-harness-smoke-native.sh"

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
backup_path "$AGENT_HOME/.local/bin/codex" "agent-codex-bin"
backup_path "$AGENT_HOME/.codex/auth.json" "agent-codex-auth"
backup_path "$AGENT_HOME/.opencode/bin/opencode" "agent-opencode-current-bin"
backup_path "$AGENT_HOME/.local/bin/opencode" "agent-opencode-legacy-bin"
backup_path "$AGENT_HOME/.local/share/opencode/auth.json" "agent-opencode-auth"
backup_path "$AGENT_HOME/.local/bin/gemini" "agent-gemini-bin"
backup_path "$AGENT_HOME/.gemini/oauth_creds.json" "agent-gemini-oauth"
backup_path "$AGENT_HOME/.gemini/google_accounts.json" "agent-gemini-accounts"
backup_path "$AGENT_HOME/.local/bin/qwen" "agent-qwen-bin"
backup_path "$AGENT_HOME/.qwen" "agent-qwen-state"
backup_path "$AGENT_HOME/.local/bin/cursor-agent" "agent-cursor-agent-bin"
backup_path "$AGENT_HOME/.cursor" "agent-cursor-state"
backup_path "$HOME/.hazmat/secrets/claude/credentials.json" "host-claude-credentials"
backup_path "$HOME/.hazmat/secrets/claude/state.json" "host-claude-state"
backup_path "$HOME/.hazmat/secrets/codex/auth.json" "host-codex-auth"
backup_path "$HOME/.hazmat/secrets/opencode/auth.json" "host-opencode-auth"
backup_path "$HOME/.hazmat/secrets/gemini/oauth_creds.json" "host-gemini-oauth"
backup_path "$HOME/.hazmat/secrets/gemini/google_accounts.json" "host-gemini-accounts"
backup_path "$HOME/.hazmat/secrets/providers/anthropic-api-key" "host-provider-anthropic"
backup_path "$HOME/.hazmat/secrets/providers/openai-api-key" "host-provider-openai"
backup_path "$HOME/.hazmat/secrets/providers/gemini-api-key" "host-provider-gemini"
backup_path "$HOME/.hazmat/secrets/providers/openrouter-api-key" "host-provider-openrouter"

record_absent_agent_dir "$AGENT_HOME/.hazmat/hermes"
record_absent_agent_dir "$AGENT_HOME/.hazmat"
record_absent_agent_dir "$AGENT_HOME/.local/bin"
record_absent_agent_dir "$AGENT_HOME/.claude"
record_absent_agent_dir "$AGENT_HOME/.codex"
record_absent_agent_dir "$AGENT_HOME/.opencode/bin"
record_absent_agent_dir "$AGENT_HOME/.opencode"
record_absent_agent_dir "$AGENT_HOME/.local/share/opencode"
record_absent_agent_dir "$AGENT_HOME/.gemini"
record_absent_agent_dir "$AGENT_HOME/.qwen"
record_absent_agent_dir "$AGENT_HOME/.cursor"

PROJECT="$(mktemp -d /tmp/hazmat-harness-project.XXXXXX)"

write_host_secret "$HOME/.hazmat/secrets/providers/anthropic-api-key" "stored-anthropic-provider"
write_host_secret "$HOME/.hazmat/secrets/providers/openai-api-key" "stored-openai-provider"
write_host_secret "$HOME/.hazmat/secrets/providers/gemini-api-key" "stored-gemini-provider"
write_host_secret "$HOME/.hazmat/secrets/providers/openrouter-api-key" "stored-openrouter-provider"

phase "Hermes foreground launch"
FAKE_HERMES="$TMPDIR_SMOKE/hermes"
cat > "$FAKE_HERMES" <<'EOF'
#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 40; }
case "${HERMES_HOME:-}" in
  "$HOME/.hazmat/hermes/projects/"*) ;;
  *)
	  echo "unexpected HERMES_HOME=${HERMES_HOME:-}" >&2
	  exit 41
	  ;;
esac
test "${ANTHROPIC_API_KEY:-}" = "stored-anthropic-provider" || { echo "missing ANTHROPIC_API_KEY" >&2; exit 42; }
test "${OPENAI_API_KEY:-}" = "stored-openai-provider" || { echo "missing OPENAI_API_KEY" >&2; exit 43; }
test "${GEMINI_API_KEY:-}" = "stored-gemini-provider" || { echo "missing GEMINI_API_KEY" >&2; exit 44; }
test "${OPENROUTER_API_KEY:-}" = "stored-openrouter-provider" || { echo "missing OPENROUTER_API_KEY" >&2; exit 45; }
if [ "${1:-}" = "--version" ]; then
  mkdir -p "$HERMES_HOME"
  echo "hermes fake smoke"
  exit 0
fi
echo "unexpected Hermes args: $*" >&2
exit 46
EOF
install_agent_executable "$FAKE_HERMES" "$AGENT_HOME/.local/bin/hermes"

HERMES_OUT="$TMPDIR_SMOKE/hermes.out"
"$HAZMAT" hermes --no-backup --skip-harness-assets-sync -C "$PROJECT" -- --version > "$HERMES_OUT" 2>&1
assert_file_contains "$HERMES_OUT" "hermes fake smoke" "Hermes fake CLI ran through hazmat hermes"
sudo -n -u agent /usr/bin/test -d "$AGENT_HOME/.hazmat/hermes/projects" \
    && pass "Hermes managed project state root exists" \
    || die "Hermes managed state root missing"

phase "Claude auth harvest guard"
write_host_secret "$HOME/.hazmat/secrets/claude/credentials.json" \
    '{"sessionKey":"stored-token","refreshToken":"stored-refresh"}'
write_host_secret "$HOME/.hazmat/secrets/claude/state.json" \
    '{"oauthAccount":{"emailAddress":"smoke@example.com"},"userID":"u-smoke","hasAvailableSubscription":true}'
remove_agent_paths "$AGENT_HOME/.claude/.credentials.json" "$AGENT_HOME/.claude.json"

FAKE_CLAUDE="$TMPDIR_SMOKE/claude"
cat > "$FAKE_CLAUDE" <<'EOF'
#!/bin/sh
set -eu
cred="$HOME/.claude/.credentials.json"
state="$HOME/.claude.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 50; }
test "${ANTHROPIC_API_KEY:-}" = "stored-anthropic-provider" || { echo "missing ANTHROPIC_API_KEY" >&2; exit 51; }
test -f "$cred" || { echo "missing materialized Claude credentials" >&2; exit 52; }
test -f "$state" || { echo "missing materialized Claude state" >&2; exit 53; }
grep -Fq "stored-token" "$cred" || { echo "missing stored token" >&2; exit 54; }
grep -Fq "oauthAccount" "$state" || { echo "missing stored state" >&2; exit 55; }
printf '{}\n' > "$cred"
printf '{"projects":{"hazmat-smoke":true}}\n' > "$state"
echo "FAKE_CLAUDE_OK"
EOF
install_agent_executable "$FAKE_CLAUDE" "$AGENT_HOME/.local/bin/claude"

CLAUDE_OUT="$TMPDIR_SMOKE/claude.out"
"$HAZMAT" claude --no-backup --skip-harness-assets-sync -C "$PROJECT" -p "auth smoke" > "$CLAUDE_OUT" 2>&1
assert_file_contains "$CLAUDE_OUT" "FAKE_CLAUDE_OK" "Claude fake CLI ran through hazmat claude"
assert_file_contains "$HOME/.hazmat/secrets/claude/credentials.json" "stored-token" "Host-owned Claude credentials survived logged-out rewrite"
assert_file_contains "$HOME/.hazmat/secrets/claude/state.json" "oauthAccount" "Host-owned Claude state survived logged-out rewrite"
assert_agent_file_absent "$AGENT_HOME/.claude/.credentials.json" "Claude credential residue removed from agent home"

phase "Codex auth harvest"
write_host_secret "$HOME/.hazmat/secrets/codex/auth.json" \
    '{"tokens":{"access":"stored-codex-access"},"refresh":"stored-codex-refresh"}'
remove_agent_paths "$AGENT_HOME/.codex/auth.json"

FAKE_CODEX="$TMPDIR_SMOKE/codex"
cat > "$FAKE_CODEX" <<'EOF'
#!/bin/sh
set -eu
auth="$HOME/.codex/auth.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 60; }
test "${OPENAI_API_KEY:-}" = "stored-openai-provider" || { echo "missing OPENAI_API_KEY" >&2; exit 61; }
test -f "$auth" || { echo "missing materialized Codex auth" >&2; exit 62; }
grep -Fq "stored-codex-access" "$auth" || { echo "missing stored Codex auth" >&2; exit 63; }
printf '{"tokens":{"access":"updated-codex-access"}}\n' > "$auth"
case " $* " in
  *" exec "*"codex smoke"*) ;;
  *) echo "unexpected Codex args: $*" >&2; exit 64 ;;
esac
echo "FAKE_CODEX_OK"
EOF
install_agent_executable "$FAKE_CODEX" "$AGENT_HOME/.local/bin/codex"

CODEX_OUT="$TMPDIR_SMOKE/codex.out"
"$HAZMAT" codex --no-backup --skip-harness-assets-sync -C "$PROJECT" exec "codex smoke" > "$CODEX_OUT" 2>&1
assert_file_contains "$CODEX_OUT" "FAKE_CODEX_OK" "Codex fake CLI ran through hazmat codex"
assert_file_contains "$HOME/.hazmat/secrets/codex/auth.json" "updated-codex-access" "Host-owned Codex auth harvested updated runtime auth"
assert_agent_file_absent "$AGENT_HOME/.codex/auth.json" "Codex auth residue removed from agent home"

phase "OpenCode auth harvest"
write_host_secret "$HOME/.hazmat/secrets/opencode/auth.json" \
    '{"providers":{"anthropic":{"token":"stored-opencode-token"}}}'
remove_agent_paths "$AGENT_HOME/.local/share/opencode/auth.json"

FAKE_OPENCODE="$TMPDIR_SMOKE/opencode"
cat > "$FAKE_OPENCODE" <<'EOF'
#!/bin/sh
set -eu
auth="$HOME/.local/share/opencode/auth.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 70; }
test -f "$auth" || { echo "missing materialized OpenCode auth" >&2; exit 71; }
grep -Fq "stored-opencode-token" "$auth" || { echo "missing stored OpenCode auth" >&2; exit 72; }
printf '{"providers":{"anthropic":{"token":"updated-opencode-token"}}}\n' > "$auth"
case " $* " in
  *" run "*"opencode smoke"*) ;;
  *) echo "unexpected OpenCode args: $*" >&2; exit 73 ;;
esac
echo "FAKE_OPENCODE_OK"
EOF
install_agent_executable "$FAKE_OPENCODE" "$AGENT_HOME/.opencode/bin/opencode"

OPENCODE_OUT="$TMPDIR_SMOKE/opencode.out"
"$HAZMAT" opencode --no-backup --skip-harness-assets-sync -C "$PROJECT" run "opencode smoke" > "$OPENCODE_OUT" 2>&1
assert_file_contains "$OPENCODE_OUT" "FAKE_OPENCODE_OK" "OpenCode fake CLI ran through hazmat opencode"
assert_file_contains "$HOME/.hazmat/secrets/opencode/auth.json" "updated-opencode-token" "Host-owned OpenCode auth harvested updated runtime auth"
assert_agent_file_absent "$AGENT_HOME/.local/share/opencode/auth.json" "OpenCode auth residue removed from agent home"

phase "Gemini auth harvest"
write_host_secret "$HOME/.hazmat/secrets/gemini/oauth_creds.json" \
    '{"access_token":"stored-gemini-access","refresh_token":"stored-gemini-refresh"}'
write_host_secret "$HOME/.hazmat/secrets/gemini/google_accounts.json" \
    '{"active":"stored-gemini-account"}'
remove_agent_paths "$AGENT_HOME/.gemini/oauth_creds.json" "$AGENT_HOME/.gemini/google_accounts.json"

FAKE_GEMINI="$TMPDIR_SMOKE/gemini"
cat > "$FAKE_GEMINI" <<'EOF'
#!/bin/sh
set -eu
oauth="$HOME/.gemini/oauth_creds.json"
accounts="$HOME/.gemini/google_accounts.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 80; }
test "${GEMINI_API_KEY:-}" = "stored-gemini-provider" || { echo "missing GEMINI_API_KEY" >&2; exit 81; }
test -f "$oauth" || { echo "missing materialized Gemini OAuth" >&2; exit 82; }
test -f "$accounts" || { echo "missing materialized Gemini accounts" >&2; exit 83; }
grep -Fq "stored-gemini-access" "$oauth" || { echo "missing stored Gemini OAuth" >&2; exit 84; }
grep -Fq "stored-gemini-account" "$accounts" || { echo "missing stored Gemini accounts" >&2; exit 85; }
printf '{"access_token":"updated-gemini-access"}\n' > "$oauth"
printf '{"active":"updated-gemini-account"}\n' > "$accounts"
case " $* " in
  *" -p "*"gemini smoke"*) ;;
  *) echo "unexpected Gemini args: $*" >&2; exit 86 ;;
esac
echo "FAKE_GEMINI_OK"
EOF
install_agent_executable "$FAKE_GEMINI" "$AGENT_HOME/.local/bin/gemini"

GEMINI_OUT="$TMPDIR_SMOKE/gemini.out"
"$HAZMAT" gemini --no-backup --skip-harness-assets-sync -C "$PROJECT" -p "gemini smoke" > "$GEMINI_OUT" 2>&1
assert_file_contains "$GEMINI_OUT" "FAKE_GEMINI_OK" "Gemini fake CLI ran through hazmat gemini"
assert_file_contains "$HOME/.hazmat/secrets/gemini/oauth_creds.json" "updated-gemini-access" "Host-owned Gemini OAuth harvested updated runtime auth"
assert_file_contains "$HOME/.hazmat/secrets/gemini/google_accounts.json" "updated-gemini-account" "Host-owned Gemini accounts harvested updated runtime auth"
assert_agent_file_absent "$AGENT_HOME/.gemini/oauth_creds.json" "Gemini OAuth residue removed from agent home"
assert_agent_file_absent "$AGENT_HOME/.gemini/google_accounts.json" "Gemini account residue removed from agent home"

phase "Qwen foreground launch"
FAKE_QWEN="$TMPDIR_SMOKE/qwen"
cat > "$FAKE_QWEN" <<'EOF'
#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 90; }
if [ "${1:-}" != "--yolo" ]; then
  echo "expected --yolo as first Qwen arg, got: $*" >&2
  exit 91
fi
mkdir -p "$HOME/.qwen"
case " $* " in
  *" -p "*"qwen smoke"*) ;;
  *) echo "unexpected Qwen args: $*" >&2; exit 92 ;;
esac
echo "FAKE_QWEN_OK"
EOF
install_agent_executable "$FAKE_QWEN" "$AGENT_HOME/.local/bin/qwen"

QWEN_OUT="$TMPDIR_SMOKE/qwen.out"
"$HAZMAT" qwen --no-backup --skip-harness-assets-sync -C "$PROJECT" --yolo -p "qwen smoke" > "$QWEN_OUT" 2>&1
assert_file_contains "$QWEN_OUT" "FAKE_QWEN_OK" "Qwen fake CLI ran through hazmat qwen"
sudo -n -u agent /usr/bin/test -d "$AGENT_HOME/.qwen" \
    && pass "Qwen contained state directory exists" \
    || die "Qwen contained state directory missing"

phase "Cursor Agent foreground launch"
FAKE_CURSOR_AGENT="$TMPDIR_SMOKE/cursor-agent"
cat > "$FAKE_CURSOR_AGENT" <<'EOF'
#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 100; }
test "$#" -eq 8 || { echo "unexpected Cursor Agent arg count: $#" >&2; exit 101; }
test "${1:-}" = "--print" || { echo "missing --print" >&2; exit 102; }
test "${2:-}" = "--output-format" || { echo "missing --output-format" >&2; exit 103; }
test "${3:-}" = "stream-json" || { echo "unexpected output format: ${3:-}" >&2; exit 104; }
test "${4:-}" = "--stream-partial-output" || { echo "missing --stream-partial-output" >&2; exit 105; }
test "${5:-}" = "--force" || { echo "missing --force" >&2; exit 106; }
test "${6:-}" = "--trust" || { echo "missing --trust" >&2; exit 107; }
test "${7:-}" = "--workspace" || { echo "missing --workspace" >&2; exit 108; }
test "${8:-}" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected workspace: ${8:-}" >&2; exit 109; }
mkdir -p "$HOME/.cursor"
echo "FAKE_CURSOR_AGENT_OK"
EOF
install_agent_executable "$FAKE_CURSOR_AGENT" "$AGENT_HOME/.local/bin/cursor-agent"

CURSOR_AGENT_OUT="$TMPDIR_SMOKE/cursor-agent.out"
"$HAZMAT" cursor-agent --no-backup --skip-harness-assets-sync -C "$PROJECT" -- --print --output-format stream-json --stream-partial-output --force --trust --workspace "$PROJECT" > "$CURSOR_AGENT_OUT" 2>&1
assert_file_contains "$CURSOR_AGENT_OUT" "FAKE_CURSOR_AGENT_OK" "Cursor Agent fake CLI ran through hazmat cursor-agent"
sudo -n -u agent /usr/bin/test -d "$AGENT_HOME/.cursor" \
    && pass "Cursor Agent contained state directory exists" \
    || die "Cursor Agent contained state directory missing"

printf "\n\033[32m  All %d harness smoke checks passed.\033[0m\n" "$PASS"
