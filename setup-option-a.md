# Option A Setup: Dedicated `agent` User + Soft pf Blocklist

Complete setup guide for sandboxing Claude Code on macOS using a dedicated user account with blocklist-based network restrictions.

## Architecture

```
Your user (dr)                     Sandbox user (agent)
├── ~/.ssh/           INVISIBLE    ├── ~/.claude/        (own credentials)
├── ~/.aws/           INVISIBLE    ├── ~/.gitconfig      (own git identity)
├── ~/.gnupg/         INVISIBLE    ├── ~/.ssh/id_ed25519 (scoped deploy key)
├── ~/Library/        INVISIBLE    └── (shared workspace access)
├── Keychain          INVISIBLE
├── Browser data      INVISIBLE    pf blocklist (kernel-enforced):
├── ~/Documents/      INVISIBLE      ✗ SMTP, IRC, FTP, Telnet, SMB
└── ~/workspace/      INVISIBLE      ✗ RDP, VNC, Tor, VPN, ICMP
                                     ✓ everything else allowed
                                   DNS blocklist:
                                     ✗ tunnel services (ngrok, etc.)
                                     ✗ paste/file-sharing services
                                     ✗ webhook capture services
                                   LuLu:
                                     ◎ monitors all outbound connections
```

All Claude Code work happens as `agent`. Your main user is for everything else.

### Security Model

The primary defense is **user isolation** — the agent physically cannot access your critical data (SSH keys, Keychain, cloud creds, browser data). The network blocklist catches obvious exfiltration channels (SMTP, tunnel services, paste sites). LuLu provides visibility. Together these form a practical defense-in-depth stack without breaking the agent's ability to fetch docs, search the web, and download packages.

See [attack-surface-deep-dive.md](attack-surface-deep-dive.md) for the full threat analysis and [soft-pf-blocklist.md](soft-pf-blocklist.md) for the blocklist philosophy.

---

## Prerequisites

- macOS Sequoia or later (Apple Silicon)
- Admin access to your Mac
- Anthropic API key
- GitHub account with SSH key support

---

## Step 1: Create the `agent` User

```bash
# Create user
sudo dscl . -create /Users/agent
sudo dscl . -create /Users/agent UserShell /bin/zsh
sudo dscl . -create /Users/agent UniqueID 599
sudo dscl . -create /Users/agent PrimaryGroupID 20
sudo dscl . -create /Users/agent NFSHomeDirectory /Users/agent

# Create home directory
sudo mkdir -p /Users/agent
sudo chown agent:staff /Users/agent
sudo createhomedir -c -u agent 2>/dev/null || true

# Set a password (you'll need this for sudo -u)
sudo passwd agent

# Hide from login screen (keeps macOS login window clean)
sudo dscl . -create /Users/agent IsHidden 1
```

### Verify

```bash
id agent
# Should show: uid=599(agent) gid=20(staff) ...

ls -la /Users/agent/
# Should show home directory owned by agent:staff
```

---

## Step 2: Shared Workspace

Projects live in a shared location accessible to both users. Your home directory stays untouched.

```bash
# Create shared workspace
sudo mkdir -p /Users/Shared/workspace
sudo chown dr:staff /Users/Shared/workspace
sudo chmod 770 /Users/Shared/workspace

# Convenience symlink from your user
ln -s /Users/Shared/workspace ~/workspace-shared

# Convenience symlink from agent user
sudo -u agent ln -s /Users/Shared/workspace /Users/agent/workspace
```

### Move or Clone Projects

```bash
# Option 1: Move existing projects
mv ~/workspace/my-project /Users/Shared/workspace/

# Option 2: Clone fresh
cd /Users/Shared/workspace
git clone git@github.com:you/my-project.git

# Option 3: Symlink individual projects (keeps originals in place)
ln -s ~/workspace/my-project /Users/Shared/workspace/my-project
# Note: agent can read/write through symlinks if target permissions allow.
# For maximum isolation, clone or move instead of symlinking.
```

---

## Step 3: Install Tools for `agent`

```bash
sudo -u agent -i

# Claude Code
curl -fsSL https://claude.ai/install.sh | bash

# Verify
claude --version

# Git config
git config --global user.name "Your Name"
git config --global user.email "you@example.com"

# SSH key for GitHub (scoped — separate from your main key)
ssh-keygen -t ed25519 -C "agent@$(hostname -s)" -f ~/.ssh/id_ed25519
cat ~/.ssh/id_ed25519.pub
# --> Add this to your GitHub account (Settings > SSH keys)
#     or as a deploy key on specific repos

# Test GitHub SSH
ssh -T git@github.com

# Anthropic API key
echo 'export ANTHROPIC_API_KEY="sk-ant-api03-YOUR-KEY-HERE"' >> ~/.zshrc
source ~/.zshrc

# Set restrictive umask (prevents /tmp leakage)
echo 'umask 077' >> ~/.zshrc

# Exit back to your user
exit
```

