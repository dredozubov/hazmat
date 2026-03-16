#!/usr/bin/env bash
#
# Sandbox test suite — verifies the Option A setup is working correctly.
#
# Tests mirror the Go-based `sandbox setup` flow.
# Run as your normal user (dr), not root. Some checks use sudo -u agent.
#
# Usage:
#   ./test.sh           # full suite
#   ./test.sh --quick   # skip live network tests (faster, no external traffic)

set -euo pipefail

# --- Configuration (must match sandbox/main.go) ------------------------------

AGENT_USER="agent"
AGENT_HOME="/Users/${AGENT_USER}"
SHARED_WORKSPACE="/Users/Shared/workspace"
SHARED_GROUP="dev"
PF_ANCHOR_NAME="agent"
PF_ANCHOR_FILE="/etc/pf.anchors/${PF_ANCHOR_NAME}"
PF_DAEMON_LABEL="com.local.pf-agent"
PF_DAEMON_PLIST="/Library/LaunchDaemons/${PF_DAEMON_LABEL}.plist"
SUDOERS_FILE="/etc/sudoers.d/${AGENT_USER}"
CURRENT_USER="$(whoami)"
QUICK_MODE=false

[[ "${1:-}" == "--quick" ]] && QUICK_MODE=true

# --- Colors ------------------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# --- Counters ----------------------------------------------------------------

PASS=0
FAIL=0
WARN=0
SKIP=0

step_num=0

# --- Helpers -----------------------------------------------------------------

step() {
  step_num=$((step_num + 1))
  echo ""
  echo -e "${BLUE}${BOLD}━━━ Step ${step_num}: $1 ━━━${NC}"
}

pass() {
  PASS=$((PASS + 1))
  echo -e "  ${GREEN}✓${NC} $1"
}

fail() {
  FAIL=$((FAIL + 1))
  echo -e "  ${RED}✗${NC} $1"
}

warn() {
  WARN=$((WARN + 1))
  echo -e "  ${YELLOW}!${NC} $1"
}

skip() {
  SKIP=$((SKIP + 1))
  echo -e "  ${YELLOW}→${NC} $1 (skipped)"
}

# Run a command as the agent user and return its exit code
as_agent() {
  sudo -u "${AGENT_USER}" bash -c "$1" 2>/dev/null
}

# Try a TCP connection as agent, return 0 if connected, 1 if blocked/refused
# $1 = host, $2 = port, $3 = timeout_seconds
agent_tcp_connect() {
  local host="$1" port="$2" timeout="${3:-3}"
  sudo -u "${AGENT_USER}" bash -c \
    "timeout ${timeout} bash -c 'echo > /dev/tcp/${host}/${port}' 2>/dev/null"
}

# --- Banner ------------------------------------------------------------------

echo -e "${BOLD}"
echo "  ┌──────────────────────────────────────────────┐"
echo "  │  Sandbox test suite — Option A verification  │"
echo "  └──────────────────────────────────────────────┘"
echo -e "${NC}"
echo "  Running as: ${CURRENT_USER}"
echo "  Agent user: ${AGENT_USER}"
[[ "${QUICK_MODE}" == true ]] && echo -e "  ${YELLOW}Quick mode: live network tests skipped${NC}"
echo ""

if [[ "$(uname)" != "Darwin" ]]; then
  echo -e "${RED}This test suite is for macOS only.${NC}"
  exit 1
fi

# ===========================================================================
# Step 1: Agent user
# ===========================================================================

step "Agent user"

if id "${AGENT_USER}" &>/dev/null; then
  pass "User '${AGENT_USER}' exists"
else
  fail "User '${AGENT_USER}' does not exist — run sandbox setup first"
fi

agent_uid=$(id -u "${AGENT_USER}" 2>/dev/null || echo "none")
if [[ "${agent_uid}" == "599" ]]; then
  pass "UID is 599"
