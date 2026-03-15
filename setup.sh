#!/usr/bin/env bash
#
# Option A Setup: Dedicated "agent" user + soft pf blocklist
# See setup-option-a.md for full documentation.
#
# Usage:
#   chmod +x setup.sh
#   ./setup.sh
#
# Each step is idempotent — safe to run multiple times.
# Run as your normal user (not root). The script uses sudo where needed.

set -euo pipefail

# --- Configuration -----------------------------------------------------------

AGENT_USER="agent"
AGENT_UID="599"
AGENT_HOME="/Users/${AGENT_USER}"
SHARED_WORKSPACE="/Users/Shared/workspace"
SHARED_GROUP="dev"
SHARED_GID="599"
CURRENT_USER="$(whoami)"
PF_ANCHOR_NAME="agent"
PF_ANCHOR_FILE="/etc/pf.anchors/${PF_ANCHOR_NAME}"
PF_DAEMON_LABEL="com.local.pf-agent"
PF_DAEMON_PLIST="/Library/LaunchDaemons/${PF_DAEMON_LABEL}.plist"
SUDOERS_FILE="/etc/sudoers.d/${AGENT_USER}"

# --- Colors -------------------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

step_num=0

step() {
  step_num=$((step_num + 1))
  echo ""
  echo -e "${BLUE}${BOLD}━━━ Step ${step_num}: $1 ━━━${NC}"
}

ok() {
  echo -e "  ${GREEN}✓${NC} $1"
}

skip() {
  echo -e "  ${YELLOW}→${NC} $1 (already done)"
}

warn() {
  echo -e "  ${YELLOW}!${NC} $1"
}

fail() {
  echo -e "  ${RED}✗${NC} $1"
  exit 1
}

ask() {
  if [[ ! -t 0 ]]; then
    warn "Non-interactive mode: skipping '$1'"
    return 1
  fi
  echo -e -n "  ${BOLD}$1${NC} [y/N] "
  read -r answer
  [[ "$answer" =~ ^[Yy]$ ]]
}

# --- Preflight ----------------------------------------------------------------

echo -e "${BOLD}"
echo "  ┌──────────────────────────────────────────────────┐"
echo "  │  Option A: Dedicated agent user + soft blocklist │"
echo "  └──────────────────────────────────────────────────┘"
echo -e "${NC}"
echo "  This script will:"
echo "    1. Create a hidden '${AGENT_USER}' macOS user"
echo "    2. Create a '${SHARED_GROUP}' group (only ${CURRENT_USER} + ${AGENT_USER})"
echo "    3. Set up a shared workspace at ${SHARED_WORKSPACE}"
echo "    4. Harden known macOS isolation gaps"
echo "    5. Configure passwordless sudo (${CURRENT_USER} → ${AGENT_USER})"
echo "    6. Install a pf port blocklist (SMTP, IRC, FTP, Tor, etc.)"
echo "    7. Add a DNS domain blocklist (tunnel/paste/fileshare services)"
echo "    8. Persist firewall rules across reboots"
echo ""
echo "  You'll need to manually install afterward:"
echo "    • Claude Code (as the agent user)"
echo "    • An SSH key for GitHub (as the agent user)"
echo "    • LuLu network monitor (optional, recommended)"
echo ""

if [[ "$(uname)" != "Darwin" ]]; then
  fail "This script is for macOS only."
fi

trap 'echo -e "\n${RED}${BOLD}Setup interrupted — some steps may be incomplete.${NC}\nSee setup-option-a.md § Uninstall / Rollback" >&2' ERR

if [[ "${CURRENT_USER}" == "root" ]]; then
  fail "Run as your normal user, not root. The script uses sudo where needed."
fi

if ! ask "Proceed with setup?"; then
  echo "  Aborted."
  exit 0
fi

# --- Step 1: Create agent user ------------------------------------------------

step "Create '${AGENT_USER}' user"

if id "${AGENT_USER}" &>/dev/null; then
  skip "User '${AGENT_USER}' already exists (uid=$(id -u ${AGENT_USER}))"
