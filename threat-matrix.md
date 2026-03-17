# Threat Matrix: Which Tier Blocks Which Risk

Use this file after [overview.md](overview.md) when you already know the candidate tiers and want to ask: "Does this tier actually stop the problem I care about?"

## Fast Selection by Primary Concern

| Primary concern | Minimum sensible tier | Why | Read next |
|-----------------|-----------------------|-----|-----------|
| Protect `~/.ssh`, cloud creds, or `~/.claude` from agent access | Tier 2 | Separate macOS user changes what the agent can reach | [attack-surface-deep-dive.md](attack-surface-deep-dive.md) |
| Prevent easy network exfiltration | Tier 2 | Separate-user `pf` helps, but Docker changes the story | [soft-pf-blocklist.md](soft-pf-blocklist.md) |
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

### Network and Localhost Surface

| Attack vector | Tier 0 | Tier 1 | Tier 2 | Tier 3 | Tier 4 |
|--------------|--------|--------|--------|--------|--------|
| Agent exfiltrates via `curl` or `wget` | Partial | Yes | Yes | Yes | Yes |
| Network exfiltration via DNS | No | No | Partial | Yes | Yes |
| Lateral movement on localhost or LAN | No | Partial | Partial | Yes | Yes |
| Prompt injection via MCP server | Partial | Partial | Partial | Yes | Yes |

### Escape, Persistence, and Recovery Surface

| Attack vector | Tier 0 | Tier 1 | Tier 2 | Tier 3 | Tier 4 |
|--------------|--------|--------|--------|--------|--------|
| Malicious repo content triggers destructive shell commands | No | Partial | Yes | Yes | Yes |
| Supply-chain install scripts or poisoned dependencies | No | No | Partial | Partial | Yes |
| Agent installs a persistent backdoor | No | Partial | Yes | Yes | Yes |
| Agent escalates via shared Docker socket | No | No | No | Yes | Yes |
| Agent consumes all CPU or memory | No | No | No | Yes | Yes |
| Full host compromise | No | No | No | Partial | Yes |

## Important Caveats

- Tier 0 and Tier 1 are primarily process-level controls. They are not strong answers to broad host exposure.
- Tier 2 is strong for the dedicated-user model, but it intentionally does not support Docker. If a repo needs Docker, move to Tier 3 rather than weakening Tier 2.
- Tier 2 `pf` rules are not a complete answer for every exfiltration path, and they do not meaningfully constrain container traffic.
- Tier 3 is only a strong answer when the agent is using its own daemon or microVM, not the host daemon.
- Tier 4 is the best recovery story because rollback is part of the design, not an afterthought.

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