### Additional Tools (as needed)

```bash
sudo -u agent -i

# Node.js (if not already system-wide)
# Option A: Use system node (from homebrew)
# Homebrew node at /opt/homebrew/bin/node is typically world-readable

# Option B: Install nvm for the agent user
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash
source ~/.zshrc
nvm install 22

# Python (if needed)
# System python3 is available; or install pyenv for the agent user

# Terraform, kubectl, etc. (if doing infra work)
# Install per-tool as needed into agent's home or use system-wide binaries

exit
```

---

## Step 4: Harden Known Gaps

Research identified several macOS user isolation gaps. Fix them before running the agent.

### 4a: Docker Socket

The Docker socket is often world-accessible, granting root-equivalent access to the Docker VM. **This is the most critical gap.**

```bash
# Check current permissions
ls -la ~/.docker/run/docker.sock 2>/dev/null || ls -la /var/run/docker.sock 2>/dev/null

# Restrict to your user only
chmod 700 ~/.docker/run/docker.sock 2>/dev/null

# Or: simply quit Docker Desktop when running agent sessions
```

### 4b: Restrictive umask for your user too

Prevent your files in `/tmp` and shared locations from being world-readable:

```bash
echo 'umask 077' >> ~/.zshrc
source ~/.zshrc
```

### 4c: Set restrictive umask on shared workspace

Files created in the shared workspace should be readable by both users (staff group) but not others:

```bash
# Ensure new files in shared workspace inherit group permissions
chmod g+s /Users/Shared/workspace
```

### 4d: Passwordless sudo for user switching

```bash
sudo visudo -f /etc/sudoers.d/agent
```

Add:

```
dr ALL=(agent) NOPASSWD: ALL
```

This lets `dr` switch to `agent` without a password. The agent user still can't `sudo` to anything else.

---

## Step 5: Configure pf Blocklist

### Create the Anchor File

This uses the **soft blocklist approach** — block known-bad ports and protocols, allow everything else.

```bash
sudo tee /etc/pf.anchors/agent << 'ANCHOR'
# =============================================================
# Soft blocklist for the "agent" user
# Block known-bad ports/protocols; allow everything else
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
```

### Register the Anchor

```bash
sudo cp /etc/pf.conf /etc/pf.conf.backup.$(date +%Y%m%d)

# Check if anchor already exists
grep -q 'anchor "agent"' /etc/pf.conf || {
  sudo tee -a /etc/pf.conf << 'EOF'

# Claude Code sandbox user blocklist
anchor "agent"
load anchor "agent" from "/etc/pf.anchors/agent"
EOF
}
```

### Enable

```bash
sudo pfctl -f /etc/pf.conf
sudo pfctl -e
```

### Test

```bash
# Should succeed (general web access allowed)
sudo -u agent curl -sI --max-time 5 https://api.anthropic.com
sudo -u agent curl -sI --max-time 5 https://github.com
sudo -u agent curl -sI --max-time 5 https://example.com
sudo -u agent curl -sI --max-time 5 https://stackoverflow.com

# Should fail (blocked ports)
sudo -u agent curl -sI --max-time 5 smtp://mail.example.com:25   # SMTP blocked
sudo -u agent ping -c 1 8.8.8.8                                  # ICMP blocked

# Your main user should be unaffected
curl -sI --max-time 5 https://example.com
ping -c 1 8.8.8.8
```

---

## Step 6: DNS Domain Blocklist

pf can't filter by domain name, so we add a DNS-level blocklist for tunnel services, paste services, and file sharing services. Choose one approach:

### Option A: /etc/hosts (Simplest, No Dependencies)

```bash
sudo tee -a /etc/hosts << 'EOF'

# === AI Agent Blocklist: Tunnel Services ===
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

# === AI Agent Blocklist: Paste Services ===
0.0.0.0 pastebin.com
0.0.0.0 paste.ee
0.0.0.0 ghostbin.com
0.0.0.0 hastebin.com
0.0.0.0 dpaste.org
0.0.0.0 justpaste.it
0.0.0.0 rentry.co
0.0.0.0 ix.io

# === AI Agent Blocklist: File Sharing ===
0.0.0.0 transfer.sh
0.0.0.0 file.io
0.0.0.0 gofile.io
0.0.0.0 catbox.moe
0.0.0.0 filebin.net

# === AI Agent Blocklist: Webhook Capture ===
0.0.0.0 webhook.site
0.0.0.0 requestbin.com
0.0.0.0 pipedream.com
0.0.0.0 hookbin.com
0.0.0.0 beeceptor.com
EOF

# Flush DNS cache
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder
```