else
  # Check if UID is taken
  if dscl . -list /Users UniqueID 2>/dev/null | awk '{print $2}' | grep -q "^${AGENT_UID}$"; then
    fail "UID ${AGENT_UID} is already taken by another user. Edit AGENT_UID in this script."
  fi

  sudo dscl . -create "/Users/${AGENT_USER}"
  sudo dscl . -create "/Users/${AGENT_USER}" UserShell /bin/zsh
  sudo dscl . -create "/Users/${AGENT_USER}" UniqueID "${AGENT_UID}"
  sudo dscl . -create "/Users/${AGENT_USER}" PrimaryGroupID 20
  sudo dscl . -create "/Users/${AGENT_USER}" NFSHomeDirectory "${AGENT_HOME}"
  ok "User record created"

  sudo mkdir -p "${AGENT_HOME}"
  sudo chown "${AGENT_USER}:staff" "${AGENT_HOME}"
  sudo createhomedir -c -u "${AGENT_USER}" 2>/dev/null || true
  ok "Home directory created at ${AGENT_HOME}"

  # Hide from login screen
  sudo dscl . -create "/Users/${AGENT_USER}" IsHidden 1
  ok "Hidden from login screen"

  echo ""
  echo -e "  ${BOLD}Set a password for the '${AGENT_USER}' user:${NC}"
  sudo passwd "${AGENT_USER}"
  ok "Password set"
fi

# Verify
if ! id "${AGENT_USER}" &>/dev/null; then
  fail "User '${AGENT_USER}' does not exist after creation attempt."
fi

# --- Step 2: Create 'dev' group ----------------------------------------------

step "Create '${SHARED_GROUP}' group"

if dscl . -read "/Groups/${SHARED_GROUP}" &>/dev/null; then
  skip "Group '${SHARED_GROUP}' already exists (gid=$(dscl . -read /Groups/${SHARED_GROUP} PrimaryGroupID | awk '{print $2}'))"
else
  # Check if GID is taken
  if dscl . -list /Groups PrimaryGroupID 2>/dev/null | awk '{print $2}' | grep -q "^${SHARED_GID}$"; then
    fail "GID ${SHARED_GID} is already taken. Edit SHARED_GID in this script."
  fi

  sudo dscl . -create "/Groups/${SHARED_GROUP}"
  sudo dscl . -create "/Groups/${SHARED_GROUP}" PrimaryGroupID "${SHARED_GID}"
  sudo dscl . -create "/Groups/${SHARED_GROUP}" RealName "Shared dev workspace"
  ok "Group '${SHARED_GROUP}' created (gid=${SHARED_GID})"
fi

# Ensure both users are members
for user in "${CURRENT_USER}" "${AGENT_USER}"; do
  if dscl . -read "/Groups/${SHARED_GROUP}" GroupMembership 2>/dev/null | grep -qw "${user}"; then
    skip "${user} is already a member of '${SHARED_GROUP}'"
  else
    sudo dscl . -append "/Groups/${SHARED_GROUP}" GroupMembership "${user}"
    ok "Added ${user} to '${SHARED_GROUP}'"
  fi
done

# --- Step 3: Shared workspace ------------------------------------------------

step "Set up shared workspace"

if [[ -d "${SHARED_WORKSPACE}" ]]; then
  skip "Shared workspace exists at ${SHARED_WORKSPACE}"
else
  sudo mkdir -p "${SHARED_WORKSPACE}"
  ok "Created ${SHARED_WORKSPACE}"
fi

# Fix ownership and permissions — use dev group, not staff
sudo chown "${CURRENT_USER}:${SHARED_GROUP}" "${SHARED_WORKSPACE}"
sudo chmod 770 "${SHARED_WORKSPACE}"
# setgid so new files inherit the dev group
sudo chmod g+s "${SHARED_WORKSPACE}"
ok "Permissions: 770, setgid, owner ${CURRENT_USER}:${SHARED_GROUP}"

# Symlink from your user
if [[ -L "${HOME}/workspace-shared" ]]; then
  skip "Symlink ~/workspace-shared already exists"
elif [[ -e "${HOME}/workspace-shared" ]]; then
  warn "~/workspace-shared exists but is not a symlink — skipping"
else
  ln -s "${SHARED_WORKSPACE}" "${HOME}/workspace-shared"
  ok "Created symlink ~/workspace-shared → ${SHARED_WORKSPACE}"
fi

# Symlink from agent user
if sudo -u "${AGENT_USER}" test -L "${AGENT_HOME}/workspace"; then
  skip "Symlink ${AGENT_HOME}/workspace already exists"
elif sudo -u "${AGENT_USER}" test -e "${AGENT_HOME}/workspace"; then
  warn "${AGENT_HOME}/workspace exists but is not a symlink — skipping"
else
  sudo -u "${AGENT_USER}" ln -s "${SHARED_WORKSPACE}" "${AGENT_HOME}/workspace"
  ok "Created symlink ${AGENT_HOME}/workspace → ${SHARED_WORKSPACE}"
fi

# --- Step 4: Harden known gaps ------------------------------------------------

