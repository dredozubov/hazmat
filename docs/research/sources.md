# Sources and References

## Official Documentation

- [Claude Code Sandboxing](https://code.claude.com/docs/en/sandboxing) — Built-in sandbox docs
- [Claude Code Security](https://code.claude.com/docs/en/security) — Permission model
- [Claude Code Devcontainer](https://code.claude.com/docs/en/devcontainer) — Official devcontainer reference
- [Claude Code Hooks](https://code.claude.com/docs/en/hooks) — Hooks reference
- [Claude Code Settings](https://code.claude.com/docs/en/settings) — Settings reference
- [Anthropic API IP Addresses](https://platform.claude.com/docs/en/api/ip-addresses) — For firewall allowlisting

## Anthropic Engineering

- [Claude Code Sandboxing Architecture](https://www.anthropic.com/engineering/claude-code-sandboxing) — How the built-in sandbox works

## Docker & Containers

- [Docker Sandboxes for Claude Code](https://docs.docker.com/ai/sandboxes/agents/claude-code/) — Docker Sandbox setup
- [Docker Sandboxes Blog Post](https://www.docker.com/blog/docker-sandboxes-run-claude-code-and-other-coding-agents-unsupervised-but-safely/)
- [Docker Sandbox Get Started](https://docs.docker.com/ai/sandboxes/get-started/)
- [Docker Security Cheat Sheet (OWASP)](https://cheatsheetseries.owasp.org/cheatsheets/Docker_Security_Cheat_Sheet.html)
- [Enhanced Container Isolation](https://docs.docker.com/enterprise/security/hardened-desktop/enhanced-container-isolation/)

## Devcontainers

- [Trail of Bits Claude Code Devcontainer](https://github.com/trailofbits/claude-code-devcontainer)
- [How to Safely Run AI Agents Inside a DevContainer](https://codewithandrea.com/articles/run-ai-agents-inside-devcontainer/)
- [Coding Agents in Secured VS Code Dev Containers](https://www.danieldemmel.me/blog/coding-agents-in-secured-vscode-dev-containers)
- [Running Claude Code in Docker with Network Isolation](https://shaharia.com/blog/run-claude-code-docker-network-isolation/)
- [Claude Code Docker Tutorial (DataCamp)](https://www.datacamp.com/tutorial/claude-code-docker)

## macOS Seatbelt / sandbox-exec

- [sandbox-exec: macOS's Command-Line Sandboxing Tool](https://igorstechnoclub.com/sandbox-exec/) — Best introductory guide
- [Sandboxing on macOS](https://bdash.net.nz/posts/sandboxing-on-macos/) — Technical deep dive
- [macOS Sandbox (HackTricks)](https://book.hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-sandbox/index.html)
- [macOS Sandbox Debug & Bypass (HackTricks)](https://book.hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-sandbox/macos-sandbox-debug-and-bypass/index.html)
- [macOS Sandbox Profiles (SBPL Guide)](https://gist.github.com/cedws/b7dd60878a208be09ae7d766e6317061)
- [OSX Sandbox Seatbelt Profiles](https://github.com/s7ephen/OSX-Sandbox--Seatbelt--Profiles)
- [Custom macOS Sandbox Profiles](https://github.com/Ozymandias42/macOS-Sandbox-Profiles)

## Open Source Sandbox Tools

- [nono — Secure Sandbox for AI Agents](https://github.com/always-further/nono) — [HuggingFace blog](https://huggingface.co/blog/lukehinds/nono-agent-sandbox)
- [claude-sandbox for macOS](https://dev.to/kohkimakimoto/claude-sandbox-yet-another-sandboxing-tool-for-claude-code-on-macos-o6n)
- [agent-seatbelt-sandbox](https://github.com/michaelneale/agent-seatbelt-sandbox)
- [ai-jail macOS Seatbelt](https://deepwiki.com/akitaonrails/ai-jail/4.5-macos:-seatbelt-sandboxing)
- [Trail of Bits Seatbelt Sandboxer Plugin](https://github.com/trailofbits/skills/tree/main/plugins/seatbelt-sandboxer)
- [SandVault](https://github.com/webcoyote/sandvault) — Automated user isolation
- [Alcoholless (NTT Labs)](https://medium.com/nttlabs/alcoholless-a-lightweight-security-sandbox-for-macos-programs-homebrew-ai-agents-etc-ccf0d1927301)
- [sandbox-runtime (Anthropic)](https://github.com/anthropic-experimental/sandbox-runtime) — Open source sandbox npm package
- [claude-code-sandbox (neko-kai)](https://github.com/neko-kai/claude-code-sandbox) — SBPL profiles
- [claude-code-secure-container](https://github.com/kydycode/claude-code-secure-container)
- [claudebox](https://github.com/RchGrav/claudebox)
- [rivet-dev/sandbox-agent](https://github.com/rivet-dev/sandbox-agent) — Universal adapter for agent sandboxes

## VM Isolation Tools

- [Lima](https://github.com/lima-vm/lima) — Free, CNCF-incubating Linux VMs on macOS
- [Lima CNCF Incubation Announcement](https://www.cncf.io/blog/2025/11/11/lima-becomes-a-cncf-incubating-project/)
- [lima-devbox (Claude Skill)](https://github.com/recodelabs/lima-devbox)
- [Lume](https://cua.ai/docs/lume) — MIT-licensed macOS/Linux VM tool for AI agents
- [Lume Comparison](https://cua.ai/docs/lume/guide/getting-started/comparison)
- [From Lume to Containerization (Blog)](https://cua.ai/blog/lume-to-containerization)
- [Tart](https://tart.run/) — Security-focused macOS VMs by Cirrus Labs
- [Tart Licensing](https://tart.run/licensing/)
- [UTM](https://mac.getutm.app/) — General-purpose VMs
- [OrbStack Architecture](https://docs.orbstack.dev/architecture)

## macOS Security Internals

- [Configuring the Hardened Runtime (Apple)](https://developer.apple.com/documentation/xcode/configuring-the-hardened-runtime)
- [The Hardened Runtime Explained (Eclectic Light)](https://eclecticlight.co/2019/08/10/the-hardened-runtime-explained/)
- [macOS TCC (HackTricks)](https://angelica.gitbook.io/hacktricks/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-tcc)
- [Permissions, Privacy and TCC (Eclectic Light)](https://eclecticlight.co/2025/11/08/explainer-permissions-privacy-and-tcc/)
- [System Integrity Protection (Apple)](https://support.apple.com/guide/security/system-integrity-protection-secb7ea06b49/web)
- [Launch Constraints Deep Dive (theevilbit)](https://theevilbit.github.io/posts/launch_constraints_deep_dive/)
- [Applying Launch Constraints (Apple)](https://developer.apple.com/documentation/security/applying-launch-environment-and-library-constraints)
- [FileVault and Volume Encryption (Eclectic Light)](https://eclecticlight.co/2025/01/10/filevault-and-volume-encryption-explained/)
- [Volume Encryption with FileVault (Apple)](https://support.apple.com/guide/security/volume-encryption-with-filevault-sec4c6dc1b6e/web)
- [CVE-2025-31191 Sandbox Escape (Microsoft)](https://www.microsoft.com/en-us/security/blog/2025/05/01/analyzing-cve-2025-31191-a-macos-security-scoped-bookmarks-based-sandbox-escape/)

## Network Security

- [LuLu (Objective-See)](https://objective-see.org/products/lulu.html)
- [LuLu GitHub](https://github.com/objective-see/LuLu)
- [Little Snitch](https://www.obdev.at/products/littlesnitch)
- [Vallum Firewall](https://vallumfirewall.com/)
- [AppFirewall (open source)](https://github.com/doug-leith/appFirewall)
- [pf Firewall Rules on macOS](https://blog.neilsabol.site/post/quickly-easily-adding-pf-packet-filter-firewall-rules-macos-osx/)
- [pf on macOS](https://jaytaylor.com/notes/node/1734370466000.html)
- [OpenBSD pf.conf Manual](https://man.openbsd.org/pf.conf) — `user` keyword docs
- [Murus Firewall](https://murusfirewall.com/)
- [Tailscale ACLs](https://tailscale.com/kb/1018/acls)
- [Tailscale Exit Nodes](https://tailscale.com/docs/features/exit-nodes)

## Security Analysis & Incidents

- [Claude Code Security Best Practices (Backslash)](https://www.backslash.security/blog/claude-code-security-best-practices)
- [Claude Code dangerously-skip-permissions Guide](https://www.ksred.com/claude-code-dangerously-skip-permissions-when-to-use-it-and-when-you-absolutely-shouldnt/)
- [Claude Code dangerously-skip-permissions (Thomas Wiegold)](https://thomas-wiegold.com/blog/claude-code-dangerously-skip-permissions/)
- [Indirect Prompt Injection in Claude Code (Lasso)](https://www.lasso.security/blog/the-hidden-backdoor-in-claude-coding-assistant)
- [RCE and API Token Exfiltration CVEs (Check Point)](https://research.checkpoint.com/2026/rce-and-api-token-exfiltration-through-claude-code-project-files-cve-2025-59536/)
- [Claude Code Flaws (The Hacker News)](https://thehackernews.com/2026/02/claude-code-flaws-allow-remote-code.html)
- [Lasso claude-hooks (Prompt Injection Detection)](https://github.com/lasso-security/claude-hooks)
- [Claude Code Permissions Guide (eesel.ai)](https://www.eesel.ai/blog/claude-code-permissions)
- [Permissions and Security (SFEIR)](https://institute.sfeir.com/en/claude-code/claude-code-permissions-and-security/)

## Industry Comparisons

- [Cursor Agent Sandboxing](https://cursor.com/blog/agent-sandboxing) — How Cursor uses Seatbelt
- [Deep Dive on Agent Sandboxes](https://pierce.dev/notes/a-deep-dive-on-agent-sandboxes) — Isolation technology comparison
- [How to Sandbox AI Agents in 2026 (Northflank)](https://northflank.com/blog/how-to-sandbox-ai-agents)
- [Best Code Execution Sandbox for AI Agents (Northflank)](https://northflank.com/blog/best-code-execution-sandbox-for-ai-agents)
- [Sandboxing Claude Code on macOS (Infralovers)](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)
- [Sandbox Your AI Dev Tools (Lima Guide)](https://www.metachris.dev/2025/11/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima/)
- [Let's Discuss Sandbox Isolation](https://www.shayon.dev/post/2026/52/lets-discuss-sandbox-isolation/)
- [Apple Validates Hypervisor-Isolated Containers (Edera)](https://edera.dev/stories/apple-just-validated-hypervisor-isolated-containers-heres-what-that-means)
- [GitHub Copilot Coding Agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent)
- [Cursor vs Windsurf vs Claude Code 2026](https://dev.to/pockit_tools/cursor-vs-windsurf-vs-claude-code-in-2026-the-honest-comparison-after-using-all-three-3gof)

## Community Discussion

- [HN: How Are You Sandboxing Coding Agents?](https://news.ycombinator.com/item?id=46400129)
- [HN: sandbox-exec Deprecation Discussion](https://news.ycombinator.com/item?id=44283454)
- [Sandboxing AI Coding Agents (mfyz)](https://mfyz.com/ai-coding-agent-sandbox-container/)

## Apple Technologies

- [Meet Containerization (WWDC25)](https://developer.apple.com/videos/play/wwdc2025/346/)
- [What is Apple Container?](https://medium.com/@kielhyre/what-is-apple-container-a49728ce6c2b)
- [Apple Containers vs Docker Desktop vs OrbStack](https://www.repoflow.io/blog/apple-containers-vs-docker-desktop-vs-orbstack)
- [Apple Containers Technical Comparison (The New Stack)](https://thenewstack.io/apple-containers-on-macos-a-technical-comparison-with-docker/)
