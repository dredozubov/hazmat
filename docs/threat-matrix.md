# Threat Matrix: Which Tier Blocks Which Risk

Use this file after [overview.md](overview.md) when you already know the candidate tiers and want to ask: "Does this tier actually stop the problem I care about?"

## Fast Selection by Primary Concern

| Primary concern | Minimum sensible tier | Why | Read next |
|-----------------|-----------------------|-----|-----------|
| Protect `~/.ssh`, cloud creds, or `~/.claude` from agent access | Tier 2 | Separate macOS user changes what the agent can reach | [attack-surface-deep-dive.md](research/attack-surface-deep-dive.md) |
| Prevent easy network exfiltration | Tier 2 | Separate-user `pf` helps, but Docker changes the story | [soft-pf-blocklist.md](research/soft-pf-blocklist.md) |
| Run Docker or Compose safely | Tier 3 | Shared host daemon is the wrong boundary | [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) |
| Recover cleanly from compromise or long autonomy | Tier 4 | Snapshot rollback is the strongest recovery model | [tier4-vm-isolation.md](tier4-vm-isolation.md) |
| Keep native performance on non-Docker work | Tier 2 | Strongest no-VM option with manageable friction | [setup-option-a.md](setup-option-a.md) |

## Protection Matrix

Legend:

- `Yes` means the tier is designed to address the problem directly.
- `Partial` means it helps, but important bypasses or caveats remain.
- `No` means it should not be relied on for that threat.

### Filesystem and Credential Surface

| Attack vector | Tier 0 | Tier 1 | Tier 2 | Tier 3 | Tier 4 |
|--------------|--------|--------|--------|--------|--------|
| Agent reads `~/.ssh`, `~/.aws` | Partial | Yes | Yes | Yes | Yes |
| Infostealer targets `~/.claude/` | No | No | Yes | Yes | Yes |
| Agent modifies `~/.zshrc` or similar | Partial | Yes | Yes | Yes | Yes |
| Agent accesses browser cookies or saved credentials | No | Partial | Yes | Yes | Yes |
| Agent accesses Keychain-adjacent material | No | Partial | Yes | Yes | Yes |
| MCP child processes inherit agent user's full shell env (API keys, SSH_AUTH_SOCK) | No | No | No¹ | No¹ | No¹ |
| MEMORY.md injection → cross-session persistent instructions | No | No | No | No | Yes² |
| Any main-user process reads `~/.claude/history.jsonl`, `paste-cache/`, `~/.claude.json` | No | No | No⁵ | No⁵ | No⁵ |
| Settings misconfiguration removes permission gates (`skipDangerousModePermissionPrompt`) | No | No | Partial⁶ | Partial⁶ | Yes⁷ |
| World-writable `git-worktrees.json` (0666) → worktree injection | No | No | Partial⁸ | Partial⁸ | Yes⁷ |
| Agent writes `.hazmat/integrations.yaml` to recommend integrations for next session | N/A | N/A | Partial⁹ | Partial⁹ | Yes⁷ |
| Host-level attacker pre-fills `~/.hazmat/integration-approvals.yaml` to bypass integration approval prompt | N/A | N/A | No¹⁰ | No¹⁰ | No¹⁰ |

### Network and Localhost Surface

| Attack vector | Tier 0 | Tier 1 | Tier 2 | Tier 3 | Tier 4 |
|--------------|--------|--------|--------|--------|--------|
| Agent exfiltrates via `curl` or `wget` | Partial | Yes | Yes | Yes | Yes |
| Network exfiltration via DNS | No | No | Partial | Yes | Yes |
| Lateral movement on localhost or LAN | No | Partial | Partial | Yes | Yes |
| Prompt injection via MCP server | Partial | Partial | Partial | Yes | Yes |
| SSH agent socket abuse (SSH_AUTH_SOCK → auth to remote servers without key exfil) | No | No | No³ | No³ | No³ |
| MCP supply chain: compromised npm/PyPI package exfiltrates credentials at spawn time | No | No | Partial⁴ | Partial⁴ | Partial⁴ |

### Escape, Persistence, and Recovery Surface