step "Harden known macOS isolation gaps"

# 4a: Docker socket
docker_sock="${HOME}/.docker/run/docker.sock"
if [[ -S "${docker_sock}" ]]; then
  current_perms=$(stat -f '%Lp' "${docker_sock}" 2>/dev/null || echo "unknown")
  if [[ "${current_perms}" == "700" ]]; then
    skip "Docker socket already restricted (700)"
  else
    chmod 700 "${docker_sock}"
    ok "Docker socket restricted to owner only (was ${current_perms})"
  fi
else
  skip "Docker socket not found (Docker Desktop not running or not installed)"
fi

# 4b: Restrictive umask for agent user
agent_zshrc="${AGENT_HOME}/.zshrc"
if sudo -u "${AGENT_USER}" grep -q 'umask 077' "${agent_zshrc}" 2>/dev/null; then
  skip "umask 077 already set in agent's .zshrc"
else
  echo 'umask 077' | sudo -u "${AGENT_USER}" tee -a "${agent_zshrc}" >/dev/null
  ok "Set umask 077 in agent's .zshrc"
fi

# 4c: Restrictive umask for your user
if grep -q 'umask 077' "${HOME}/.zshrc" 2>/dev/null; then
  skip "umask 077 already set in your .zshrc"
else
  if ask "Add 'umask 077' to your .zshrc? (prevents /tmp leakage)"; then
    echo 'umask 077' >> "${HOME}/.zshrc"
    ok "Set umask 077 in your .zshrc"
  else
    warn "Skipped. Consider adding 'umask 077' manually."
  fi
fi

# --- Step 5: Passwordless sudo ------------------------------------------------

step "Configure passwordless sudo (${CURRENT_USER} → ${AGENT_USER})"

if [[ -f "${SUDOERS_FILE}" ]] && sudo grep -q "${CURRENT_USER}" "${SUDOERS_FILE}" 2>/dev/null; then
  skip "Sudoers entry already exists"
else
  echo "${CURRENT_USER} ALL=(${AGENT_USER}) NOPASSWD: ALL" | sudo tee "${SUDOERS_FILE}" >/dev/null
  sudo chmod 440 "${SUDOERS_FILE}"

  # Validate sudoers syntax
  if sudo visudo -c -f "${SUDOERS_FILE}" &>/dev/null; then
    ok "Sudoers entry created: ${CURRENT_USER} can switch to ${AGENT_USER} without password"
  else
    sudo rm -f "${SUDOERS_FILE}"
    fail "Sudoers syntax check failed — entry removed. Please configure manually."
  fi
fi

# Test
if sudo -u "${AGENT_USER}" whoami &>/dev/null; then
  ok "Verified: sudo -u ${AGENT_USER} works"
else
  warn "sudo -u ${AGENT_USER} failed — you may be prompted for a password"
fi

# --- Step 6: pf port blocklist ------------------------------------------------

step "Configure pf port blocklist"

if [[ -f "${PF_ANCHOR_FILE}" ]]; then
  skip "pf anchor file already exists at ${PF_ANCHOR_FILE}"
  warn "To replace it, delete it first: sudo rm ${PF_ANCHOR_FILE}"
else
  sudo tee "${PF_ANCHOR_FILE}" >/dev/null << 'ANCHOR'
# =============================================================
# Soft blocklist for the "agent" user
# Block known-bad ports/protocols; allow everything else
# Generated by setup.sh
# =============================================================

# --- Block exfiltration / C2 protocols ---

# SMTP (email exfiltration)
block return out quick proto tcp from any to any port { 25, 465, 587 } user agent

# IRC (C2 channel)
block return out quick proto tcp from any to any port { 6660, 6661, 6662, 6663, 6664, 6665, 6666, 6667, 6668, 6669, 6697 } user agent

# FTP (legacy file transfer)
block return out quick proto tcp from any to any port { 20, 21 } user agent

# Telnet (insecure remote access)
block return out quick proto tcp from any to any port 23 user agent

# SMB (lateral movement)
block return out quick proto tcp from any to any port 445 user agent

# RDP (remote desktop)
block return out quick proto tcp from any to any port 3389 user agent

# VNC (remote desktop)
block return out quick proto tcp from any to any port { 5900, 5901 } user agent

# Tor (anonymous exfiltration)
block return out quick proto tcp from any to any port { 9050, 9150 } user agent

# SOCKS proxy
block return out quick proto tcp from any to any port 1080 user agent

# VPN protocols
block return out quick proto tcp from any to any port { 1194, 1723 } user agent
block return out quick proto udp from any to any port { 1194, 1723, 4500 } user agent

