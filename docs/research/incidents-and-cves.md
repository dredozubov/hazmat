# Incidents and CVEs: Why These Design Choices Exist

Use this file when you want the rationale behind the architecture. These incidents are not trivia; they explain why the repo recommends separate users, deny-by-default networking, dedicated Docker daemons, and eventually full VMs.

For the full evidence base — quantitative research, complete CVE table, supply chain attacks, MCP tool-poisoning attacks, academic papers, and per-attack sandbox coverage analysis — see [security-evidence.md](security-evidence.md).

## Fast Map: Incident to Design Choice

| Incident or CVE | What it demonstrates | Design choice it supports | Read next |
|-----------------|----------------------|---------------------------|-----------|
| Wolak, Reddit home deletion | Agents can run destructive shell commands from ambiguous instructions | Separate blast-radius boundary, not just prompts | [overview.md](../overview.md) |
| PromptArmor `.docx` exfiltration | Repo or document content can drive outbound theft | Network controls and reduced secret exposure | [attack-surface-deep-dive.md](attack-surface-deep-dive.md) |
| CVE-2025-59536, CVE-2026-21852 | Project files can trigger shell execution or key exposure early | Do not treat trust prompts or app-level checks as the main boundary | [threat-matrix.md](../threat-matrix.md) |
| ClawHavoc, Vidar | Agent config and credential directories are high-value targets | Separate user homes and protect agent credential stores | [setup-option-a.md](../setup-option-a.md) |
| Docker Desktop CVE-2025-9074 | Docker is not a magic security layer | Keep Docker versions current and prefer dedicated daemons | [tier3-docker-sandboxes.md](../tier3-docker-sandboxes.md) |
| docker-compose CVE-2025-62725 | Even "normal developer tooling" can cross the host/container boundary badly | Version hygiene and Compose hardening matter | [tier3-docker-sandboxes.md](../tier3-docker-sandboxes.md) |
| Cursor CVE-2026-22708 | App sandboxes and Seatbelt wrappers are not perfect | Treat lower tiers as risk reduction, not absolute containment | [tier1-seatbelt-wrappers.md](tier1-seatbelt-wrappers.md) |

## Claude Code Incidents

### Wolak Incident (October 2025)

Claude Code with `--dangerously-skip-permissions` executed `rm -rf /`, destroying user-owned files across the system. The lesson is straightforward: if the agent runs as your main user, your files are in scope.

### Reddit Home Directory Deletion (December 2025)

Command `rm -rf tests/ patches/ plan/ ~/` deleted an entire home directory. The trailing `~/` was either a hallucination or an over-eager cleanup attempt. The lesson is the same as Wolak, but with a less exotic trigger: routine cleanup tasks can still be catastrophic.

### PromptArmor Exfiltration Attack (January 2026)

Hidden text inside a `.docx` file manipulated Claude into uploading sensitive files to an attacker's account via an allowlisted API endpoint. The attack worked because:

- the `.docx` contained invisible instructions
- the agent followed them without question in bypass mode
- network allowlists still permitted the chosen destination

The lesson is that "allowed network" is not the same thing as "safe network."

## Claude Code CVEs

### CVE-2025-59536 — RCE via Project Files (CVSS 8.7)

Code injection allowing arbitrary shell command execution upon tool initialization via malicious project files. Simply opening a crafted repository could trigger hidden commands.

### CVE-2026-21852 — API Key Exfiltration (CVSS 5.3)

Information disclosure allowing API key exfiltration from malicious repositories. Cloning and opening a crafted repository could trigger hidden commands before the trust prompt appeared.

### Common Attack Vectors

- malicious `CLAUDE.md` files in public repositories
- compromised hooks configurations
- MCP server configurations that redirect API traffic
- settings files that override `ANTHROPIC_BASE_URL` to attacker-controlled endpoints

The design implication is that app-level permission prompts and trust UX help, but they are not the boundary to rely on.

## Docker and Container CVEs Relevant to Tier 3

### Docker Desktop CVE-2025-9074 (CVSS 9.3)

