# Overview: Sandboxing Claude Code Auto Mode on macOS

## The Core Problem

Claude Code in auto mode (`--dangerously-skip-permissions`) bypasses all permission checks — file writes, shell commands, network calls, MCP server trust — everything runs without prompting. A single prompt injection, malicious CLAUDE.md, or compromised MCP server can execute arbitrary commands with your full user privileges.

You need a **blast radius limiter** — something that contains the damage when (not if) something goes wrong.

## Tier Overview

| Tier | Isolation | Effort | Performance | Cost | Best For |
|------|-----------|--------|-------------|------|----------|
| **0** | Claude Code built-in `/sandbox` | 2 min | Native | Free | Daily work on trusted repos |
| **1** | Seatbelt wrappers (nono, claude-sandbox) | 5 min | Native | Free | Tighter filesystem + network control |
| **2** | Dedicated macOS user + pf firewall | 30 min | Native | Free | Strong isolation without VMs |
| **3** | Docker Sandboxes / Devcontainers | 15 min | ~90% native | Free (Docker Desktop personal) | Best balance of security + UX |
| **4** | Full VM (Lima / Lume / Tart) | 45 min | ~85-95% native | Free | Maximum isolation, autonomous agents |

For the dedicated-user setup in [setup-option-a.md](setup-option-a.md), the key UX improvement is a host-side command surface:

- `claude-sandbox` launches Claude directly in the sandbox
- `agent-shell` opens an interactive sandboxed shell
- `agent-exec` runs one-off tools like `make`, `npx`, `uv`, and `uvx`

That keeps the security boundary intact without making the user live inside `sudo -u agent -i`.

## Decision Flowchart

```
Do you trust the codebase you're working on?
|
+-- Yes, it's my own code
|   +-- Tier 0 (built-in /sandbox) is sufficient
|       Add Tier 1 (nono) for extra safety
|
+-- Mostly, but it has third-party deps / MCP servers
|   +-- Tier 2 (dedicated user + pf) or Tier 3 (Docker Sandbox)
|
+-- No, it's an unfamiliar repo / open source contribution
|   +-- Tier 3 (Docker Sandbox) minimum
|       Tier 4 (VM) recommended
|
+-- Running fully autonomous (24/7, no supervision)
    +-- Tier 4 (VM) required
        With golden snapshot for rollback
        With iptables/pf network lockdown
```

## Claude Code Permission Modes

Claude Code has five permission modes, from most restrictive to most permissive:

| Mode | Behavior |
|------|----------|
| **Plan Mode** | Read-only. Claude can analyze but cannot modify anything. |
| **Normal Mode** | Default. Prompts for confirmation before every sensitive operation. |
| **Auto-accept Edits** | Auto-approves file read/write. Shell commands still require validation. (Shift+Tab) |
| **Don't Ask Mode** | Auto-denies all tool usage unless pre-approved via `/permissions`. |
| **Bypass Mode** (`--dangerously-skip-permissions`) | Skips ALL permission checks. Everything runs without prompting. |

## Why Sandboxing Matters

The permission system is a **software guardrail**, not a security boundary. It can be bypassed by:

- Prompt injection via malicious files in the repo (CLAUDE.md, .docx, comments in code)
- Compromised MCP servers
- Malicious npm packages running post-install scripts
- CVEs in Claude Code itself (CVE-2025-59536, CVE-2026-21852)

Sandboxing provides an **OS-level or hardware-level boundary** that cannot be bypassed by the agent regardless of what instructions it receives.
