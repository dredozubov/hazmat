# Supply Chain Hardening Brief

**Date:** 2026-03-31
**Context:** axios npm compromise (axios/axios#10604) — maintainer account hijacked, malicious versions 1.14.1 and 0.30.4 published with a RAT delivered via `postinstall` hook in an injected dependency (`plain-crypto-js`). Payload executed within 2 seconds of `npm install`, before dependency resolution finished. Dropper self-deleted and rewrote its own `package.json` to evade forensics.

**Audience:** hazmat contributors

---

## What hazmat already mitigates

The axios attack deploys a cross-platform RAT that phones home to a C2 server and provides shell access, file enumeration, and binary injection. Against a hazmat-contained agent:

| Attack step | Hazmat mitigation | Gap? |
|---|---|---|
| `postinstall` runs as agent user | User isolation: no access to main user's `~/.ssh`, `~/.aws`, keychains | No gap |
| RAT writes to `/Library/Caches/com.apple.act.mond` | Seatbelt denies writes outside allowed paths | No gap |
| RAT beacons to `sfrclak.com:8000` | pf blocks non-443 egress; DNS blocklist is bypassable but port 8000 is blocked | No gap for this specific C2 |
| RAT exfils data over HTTPS to novel domain | **pf allows all port 443** | **Gap** |
| Malicious package reads exported API keys | MCP/child processes inherit full env | **Gap** |
| Malicious package reads project source | Seatbelt allows project dir read+write | By design (agent needs project access) |

**Bottom line:** hazmat blocks this specific RAT's persistence and C2 channel, but the general attack pattern (dependency runs code at install time with inherited credentials and HTTPS egress) is only partially mitigated.

---

## Recommended changes

### 1. Block `postinstall` scripts by default

The axios payload was delivered entirely through npm's `postinstall` hook. The agent should never run arbitrary lifecycle scripts from dependencies.

**bootstrap.go** — add to agent's `~/.npmrc`:
```
ignore-scripts=true
```

**Impact:** Some legitimate packages need postinstall (e.g., `esbuild`, `sharp`, `bcrypt` for native compilation). Document an allowlist mechanism:
```
# In project's .npmrc or via CLI override:
# npm install --ignore-scripts=false sharp
```

**Why not optional:** The axios attack executed in 2 seconds — before a human could intervene. The dropper self-deleted. There is no detection window. Prevention is the only viable control.

### 2. Add `sfrclak.com` (and future C2 domains) to DNS blocklist

**init.go** `hostsBlocklistContent` — add a "known C2" section:
```
# Known supply-chain C2 domains
0.0.0.0 sfrclak.com
```

This is reactive (only helps after IOCs are published), but it costs nothing and compounds over time.

### 3. Scope credentials per MCP server

Currently every MCP child inherits the full parent environment. A compromised npm package at MCP spawn time gets every exported API key.

**Proposed:** `hazmat claude` should scrub the environment before exec, passing only:
- `HOME`, `PATH`, `SHELL`, `TERM`, `USER`, `LANG`
- `ANTHROPIC_API_KEY` (required for Claude itself)
- Explicitly declared per-project vars (from a manifest or `.hazmat-env`)

Everything else (`GITHUB_PERSONAL_ACCESS_TOKEN`, `OPENROUTER_API_KEY`, `SSH_AUTH_SOCK`, etc.) should be opt-in per session or per MCP server, not inherited by default.

**SSH_AUTH_SOCK specifically:** Unset it in the agent environment. If a project needs git-over-ssh, prefer a Hazmat-managed per-project selection of a dedicated key from a chosen host-owned directory that stays in host-owned storage and is loaded into a fresh session-local `ssh-agent` at launch time, rather than forwarding the main user's agent socket.

### 4. Pin MCP server versions

Claude spawns MCP servers via `npm exec @modelcontextprotocol/server-github` with no version lock. A compromised MCP package gets immediate code execution.

**Proposed:** hazmat should generate a `~/.claude/mcp-lockfile.json` (or equivalent) that pins MCP server packages to exact versions with integrity hashes. The pre-tool-use hook can enforce this:

```bash
# pre-tool-use.sh — block MCP spawns that don't match lockfile
tool=$(echo "$HOOK_INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('tool_name',''))")
if [ "$tool" = "Bash" ]; then
  cmd=$(echo "$HOOK_INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('tool_input',{}).get('command',''))")
  if echo "$cmd" | grep -qE "npm exec|npx |uv tool|uvx "; then
    echo "MCP server spawn blocked — use pinned versions via hazmat mcp-lock"
    exit 2
  fi
fi
```

This is a blunt instrument. A proper implementation would verify the resolved version against the lockfile rather than blocking outright.

### 5. Monitor network egress during install

The axios RAT beaconed to port 8000, which pf already blocks. But a more sophisticated attacker would use port 443. Since blocking all HTTPS is not feasible:

**Proposed:** Add an `--audit-install` mode to `hazmat exec` that:
1. Snapshots open connections before `npm install`
2. Runs install with `--ignore-scripts`
3. Diffs new outbound connections during a subsequent `npm rebuild`
4. Flags any connections to hosts not in an allowlist (npm registry, github.com, etc.)

This is detection, not prevention — but it closes the forensic gap that the axios dropper exploited (self-deletion made post-mortem analysis impossible).

### 6. Verify Claude Code installation integrity

`bootstrap.go` pipes `curl | bash` with no verification. If `claude.ai` is compromised or MITM'd, the agent binary itself is backdoored.

**Proposed:** After installation, verify the binary:
```bash
# Compare installed binary hash against published hash from a second source
npm view @anthropic-ai/claude-code dist.integrity
shasum -a 512 ~/.local/share/claude/versions/$(claude --version)
```

Or pin to a known-good version in bootstrap and skip the install script entirely:
```bash
npm install -g @anthropic-ai/claude-code@2.1.87
```

---

## What NOT to do

- **Don't block npm registry access.** The agent needs to install project dependencies. Blocking npm breaks normal development workflows.
- **Don't add `plain-crypto-js` to any blocklist.** It's an attacker-created package, not a legitimate dependency. It will never appear in a clean dependency tree. Blocking it gives false confidence.
- **Don't rely on `npm audit` for detection.** The axios dropper replaced its own `package.json` and deleted `setup.js`. Post-infection forensics via audit/lockfile are unreliable. Real-time monitoring during install is the only reliable detection.
- **Don't add behavioral rules to Claude's deny list for this.** Rules like `Bash(npm install*)` break normal workflows. The OS-level controls (ignore-scripts, user isolation, seatbelt) are the correct enforcement layer.

---

## Priority order

| # | Change | Effort | Impact |
|---|---|---|---|
| 1 | `ignore-scripts=true` in agent npmrc | Trivial | Blocks entire class of npm postinstall attacks |
| 2 | Scrub env before exec (credential scoping) | Medium | Limits blast radius of any compromised dependency |
| 3 | Add known C2 to DNS blocklist | Trivial | Reactive, but free |
| 4 | Pin MCP server versions | Medium | Prevents supply-chain attacks at MCP spawn |
| 5 | Audit-install mode | High | Detection for HTTPS exfil during install |
| 6 | Verify Claude binary integrity | Low | Prevents compromised bootstrap |

---

## References

- [StepSecurity: axios Compromised on npm](https://www.stepsecurity.io/blog/axios-compromised-on-npm-malicious-versions-drop-remote-access-trojan)
- [GitHub Issue: axios/axios#10604](https://github.com/axios/axios/issues/10604)
- [hazmat attack-surface-deep-dive.md](research/attack-surface-deep-dive.md) — MCP credential inheritance, npm exec risks
- [hazmat threat-matrix.md](threat-matrix.md) — HTTPS exfil gap
- [hazmat design-assumptions.md](design-assumptions.md) — trust model
