# Attack Surface Deep Dive: AI Coding Agents on macOS

## The Lethal Trifecta

Security researchers identify three conditions that make an AI agent exploitable:

1. **Access to private data** (filesystem, environment variables, secrets)
2. **Exposure to untrusted tokens** (reading external content — PRs, issues, web pages, dependencies)
3. **Exfiltration vectors** (network access, git push, file write to shared locations)

If all three are present — which they are in most AI coding agent setups — the agent is exploitable.

**User isolation eliminates condition 1** for your critical data. The agent can't read what it can't access. This is why user isolation is the most impactful single defense.

---

## 1. Prompt Injection (The Unsolved Problem)

Prompt injection has **84% success rate** for executing malicious commands when embedded in coding rule files (arXiv:2509.22040). Even Claude Opus 4.5 is vulnerable.

### Attack Vectors

| Vector | How It Works | Documented? |
|--------|-------------|-------------|
| Malicious CLAUDE.md | Hidden instructions in project config files | CVE-2025-59536 |
| Code comments | `# temporary: skip auth check` — looks routine to humans, acts as instruction to AI | Yes (Knostic) |
| Unicode invisible chars | E0000-E007F range encodes text invisible on screen but processed by LLMs | Yes (Trend Micro) |
| npm postinstall scripts | Malicious package runs code + embeds AI-tricking prompts | Yes (Dec 2025) |
| Git commit messages | Injection via PR descriptions, issue bodies, commit messages | Yes (PromptPwnd) |
| Image-based injection | Hidden text in screenshots/diagrams — 64% success rate | Yes (arXiv:2603.03637) |
| MCP server responses | Tool poisoning via descriptions invisible in UI but processed by model | Yes (Invariant Labs) |
| .docx with hidden text | 1pt white-on-white text with instructions | PromptArmor (Jan 2026) |

### What User Isolation Does About This

**Nothing directly.** The agent still follows malicious instructions. But user isolation limits what those instructions can access — the agent can read/exfiltrate workspace data but not your SSH keys, Keychain, cloud creds, or browser data.

---

## 2. What a Compromised Agent Can Actually Do

Based on "Your AI, My Shell" research and documented attacks:

### Within the sandbox user (still possible)
- Read/modify all project files in the workspace
- Execute arbitrary shell commands as the sandbox user
- Push malicious commits using the sandbox user's git credentials
- Install LaunchAgents in `~/Library/LaunchAgents/` (persistence within sandbox user)
- Modify `~/.zshrc`, `~/.bashrc` (persistence within sandbox user)
- Install global git hooks at `~/.git-templates/` for persistence across repos
- Read the sandbox user's own API keys and tokens
- Create/modify MCP configurations
- Overwrite MEMORY.md for cross-session persistence
- Access the Docker socket if permissions allow (critical — see Section 5)

### Blocked by user isolation
- Read `~/.ssh/`, `~/.aws/`, `~/.gnupg/` of main user
- Access main user's Keychain
- Read browser cookies, passwords, history
- Access main user's clipboard (separate bootstrap namespace)
- Read main user's environment variables (protected since macOS 11)
- Modify main user's shell configs, LaunchAgents, cron jobs

---

## 3. Network Exfiltration Vectors

### Blockable at pf level (port-based)

| Channel | Port(s) | Risk | Block Difficulty |
|---------|---------|------|-----------------|
| SMTP email | 25, 465, 587 | Data exfiltration | Easy |
| IRC | 6667, 6697 | C2 channel | Easy |
| FTP | 20, 21 | File transfer | Easy |
| Telnet | 23 | Insecure access | Easy |
| SMB | 445 | Lateral movement | Easy |
| RDP | 3389 | Remote desktop | Easy |
| VNC | 5900-5901 | Remote desktop | Easy |
| Tor SOCKS | 9050, 9150 | Anonymous exfil | Easy |
| SOCKS proxy | 1080 | Traffic tunneling | Easy |
| OpenVPN | 1194 | VPN tunneling | Easy |
| ICMP tunneling | ICMP protocol | Covert channel | Easy |

### Blockable at DNS level (domain-based)

