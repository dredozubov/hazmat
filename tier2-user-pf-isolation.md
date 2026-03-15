# Tier 2: Dedicated macOS User + pf Firewall

**Effort:** 30 minutes | **Performance:** Native | **Cost:** Free

This approach gives the agent a completely separate user identity. It physically cannot read your `~/.ssh`, `~/.aws`, Keychain, or browser data.

## Step 1: Create a Sandbox User

```bash
sudo dscl . -create /Users/claudeagent
sudo dscl . -create /Users/claudeagent UserShell /bin/zsh
sudo dscl . -create /Users/claudeagent UniqueID 599
sudo dscl . -create /Users/claudeagent PrimaryGroupID 20
sudo dscl . -create /Users/claudeagent NFSHomeDirectory /Users/claudeagent
sudo mkdir -p /Users/claudeagent
sudo chown claudeagent:staff /Users/claudeagent
sudo createhomedir -c -u claudeagent 2>/dev/null || true
```

## Step 2: Create an Encrypted APFS Volume

Open **Disk Utility** > select your APFS container > click "+" > name it `AgentWorkspace` > choose **APFS (Encrypted)** > set a password.

```bash
sudo chown claudeagent:staff /Volumes/AgentWorkspace
sudo chmod 700 /Volumes/AgentWorkspace
```

Each encrypted volume has its own password, separate from FileVault. Encryption keys are handled by the Secure Enclave on Apple Silicon.

## Step 3: Install Claude Code for the Sandbox User

```bash
sudo -u claudeagent -i
curl -fsSL https://claude.ai/install.sh | bash
echo 'export ANTHROPIC_API_KEY="sk-ant-..."' >> ~/.zshrc
```

## Step 4: Configure pf Firewall Rules

Create `/etc/pf.anchors/claudeagent`:

```
# Block all outgoing from claudeagent
block out quick proto tcp from any to any user claudeagent
block out quick proto udp from any to any user claudeagent

# Allow DNS
pass out quick proto udp from any to any port 53 user claudeagent
pass out quick proto tcp from any to any port 53 user claudeagent

# Allow Anthropic API (official IP range)
pass out quick proto tcp from any to 160.79.104.0/23 port 443 user claudeagent

# Allow GitHub (for git operations)
pass out quick proto tcp from any to 140.82.112.0/20 port 443 user claudeagent

# Allow npm registry
pass out quick proto tcp from any to 104.16.0.0/12 port 443 user claudeagent
```

Add to `/etc/pf.conf`:

```
anchor "claudeagent"
load anchor "claudeagent" from "/etc/pf.anchors/claudeagent"
```

Enable:

```bash
sudo pfctl -f /etc/pf.conf
sudo pfctl -e
```

**Note:** pf's `user` keyword matches by UID on the socket. It works for TCP and UDP only. Domain names in rules are resolved to IPs at rule-load time (not dynamically).

## Step 5: Run Claude Code

```bash
sudo -u claudeagent -i
cd /Volumes/AgentWorkspace/my-project
claude --dangerously-skip-permissions
```

Or via SSH (enable Remote Login for claudeagent in System Settings):

```bash
ssh claudeagent@localhost
```

## Step 6 (Optional): Add LuLu for Monitoring

Install [LuLu](https://objective-see.org/products/lulu.html) (free, open source). It alerts on outbound connections, giving visibility into agent network activity.

## Automated Alternative: SandVault

```bash
brew install sandvault
sv build          # creates sandvault-$USER account automatically
sv claude         # runs Claude Code in the sandboxed user
```

SandVault automates user creation, filesystem restrictions, and `sandbox-exec` layering.

## Another Alternative: Alcoholless (NTT Labs)

Runs commands as a separate user with a copy of the current directory (synced via rsync), then syncs changed files back on exit.

Source: [NTT Labs blog](https://medium.com/nttlabs/alcoholless-a-lightweight-security-sandbox-for-macos-programs-homebrew-ai-agents-etc-ccf0d1927301)

## What This Protects Against

| Threat | Protected? |
|--------|-----------|
| Agent reads `~/.ssh`, `~/.aws`, Keychain | Yes — separate user |
| Infostealer harvesting `~/.claude/` from your main user | Yes — credentials in sandbox user's home |
| Shell commands damaging your files | Yes — no write access outside workspace |
| Network exfiltration via curl/wget | Yes — pf blocks at kernel level |
| Prompt injection running arbitrary commands | Partially — commands run but with limited blast radius |
| Agent modifying `~/.zshrc` for persistence | Yes — sandbox user's zshrc only |

## What This Does NOT Protect Against

- Reading world-readable files (`/etc`, `/usr/local`, shared folders)
- Consuming system resources (CPU, memory, disk) — no quotas
- Privilege escalation via system vulnerabilities
- Attacks targeting the sandbox user's own `~/.claude/` credentials

## Persistence Across Reboots

pf rules do not persist across macOS upgrades (the system resets `pf.conf`). Create a LaunchDaemon to reload rules on boot:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.local.pfanchor</string>
    <key>ProgramArguments</key>
    <array>
        <string>/sbin/pfctl</string>
        <string>-f</string>
        <string>/etc/pf.conf</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
```

Save to `/Library/LaunchDaemons/com.local.pfanchor.plist`.
