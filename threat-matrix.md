# Threat Matrix: What Each Tier Protects Against

## Protection Matrix

| Attack Vector | Tier 0 (Built-in) | Tier 1 (Seatbelt) | Tier 2 (User+pf) | Tier 3 (Docker) | Tier 4 (VM) |
|--------------|-------------------|-------------------|-------------------|-----------------|-------------|
| Agent reads `~/.ssh`, `~/.aws` | Partial (denyRead) | **Yes** | **Yes** | **Yes** | **Yes** |
| Agent exfiltrates via `curl` | Partial (proxy) | **Yes** (no network) | **Yes** (pf) | **Yes** | **Yes** |
| Malicious CLAUDE.md runs `rm -rf ~` | No | Partial (write deny) | **Yes** (separate user) | **Yes** | **Yes** |
| Infostealer targets `~/.claude/` | No | No | **Yes** (separate home) | **Yes** | **Yes** |
| Prompt injection via MCP server | Partial | Partial | Partial | **Yes** | **Yes** |
| Supply chain (malicious npm package) | No | No | Partial | Partial | **Yes** (snapshot) |
| Agent modifies `~/.zshrc` | Partial | **Yes** | **Yes** | **Yes** | **Yes** |
| Agent accesses Keychain | No | **Yes** (Seatbelt) | **Yes** (separate user) | **Yes** | **Yes** |
| Agent escalates via Docker socket | No | No | N/A | **Yes** (if not mounted) | **Yes** |
| Full system compromise | No | No | No | Partial | **Yes** (VM boundary) |
| Agent consumes all CPU/memory | No | No | No | **Yes** (resource limits) | **Yes** |
| Agent installs persistent backdoor | No | Partial | **Yes** (limited user) | **Yes** (ephemeral) | **Yes** (snapshot) |
| Network exfiltration via DNS | No | No | Partial (pf) | **Yes** (proxy) | **Yes** (iptables) |
| Agent reads browser cookies/passwords | No | **Yes** (path deny) | **Yes** (separate user) | **Yes** | **Yes** |

## Threat Descriptions

### Credential Theft
**Risk:** Agent reads SSH keys, cloud credentials, API tokens from well-known paths.
**Real incident:** Vidar infostealer targeting `~/.openclaw/` (Feb 2026).
**Mitigation:** Tier 1+ (Seatbelt path denials) or Tier 2+ (separate user).

### Data Exfiltration
**Risk:** Agent sends sensitive file contents to an external server via curl, wget, or DNS tunneling.
**Real incident:** PromptArmor `.docx` exfiltration (Jan 2026).
**Mitigation:** Tier 1+ (network deny) or Tier 2+ (pf firewall).

### Destructive Commands
**Risk:** Agent runs `rm -rf`, overwrites config files, or corrupts data.
**Real incident:** Wolak `rm -rf /` (Oct 2025), Reddit home dir deletion (Dec 2025).
**Mitigation:** Tier 1+ (write restrictions) or Tier 2+ (separate user).

### Supply Chain Poisoning
**Risk:** Malicious CLAUDE.md, compromised MCP servers, or poisoned npm packages.
**Real incident:** ClawHavoc 824+ malicious skills (Feb 2026), CVE-2025-59536.
**Mitigation:** Tier 3+ (container isolation) or Tier 4 (VM with snapshot rollback).

### Persistent Backdoor
**Risk:** Agent modifies shell config (`.zshrc`, `.bashrc`), cron jobs, or LaunchAgents.
**Mitigation:** Tier 2+ (sandbox user's configs only) or Tier 3+ (ephemeral containers).

### Lateral Movement
**Risk:** Compromised agent discovers and attacks other services on localhost or LAN.
**Real incident:** ClawJacked WebSocket hijack (Feb 2026).
**Mitigation:** Tier 2+ (pf network restrictions) or Tier 3+ (isolated network).

### Resource Exhaustion
**Risk:** Agent forks excessively, fills disk, or consumes all memory.
**Mitigation:** Tier 3+ (Docker resource limits) or Tier 4 (VM resource allocation).

## Risk-Based Tier Selection

### Low Risk (Tier 0-1)
- Working on your own private code
- No MCP servers connected
- No sensitive files in the project
- You're watching the terminal

### Medium Risk (Tier 2-3)
- Working on repos with third-party dependencies
- MCP servers connected
- Sensitive files exist on the machine
- Running unattended for short periods

### High Risk (Tier 4)
- Working on unfamiliar/untrusted repositories
- Running fully autonomous (24/7)
- Handling sensitive data or credentials
- Multiple agents running concurrently
- Connected to external messaging channels
