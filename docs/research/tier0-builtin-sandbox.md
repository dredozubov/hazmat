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
- Still uses the deprecated `sandbox-exec` API (functional but unsupported by Apple)
- Does not isolate credentials in `~/.claude/` from other processes running as your user

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