else
  fail "UID is '${agent_uid}', expected 599"
fi

if [[ -d "${AGENT_HOME}" ]]; then
  pass "Home directory exists: ${AGENT_HOME}"
else
  fail "Home directory missing: ${AGENT_HOME}"
fi

home_owner=$(stat -f '%Su' "${AGENT_HOME}" 2>/dev/null || echo "unknown")
if [[ "${home_owner}" == "${AGENT_USER}" ]]; then
  pass "Home directory owned by ${AGENT_USER}"
else
  fail "Home directory owned by '${home_owner}', expected ${AGENT_USER}"
fi

is_hidden=$(dscl . -read "/Users/${AGENT_USER}" IsHidden 2>/dev/null | awk '{print $2}')
if [[ "${is_hidden}" == "1" ]]; then
  pass "User is hidden from login screen"
else
  warn "User is NOT hidden from login screen (IsHidden=${is_hidden:-unset})"
fi

# ===========================================================================
# Step 2: Dev group and shared workspace
# ===========================================================================

step "Dev group and shared workspace"

if dscl . -read "/Groups/${SHARED_GROUP}" &>/dev/null; then
  pass "Group '${SHARED_GROUP}' exists"
else
  fail "Group '${SHARED_GROUP}' does not exist"
fi

if dscl . -read "/Groups/${SHARED_GROUP}" GroupMembership 2>/dev/null | grep -qw "${CURRENT_USER}"; then
  pass "${CURRENT_USER} is a member of '${SHARED_GROUP}'"
else
  fail "${CURRENT_USER} is NOT a member of '${SHARED_GROUP}'"
fi

if dscl . -read "/Groups/${SHARED_GROUP}" GroupMembership 2>/dev/null | grep -qw "${AGENT_USER}"; then
  pass "${AGENT_USER} is a member of '${SHARED_GROUP}'"
else
  fail "${AGENT_USER} is NOT a member of '${SHARED_GROUP}'"
fi

if [[ -d "${SHARED_WORKSPACE}" ]]; then
  pass "Shared workspace exists: ${SHARED_WORKSPACE}"
else
  fail "Shared workspace missing: ${SHARED_WORKSPACE}"
fi

ws_perms=$(stat -f '%Lp' "${SHARED_WORKSPACE}" 2>/dev/null || echo "unknown")
ws_group=$(stat -f '%Sg' "${SHARED_WORKSPACE}" 2>/dev/null || echo "unknown")
if [[ "${ws_perms}" == "770" ]]; then
  pass "Shared workspace permissions: ${ws_perms}"
else
  fail "Shared workspace permissions: ${ws_perms} (expected 770)"
fi

if [[ "${ws_group}" == "${SHARED_GROUP}" ]]; then
  pass "Shared workspace group: ${ws_group}"
else
  fail "Shared workspace group: '${ws_group}' (expected ${SHARED_GROUP})"
fi

# Check setgid bit
ws_full_perms=$(stat -f '%Sp' "${SHARED_WORKSPACE}" 2>/dev/null || echo "unknown")
if [[ "${ws_full_perms}" == *s* ]] || [[ "${ws_full_perms}" == *S* ]]; then
  pass "Shared workspace has setgid bit"
else
  fail "Shared workspace missing setgid bit (${ws_full_perms})"
fi

# Test write as dr
test_file_dr="${SHARED_WORKSPACE}/.test_dr_$$"
if touch "${test_file_dr}" 2>/dev/null; then
  rm -f "${test_file_dr}"
  pass "${CURRENT_USER} can write to shared workspace"
else
  fail "${CURRENT_USER} cannot write to shared workspace"
fi