| Attack vector | Tier 0 | Tier 1 | Tier 2 | Tier 3 | Tier 4 |
|--------------|--------|--------|--------|--------|--------|
| Malicious repo content triggers destructive shell commands | No | Partial | Yes | Yes | Yes |
| Supply-chain install scripts or poisoned dependencies | No | No | Partial | Partial | Yes |
| Agent installs a persistent backdoor | No | Partial | Yes | Yes | Yes |
| Agent escalates via shared Docker socket | No | No | No | Yes | Yes |
| Agent consumes all CPU or memory | No | No | No | Yes | Yes |
| Full host compromise | No | No | No | Partial | Yes |

## Matrix Footnotes

1. **MCP env inheritance is not addressed by any tier.** All tiers run MCP server subprocesses that inherit the agent user's shell environment. Tier 2 limits *which user's* environment is exposed (agent user, not main user), but if the agent user has API keys or SSH_AUTH_SOCK in its shell profile, those reach every MCP child. `CLAUDE_CODE_DONT_INHERIT_ENV=1` does not suppress this (confirmed by testing). Mitigation: export only the specific credentials each MCP server legitimately needs, or use `env -i` to strip-and-reconstruct the environment before launching Claude.

2. **MEMORY.md persistence is only cleanly addressed by Tier 4 snapshot rollback.** Lower tiers cannot distinguish a poisoned memory entry from a legitimate user preference. Detection requires diffing the memory files after each session.

3. **SSH agent abuse bypasses all filesystem-based isolation.** No tier removes `SSH_AUTH_SOCK` from the environment. An MCP server or a compromised subprocess with code execution can authenticate to any SSH server the user's key has access to, without reading the key from disk. Mitigation: don't export `SSH_AUTH_SOCK` in the agent user's shell, or use a separate SSH key with limited access for agent sessions.