**Limitation:** System-wide (affects all users). Doesn't block subdomains (e.g., `abc123.ngrok-free.app`). If you need wildcard blocking, use Option B.

### Option B: dnsmasq (Wildcard Blocking)

```bash
brew install dnsmasq

# Add blocklist with wildcard support
cat >> /opt/homebrew/etc/dnsmasq.conf << 'EOF'

# === AI Agent Blocklist ===
address=/ngrok.io/0.0.0.0
address=/ngrok.com/0.0.0.0
address=/ngrok-free.app/0.0.0.0
address=/trycloudflare.com/0.0.0.0
address=/serveo.net/0.0.0.0
address=/localtunnel.me/0.0.0.0
address=/localhost.run/0.0.0.0
address=/localxpose.io/0.0.0.0
address=/pagekite.me/0.0.0.0
address=/bore.digital/0.0.0.0
address=/localtonet.com/0.0.0.0
address=/zrok.io/0.0.0.0
address=/devtunnels.ms/0.0.0.0
address=/loca.lt/0.0.0.0
address=/pastebin.com/0.0.0.0
address=/paste.ee/0.0.0.0
address=/hastebin.com/0.0.0.0
address=/dpaste.org/0.0.0.0
address=/justpaste.it/0.0.0.0
address=/rentry.co/0.0.0.0
address=/ix.io/0.0.0.0
address=/transfer.sh/0.0.0.0
address=/file.io/0.0.0.0
address=/gofile.io/0.0.0.0
address=/catbox.moe/0.0.0.0
address=/filebin.net/0.0.0.0
address=/webhook.site/0.0.0.0
address=/requestbin.com/0.0.0.0
address=/pipedream.com/0.0.0.0
address=/hookbin.com/0.0.0.0
address=/beeceptor.com/0.0.0.0
EOF

sudo brew services start dnsmasq

# Set system DNS: System Settings > Network > Wi-Fi > Details > DNS
# Add 127.0.0.1 as the first DNS server
```

dnsmasq `address=` directives block all subdomains automatically (e.g., `*.ngrok.io`).

### Option C: NextDNS (Cloud-Based, Managed)