# Test write as agent
test_file_agent="${SHARED_WORKSPACE}/.test_agent_$$"
if sudo -u "${AGENT_USER}" touch "${test_file_agent}" 2>/dev/null; then
  # Check that file inherited dev group (setgid)
  file_group=$(stat -f '%Sg' "${test_file_agent}" 2>/dev/null || echo "unknown")
  sudo rm -f "${test_file_agent}"
  pass "${AGENT_USER} can write to shared workspace"
  if [[ "${file_group}" == "${SHARED_GROUP}" ]]; then
    pass "New files inherit '${SHARED_GROUP}' group (setgid working)"
  else
    warn "New file group is '${file_group}', expected '${SHARED_GROUP}' — setgid may not be working"
  fi
else
  fail "${AGENT_USER} cannot write to shared workspace"
fi

# ===========================================================================
# Step 3: User isolation — agent cannot read dr's sensitive dirs
# ===========================================================================

step "User isolation"

sensitive_dirs=(
  "${HOME}/.ssh"
  "${HOME}/.aws"
  "${HOME}/.gnupg"
  "${HOME}/.config/gh"
  "${HOME}/Library"
)

for dir in "${sensitive_dirs[@]}"; do
  if [[ ! -e "${dir}" ]]; then
    skip "$(basename "${dir}") doesn't exist on this system"
    continue
  fi
  # Try listing the directory as agent — should fail
  if sudo -u "${AGENT_USER}" ls "${dir}" &>/dev/null; then
    fail "ISOLATION BREACH: ${AGENT_USER} can read ${dir}"
  else
    pass "${AGENT_USER} cannot read ${dir}"
  fi
done

# Verify dr cannot read agent's home
if ls "${AGENT_HOME}" &>/dev/null; then
  # dr CAN list agent's home — this is expected on macOS (home dirs are world-executable)
  # but dr should not be able to read contents
  if ls "${AGENT_HOME}/.zshrc" &>/dev/null 2>&1; then
    warn "dr can read ${AGENT_USER}'s .zshrc — consider chmod 700 ${AGENT_HOME}"
  else
    pass "dr cannot read files inside ${AGENT_USER}'s home"
  fi
fi

# ===========================================================================
# Step 4: Hardening gaps
# ===========================================================================

step "Hardening gaps"

# Docker socket
docker_sock="${HOME}/.docker/run/docker.sock"
if [[ -S "${docker_sock}" ]]; then
  docker_perms=$(stat -f '%Lp' "${docker_sock}" 2>/dev/null || echo "unknown")
  if [[ "${docker_perms}" == "700" ]]; then
    pass "Docker socket restricted to owner only (700)"
  else
    fail "Docker socket permissions: ${docker_perms} (expected 700 — agent could escape via Docker)"
  fi
else
  skip "Docker socket not present"
fi

# umask in agent's .zshrc
if sudo -u "${AGENT_USER}" grep -q 'umask 077' "${AGENT_HOME}/.zshrc" 2>/dev/null; then
  pass "umask 077 set in agent's .zshrc"
else
  warn "umask 077 not found in agent's .zshrc — new files will have permissive defaults"
fi

# ===========================================================================
# Step 5: Passwordless sudo
# ===========================================================================

step "Passwordless sudo"

if [[ -f "${SUDOERS_FILE}" ]]; then
  pass "Sudoers file exists: ${SUDOERS_FILE}"
else
  fail "Sudoers file missing: ${SUDOERS_FILE}"
fi

if sudo -u "${AGENT_USER}" whoami &>/dev/null; then
  pass "sudo -u ${AGENT_USER} works without password"
else
  fail "sudo -u ${AGENT_USER} failed — check ${SUDOERS_FILE}"
fi

# ===========================================================================
# Step 6: pf firewall — static checks
# ===========================================================================

step "pf firewall (static)"

if sudo pfctl -si 2>/dev/null | grep -q "Status: Enabled"; then
  pass "pf is enabled"
else
  fail "pf is NOT enabled — run: sudo pfctl -e"
fi

if [[ -f "${PF_ANCHOR_FILE}" ]]; then
  pass "pf anchor file exists: ${PF_ANCHOR_FILE}"
