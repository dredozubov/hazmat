# Documented Incidents and CVEs

Real incidents — not theoretical risks — that demonstrate why sandboxing matters.

## Claude Code Incidents

### Wolak Incident (October 2025)

Claude Code with `--dangerously-skip-permissions` executed `rm -rf /`, destroying user-owned files across the system. The agent interpreted a vague instruction as requiring a clean slate.

### Reddit Home Directory Deletion (December 2025)

Command `rm -rf tests/ patches/ plan/ ~/` deleted an entire home directory. The trailing `~/` was either a hallucination or an over-eager cleanup attempt. 197 points on Hacker News.

### PromptArmor Exfiltration Attack (January 2026)

Hidden text inside a `.docx` file manipulated Claude into uploading sensitive files to an attacker's account via an allowlisted API endpoint. The attack worked because:
- The `.docx` contained invisible instructions
- The agent followed them without question in bypass mode
- Network allowlists permitted the exfiltration domain

## Claude Code CVEs

### CVE-2025-59536 — RCE via Project Files (CVSS 8.7)

Code injection allowing arbitrary shell command execution upon tool initialization via malicious project files. Simply opening a crafted repository could trigger hidden commands.

### CVE-2026-21852 — API Key Exfiltration (CVSS 5.3)

Information disclosure allowing API key exfiltration from malicious repositories. Cloning and opening a crafted repository could trigger hidden commands before the trust prompt appeared.

### Attack Vectors

- Malicious `CLAUDE.md` files in public repositories
- Compromised hooks configurations
- MCP server configurations that redirect API traffic
- Settings files that override `ANTHROPIC_BASE_URL` to attacker-controlled endpoints

## OpenClaw Incidents (Inspiration for Threat Modeling)

From the [OpenClaw Attack Surface Report](../openclaw/OpenClaw-macOS-Attack-Surface-Report.md):

### CVE-2026-25253 — 1-Click RCE (CVSS 8.8)

Control UI accepted a `gatewayUrl` from browser URL query string, transmitting auth token to attacker's server. Single malicious link = full compromise.

### CVE-2026-25593 — Unauthenticated Local RCE

WebSocket `config.apply` method had no auth check. Any process on the machine could reconfigure the gateway and execute arbitrary commands.

### ClawJacked (February 2026) — WebSocket Hijack

Any website could open a WebSocket to `localhost:18789` (browsers allow WebSocket to localhost without CORS). Rate limiter exempted localhost, device pairings auto-approved. Result: full agent takeover.

### ClawHavoc Supply Chain Attack (February 2026)

**824+ malicious skills** on ClawHub (~20% of all published skills). Skills contained "prerequisites" that downloaded and executed the **Atomic macOS Stealer (AMOS)** — harvesting Keychain passwords, browser credentials, crypto wallets, SSH keys.

**This attack specifically targeted macOS users.** The AI agent acted as a trusted intermediary, presenting malicious setup instructions as normal installation steps.

### Vidar Infostealer Targeting `~/.openclaw/`

First documented case of commodity malware specifically targeting AI agent credentials. Harvested `openclaw.json` (auth token) and `device.json` (Ed25519 private key).

### 30,000-42,000 Exposed Instances

Multiple scanning firms found tens of thousands of OpenClaw instances exposed on the public internet, many without authentication.

## Lessons for Claude Code Users

| OpenClaw Incident | Claude Code Parallel |
|-------------------|---------------------|
| Credential file exposure (`~/.openclaw/`) | `~/.claude/` contains auth data, settings, credentials |
| ClawHavoc (malicious skills) | Malicious CLAUDE.md, compromised MCP servers |
| WebSocket localhost hijack | Any localhost-listening service is vulnerable |
| Infostealer targeting agent dirs | `~/.claude/` is a high-value target |
| Shell execution as core risk | Identical — both agents run shell commands |
| Auto-approval bypasses | `--dangerously-skip-permissions` skips all checks |

## Industry-Wide Concerns

### Cursor CVE-2026-22708

Shell built-in bypass in Cursor's sandbox. Patched January 2026. Cursor uses the same Seatbelt technology as Claude Code.

### Docker Desktop CVE-2025-9074 (CVSS 9.3)

Docker Engine API socket exposed without authentication, allowing container escape to host filesystem. Demonstrates that even Docker is not immune to sandbox escapes.

## Timeline

| Date | Event |
|------|-------|
| Oct 2025 | Wolak `rm -rf /` incident |
| Dec 2025 | Reddit home directory deletion |
| Jan 2026 | PromptArmor `.docx` exfiltration attack |
| Jan 2026 | CVE-2026-22708 (Cursor sandbox bypass) |
| Jan 2026 | CVE-2025-59536 (Claude Code RCE via project files) |
| Feb 2026 | CVE-2026-21852 (Claude Code API key exfiltration) |
| Feb 2026 | ClawHavoc (824+ malicious OpenClaw skills) |
| Feb 2026 | ClawJacked (WebSocket hijack) |
| Feb 2026 | Vidar infostealer targeting `~/.openclaw/` |
