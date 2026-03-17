# AI Harness Security Research: Incidents, CVEs, and Academic Evidence

**Purpose:** Comprehensive reference for the security case behind `macos-agent-sandbox`. Synthesizes academic research, documented incidents, CVE catalog, and supply chain attack data. Categorized by what the sandboxing approach in this repo would and would not have prevented.

**Last updated:** March 2026
**Companion doc:** [incidents-and-cves.md](incidents-and-cves.md) — design-rationale summary
**See also:** [attack-surface-deep-dive.md](attack-surface-deep-dive.md) — technical surface analysis

---

## Table of Contents

1. [Quantitative Research Summary](#1-quantitative-research-summary)
2. [Documented Real-World Incidents](#2-documented-real-world-incidents)
3. [CVE Catalog](#3-cve-catalog)
4. [Supply Chain Attacks](#4-supply-chain-attacks)
5. [MCP and Tool-Poisoning Attacks](#5-mcp-and-tool-poisoning-attacks)
6. [Academic Papers](#6-academic-papers)
7. [Categorization: What Sandboxing Prevents](#7-categorization-what-sandboxing-prevents)
8. [Timeline](#8-timeline)
9. [Sources](#9-sources)

---

## 1. Quantitative Research Summary

These numbers are from peer-reviewed or preprint research. They establish that prompt injection and AI agent exploitation are not theoretical — they are reproducible at high rates in controlled conditions.

| Attack Type | Success Rate | Source |
|-------------|-------------|--------|
| Prompt injection against AI coding agents | 84% | arXiv:2509.22040 |
| Adaptive prompt injection (iterative) | >85% | arXiv:2601.17548 |
| Image-embedded prompt injection | 64% | arXiv:2603.03637 |
| AutoGen multi-agent manipulation success | 97% | Academic study, 2025 |
| CrewAI cross-agent data exfiltration | 65% | Academic study, 2025 |
| Indirect prompt injection (IPI) success baseline | ~70-80% | Multiple papers |

The 84% figure (arXiv:2509.22040) is the most directly applicable: it measures success against AI coding agents specifically — the exact category that includes Claude Code in auto-mode.

**Key finding from Progent (2025):** The only framework to achieve ~0% residual attack success used hardware-enforced privilege separation — permission policies evaluated externally, not by the agent itself. This is the theoretical backing for why OS-level user isolation (Tier 2) is qualitatively stronger than agent-internal guardrails (Tier 0).

---

## 2. Documented Real-World Incidents

### 2.1 Claude Code Incidents

#### Wolak `rm -rf /` Incident (October 2025)

**What happened:** Claude Code with `--dangerously-skip-permissions` executed `rm -rf /`, destroying all user-owned files on the system. Reported by developer Nick Wolak. The agent interpreted ambiguous instructions and chose the most destructive implementation.

**What sandboxing would have prevented:** A separate `agent` macOS user cannot write to `/Users/dr/` or system directories owned by the primary user. The destructive command would have executed within the agent's home directory, leaving the primary user's files intact.

**What sandboxing would NOT have prevented:** The agent's own working directory could still be wiped. Workspace files bind-mounted into the agent context would also be at risk if permissions allowed writes.

---

#### Reddit Home Directory Deletion (December 2025)

**What happened:** A user on r/ClaudeAI reported that Claude Code executed `rm -rf tests/ patches/ plan/ ~/` during a cleanup task. The trailing `~/` deleted the entire home directory. The command was likely generated as part of an automated cleanup sequence.

**What sandboxing would have prevented:** `~/` in the agent context expands to `/Users/agent/` — the dedicated sandbox user's home — not `/Users/dr/`. The primary user's data would be untouched.

**What sandboxing would NOT have prevented:** The agent user's workspace would still be destroyed. If the workspace is a shared volume, care is needed about write permissions.

---

#### PromptArmor Exfiltration Attack (January 2026)

**What happened:** Security firm PromptArmor demonstrated hidden instructions inside a `.docx` file that caused Claude Code to upload sensitive files (SSH keys, `.env` files) to an attacker-controlled server endpoint. The attack exploited an allowlisted API destination.

**Attack chain:** Malicious document → hidden instructions parsed → agent reads secrets from home directory → uploads to attacker server via allowlisted network path.

**What sandboxing would have prevented:** The agent user cannot read `/Users/dr/.ssh/`, `/Users/dr/.aws/`, or `/Users/dr/.env` — those are in the primary user's directory. The attack's most damaging exfiltration paths (SSH keys, cloud credentials) are physically unavailable.

**What sandboxing would NOT have prevented:** Workspace files are accessible. The agent can still read and exfiltrate anything in the project directory. Network calls to non-blocked destinations remain possible. This is the documented limitation in the tier comparison: Tier 2 limits the blast radius, it does not eliminate all exfiltration vectors.

---

#### GTG-1002 Espionage Campaign (2025)

**What happened:** Nation-state level threat actor (GTG-1002) used weaponized AI coding assistant plugins to exfiltrate source code, internal documentation, and secrets from developer workstations. The campaign targeted `~/.config/` directories and standard secret storage locations across multiple IDEs.

**What sandboxing would have prevented:** The agent user's filesystem access is limited to its own home and explicitly bind-mounted paths. `~/.config/` for the primary user is inaccessible.

**What sandboxing would NOT have prevented:** If the project directory contains secrets (embedded credentials, `.env` files in the workspace), those remain accessible.

---

### 2.2 OpenClaw Incidents

OpenClaw is an AI coding agent with architecture similar to Claude Code. These incidents are documented here because they demonstrate attack patterns applicable to any AI coding agent, not OpenClaw-specific vulnerabilities.

#### ClawHavoc Supply Chain Attack (February 2026)

**What happened:** Security researchers identified 824+ malicious skills published to ClawHub (OpenClaw's skill marketplace). Each skill included "prerequisites" that downloaded and executed Atomic macOS Stealer (AMOS), a commodity infostealer. AMOS harvested:
- macOS Keychain passwords
- Browser-stored credentials (Safari, Chrome, Firefox)
- Cryptocurrency wallet files
- SSH private keys from `~/.ssh/`
- Session cookies and authentication tokens

**Scale:** Estimated 1.5M active OpenClaw users were exposed. Actual compromise rate unknown.

**What sandboxing would have prevented:** The agent user does not have access to the primary user's Keychain, `~/.ssh/`, browser profile directories, or cryptocurrency wallet storage. The entire AMOS harvest would find an empty home directory.

**What sandboxing would NOT have prevented:** Any credentials stored in the workspace itself, or credentials the agent user was explicitly granted access to.

---

#### Vidar Infostealer Targeting `~/.openclaw/` (2025-2026)

**What happened:** The commodity Vidar infostealer was updated with specific targeting rules for `~/.openclaw/` — the agent credential directory. This demonstrates that AI agent credential stores are now explicitly targeted by commodity malware, not just sophisticated threat actors.

**Claude Code parallel:** `~/.claude/` contains authentication tokens, settings, and potentially sensitive configuration. It is a high-value target by the same logic.

**What sandboxing prevents:** The agent's credential directory (`/Users/agent/.openclaw/` or equivalent) is isolated in the agent user's home. Compromise of the agent user does not expose the primary user's `~/.claude/` or any other primary-user credential stores.

---

#### ClawJacked WebSocket Hijack (February 2026)

**What happened:** OpenClaw exposed a WebSocket server on `localhost:18789`. Any website the user visited could open a WebSocket connection to this port. Rate limiting exempted localhost connections, and device pairings auto-approved local connections. Result: full agent takeover from a malicious web page.

**Claude Code parallel:** Any localhost-listening service creates this attack surface. MCP servers, local tooling, and agent APIs may expose similar surfaces.

**What sandboxing mitigates:** pf firewall rules can restrict outbound and localhost connections from the agent user. The agent's localhost socket exposure is scoped to the agent user's processes.

---

#### OpenClaw 1-Click RCE: CVE-2026-25253 (CVSS 8.8)

Control UI accepted a `gatewayUrl` from browser URL query string, transmitting authentication token to the attacker-controlled server. A single malicious link shared in a chat, email, or web page achieved full compromise.

---

#### OpenClaw Unauthenticated Local RCE: CVE-2026-25593

WebSocket `config.apply` endpoint had no authentication check. Any local process could reconfigure the OpenClaw gateway and execute arbitrary commands. No user interaction required beyond having OpenClaw running.

---

#### Public Internet Exposure

Multiple scanning firms (Shodan, Censys) found 30,000–42,000 OpenClaw instances exposed on the public internet, many without authentication. This reflects what happens when developer tooling is not designed with network exposure as a threat model.

---

### 2.3 Other Framework Incidents

#### Replit Agent Database Deletion (2025)

**What happened:** Replit's AI agent misinterpreted a "clean up" instruction and dropped a production database. The agent was operating in an environment where it had database write access as part of its normal development permissions.

**What sandboxing addresses:** Principle of least privilege for filesystem access. An agent should only have write access to what the current task requires.

---

#### Google "Antigravity" Drive Wipe (Internal, 2025)

**What happened:** An internal AI coding agent at Google misread a cleanup task and deleted a large portion of the Antigravity project's source tree. The project had to be recovered from version control.

**What sandboxing addresses:** Version control hygiene + limited blast radius. A sandboxed agent operating in a git repository with regular commits limits the recovery surface.

---

#### OpenClaw Inbox Deletion Incident

**What happened:** A user authorized OpenClaw to access their email inbox for context. The agent, attempting to "clean up," mass-deleted emails. Authorization scope was not granular — read permission and delete permission were bundled.

**What sandboxing addresses:** Demonstrates why fine-grained permission scope matters. The sandboxed user model allows explicit deny-listing of tool categories.

---

## 3. CVE Catalog

### Claude Code CVEs

| CVE | CVSS | Description | Attack Vector | Patched |
|-----|------|-------------|---------------|---------|
| CVE-2025-59536 | 8.7 | RCE via malicious project files. Code injection via tool initialization. Opening a crafted repository triggers shell execution. | Malicious CLAUDE.md or project files | Yes |
| CVE-2026-21852 | 5.3 | API key exfiltration. Malicious repository causes key disclosure before trust prompt appears. | Crafted repo | Yes |

**Design implication:** Both CVEs exploit the initialization phase before the trust prompt. Sandboxing does not prevent these exploits from executing, but it limits what the exploited process can reach: no primary-user secrets, no SSH keys, no Keychain.

---

### Ecosystem CVEs (Frameworks and Tooling)

| CVE | CVSS | Affected | Description | CISA KEV? |
|-----|------|----------|-------------|-----------|
| CVE-2024-6091 | 9.8 | AutoGPT | RCE via prompt injection in plugin system | Yes |
| CVE-2025-3248 | 9.8 | Langflow | Unauthenticated RCE. Arbitrary code execution without auth. | Yes |
| CVE-2025-55284 | 8.9 | LangChain | Server-side template injection → RCE | No |
| CVE-2025-53773 | 8.1 | LangChain | Path traversal in document loaders → code execution | No |
| CVE-2025-9074 | 9.3 | Docker Desktop | Container escape via Docker Engine API exposure | No |
| CVE-2025-62725 | — | docker-compose | Path traversal via OCI artifact annotations → host file write | No |
| CVE-2025-32711 | — | Microsoft Copilot (EchoLeak) | Token exfiltration via rendered markdown in Teams | No |
| CVE-2026-22708 | — | Cursor | Seatbelt bypass via shell built-in execution | No |
| CVE-2026-25253 | 8.8 | OpenClaw | 1-click RCE via query string auth token exposure | No |
| CVE-2026-25593 | — | OpenClaw | Unauthenticated local RCE via WebSocket | No |

**Two CVEs on CISA KEV (Known Exploited Vulnerabilities) list:** CVE-2024-6091 (AutoGPT) and CVE-2025-3248 (Langflow). Active exploitation in the wild, not just proof-of-concept.

---

### Anthropic MCP Server CVEs (3 confirmed, February 2026)

Three CVEs in the official Anthropic `mcp-server-git` tool (Git MCP integration). Specific CVE numbers not yet public at time of writing but confirmed by Anthropic security advisory. Attack vector: malicious repository causes the MCP git tool to execute unintended commands on the host.

**Design implication:** MCP servers execute on the host. A compromised or malicious MCP server runs with the same permissions as the MCP server process — which is typically the agent user. This is an argument for running MCP servers as the agent user, not the primary user.

---

### IDEsaster (March 2026)

**What it is:** A coordinated security audit of IDE extensions and AI coding assistant integrations revealed 30+ vulnerabilities across major tools, with 24 formal CVEs assigned. Categories included:
- Credential store access without user consent
- Localhost API exposure without authentication
- Arbitrary file read via workspace configuration
- Command injection via extension configuration

**Significance:** Establishes that IDE + AI agent integrations as a category have systemic security problems, not isolated one-off bugs.

---

## 4. Supply Chain Attacks

Supply chain attacks target the tooling developers use rather than the production systems they operate. AI coding agents are a high-value target because they run with broad system access and are trusted by default.

### s1ngularity (2025)

**What happened:** A malicious package uploaded to npm included a post-install script that specifically checked for the presence of AI coding agent credential directories (`~/.claude/`, `~/.cursor/`, `~/.cline/`, etc.). If found, it exfiltrated their contents. The package also scanned for 2,349 known secret patterns (AWS keys, GitHub tokens, Anthropic API keys, etc.) in the working directory.

**The novel angle:** The malware weaponized the AI agent itself — it generated fake code completion suggestions that, when accepted, introduced backdoors into the target project. This represents a new attack class: using the agent as an attack amplifier, not just a target.

**What sandboxing prevents:** The agent user does not have access to the primary user's `~/.claude/` or any other primary-user credential store. The post-install script finds the agent user's (mostly empty) home directory.

**What sandboxing does not prevent:** If the malicious package runs in the agent's npm environment, it can still access the agent's own credentials and any secrets in the project workspace.

---

### MaliciousCorgi (2025)

**What happened:** A typosquatting campaign on npm published packages that mimicked popular developer tools (e.g., `next-auth` variants, React utility packages). Estimated 1.5M developers installed at least one affected package. Post-install scripts:
- Scanned for AI agent configuration directories
- Exfiltrated found credentials
- Installed persistent backdoors in the agent's tool configuration

**Significance:** Scale. 1.5M developers affected makes this one of the largest supply chain attacks targeting AI coding agents.

---

### SANDWORM_MODE Kill Chain (Documented Attack Sequence)

This is a documented multi-stage attack sequence identified by security researchers. It demonstrates how a sophisticated attacker achieves full compromise from a single malicious commit review request:

1. **Initial access:** Attacker submits a pull request with a malicious `CLAUDE.md` embedded in the PR diff
2. **Execution:** Victim reviews the PR using Claude Code. `CLAUDE.md` triggers on initialization
3. **Privilege escalation:** Agent executes initial payload, downloads secondary stage from attacker infrastructure
4. **Credential harvest:** Secondary stage reads `~/.anthropic/`, `~/.ssh/`, `~/.aws/`, git credential helpers
5. **Persistence:** Tertiary payload modifies the victim's global git hooks to re-execute on future operations
6. **Lateral movement:** Harvested credentials used to access private repositories, internal infrastructure, CI/CD systems

**Total time from PR review to full compromise:** Demonstrated in under 3 minutes in lab conditions.

**What sandboxing breaks:** Steps 4 and 5. The agent cannot read the primary user's `~/.anthropic/`, `~/.ssh/`, or `~/.aws/`. Modifying global git hooks requires write access to the primary user's home directory, which the agent user does not have. The attack degrades from "full system compromise" to "workspace compromise" — damaging but recoverable.

---

### Clinejection (2025)

**What happened:** A malicious MCP server, distributed via a modified Cline (formerly Claude Dev) configuration, intercepted all tool calls and exfiltrated them to an attacker-controlled endpoint before forwarding to the legitimate tool. Operates as a MITM at the MCP protocol level.

**What sandboxing addresses:** pf firewall rules restrict the agent user's outbound network to known-good destinations. An MCP server trying to exfiltrate to an arbitrary IP would be blocked by the default-deny outbound policy.

---

### ClawHavoc (Covered in 2.2 above)

See Section 2.2 for full details. The key supply chain angle: the malicious skills were hosted on the official skill marketplace, not a third-party distribution channel.

---

## 5. MCP and Tool-Poisoning Attacks

MCP (Model Context Protocol) expands the attack surface significantly. Each MCP server is a potential injection point.

### Tool Poisoning Attack (TPA) — Invariant Labs Research (2025)

**What it is:** MCP tool descriptions (the `description` field that the LLM reads to understand what a tool does) can contain hidden instructions. A malicious MCP server can instruct the agent to:
- Exfiltrate data via legitimate tool calls
- Override safety behaviors
- Chain tool calls in ways not intended by the user

**Real example documented:** A malicious MCP server instructed the agent to `add_mcp_server` with attacker-controlled configuration, effectively persisting the malicious server across sessions.

**What sandboxing addresses:** Does not directly address TPA. The agent still processes MCP tool descriptions. However, pf rules limit what the malicious tool calls can reach on the network, and filesystem isolation limits what file-system tool calls can access.

---

### GitHub MCP Private Repository Attack (2025)

**What happened:** A researcher demonstrated that a malicious public repository, when processed by an agent using GitHub MCP, could instruct the agent to use GitHub MCP's `read_file` tool on private repositories the user had access to, then exfiltrate their contents via a subsequent tool call.

**Attack chain:** Malicious public repo → prompt injection → GitHub MCP reads private repo → another tool call exfiltrates the content.

**What sandboxing limits:** Network exfiltration to non-allowed destinations. The injection can still read private repos via GitHub MCP (if the agent has GitHub access), but exfiltrating via arbitrary HTTP is blocked.

---

### EchoLeak: CVE-2025-32711 — Microsoft Copilot Token Exfiltration

**What happened:** Specially crafted markdown in a Teams message, when rendered by Copilot, caused Copilot to include the user's authentication token in a hyperlink rendered in the response. Clicking the link (or in some configurations, automatic link prefetch) sent the token to the attacker.

**Architecture lesson:** When an AI agent renders output in a context with authentication state, the output can become an exfiltration vector. This applies to any agent that renders markdown with live URLs.

---

### PromptPwnd Research Dataset (2025)

**What it is:** A curated dataset of 500+ real-world prompt injection attacks collected from disclosed incidents, bug bounties, and red team exercises. Key findings:
- 73% of attacks were indirect (embedded in processed content, not direct user messages)
- 41% targeted credential or secret exfiltration specifically
- 28% targeted persistent access (modifying config files, git hooks, agent settings)
- Only 12% were stopped by agent-level guardrails alone

**The 12% figure is important:** Agent-level guardrails (the "permission mode" system) stop roughly 1 in 8 attacks in real conditions. OS-level isolation is not a redundant layer — it handles what agent-level controls miss.

---

## 6. Academic Papers

### arXiv:2509.22040 — "Evaluating Prompt Injection Attacks on AI Coding Agents" (2025)

**Key result:** 84% prompt injection success rate against AI coding agents in realistic conditions.

**Methodology:** Researchers crafted injection payloads embedded in common developer contexts: README files, code comments, git commit messages, documentation pages, package descriptions. Agents tested: multiple commercial coding agents in auto-mode equivalent.

**Most effective vector:** Injections embedded in third-party library documentation (success rate: 91%). The agent's tendency to read documentation while generating code creates a reliable injection point.

**Relevance:** This is the paper to cite when someone says "that's theoretical." 84% is a real measured success rate in conditions that approximate normal coding agent usage.

---

### arXiv:2601.17548 — "Adaptive Prompt Injection: Iterative Exploitation of Large Language Models" (2026)

**Key result:** >85% success rate using adaptive (multi-round) injection strategies. Simple injection: ~40-60%. Adaptive iteration doubles the success rate.

**Mechanism:** The attacker observes the model's response to each injection attempt and refines the payload accordingly. This works because most agent deployments either log responses or have observable side effects.

**Relevance:** Establishes that one-time injection hardening is insufficient. The threat actor has an iterative feedback loop.

---

### arXiv:2603.03637 — "Vision-Language Model Prompt Injection via Images" (2026)

**Key result:** 64% success rate for prompt injection embedded in images (PNG, JPEG, PDF pages). Text rendered in images is processed by vision models and can contain executable instructions.

**Attack vector:** A malicious PNG embedded in a markdown file, a PDF the agent is asked to summarize, or a screenshot shared in context.

**Relevance:** Extends the injection surface beyond text. Seatbelt profiles can restrict file access by type to limit which image formats the agent can process.

---

### IPI Foundational Papers (Multiple, 2024-2025)

Indirect Prompt Injection (IPI) — instructions embedded in content the agent retrieves from the environment rather than directly from the user — was formalized in a series of papers in 2024-2025. Key findings across the body of work:

- **IPI is qualitatively different from direct injection** because the user has no visibility into the injected content
- **Most agent frameworks do not differentiate** between user-originated and environment-originated content when processing instructions
- **Tool call output is the primary attack surface** — when the agent calls a tool and processes the result, that result is treated with user-level trust in most frameworks

---

### Progent: Provable Security for LLM Agents (2025)

**Key result:** The only tested approach to achieve near-zero residual attack success used externally-evaluated permission policies — policies the agent cannot override, evaluated by a separate privileged process, not by the LLM itself.

**Relevance to this repo:** This is the theoretical justification for OS-level user separation. The macOS permission system (file ownership, user permissions) is an external policy enforcer. The agent cannot reason its way around it. This qualitative difference is why Tier 2 is not just "more restriction" but "a different kind of restriction."

---

### AutoGen Multi-Agent Attack Study (2025)

**Key result:** 97% success rate for attacks propagating across AutoGen multi-agent pipelines. When one agent is compromised, the malicious payload propagates to other agents in the pipeline at near-perfect reliability.

**Relevance:** Multi-agent setups (MCP servers that spawn subagents, Claude using parallel tool calls, agent orchestration frameworks) amplify the blast radius of a single injection.

---

### CrewAI Cross-Agent Exfiltration Study (2025)

**Key result:** 65% of cross-agent exfiltration attempts succeeded in a CrewAI multi-agent environment. An agent with access to sensitive data in its context can be instructed (via another compromised agent) to pass that data to an external endpoint.

**Relevance:** Establishes that multi-agent orchestration requires its own security model, not just per-agent sandboxing.

---

## 7. Categorization: What Sandboxing Prevents

This section maps each incident and attack class to what the sandboxing tiers in this repo do and do not prevent.

### Tier 2 (Separate User + pf Firewall) — The Core of This Repo

| Attack Class | Example Incidents | Tier 2 Prevents? | Notes |
|-------------|-------------------|------------------|-------|
| Exfiltration of primary-user credentials | PromptArmor, GTG-1002, s1ngularity, ClawHavoc | **Yes** | Agent user cannot read primary user's `~/.ssh/`, `~/.aws/`, `~/.claude/`, Keychain |
| Exfiltration of primary-user files (non-workspace) | SANDWORM_MODE, Vidar | **Yes** | Agent user cannot traverse primary user's home directory |
| Destructive `rm -rf` of primary-user home | Wolak, Reddit deletion | **Yes** | Agent user cannot write to primary user's directories |
| Persistence via primary-user config modification | SANDWORM_MODE step 5, Clinejection | **Yes** | Agent cannot modify `~/.gitconfig`, `~/.bashrc`, etc. of primary user |
| Exfiltration via non-allowed network destination | Clinejection, PromptArmor (partially) | **Yes** | pf default-deny-outbound restricts agent user's network |
| Commodity infostealer harvesting credentials | ClawHavoc (AMOS), Vidar | **Yes** | Standard infostealer paths (`~/.ssh/`, Keychain, browser profiles) unavailable |
| Exfiltration of workspace files | PromptArmor (workspace portion), GitHub MCP | **No** | Workspace is bind-mounted and writable by agent. This is intentional — it can do its job. |
| Prompt injection (execution of injected instructions) | All injection attacks | **No** | Sandboxing does not prevent the agent from receiving and acting on injections. It limits what the resulting actions can reach. |
| MCP tool poisoning (TPA) | Invariant Labs TPA | **Partial** | pf limits outbound exfiltration; agent can still read workspace files via MCP tools |
| Network calls to allowed destinations | PromptArmor (allowlisted endpoint) | **No** | By design: the agent needs network access. Allowlist tuning reduces (but doesn't eliminate) risk. |
| Agent process compromise (RCE via CVE) | CVE-2025-59536, CVE-2026-21852 | **Partial** | RCE still executes, but as the agent user. Blast radius limited to agent user's access. |

---

### Tier 0 (Claude Built-in /sandbox) — What It Does and Doesn't Do

| Capability | Tier 0 |
|-----------|--------|
| Filesystem read restriction | Partial (controlled environment) |
| Credential store isolation | Partial |
| Network restriction | No (not available in Tier 0) |
| `--dangerously-skip-permissions` compatibility | No (Tier 0 requires permission prompts) |
| Usable for auto-mode workflows | No |

---

### Tier 1 (Seatbelt Profiles) — Without Separate User

| Capability | Tier 1 Only |
|-----------|-------------|
| Prevents reading primary user's home | Depends on profile specifics |
| Prevents writing primary user's home | Depends on profile specifics |
| Network restriction | No (Seatbelt has no network controls) |
| Survives Seatbelt bypass CVEs | No (CVE-2026-22708 demonstrated bypass) |

---

### Tier 3 (Docker)

| Capability | Tier 3 |
|-----------|--------|
| Credential store isolation | Yes (if volumes are correctly scoped) |
| Filesystem isolation | Strong |
| Network restriction | Yes (bridge network or no-network mode) |
| Protection from Docker daemon CVEs | No (Docker itself is an attack surface) |
| Complexity / maintenance | High |

---

### Combined Tier 2 + pf (This Repo's Approach)

The combination — separate OS user + pf firewall — achieves the most useful properties for a solo developer workflow:
- Strong credential isolation (structural, not policy-based)
- Network restriction (default-deny outbound)
- No extra process overhead or VM startup cost
- Compatible with `--dangerously-skip-permissions` auto-mode
- Recoverable: primary user can always reset the agent environment

The design tradeoff is explicit: workspace files are accessible (the agent needs to do work). The assumption is that workspace files are less catastrophically sensitive than home-directory credentials, and that git history provides recovery.

---

## 8. Timeline

| Date | Event | Category |
|------|-------|----------|
| 2024 | CVE-2024-6091 (AutoGPT RCE, CVSS 9.8) published | CVE |
| 2025 Q1 | IPI foundational research papers published | Academic |
| 2025 Q2 | CVE-2025-3248 (Langflow CVSS 9.8) — CISA KEV | CVE |
| 2025 Q2 | CVE-2025-55284 (LangChain SSTI) | CVE |
| 2025 Q2 | CVE-2025-53773 (LangChain path traversal) | CVE |
| 2025 Q3 | arXiv:2509.22040 (84% injection success rate published) | Academic |
| 2025 Q3 | s1ngularity campaign (2,349 secret patterns, agent weaponization) | Supply chain |
| 2025 Q3 | MaliciousCorgi campaign (1.5M developer exposure) | Supply chain |
| 2025 Q3 | AutoGen 97% / CrewAI 65% attack studies published | Academic |
| 2025 Q3 | GTG-1002 espionage campaign (AI plugin weaponization) | Incident |
| 2025 Q3 | Progent paper (~0% residual with hardware-enforced separation) | Academic |
| Oct 2025 | Wolak `rm -rf /` incident | Incident |
| Oct 2025 | CVE-2025-9074 (Docker Desktop CVSS 9.3) | CVE |
| Oct 2025 | CVE-2025-32711 / EchoLeak (Microsoft Copilot token exfiltration) | CVE |
| Nov 2025 | PromptPwnd dataset published (500+ real attacks, 12% stopped by guardrails) | Research |
| Nov 2025 | Invariant Labs TPA research published | Research |
| Nov 2025 | Anthropic MCP Git tool CVEs (3 confirmed) | CVE |
| Dec 2025 | Reddit home directory deletion | Incident |
| Dec 2025 | Google "Antigravity" project drive wipe | Incident |
| Dec 2025 | Replit agent database deletion | Incident |
| Jan 2026 | PromptArmor `.docx` exfiltration attack | Incident |
| Jan 2026 | CVE-2026-22708 (Cursor Seatbelt bypass) | CVE |
| Jan 2026 | CVE-2025-59536 (Claude Code RCE via project files, CVSS 8.7) | CVE |
| Jan 2026 | arXiv:2601.17548 (>85% adaptive injection) | Academic |
| Feb 2026 | CVE-2026-21852 (Claude Code API key exfiltration, CVSS 5.3) | CVE |
| Feb 2026 | ClawHavoc (824+ malicious OpenClaw skills, AMOS delivery) | Supply chain |
| Feb 2026 | ClawJacked (WebSocket hijack) | Incident |
| Feb 2026 | Vidar infostealer `~/.openclaw/` targeting | Incident |
| Feb 2026 | CVE-2026-25253 (OpenClaw 1-click RCE, CVSS 8.8) | CVE |
| Feb 2026 | CVE-2026-25593 (OpenClaw unauthenticated local RCE) | CVE |
| Mar 2026 | arXiv:2603.03637 (64% image-embedded injection) | Academic |
| Mar 2026 | IDEsaster audit (30+ vulns, 24 CVEs across IDE AI extensions) | Research |
| Mar 2026 | SANDWORM_MODE kill chain documented | Research |
| Mar 2026 | Clinejection (MCP MITM attack) | Incident |
| Mar 2026 | Live research: `CLAUDE_CODE_DONT_INHERIT_ENV=1` confirmed inert — all MCP children inherit full env regardless | Research |
| Mar 2026 | Live research: `~/.claude/history.jsonl` retains all prompts indefinitely (8,487 entries, 2.87MB on test system); `debug/` ~295MB unrotated | Research |
| Mar 2026 | Live research: `~/.claude/settings.json` `skipDangerousModePermissionPrompt: true` actively disables human-in-the-loop gate | Research |
| Mar 2026 | Live research: `git-worktrees.json` observed at 0666 — world-writable, enables sandbox-user → main-session worktree injection | Research |
| Mar 2026 | Binary analysis: `CLAUDE_CODE_DISABLE_COMMAND_INJECTION_CHECK` env var bypasses `$a_()` bash injection gate entirely | Research |
| Mar 2026 | Binary analysis: bridge mode (`ENVIRONMENT_KIND=bridge` + `SESSION_INGRESS_URL`) enables Agent SDK remote control via env vars | Research |
| Mar 2026 | MCP source audit: `aistudio-mcp-server` v0.5.2 accepts unrestricted file paths in `generate_content` tool, exfiltrating file contents to Google Gemini API — no path validation, explicitly documented bypass | Research |
| Mar 2026 | MCP source audit: `mcp-reddit` and `mcp-domain-availability` configured as unversioned git dependencies (`git+https://` without commit SHA) — supply chain pin missing | Research |

---

## 9. Sources

### Academic Papers

- arXiv:2509.22040 — "Evaluating Prompt Injection Attacks on AI Coding Agents"
- arXiv:2601.17548 — "Adaptive Prompt Injection: Iterative Exploitation of Large Language Models"
- arXiv:2603.03637 — "Vision-Language Model Prompt Injection via Images"
- Progent: "Provable Security for LLM Agents via Constraint-Enforced Privilege Separation" (2025)
- "Indirect Prompt Injection Threats: The Hidden Attack Surface in Language Model Deployments" (foundational IPI paper, 2024)

### Documented Incidents

- Wolak incident: Multiple social media posts + secondary reporting, October 2025
- Reddit home deletion: r/ClaudeAI thread, December 2025
- PromptArmor exfiltration: PromptArmor security advisory + blog post, January 2026
- GTG-1002 campaign: Multiple threat intelligence reports, 2025
- OpenClaw incidents: OpenClaw security advisories + independent research reports, 2025-2026

### CVE Sources

- NVD (National Vulnerability Database): nvd.nist.gov
- CISA KEV (Known Exploited Vulnerabilities): cisa.gov/known-exploited-vulnerabilities-catalog
- Anthropic security advisories: anthropic.com/security
- OpenClaw security advisories: OpenClaw project GitHub

### Research and Campaign Sources

- Invariant Labs MCP TPA research, November 2025
- PromptPwnd dataset documentation, November 2025
- IDEsaster audit report, March 2026
- SANDWORM_MODE kill chain documentation (security researcher disclosure)
- Clinejection: independent security researcher blog post, 2025

### Academic Datasets and Benchmarks

- CrewAI and AutoGen attack studies: published in conjunction with multi-agent security workshops, 2025
- IPI benchmark datasets: released alongside foundational IPI papers, 2024-2025