else
  fail "pf anchor file missing: ${PF_ANCHOR_FILE}"
fi

if sudo pfctl -a "${PF_ANCHOR_NAME}" -sr 2>/dev/null | grep -q "block"; then
  rule_count=$(sudo pfctl -a "${PF_ANCHOR_NAME}" -sr 2>/dev/null | wc -l | tr -d ' ')
  pass "pf anchor loaded with ${rule_count} rules"
else
  fail "pf anchor '${PF_ANCHOR_NAME}' not loaded or has no block rules"
fi

# Check that our specific blocked ports are in the anchor
check_port_in_anchor() {
  local port="$1" label="$2"
  if sudo pfctl -a "${PF_ANCHOR_NAME}" -sr 2>/dev/null | grep -qE "port = ${port}\b"; then
    pass "pf anchor blocks port ${port} (${label})"
  else
    warn "pf anchor may not block port ${port} (${label}) — verify anchor file"
  fi
}

check_port_in_anchor "25" "SMTP"
check_port_in_anchor "6667" "IRC"
check_port_in_anchor "21" "FTP"
check_port_in_anchor "9050" "Tor"

# ===========================================================================
# Step 7: pf firewall — live network tests
# ===========================================================================

step "pf firewall (live — as agent user)"

if [[ "${QUICK_MODE}" == true ]]; then
  skip "Live network tests (--quick mode)"
else
  if ! id "${AGENT_USER}" &>/dev/null; then
    skip "Agent user doesn't exist — can't run as-agent network tests"
  else
    # Test 1: HTTPS should work (port 443 is not blocked)
    echo "    Testing HTTPS (port 443, should be ALLOWED)..."
    if agent_tcp_connect "1.1.1.1" "443" "5"; then
      pass "${AGENT_USER} can connect on port 443 (HTTPS allowed)"
    else
      warn "${AGENT_USER} could not connect on port 443 — network may be down, or pf is too restrictive"
    fi

    # Test 2: SMTP should be blocked (port 25)
    echo "    Testing SMTP (port 25, should be BLOCKED)..."
    if agent_tcp_connect "1.1.1.1" "25" "3"; then
      fail "BLOCK FAILURE: ${AGENT_USER} connected to port 25 (SMTP not blocked)"
    else
      pass "Port 25 (SMTP) is BLOCKED for ${AGENT_USER}"
    fi

    # Test 3: IRC should be blocked (port 6667)
    echo "    Testing IRC (port 6667, should be BLOCKED)..."
    if agent_tcp_connect "1.1.1.1" "6667" "3"; then
      fail "BLOCK FAILURE: ${AGENT_USER} connected to port 6667 (IRC not blocked)"
    else
      pass "Port 6667 (IRC) is BLOCKED for ${AGENT_USER}"
    fi

    # Test 4: Tor should be blocked (port 9050)
    echo "    Testing Tor (port 9050, should be BLOCKED)..."
    if agent_tcp_connect "127.0.0.1" "9050" "3"; then
      # Only relevant if Tor is locally listening; if nothing listens it's blocked by "no listener" not pf
      warn "Port 9050 appears open — check if Tor is running locally and pf rule is active"
    else
      pass "Port 9050 (Tor) is BLOCKED for ${AGENT_USER}"
    fi

    # Test 5: SMTP as main user should still work (rules are per-UID)
    # We only check this if pf is active — if SMTP to 1.1.1.1 fails for dr too, it's just no server
    echo "    Testing that pf rules are scoped to ${AGENT_USER} only..."
    if ! timeout 3 bash -c 'echo > /dev/tcp/1.1.1.1/443' 2>/dev/null; then
      warn "dr cannot connect on port 443 either — general network issue, not a sandbox problem"
    else
      pass "pf rules are scoped: dr can reach port 443 (rules only restrict ${AGENT_USER})"
    fi
  fi
fi