4. **pf egress filtering reduces but does not eliminate supply-chain exfil risk.** A pf allowlist blocks exfiltration to unknown destinations, but an attacker routing through an allowlisted CDN (e.g., Cloudflare, AWS CloudFront, Google Cloud Run) bypasses it. The MCP subprocess still inherits all credentials at spawn time. Mitigation: lockfile pinning for MCP packages (not currently supported by Claude's MCP config format) and vetting installed packages.

5. **`~/.claude/` disk artifacts are not protected from main-user processes by any tier.** Tier 2 protects the agent user from reading the main user's `~/.claude/`. But any process running as the main user — including Claude itself in non-Tier-2 setups, other tools, or a compromised browser — can read `~/.claude/history.jsonl` (full prompt history), `~/.claude.json` (all credentials and project paths), and `~/.claude/paste-cache/` (all pasted content). These files accumulate indefinitely. Mitigation: periodic rotation of `history.jsonl` and `paste-cache/`; treat `~/.claude.json` as a credential file and protect it accordingly.

6. **Settings misconfigurations are not addressed by any tier below Tier 4.** `~/.claude/settings.json` is owned by the main user and writable by the main user. Prompt injection or supply-chain code running as the main user can set `skipDangerousModePermissionPrompt: true`, removing the confirmation dialog for dangerous mode, or `autoAllowBashIfSandboxed: true`, removing per-command Bash approval. Tier 2 prevents the sandbox user from writing this file; it does not prevent the main user from having done so in a previous session. Mitigation: audit `~/.claude/settings.json` for these keys after any suspicious session.

7. **Tier 4 VM snapshot rollback restores `~/.claude/settings.json` and `git-worktrees.json` to clean state.** This is the only tier where these misconfigurations are cleaned up automatically.

8. **`git-worktrees.json` world-writable (0666) is a sandbox bypass.** If this file is 0666, the sandbox user can write it — injecting worktree entries that redirect Claude to attacker-controlled paths on the main user's next session. Fix: `chmod 600 ~/.claude/git-worktrees.json`. Verified permissions: 0666 observed on the research system.

9. **Agent can write `.hazmat/integrations.yaml` but cannot auto-approve it.** The project directory is agent-writable, so the agent can create or modify the repo recommendations file. However, approval runs as the invoking user before the sandbox starts — the agent user cannot write to `~/.hazmat/integration-approvals.yaml` (different UID, mode 0600). On the next session, the modified file triggers a re-prompt because the SHA-256 hash no longer matches the stored approval. The user sees exactly which integrations are requested and must explicitly approve. Integration validation (credential deny check, safe env allowlist, no executable code) provides the hard security boundary regardless of approval state. See [integrations.md](integrations.md) for the full approval flow.

10. **Integration approval does not protect against host-level attackers.** If an attacker has code execution as the host user, they can pre-fill `~/.hazmat/integration-approvals.yaml` to bypass the approval prompt. However, the blast radius is minimal: even with forged approvals, activated integrations can only add read-only dirs (checked against the credential deny list), pass through env vars from the fixed safe set, and change backup excludes. Integrations cannot widen write scope, expose credentials, execute code, or change network policy. An attacker with host-user access already has broader capabilities — they can directly edit `~/.hazmat/config.yaml`, install user integrations in `~/.hazmat/integrations/`, or tamper with the hazmat binary itself. The approval system protects against the sandboxed agent self-escalating, not against host compromise. Hardening approvals against host-level attackers would require hardware-backed trust (macOS Keychain or Secure Enclave), which is disproportionate to the integration system's deliberately limited blast radius. See [integrations.md](integrations.md) for the integration trust model and [design-assumptions.md](design-assumptions.md) for the approval trust boundary analysis.

## Important Caveats

- Tier 0 and Tier 1 are primarily process-level controls. They are not strong answers to broad host exposure.
- Tier 2 is strong for the dedicated-user model, but it intentionally does not support Docker. If a repo needs Docker, move to Tier 3 rather than weakening Tier 2.
- Tier 2 `pf` rules are not a complete answer for every exfiltration path, and they do not meaningfully constrain container traffic.
- Tier 3 is only a strong answer when the agent is using its own daemon or microVM, not the host daemon.
- Tier 4 is the best recovery story because rollback is part of the design, not an afterthought.
- **No tier currently addresses MCP env inheritance, SSH agent socket abuse, or MCP supply-chain attacks against the agent user's own credentials.** These require operational controls (what you export into the shell) not sandbox architecture controls.
- **`~/.claude/` accumulated artifacts (history, paste-cache, debug logs) are a high-value target that no tier protects from main-user processes.** These grow indefinitely and should be rotated manually.
- **Operational settings (`skipDangerousModePermissionPrompt`, world-writable `git-worktrees.json`) can silently remove tier protections.** Audit `~/.claude/settings.json` and fix file permissions as part of Tier 2 setup.
- **Integration approval protects against agent self-escalation, not host compromise.** The approval prompt and hash-keyed record prevent the sandboxed agent from silently activating integrations on the next session. They do not prevent an attacker with host-user code execution from forging approvals — but such an attacker already has broader access than integrations can provide. Integration validation (not approval) is the security boundary.

## Threat Notes

### Credential Theft

Risk: the agent reads SSH keys, cloud credentials, API tokens, or agent auth material from well-known paths.

Relevant incidents: Vidar targeting `~/.openclaw/`, general infostealer patterns, and the fact that `~/.claude/` is a high-value directory.

Best answer: Tier 2 or stronger.

### Data Exfiltration

Risk: the agent ships data out through HTTP, DNS, or allowed third-party endpoints.

Relevant incidents: PromptArmor `.docx` exfiltration and multiple localhost/network abuse cases.

Best answer: Tier 2 or stronger, with the reminder that Docker workloads need Tier 3 networking rather than Tier 2 host-user `pf` assumptions.

### Destructive Commands

Risk: the agent runs `rm -rf`, rewrites config files, or trashes working state.

Relevant incidents: Wolak and the Reddit home-directory deletion.

Best answer: Tier 2 or stronger.

### Supply-Chain Poisoning

Risk: malicious packages, hooks, CLAUDE instructions, or skills execute attacker-controlled code.

Relevant incidents: ClawHavoc, CVE-2025-59536, and similar project-file execution paths.

Best answer: Tier 3 or stronger, with Tier 4 preferred when recovery matters.

## Risk-Based Tier Selection

### Low Risk

- Working on your own private code
- No sensitive material on the machine
- No MCP servers, or only trusted ones
- You are watching the terminal

Tier 0 or Tier 1 can be reasonable here.

### Medium Risk

- Third-party dependencies or MCP servers are involved
- Sensitive material exists on the machine
- You want a stronger default for day-to-day development
- The repo may need Docker

Use Tier 2 if Docker is not required. Use Tier 3 if it is.

### High Risk

- The repository is unfamiliar or hostile
- The agent runs unattended
- Sensitive data or credentials matter
- Multiple agents run concurrently

Tier 4 is the right default.