1. Create free account at [nextdns.io](https://nextdns.io)
2. Add the domains above to your custom denylist
3. Configure macOS DNS to NextDNS endpoints
4. Free tier: 300,000 queries/month

---

## Step 7: Persist Firewall Across Reboots

```bash
sudo tee /Library/LaunchDaemons/com.local.pf-agent.plist << 'XML'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.local.pf-agent</string>
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

sudo chmod 644 /Library/LaunchDaemons/com.local.pf-agent.plist
sudo launchctl load /Library/LaunchDaemons/com.local.pf-agent.plist
```

---

## Step 8: LuLu Network Monitor (Recommended)

LuLu provides real-time visibility into what processes are connecting where.

1. Download from [objective-see.org/products/lulu.html](https://objective-see.org/products/lulu.html)
2. Install and grant the Network Extension permission
3. LuLu will alert on new outbound connections from any user

LuLu and pf are independent layers — pf blocks at the packet level, LuLu monitors at the application level. If something suspicious shows up in LuLu, add it to the pf blocklist or DNS blocklist.

---

## Daily Workflow

### Starting a Session

```bash
# Switch to the agent user
sudo -u agent -i

# Go to your project
cd ~/workspace/my-project

# Run Claude Code
claude
```

### iTerm Shortcut (Recommended)

Create a dedicated iTerm profile for the agent user:

1. **iTerm > Settings > Profiles > +** (create new profile)
2. Name: `Agent`
3. **General > Command:** select "Login Shell"
4. **General > Send text at start:** `sudo -u agent -i`
5. **Colors:** pick a distinct color scheme (e.g., red accent) so you always know which tab is sandboxed
6. Optionally assign a hotkey (e.g., `Ctrl+Shift+A`) to open a new Agent tab

### Ending a Session

```bash
exit
```

---

## Cloud Credentials for Infra Work

The agent user needs its own credentials. This is a security win — it enforces least-privilege.

### AWS

```bash
sudo -u agent -i

# Create ~/.aws/credentials with a scoped IAM user
mkdir -p ~/.aws
cat > ~/.aws/credentials << 'EOF'
[default]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
EOF
chmod 600 ~/.aws/credentials
```

### GCP / Other Clouds

Same pattern: create scoped service accounts, store credentials in agent's home directory.

### GitHub

The agent's SSH key should be scoped. Options:
- **Deploy keys** on specific repos (read-only or read-write per repo)
- **Fine-grained personal access token** with limited repo access
- **Separate GitHub account** for maximum isolation

---

## Defense Layers Summary

```
Layer 1: User isolation (strongest layer)
  └─ Agent cannot access your SSH keys, Keychain, cloud creds, browser data

Layer 2: pf port blocklist
  └─ Blocks SMTP, IRC, FTP, Telnet, SMB, RDP, VNC, Tor, VPN, ICMP

Layer 3: DNS domain blocklist
  └─ Blocks tunnel services, paste services, file sharing, webhook capture

Layer 4: LuLu monitoring
  └─ Real-time visibility into outbound connections

Layer 5: Scoped credentials
  └─ Agent has limited GitHub key, limited cloud permissions
  └─ If compromised, blast radius is small

Layer 6: Docker socket hardening
  └─ Prevents container escape to root-equivalent access
```

---

## Hardening Checklist

- [ ] `agent` user created and hidden from login screen
- [ ] Shared workspace set up with correct permissions (770, setgid)
- [ ] Claude Code installed and authenticated as `agent`
- [ ] SSH key created for `agent`, added to GitHub (scoped)
- [ ] Docker socket restricted (`chmod 700`)
- [ ] `umask 077` set for both users
- [ ] Passwordless sudo configured (`dr` -> `agent`)
- [ ] pf blocklist anchor created and tested
- [ ] DNS domain blocklist applied (/etc/hosts, dnsmasq, or NextDNS)
- [ ] pf LaunchDaemon installed for reboot persistence
- [ ] LuLu installed for network monitoring
- [ ] iTerm profile created for quick access
- [ ] Verified your main user (`dr`) is unaffected by pf rules

### Optional Additions

- [ ] Seatbelt profile layered on top (see [seatbelt-profile-reference.md](seatbelt-profile-reference.md))
- [ ] Cloud credentials scoped and stored in agent's home
- [ ] Cron watchdog for runaway processes: `*/5 * * * * [ $(pgrep -u agent -c) -gt 50 ] && pkill -u agent`
- [ ] Periodic review of LuLu logs for anomalous connections

---

## Uninstall / Rollback

```bash
# Remove pf rules
sudo sed -i '' '/anchor "agent"/d' /etc/pf.conf
sudo sed -i '' '/load anchor "agent"/d' /etc/pf.conf
sudo rm -f /etc/pf.anchors/agent
sudo pfctl -f /etc/pf.conf

# Remove LaunchDaemon
sudo launchctl unload /Library/LaunchDaemons/com.local.pf-agent.plist 2>/dev/null
sudo rm -f /Library/LaunchDaemons/com.local.pf-agent.plist

# Remove DNS blocklist entries from /etc/hosts (if used)
sudo sed -i '' '/AI Agent Blocklist/,/^$/d' /etc/hosts
sudo dscacheutil -flushcache

# Remove sudoers entry
sudo rm -f /etc/sudoers.d/agent

# Remove user
sudo dscl . -delete /Users/agent

# Remove home directory (if you want to delete everything)
sudo rm -rf /Users/agent

# Remove shared workspace (if no longer needed)
# sudo rm -rf /Users/Shared/workspace
```

---

## Troubleshooting

### Claude Code can't connect to Anthropic API

```bash
# Check pf is running and rules are loaded
sudo pfctl -si | grep Status
sudo pfctl -a agent -sr

# Test connectivity as agent
sudo -u agent curl -v --max-time 10 https://api.anthropic.com/v1/messages

# The soft blocklist should NOT block HTTPS — if it does, check rule ordering
```

### A website or service is unexpectedly blocked

```bash
# Check if it's in the DNS blocklist
dig +short suspicious-domain.com
# Returns 0.0.0.0? It's DNS-blocked. Remove from /etc/hosts or dnsmasq config.

# Check if it's a blocked port
sudo -u agent curl -v --max-time 5 https://the-service.com
# Connection refused on a blocked port? Add an exception to pf.
```

### Git push/pull fails

```bash
sudo -u agent ssh -vT git@github.com
```

SSH (port 22) is allowed by the soft blocklist. If it still fails, check the agent's SSH key configuration.

### npm install fails for specific packages

Some packages download binaries from custom CDNs. These should work with the soft blocklist (all HTTPS is allowed). If a download URL is DNS-blocked, remove it from the blocklist.

### Agent user can't access shared workspace

```bash
ls -la /Users/Shared/workspace
# Should be drwxrwx--- (or drwxrws---) with owner dr:staff
# agent is in the staff group by default, so group access works

# Fix if needed:
sudo chmod 770 /Users/Shared/workspace
```