# XMPP (messaging / C2)
block return out quick proto tcp from any to any port { 5222, 5269 } user agent

# --- Block ICMP tunneling ---
block return out quick proto icmp from any to any user agent

# --- Allow everything else ---
pass out quick user agent
ANCHOR
  ok "Created pf anchor at ${PF_ANCHOR_FILE}"
fi

# Register anchor in pf.conf
if grep -q "anchor \"${PF_ANCHOR_NAME}\"" /etc/pf.conf 2>/dev/null; then
  skip "Anchor already registered in /etc/pf.conf"
else
  # Backup
  sudo cp /etc/pf.conf "/etc/pf.conf.backup.$(date +%Y%m%d%H%M%S)"
  ok "Backed up /etc/pf.conf"

  sudo tee -a /etc/pf.conf >/dev/null << EOF

# Claude Code sandbox user blocklist
anchor "${PF_ANCHOR_NAME}"
load anchor "${PF_ANCHOR_NAME}" from "${PF_ANCHOR_FILE}"
EOF
  ok "Registered anchor in /etc/pf.conf"
fi

# Load rules
if sudo pfctl -f /etc/pf.conf 2>/tmp/pf-setup-err; then
  ok "pf rules loaded"
else
  warn "pfctl failed to load rules: $(cat /tmp/pf-setup-err)"
fi
rm -f /tmp/pf-setup-err
sudo pfctl -e 2>/dev/null || true
ok "pf enabled"

# --- Step 7: DNS domain blocklist ---------------------------------------------

step "Configure DNS domain blocklist"

HOSTS_MARKER="# === AI Agent Blocklist ==="

if grep -q "${HOSTS_MARKER}" /etc/hosts 2>/dev/null; then
  skip "DNS blocklist already present in /etc/hosts"
  warn "To replace it, remove the block between '${HOSTS_MARKER}' markers first."
else
  if ask "Add DNS blocklist to /etc/hosts? (blocks tunnel/paste/fileshare domains system-wide)"; then
    sudo tee -a /etc/hosts >/dev/null << 'HOSTS'

# === AI Agent Blocklist ===
# Tunnel services
0.0.0.0 ngrok.io
0.0.0.0 ngrok.com
0.0.0.0 ngrok-free.app
0.0.0.0 tunnel.cloudflare.com
0.0.0.0 trycloudflare.com
0.0.0.0 serveo.net
0.0.0.0 localtunnel.me
0.0.0.0 localhost.run
0.0.0.0 localxpose.io
0.0.0.0 pagekite.me
0.0.0.0 bore.digital
0.0.0.0 localtonet.com
0.0.0.0 zrok.io
0.0.0.0 devtunnels.ms
0.0.0.0 loca.lt
0.0.0.0 tunnelmole.com
0.0.0.0 playit.gg
0.0.0.0 pinggy.io
0.0.0.0 lokal.so
0.0.0.0 telebit.cloud
0.0.0.0 loophole.cloud
# Paste services
0.0.0.0 pastebin.com
0.0.0.0 paste.ee
0.0.0.0 ghostbin.com
0.0.0.0 hastebin.com
0.0.0.0 dpaste.org
0.0.0.0 justpaste.it
0.0.0.0 rentry.co
0.0.0.0 ix.io
# File sharing
0.0.0.0 transfer.sh
0.0.0.0 file.io
0.0.0.0 gofile.io
0.0.0.0 catbox.moe
0.0.0.0 filebin.net
# Webhook capture
0.0.0.0 webhook.site
0.0.0.0 requestbin.com
0.0.0.0 pipedream.com
0.0.0.0 hookbin.com
0.0.0.0 beeceptor.com
# === End AI Agent Blocklist ===
HOSTS

    # Flush DNS cache
    sudo dscacheutil -flushcache 2>/dev/null || true
    sudo killall -HUP mDNSResponder 2>/dev/null || true
    ok "DNS blocklist added to /etc/hosts and cache flushed"
    warn "This is system-wide. /etc/hosts does not block subdomains."
    warn "For wildcard blocking (*.ngrok.io), use dnsmasq — see soft-pf-blocklist.md"
  else
    warn "Skipped DNS blocklist. See soft-pf-blocklist.md for alternatives (dnsmasq, NextDNS)."
  fi
fi

# --- Step 8: Persist firewall across reboots ----------------------------------

step "Persist firewall across reboots"

if [[ -f "${PF_DAEMON_PLIST}" ]]; then
  skip "LaunchDaemon already exists at ${PF_DAEMON_PLIST}"