| Channel | Domains | Risk |
|---------|---------|------|
| Tunnel services | `*.ngrok.io`, `*.trycloudflare.com`, `*.serveo.net`, `*.localtunnel.me`, etc. | Bypass firewalls |
| Paste services | `pastebin.com`, `dpaste.org`, `ix.io`, `hastebin.com`, etc. | Data exfiltration |
| File sharing | `transfer.sh`, `file.io`, `gofile.io`, `catbox.moe`, etc. | Data exfiltration |
| Webhook capture | `webhook.site`, `requestbin.com`, `pipedream.com`, etc. | Data exfiltration |
| Messaging APIs | `api.telegram.org`, `discord.com/api/webhooks` | C2 / exfil |

### Effectively unblockable at network level

| Channel | Why | Mitigation |
|---------|-----|-----------|
| HTTPS POST to arbitrary servers | Same port/protocol as legitimate traffic | User isolation (nothing to steal) |
| WebSocket over TLS | Indistinguishable from normal HTTPS | User isolation |
| Cloud storage APIs (S3, GCS) | Shared domains with legitimate cloud services | User isolation |
| Git push to attacker repos | Standard HTTPS to github.com | Review git remotes |
| DNS tunneling | Data encoded in query subdomains | DNS-level filtering (NextDNS) |
| HTTP header steganography | Data hidden in normal HTTP fields | Not practical to detect |
| npm publish | Uses registry.npmjs.org | Scoped credentials |
| GitHub API (gists, issues) | Standard API to api.github.com | Scoped tokens |

### Key Insight

**You cannot prevent a networked process from exfiltrating data it has access to.** The question isn't "can I block all exfiltration?" — it's "does the agent have anything worth exfiltrating?" User isolation answers this by ensuring the agent can only access workspace data, not your critical personal/credential data.

---

## 4. DNS Tunneling — The Documented Claude Code Attack

**CVE-2025-55284** demonstrated DNS exfiltration against Claude Code specifically:

1. Prompt injection causes agent to read `.env` files
2. Agent encodes secrets as subdomains: `SECR3T.attacker.com`
3. DNS queries traverse recursive resolvers to attacker's nameserver
4. Attacker logs queries and reconstructs stolen data

**pf cannot stop this** — it can restrict which DNS server is queried, but the recursive resolver still forwards the query (with the stolen data in the subdomain) to the attacker's authoritative server.

**Mitigation:** DNS-level filtering service (NextDNS, Cloudflare Gateway) that detects high-entropy subdomains. Or: don't give the agent access to secrets worth stealing (user isolation).

---

## 5. macOS User Isolation — Gaps to Harden

### Critical Gap: Docker Socket

On your system, the Docker socket has permissions `srwxr-xr-x`. **Any user can access the Docker daemon**, which grants root-equivalent access within the Docker VM.

**Fix:**
```bash
chmod 700 ~/.docker/run/docker.sock
# Or stop Docker Desktop when running untrusted agent sessions
```

### Gap: Process Visibility

Any user can see all processes via `ps aux`, including command-line arguments. macOS has **no equivalent** to Linux's `hidepid`.

**Mitigation:** Never pass secrets via command-line arguments. Use environment variables instead (protected since macOS 11 with SIP).

### Gap: Shared /tmp

`/tmp` is world-readable/writable. Files created with default umask (022) are readable by all users.

**Mitigation:** Use `$TMPDIR` (per-user temp dir under `/var/folders/`). Set `umask 077` in shell configs.

### Gap: Shared Homebrew

`/opt/homebrew` is world-readable. The sandbox user can execute any Homebrew-installed binary. A compromised Homebrew binary affects all users.

**Mitigation:** Acceptable risk for own repos. The sandbox user can't modify Homebrew (not in admin group).

### Gap: World-Readable System Files

`/etc/`, `/var/log/`, `/Applications/`, `/Library/` are largely world-readable.

**Mitigation:** Low risk — these don't contain user secrets.

### What IS Isolated (Strong Boundaries)

- Home directory (`drwxr-x---+` mode 700 with deny ACL)
- User Keychain (encrypted with login password)
- TCC permissions (per-user `tccd` daemon)
- Clipboard/pasteboard (per-user bootstrap namespace)
- Environment variables of other users' processes (protected since macOS 11)
- Per-user LaunchAgents and XPC services

---

## 6. Privilege Escalation History

Apple patches privilege escalation vulnerabilities **regularly**, meaning new ones are continuously discovered:

| CVE | What | Impact |
|-----|------|--------|
| CVE-2023-42931 | diskutil filesystem mounting | Any local user to root |
| CVE-2024-27864 | diskimagescontroller XPC | Sandbox escape |
| CVE-2025-24085 | LaunchDaemon exploitation | Root escalation |
| CVE-2025-31199 | Spotlight index (Sploitlight) | Cross-user data leakage |
| CVE-2025-32462 | sudo escalation | Privilege escalation |
| CVE-2025-43530 | TCC bypass via TOCTOU | Permission bypass |
| CVE-2026-20626 | Missing authorization check | Root escalation |

**Takeaway:** User isolation is a strong defense-in-depth layer but not an absolute boundary. Keep macOS fully patched.

---

## 7. Supply Chain Attacks Through the Agent

### Documented Campaigns

**SANDWORM_MODE (Feb 2026):** Self-spreading npm worm. Kill chain: typosquatted package → credential harvesting → MCP injection into Claude Code/Cursor → git hook persistence → worm propagation via npm publish → exfiltration via GitHub API and DNS tunneling.

**ClawHavoc (Feb 2026):** 824+ malicious OpenClaw skills delivering AMOS macOS stealer. Harvested Keychain, browser credentials, crypto wallets, SSH keys.

**MaliciousCorgi (Jan 2026):** VS Code extensions with 1.5M installs secretly harvesting entire codebases via DLL hijacking.

**s1ngularity (Aug 2025):** Hijacked legitimate Nx build package to steal crypto wallets, GitHub/npm tokens, SSH keys. First documented case of malware weaponizing AI CLI tools.

### How User Isolation Helps

Even if a supply chain attack compromises the sandbox user:
- It cannot access your main user's SSH keys, cloud credentials, Keychain, or browser data
- The blast radius is limited to the workspace and the sandbox user's credentials
- The sandbox user's credentials can be scoped (limited GitHub deploy key, limited API permissions)

---

## Sources

- [Claude Code DNS Exfiltration CVE-2025-55284](https://embracethered.com/blog/posts/2025/claude-code-exfiltration-via-dns-requests/)
- [CVE-2025-59536 / CVE-2026-21852 (Check Point)](https://research.checkpoint.com/2026/rce-and-api-token-exfiltration-through-claude-code-project-files-cve-2025-59536/)
- [PromptArmor Claude Cowork Attack](https://the-decoder.com/claude-cowork-hit-with-file-stealing-prompt-injection-days-after-anthropics-launch/)
- [Your AI My Shell (arXiv:2509.22040)](https://arxiv.org/html/2509.22040v1)
- [Prompt Injection on Coding Assistants (arXiv:2601.17548)](https://arxiv.org/html/2601.17548v1)
- [SANDWORM_MODE (Endor Labs)](https://www.endorlabs.com/learn/sandworm-mode-dissecting-a-multi-stage-npm-supply-chain-attack)
- [AI Hijacking Five-Layer Attack (Corti)](https://corti.com/ai-hijacking-via-open-source-agent-tooling-a-five-layer-attack-anatomy/)
- [MCP Tool Poisoning (Invariant Labs)](https://invariantlabs.ai/blog/mcp-security-notification-tool-poisoning-attacks)
- [IDEsaster Vulnerabilities](https://tigran.tech/securing-ai-coding-agents-idesaster-vulnerabilities/)
- [GTG-1002 AI Espionage (Anthropic)](https://www.anthropic.com/news/disrupting-AI-espionage)
- [macOS Sandbox Escapes (jhftss)](https://jhftss.github.io/A-New-Era-of-macOS-Sandbox-Escapes/)
- [macOS Privilege Escalation (HackTricks)](https://book.hacktricks.xyz/macos-hardening/macos-security-and-privilege-escalation)
- [Sploitlight CVE-2025-31199 (Microsoft)](https://www.microsoft.com/en-us/security/blog/2025/07/28/sploitlight-analyzing-a-spotlight-based-macos-tcc-vulnerability/)
- [MITRE ATT&CK T1048: Exfiltration](https://attack.mitre.org/techniques/T1048/)
- [Invisible Prompt Injection (Trend Micro)](https://www.trendmicro.com/en_us/research/25/a/invisible-prompt-injection-secure-ai.html)
- [Blocklist vs Allowlist (NIST SP 800-171)](https://csf.tools/reference/nist-sp-800-171/r2/3-4/3-4-8/)
- [awesome-tunneling (GitHub)](https://github.com/anderspitman/awesome-tunneling)