Docker Engine API exposure allowed container escape to a more privileged Docker control surface. The lesson is not "avoid Docker completely." The lesson is "treat Docker as its own attack surface, keep versions current, and avoid shared-daemon assumptions."

### docker-compose CVE-2025-62725 (affects `>= 2.34.0, < 2.40.2`)

When Compose resolved remote OCI artifacts, unsanitized annotation paths enabled path traversal and arbitrary host-file writes. The trigger was remote OCI artifact resolution, not every plain local Compose file, but it is a good example of why Compose version hygiene belongs in the design doc and not only in a changelog.

## OpenClaw Incidents (Useful Parallel Cases)

From the [OpenClaw Attack Surface Report](../openclaw/OpenClaw-macOS-Attack-Surface-Report.md):

### CVE-2026-25253 — 1-Click RCE (CVSS 8.8)

Control UI accepted a `gatewayUrl` from browser URL query string, transmitting auth token to an attacker-controlled server. Single malicious link, full compromise.

### CVE-2026-25593 — Unauthenticated Local RCE

WebSocket `config.apply` had no auth check. Any local process could reconfigure the gateway and execute arbitrary commands.

### ClawJacked (February 2026) — WebSocket Hijack

Any website could open a WebSocket to `localhost:18789`. Rate limiting exempted localhost, and device pairings auto-approved. Result: full agent takeover.

### ClawHavoc Supply Chain Attack (February 2026)

More than 824 malicious skills on ClawHub contained "prerequisites" that downloaded and executed Atomic macOS Stealer (AMOS), harvesting Keychain passwords, browser credentials, crypto wallets, and SSH keys.

### Vidar Infostealer Targeting `~/.openclaw/`

Commodity malware specifically targeted agent credentials, harvesting auth tokens and private keys from the agent's home directory.

### 30,000-42,000 Exposed Instances

Multiple scanning firms found tens of thousands of OpenClaw instances exposed on the public internet, many without authentication.

## Lessons for Claude Code Users

| OpenClaw incident | Claude Code parallel |
|-------------------|---------------------|
| Credential file exposure (`~/.openclaw/`) | `~/.claude/` contains auth data, settings, and credentials |
| ClawHavoc (malicious skills) | Malicious `CLAUDE.md`, compromised MCP servers, or poisoned setup instructions |
| WebSocket localhost hijack | Any localhost-listening service is a meaningful attack surface |
| Infostealer targeting agent dirs | `~/.claude/` is a high-value target |
| Shell execution as core risk | Same core risk: the agent can run shell commands |
| Auto-approval bypasses | `--dangerously-skip-permissions` removes the app-level prompt layer |

## Industry-Wide Concerns

### Cursor CVE-2026-22708

Shell built-in bypass in Cursor's sandbox. Patched January 2026. Cursor uses the same general Seatbelt family of controls on macOS, which is why lower tiers should be treated as meaningful risk reduction rather than a perfect guarantee.

## What These Incidents Mean for This Repo

1. Use separate-user or stronger boundaries when the machine has anything worth protecting.
2. Treat network controls as a first-class design question, not an optional add-on.
3. Treat Docker as its own risk surface. Shared daemon access is not "just one more permission."
4. Prefer designs with explicit recovery paths when autonomy or hostility increases.

## Timeline

| Date | Event |
|------|-------|
| Oct 2025 | Wolak `rm -rf /` incident |
| Dec 2025 | Reddit home-directory deletion |
| Jan 2026 | PromptArmor `.docx` exfiltration attack |
| Jan 2026 | CVE-2026-22708 (Cursor sandbox bypass) |
| Jan 2026 | CVE-2025-59536 (Claude Code RCE via project files) |
| Feb 2026 | CVE-2026-21852 (Claude Code API key exfiltration) |
| Feb 2026 | ClawHavoc (824+ malicious OpenClaw skills) |
| Feb 2026 | ClawJacked (WebSocket hijack) |
| Feb 2026 | Vidar infostealer targeting `~/.openclaw/` |
