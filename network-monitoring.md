# Network Monitoring & Firewalling

Supplement any tier with network visibility and control.

## Anthropic API IP Addresses (For Firewall Rules)

Anthropic publishes fixed IP addresses for firewall configuration:

- **Inbound (API endpoints):** `160.79.104.0/23` (IPv4), `2607:6bc0::/48` (IPv6)
- **Outbound:** `160.79.104.0/21` (IPv4)

Source: [Anthropic API IP Addresses](https://platform.claude.com/docs/en/api/ip-addresses)

## LuLu (Free, Open Source)

Per-process outbound firewall by Objective-See.

- **Per-process rules:** Rules specified by binary path + remote address/domain + port + allow/block
- **Block-all-except-whitelist pattern:**
  1. Create a **block** rule for the process with remote address `*`
  2. Create an **allow** rule for the process with remote address `api.anthropic.com`
  3. More specific rules take precedence
- **Block Mode:** Global mode where all traffic is blocked unless explicitly allowed
- **Regex support:** Remote address field supports regular expressions
- **Limitation:** Only monitors outgoing traffic

Install: Download from [objective-see.org/products/lulu.html](https://objective-see.org/products/lulu.html)

## Little Snitch ($59)

More polished per-process firewall with granular rules.

- Domain-level filtering per application
- Silent mode: auto-deny all connections not covered by rules
- DNS encryption (v6+) and integrated blocklists
- Connection inspector for real-time monitoring

## pf (Packet Filter) — Built-in, Free

macOS includes `pf` (derived from OpenBSD 4.5). The most powerful free option.

### Per-User Rules

pf supports the `user` keyword, matching packets by socket UID:

```
# /etc/pf.anchors/agentsandbox

# Block all outgoing from sandboxed user
block out quick proto tcp from any to any user claudeagent
block out quick proto udp from any to any user claudeagent

# Allow DNS
pass out quick proto udp from any to any port 53 user claudeagent
pass out quick proto tcp from any to any port 53 user claudeagent

# Allow Anthropic API
pass out quick proto tcp from any to 160.79.104.0/23 port 443 user claudeagent

# Allow GitHub
pass out quick proto tcp from any to 140.82.112.0/20 port 443 user claudeagent

# Allow npm registry (Cloudflare IPs)
pass out quick proto tcp from any to 104.16.0.0/12 port 443 user claudeagent
```

### Loading Rules

Add to `/etc/pf.conf`:
```
anchor "agentsandbox"
load anchor "agentsandbox" from "/etc/pf.anchors/agentsandbox"
```

Enable:
```bash
sudo pfctl -f /etc/pf.conf
sudo pfctl -e
```

### Limitations

- `user` keyword only works for TCP and UDP
- Domain names resolved to IPs at rule-load time only (not dynamically)
- Cannot filter by process name or PID — only by UID
- Rules don't persist across macOS upgrades (system resets `pf.conf`)

### Persistence via LaunchDaemon

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

## Other Firewall Options

| Tool | Cost | Notes |
|------|------|-------|
| **Vallum** | $15 | Identifies processes by signature fingerprint (not just path). pf-inspired syntax. |
| **AppFirewall** | Free (open source) | Whitelist mode, hostname-list-based filtering. Works by TCP RST injection. |
| **Murus** | Free / $29 pro | GUI frontend for pf. Makes rule creation easier. |

## Tailscale for Device-Level Isolation

If running the agent in a VM with Tailscale installed:

- Configure Tailscale ACLs so the VM can only reach Anthropic API endpoints
- Tag devices and create network-layer rules
- `ExitNodeID` policy can force traffic through a specific exit node for inspection
- Operates at device level, not process level — all processes in the VM share the same network config

Best pattern: VM + Tailscale ACLs = device-level network isolation + VM-level filesystem isolation.

## iptables (Inside Linux VMs)

For Lima, Lume, or Tart Linux guests:

```bash
iptables -P OUTPUT DROP
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -p tcp -d 160.79.104.0/23 --dport 443 -j ACCEPT
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp -d 140.82.112.0/20 --dport 443 -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
```
