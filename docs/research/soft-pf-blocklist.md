# Soft pf Blocklist: Block Known-Bad, Allow Everything Else

## Philosophy

Allowlisting breaks a coding agent's workflow — it needs to fetch docs, search the web, download packages from arbitrary CDNs, and hit APIs you can't predict. The real security value is **user isolation** (the agent can't read your critical data), not network lockdown.

The soft pf approach:
- **Block ports** used by protocols the agent should never need (SMTP, IRC, FTP, etc.)
- **Block domains** of known exfiltration infrastructure (tunnel services, paste services)
- **Allow everything else** — HTTPS, HTTP, SSH, DNS all work normally
- **Monitor** with LuLu for anomaly detection

This stops the low-hanging fruit while keeping the agent fully functional.

---

## pf Rules: Port-Based Blocking

### `/etc/pf.anchors/agent`

```
# =============================================================
# Soft blocklist for the "agent" user
# Block known-bad ports and protocols; allow everything else
# =============================================================

# --- Block exfiltration/C2 protocols ---

# SMTP (email exfiltration)
block out quick proto tcp from any to any port { 25, 465, 587 } user agent

# IRC (C2 channel)
block out quick proto tcp from any to any port { 6660, 6661, 6662, 6663, 6664, 6665, 6666, 6667, 6668, 6669, 6697 } user agent

# FTP (legacy file transfer)
block out quick proto tcp from any to any port { 20, 21 } user agent

# Telnet (insecure remote access)
block out quick proto tcp from any to any port 23 user agent

# SMB (lateral movement)
block out quick proto tcp from any to any port 445 user agent

# RDP (remote desktop)
block out quick proto tcp from any to any port 3389 user agent

# VNC (remote desktop)
block out quick proto tcp from any to any port { 5900, 5901 } user agent

# Tor (anonymous exfiltration)
block out quick proto tcp from any to any port { 9050, 9150 } user agent

# SOCKS proxy
block out quick proto tcp from any to any port 1080 user agent

# VPN protocols
block out quick proto tcp from any to any port { 1194, 1723 } user agent
block out quick proto udp from any to any port { 1194, 1723, 4500 } user agent

# XMPP (messaging/C2)
block out quick proto tcp from any to any port { 5222, 5269 } user agent

# --- Block ICMP tunneling ---
block out quick proto icmp from any to any user agent

# --- Allow everything else ---
pass out quick user agent
```

### Why Not Block SSH (Port 22)?

SSH is needed for `git clone/push/pull` over SSH. Blocking it forces HTTPS-only git, which works but adds friction. If you exclusively use HTTPS git URLs, you can add:

```
# Optional: block outbound SSH (forces HTTPS git)
# block out quick proto tcp from any to any port 22 user agent
```

### Why Not Block High Ports?

Ephemeral ports (1024-65535) are used by legitimate HTTPS responses, package downloads, and dev servers. Blocking them breaks everything.

---

## DNS-Level Blocking: Domain-Based Protection

pf can't filter by domain. For blocking tunnel services, paste services, and file sharing by domain name, use one of these approaches.

### Option 1: NextDNS (Easiest, Cloud-Based)

1. Create free account at [nextdns.io](https://nextdns.io)
2. Add a custom denylist with the domains below
3. Configure macOS system DNS to NextDNS endpoints
4. Free tier: 300,000 queries/month (enough for individual use)

**Limitation:** System-wide, not per-user. All users on the Mac get the same filtering. Acceptable if you don't use these services yourself.

### Option 2: Local dnsmasq (Self-Hosted, Per-Machine)

```bash
brew install dnsmasq

# Add blocklist entries
cat >> /opt/homebrew/etc/dnsmasq.conf << 'EOF'

# === Tunnel services ===
address=/ngrok.io/0.0.0.0
address=/ngrok.com/0.0.0.0
address=/ngrok-free.app/0.0.0.0
address=/trycloudflare.com/0.0.0.0
address=/serveo.net/0.0.0.0
address=/localtunnel.me/0.0.0.0
address=/localhost.run/0.0.0.0
address=/localxpose.io/0.0.0.0
address=/pagekite.me/0.0.0.0
address=/telebit.cloud/0.0.0.0
address=/loophole.cloud/0.0.0.0
address=/pinggy.io/0.0.0.0
address=/bore.digital/0.0.0.0
address=/localtonet.com/0.0.0.0
address=/playit.gg/0.0.0.0
address=/zrok.io/0.0.0.0
address=/lokal.so/0.0.0.0
address=/devtunnels.ms/0.0.0.0
address=/loca.lt/0.0.0.0
address=/tunnelmole.com/0.0.0.0

# === Paste services ===
address=/pastebin.com/0.0.0.0
address=/paste.ee/0.0.0.0
address=/ghostbin.com/0.0.0.0
address=/controlc.com/0.0.0.0
address=/hastebin.com/0.0.0.0
address=/dpaste.org/0.0.0.0
address=/justpaste.it/0.0.0.0
address=/rentry.co/0.0.0.0
address=/ix.io/0.0.0.0

# === File sharing services ===
address=/transfer.sh/0.0.0.0
address=/file.io/0.0.0.0
address=/gofile.io/0.0.0.0
address=/catbox.moe/0.0.0.0
address=/filebin.net/0.0.0.0
address=/fromsmash.com/0.0.0.0
address=/swisstransfer.com/0.0.0.0

# === Webhook/request capture ===
address=/webhook.site/0.0.0.0
address=/requestbin.com/0.0.0.0
address=/pipedream.com/0.0.0.0
address=/hookbin.com/0.0.0.0
address=/beeceptor.com/0.0.0.0

EOF

# Start dnsmasq
sudo brew services start dnsmasq

# Set system DNS to 127.0.0.1
# System Settings > Network > Wi-Fi > Details > DNS > add 127.0.0.1 as first entry
```

**Limitation:** Also system-wide. But these domains are rarely needed legitimately.

### Option 3: /etc/hosts (Simplest, No Dependencies)

```bash
# Append blocked domains to /etc/hosts
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

**Limitation:** System-wide, doesn't block subdomains (e.g., `abc123.ngrok-free.app`). dnsmasq or NextDNS are better for wildcard blocking.

**Note:** `/etc/hosts` doesn't support wildcards. For `*.ngrok.io` style blocking, use dnsmasq or NextDNS.

---

## What This Doesn't Block (And Why That's OK)

| Vector | Why Unblockable | Why It's OK |
|--------|----------------|-------------|
| HTTPS POST to arbitrary servers | Same protocol as legitimate web | Agent can't read your SSH keys, Keychain, cloud creds (user isolation) |
| Git push to attacker repos | Standard HTTPS to github.com | Sandbox user has scoped deploy key, not your main GitHub credentials |
| DNS tunneling | Data in query subdomains | Agent can't read secrets worth tunneling (user isolation) |
| Cloud storage API upload | Shared domains with legitimate services | Agent doesn't have your AWS/GCP credentials |
| GitHub API exfiltration | Standard API calls | Sandbox user has scoped token |

**The pattern:** User isolation removes the valuable targets. The blocklist catches the obvious exfiltration channels. Together they provide practical security without breaking the agent's workflow.

---

## Defense Layers Summary

```
Layer 1: User isolation
  └─ Agent can't access your critical data (SSH, Keychain, cloud creds, browser)

Layer 2: pf port blocklist
  └─ Blocks SMTP, IRC, FTP, Telnet, SMB, RDP, VNC, Tor, VPN, ICMP

Layer 3: DNS domain blocklist
  └─ Blocks tunnel services, paste services, file sharing, webhook capture

Layer 4: LuLu monitoring
  └─ Alerts on unexpected outbound connections for anomaly detection

Layer 5: Scoped credentials
  └─ Sandbox user has limited GitHub key, limited API permissions
  └─ If stolen, blast radius is small
```

---

## Maintaining the Blocklist

### Adding New Blocked Ports

Edit `/etc/pf.anchors/agent`, then reload:
```bash
sudo pfctl -f /etc/pf.conf
```

### Adding New Blocked Domains

Depends on your DNS blocking method:
- **NextDNS:** Add via web dashboard
- **dnsmasq:** Add `address=/newdomain.com/0.0.0.0` to config, restart: `sudo brew services restart dnsmasq`
- **/etc/hosts:** Add `0.0.0.0 newdomain.com`, flush cache: `sudo dscacheutil -flushcache`

### Checking What's Being Blocked

```bash
# pf blocked packets
sudo tcpdump -n -e -i pflog0

# pf stats
sudo pfctl -si

# DNS resolution test
dig tunnel-service.example.com  # should return 0.0.0.0 if blocked
```