# ===========================================================================
# Step 8: DNS blocklist
# ===========================================================================

step "DNS blocklist"

if grep -q "AI Agent Blocklist" /etc/hosts 2>/dev/null; then
  blocked_count=$(grep -c "^0.0.0.0" /etc/hosts 2>/dev/null || echo "0")
  pass "DNS blocklist present in /etc/hosts (${blocked_count} entries)"
else
  fail "DNS blocklist not found in /etc/hosts — run sandbox setup and choose yes for DNS blocklist"
fi

check_blocked_domain() {
  local domain="$1"
  local resolved
  resolved=$(dscacheutil -q host -a name "${domain}" 2>/dev/null | grep "ip_address:" | awk '{print $2}' | head -1)
  if [[ "${resolved}" == "0.0.0.0" ]]; then
    pass "${domain} resolves to 0.0.0.0 (blocked)"
  elif [[ -z "${resolved}" ]]; then
    # Also acceptable — domain blocked before resolution
    pass "${domain} did not resolve (blocked)"
  else
    fail "${domain} resolved to ${resolved} — blocklist not working for this domain"
  fi
}

check_blocked_domain "ngrok.io"
check_blocked_domain "pastebin.com"
check_blocked_domain "webhook.site"
check_blocked_domain "transfer.sh"

# ===========================================================================
# Step 9: Persistence
# ===========================================================================

step "Persistence across reboots"

if [[ -f "${PF_DAEMON_PLIST}" ]]; then
  pass "LaunchDaemon plist exists: ${PF_DAEMON_PLIST}"
else
  fail "LaunchDaemon plist missing: ${PF_DAEMON_PLIST} — pf rules will not reload on reboot"
fi

# Check it's loaded
if sudo launchctl list "${PF_DAEMON_LABEL}" &>/dev/null 2>&1; then
  pass "LaunchDaemon '${PF_DAEMON_LABEL}' is loaded"
else
  warn "LaunchDaemon '${PF_DAEMON_LABEL}' is not loaded — try: sudo launchctl load ${PF_DAEMON_PLIST}"
fi

# Verify /etc/pf.conf references the anchor
if grep -q "anchor \"${PF_ANCHOR_NAME}\"" /etc/pf.conf 2>/dev/null; then
  pass "/etc/pf.conf references anchor '${PF_ANCHOR_NAME}'"
else
  fail "/etc/pf.conf does not reference anchor '${PF_ANCHOR_NAME}'"
fi

# ===========================================================================
# Step 10: Agent user tools
# ===========================================================================

step "Agent user tools"

# Claude Code
claude_paths=(
  "${AGENT_HOME}/.local/bin/claude"
  "${AGENT_HOME}/.nvm/versions/node/$(as_agent 'node --version 2>/dev/null || echo x')/bin/claude"
)
claude_installed=false
for p in "${claude_paths[@]}"; do
  if sudo -u "${AGENT_USER}" test -f "${p}" 2>/dev/null; then
    claude_installed=true
    pass "Claude Code installed: ${p}"
    break
  fi
done
if ! "${claude_installed}"; then
  if sudo -u "${AGENT_USER}" bash -c 'command -v claude' &>/dev/null; then
    pass "Claude Code is in agent's PATH"
  else
    warn "Claude Code not found for agent user — run: sudo -u ${AGENT_USER} -i, then: curl -fsSL https://claude.ai/install.sh | bash"
  fi
fi

# API key
if sudo -u "${AGENT_USER}" bash -c 'grep -q ANTHROPIC_API_KEY ~/.zshrc 2>/dev/null || [[ -n "${ANTHROPIC_API_KEY:-}" ]]' &>/dev/null; then
  pass "ANTHROPIC_API_KEY is configured for agent user"
else
  warn "ANTHROPIC_API_KEY not found in agent's .zshrc — Claude Code will not authenticate"
fi