else
  sudo tee "${PF_DAEMON_PLIST}" >/dev/null << XML
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${PF_DAEMON_LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/sbin/pfctl</string>
        <string>-f</string>
        <string>/etc/pf.conf</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/var/log/pf-agent.log</string>
</dict>
</plist>
XML
  sudo chmod 644 "${PF_DAEMON_PLIST}"
  sudo launchctl bootstrap system "${PF_DAEMON_PLIST}" 2>/dev/null || true
  ok "LaunchDaemon installed — pf rules will reload on boot"
fi

# --- Verification -------------------------------------------------------------

step "Verify setup"

echo ""

# User exists
if id "${AGENT_USER}" &>/dev/null; then
  ok "User '${AGENT_USER}' exists (uid=$(id -u ${AGENT_USER}))"
else
  fail "User '${AGENT_USER}' not found"
fi

# Home directory
if [[ -d "${AGENT_HOME}" ]]; then
  ok "Home directory exists at ${AGENT_HOME}"
else
  fail "Home directory missing"
fi

# Shared workspace
if [[ -d "${SHARED_WORKSPACE}" ]]; then
  perms=$(stat -f '%Sp' "${SHARED_WORKSPACE}")
  ok "Shared workspace exists at ${SHARED_WORKSPACE} (${perms})"
else
  fail "Shared workspace missing"
fi

# pf anchor
if sudo pfctl -a "${PF_ANCHOR_NAME}" -sr 2>/dev/null | grep -q "block"; then
  rule_count=$(sudo pfctl -a "${PF_ANCHOR_NAME}" -sr 2>/dev/null | wc -l | tr -d ' ')
  ok "pf anchor loaded with ${rule_count} rules"
else
  warn "pf anchor not loaded or empty — try: sudo pfctl -f /etc/pf.conf && sudo pfctl -e"
fi

# pf enabled
if sudo pfctl -si 2>/dev/null | grep -q "Status: Enabled"; then
  ok "pf is enabled"
else
  warn "pf is not enabled — run: sudo pfctl -e"
fi

# Sudo
if sudo -u "${AGENT_USER}" whoami &>/dev/null; then
  ok "Passwordless sudo works (${CURRENT_USER} → ${AGENT_USER})"
else
  warn "Passwordless sudo not working"
fi

# DNS blocklist
if grep -q "AI Agent Blocklist" /etc/hosts 2>/dev/null; then
  blocked_count=$(grep -c "^0.0.0.0" /etc/hosts 2>/dev/null || echo "0")
  ok "DNS blocklist active (${blocked_count} domains in /etc/hosts)"
else
  warn "DNS blocklist not installed in /etc/hosts"
fi

# --- Done ---------------------------------------------------------------------

echo ""
echo -e "${GREEN}${BOLD}━━━ Setup complete ━━━${NC}"
echo ""
echo "  Remaining manual steps:"
echo ""
echo -e "  ${BOLD}1. Install Claude Code as the agent user:${NC}"
echo "     sudo -u ${AGENT_USER} -i"
echo "     curl -fsSL https://claude.ai/install.sh | bash"
echo ""
echo -e "  ${BOLD}2. Set your Anthropic API key:${NC}"
echo "     echo 'export ANTHROPIC_API_KEY=\"sk-ant-...\"' >> ~/.zshrc"
echo ""
echo -e "  ${BOLD}3. Create an SSH key for GitHub:${NC}"
echo "     ssh-keygen -t ed25519 -C \"agent@\$(hostname -s)\""
echo "     cat ~/.ssh/id_ed25519.pub"
echo "     # Add to GitHub → Settings → SSH keys"
echo ""
echo -e "  ${BOLD}4. Configure git:${NC}"
echo "     git config --global user.name \"Your Name\""
echo "     git config --global user.email \"you@example.com\""
echo ""
echo -e "  ${BOLD}5. Install LuLu (optional, recommended):${NC}"
echo "     https://objective-see.org/products/lulu.html"
echo ""
echo -e "  ${BOLD}6. Create an iTerm 'Agent' profile:${NC}"
echo "     Command: Login Shell"
echo "     Send text at start: sudo -u ${AGENT_USER} -i"
echo "     Use a distinct color scheme (e.g., red accent)"
echo ""
echo "  Daily workflow:"
echo "     sudo -u ${AGENT_USER} -i"
echo "     cd ~/workspace/my-project"
echo "     claude"
echo ""
echo "  To uninstall, see setup-option-a.md § Uninstall / Rollback"
echo ""
