# Tier 0: Claude Code Built-in Sandbox

**Effort:** 2 minutes | **Performance:** Native | **Cost:** Free

Claude Code has native OS-level sandboxing using **macOS Seatbelt** (the same kernel framework Chrome, Cursor, and Codex use). It adds <15ms overhead per command.

## Enable It

Run `/sandbox` inside Claude Code, or configure in `~/.claude/settings.json`:

```json
{
  "sandbox": {
    "enabled": true,
    "filesystem": {
      "denyRead": ["~/.ssh", "~/.aws", "~/.gnupg", "~/.config/gh"],
      "allowWrite": []
    },
    "network": {
      "allowedDomains": ["api.anthropic.com", "sentry.io", "statsigapi.net"]
    },
    "allowUnsandboxedCommands": false
  }
}
```

## What It Does

- **Filesystem writes** restricted to CWD
- **Reads** allowed broadly except denied paths
- **Network** routed through a local proxy with domain allowlisting
- **Child processes** inherit all restrictions
- Reduces permission prompts by **84%** in Anthropic's internal usage

## What It Doesn't Do

- No resource limits (CPU, memory, disk)
- Network filtering is domain-based only — domain fronting can bypass it
- `allowUnixSockets` with Docker socket = sandbox escape
- Uses the deprecated `sandbox-exec` API (functional through macOS 26 Tahoe; see [macos-sandboxing-internals.md](macos-sandboxing-internals.md) for deprecation analysis)
- Does not isolate credentials in `~/.claude/` from other processes running as your user
- MCP child processes inherit all parent environment variables including API keys
- `~/.claude/settings.json` is writable — CVE-2026-25725 demonstrated sandbox escape via settings injection

## How Competitors Compare at Tier 0

Other AI tools ship similar built-in sandboxes, all using the same deprecated Seatbelt APIs:

| Tool | OS Support | Network Default | Unique Feature |
|------|-----------|-----------------|----------------|
| **Claude Code** `/sandbox` | macOS (Seatbelt) | Domain allowlist via proxy | 84% fewer permission prompts |
| **Codex CLI** (OpenAI) | macOS + Linux + Windows | **Network blocked by default** | Strongest default; Rust rewrite |
| **Gemini CLI** (Google) | macOS (Seatbelt) | Six profiles (permissive/restrictive x open/closed/proxied) | Most configurable |
| **Cursor** | macOS + Linux + Windows | Approval-gated | Dynamic policies from workspace settings |

Codex CLI has the strongest Tier 0 story: three-platform OS-native sandboxing with network off by default. Claude Code's `/sandbox` is the weakest: macOS only, domain-based network filtering, and the March 2026 source code leak ([512K lines exposed via npm source map](https://venturebeat.com/technology/claude-codes-source-code-appears-to-have-leaked-heres-what-we-know)) revealed all 23 sequential bash security validators, every feature flag, and the complete permission model.

**Key insight from UK AISI SandboxEscapeBench (March 2026):** GPT-5 escapes container sandboxes ~50% of the time at ~$1/escape. Single-layer sandboxes (Tier 0) are becoming insufficient as model capabilities improve. This validates multi-layer approaches like Hazmat's Tier 2 (user isolation + kernel sandbox + network firewall). ([Source](https://www.aisi.gov.uk/blog/can-ai-agents-escape-their-sandboxes-a-benchmark-for-safely-measuring-container-breakout-capabilities), [Paper](https://arxiv.org/abs/2603.02277))

## Critical Setting

Set `"allowUnsandboxedCommands": false` — this disables the escape hatch that lets commands fall through to unsandboxed execution when they fail inside the sandbox.

## Two Sandbox Modes

- **Auto-allow**: sandboxed commands run without permission prompts
- **Regular permissions**: sandboxed commands still require approval

## Open Source

The sandbox runtime is available as an npm package:

```bash
npx @anthropic-ai/sandbox-runtime <command-to-sandbox>
```

Source: [github.com/anthropic-experimental/sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime)

## When This Is Enough

Tier 0 is sufficient when:
- You're working on your own code
- The repo has no untrusted CLAUDE.md or MCP configs
- You don't need to protect against malware already on your system
- You accept that `~/.claude/` credentials are still accessible to other user-level processes