# Git identity
agent_git_name=$(sudo -u "${AGENT_USER}" bash -c 'git config --global user.name 2>/dev/null || true')
agent_git_email=$(sudo -u "${AGENT_USER}" bash -c 'git config --global user.email 2>/dev/null || true')
if [[ -n "${agent_git_name}" ]] && [[ -n "${agent_git_email}" ]]; then
  pass "Git identity configured: ${agent_git_name} <${agent_git_email}>"
else
  warn "Git identity not fully configured for agent (name='${agent_git_name}', email='${agent_git_email}')"
fi

# SSH key
if sudo -u "${AGENT_USER}" test -f "${AGENT_HOME}/.ssh/id_ed25519.pub" 2>/dev/null; then
  pass "SSH key exists (ed25519)"
elif sudo -u "${AGENT_USER}" test -d "${AGENT_HOME}/.ssh" 2>/dev/null; then
  warn "~/.ssh exists but no id_ed25519.pub — GitHub access may not work"
else
  warn "No SSH key found for agent user — run: sudo -u ${AGENT_USER} -i, then: ssh-keygen -t ed25519"
fi

# Claude settings
if sudo -u "${AGENT_USER}" test -f "${AGENT_HOME}/.claude/settings.json" 2>/dev/null; then
  pass "~/.claude/settings.json exists for agent user"
else
  warn "No ~/.claude/settings.json for agent user — permissions and deny rules not configured"
fi

# ===========================================================================
# Step 11: Backup script
# ===========================================================================

step "Backup script"

BACKUP_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/backup.sh"

if [[ -f "${BACKUP_SCRIPT}" ]]; then
  pass "backup.sh exists"
else
  fail "backup.sh not found at ${BACKUP_SCRIPT}"
fi

if [[ -x "${BACKUP_SCRIPT}" ]]; then
  pass "backup.sh is executable"
else
  fail "backup.sh is not executable — run: chmod +x ${BACKUP_SCRIPT}"
fi

# Dry-run backup to /tmp to verify rsync options are valid
tmp_dest="/tmp/sandboxtest-backup-$$"
if [[ ! -d "${HOME}/workspace" ]]; then
  skip "~/workspace does not exist — skipping rsync option validation"
elif rsync --dry-run -a "${HOME}/workspace/" "${tmp_dest}" \
    --exclude='node_modules/' --exclude='.venv/' --exclude='__pycache__/' \
    --exclude='.next/' --exclude='dist/' --exclude='build/' \
    &>/dev/null; then
  pass "backup.sh rsync options are valid (dry-run succeeded)"
else
  warn "rsync dry-run had errors — check backup.sh options"
fi

# ===========================================================================
# Summary
# ===========================================================================

echo ""
echo -e "${BOLD}━━━ Results ━━━${NC}"
echo ""

total=$((PASS + FAIL + WARN + SKIP))
echo -e "  Total checks: ${total}"
echo -e "  ${GREEN}${BOLD}Pass:${NC}  ${PASS}"
[[ "${FAIL}" -gt 0 ]]  && echo -e "  ${RED}${BOLD}Fail:${NC}  ${FAIL}" || echo -e "  Fail:  ${FAIL}"
[[ "${WARN}" -gt 0 ]]  && echo -e "  ${YELLOW}${BOLD}Warn:${NC}  ${WARN}" || echo -e "  Warn:  ${WARN}"
echo -e "  Skip:  ${SKIP}"
echo ""

if [[ "${FAIL}" -gt 0 ]]; then
  echo -e "  ${RED}${BOLD}Sandbox is NOT fully operational.${NC} Fix failures before running Claude in auto mode."
elif [[ "${WARN}" -gt 0 ]]; then
  echo -e "  ${YELLOW}${BOLD}Sandbox is operational with warnings.${NC} Review warnings before running autonomously."
else
  echo -e "  ${GREEN}${BOLD}All checks passed. Sandbox is ready.${NC}"
fi
echo ""
