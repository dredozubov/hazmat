# Tier 1: Seatbelt Wrappers

**Effort:** 5 minutes | **Performance:** Native | **Cost:** Free

Open-source tools that wrap `sandbox-exec` with curated profiles for AI agents. They provide tighter control than Claude Code's built-in sandbox.

## nono (Recommended)

Kernel-enforced sandbox using Seatbelt on macOS, Landlock on Linux.

```bash
brew tap always-further/nono && brew install nono
nono run --profile claude-code -- claude
```

**Features:**
- Built-in Claude Code profile blocking `~/.ssh`, `~/.aws`, shell configs
- Separates read/write: `nono run --allow ./project --write ./output -- claude`
- Apache 2.0 license
- Early-stage but functional

Source: [github.com/always-further/nono](https://github.com/always-further/nono)

## claude-sandbox

macOS-only wrapper using `sandbox-exec`.

```bash
brew install kohkimakimoto/tap/claude-sandbox
claude-sandbox  # wraps claude with Seatbelt profile
```

**Default policy:**
- Allow everything readable
- Deny all writes
- Re-allow writes to CWD, `~/.claude`, `/tmp`
- No network isolation (filesystem write restriction only)
- Has an "unboxexec" escape hatch for whitelisted commands
- Configurable via `.claude/sandbox.toml`

Source: [claude-sandbox on DEV Community](https://dev.to/kohkimakimoto/claude-sandbox-yet-another-sandboxing-tool-for-claude-code-on-macos-o6n)

## agent-seatbelt-sandbox (Two-Layer Approach)

Combines Seatbelt kernel enforcement with a local HTTP proxy.

- Blocks all outbound network except localhost at kernel level
- Proxy logs all connection attempts and supports a live-updated domain blocklist
- Even if the agent unsets proxy env vars, kernel still blocks direct connections
- Includes tests for curl, Python, Node.js, git, pip

Source: [github.com/michaelneale/agent-seatbelt-sandbox](https://github.com/michaelneale/agent-seatbelt-sandbox)

## ai-jail

Rust-based tool generating SBPL profiles dynamically.

- Comprehensive Node.js compatibility (PTY, ioctl, Mach host, IPC)
- Lockdown mode: removes all network rules and file-write rules
- Explicitly denies `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/Library/Keychains`, `~/Library/Mail`

Source: [ai-jail macOS Seatbelt docs](https://deepwiki.com/akitaonrails/ai-jail/4.5-macos:-seatbelt-sandboxing)

## Trail of Bits Seatbelt Sandboxer Plugin

A Claude Code plugin that applies Seatbelt profiles.

Source: [github.com/trailofbits/skills/tree/main/plugins/seatbelt-sandboxer](https://github.com/trailofbits/skills/tree/main/plugins/seatbelt-sandboxer)

## Gemini CLI's Seatbelt Integration (Reference)

Gemini CLI has six built-in profiles worth studying:

| Profile | Network | Filesystem |
|---------|---------|-----------|
| `permissive-open` | Full | Broad |
| `permissive-closed` | None | Broad |
| `permissive-proxied` | Proxied | Broad |
| `restrictive-open` | Full | Project only |
| `restrictive-closed` | None | Project only |
| `restrictive-proxied` | Proxied | Project only |

Custom profiles stored at `.gemini/sandbox-macos-<name>.sb`. Configured via `SEATBELT_PROFILE` env var.

## When to Use Tier 1

- You want tighter filesystem isolation than Tier 0
- You want to block network access entirely (not just domain-filter it)
- You're working on repos you mostly trust but want extra protection
- You want zero performance overhead
