# Network Monitoring & Firewalling

Supplement any tier with network visibility and control.

## All Confirmed Network Endpoints (v2.1.77, March 2026)

From live process inspection + static binary analysis. Allowlist rules should account for all of these.

### Anthropic infrastructure

| Endpoint / IP | Purpose | Notes |
|---|---|---|
| `api.anthropic.com` | Inference API, OAuth, metrics, feedback, session transcripts | Primary destination |
| `160.79.104.0/23` (IPv4) | Anthropic API inbound | Published by Anthropic |
| `2607:6bc0::/48` (IPv6) | Anthropic API inbound IPv6 | Published by Anthropic |
| `160.79.104.0/21` (IPv4) | Anthropic API outbound | Published by Anthropic |
| `mcp-proxy.anthropic.com/v1/mcp/{server_id}` | Anthropic-hosted remote MCP server proxy | Undocumented; loads remote MCP servers without local install |

Source: [Anthropic API IP Addresses](https://platform.claude.com/docs/en/api/ip-addresses)

### Telemetry and feature flags (5 separate services)

All five are confirmed present in the binary. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` suppresses Datadog, Segment, and Statsig; does not suppress Sentry (crash reporting) or Anthropic inference.

| Service | Endpoint | IP / Host | Gate | Content |
|---|---|---|---|---|
| **Datadog** | `logs.us5.datadoghq.com` | `34.149.66.137` (Google Cloud Run) | `tengu_log_datadog_events` | Event stream, metrics |
| **Segment.io** | `api.segment.io` | | `tengu_log_segment_events` | Named events (e.g., `tengu_voice_toggled`) |
| **Statsig** (4 endpoints) | `statsig.com`, `featuregates.org`, `featureassets.org`, `api.statsig.com` | All GCP `34.128.128.0` | — | Feature flag / A/B test evaluation; Anthropic can flip flags without a binary update |
| **GrowthBook** | `cdn.growthbook.io` | | — | Second feature flag CDN, separate from Statsig |
| **Sentry** | `mcp.sentry.dev/mcp` + `sentry.io` | | — | Crash/error reporting |
| **Anthropic metrics** | `api.anthropic.com/api/claude_code/metrics` | | org-level opt-in | Usage data |

**Key point:** Statsig uses 4 distinct hostnames, all resolving to the same GCP block (`34.128.128.0`). A pf rule allowing that /32 covers all four, but domain-based filters need all four entries. GrowthBook is a fifth feature-flag service operating independently.

**Telemetry gate mechanism:** Datadog and Segment gates (`tengu_log_*`) are checked against Anthropic's servers at runtime — Anthropic can enable or disable them without a binary update. You cannot locally suppress them except via `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`.

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
# Updated March 2026 — covers all confirmed Claude Code endpoints

# Block all outgoing from sandboxed user (default deny)
block out quick proto tcp from any to any user claudeagent
block out quick proto udp from any to any user claudeagent

# Allow DNS
pass out quick proto udp from any to any port 53 user claudeagent
pass out quick proto tcp from any to any port 53 user claudeagent

# Allow Anthropic API (inference, OAuth, metrics, remote MCP proxy)
pass out quick proto tcp from any to 160.79.104.0/23 port 443 user claudeagent

# Allow GitHub (MCP server, git operations)
pass out quick proto tcp from any to 140.82.112.0/20 port 443 user claudeagent

# Allow npm registry (Cloudflare IPs — for MCP server spawning via npm exec)
pass out quick proto tcp from any to 104.16.0.0/12 port 443 user claudeagent

# Allow PyPI / uv (for Python MCP servers via uvx)
# pypi.org, files.pythonhosted.org — FastAPI CDN on AWS CloudFront
pass out quick proto tcp from any to 99.84.0.0/16 port 443 user claudeagent

# ---- Telemetry (allow or block per threat model) ----
# Datadog (Google Cloud Run — logs.us5.datadoghq.com)
# pass out quick proto tcp from any to 34.128.0.0/10 port 443 user claudeagent

# Statsig feature flags (4 endpoints, same GCP block: statsig.com, featuregates.org,
#   featureassets.org, api.statsig.com — all resolve to 34.128.128.0)
# pass out quick proto tcp from any to 34.128.128.0/32 port 443 user claudeagent

# GrowthBook feature flags (cdn.growthbook.io — Cloudflare)
# Covered by Cloudflare rule above if 104.16.0.0/12 includes it; verify with dig
```

**Telemetry blocking:** Commenting out the Datadog/Statsig rules (as shown above) blocks telemetry silently. Claude continues to work — `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` is the cleaner approach as it stops the outbound attempt entirely rather than letting pf drop packets.

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
