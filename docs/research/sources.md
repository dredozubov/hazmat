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
- [Sandboxing on macOS](https://bdash.net.nz/posts/sandboxing-on-macos/) — Technical deep dive; best analysis of the deprecation gap and why App Sandbox is not a replacement
- [macOS Sandbox (HackTricks)](https://book.hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-sandbox/index.html)
- [macOS Sandbox Debug & Bypass (HackTricks)](https://book.hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-sandbox/macos-sandbox-debug-and-bypass/index.html)
- [macOS Sandbox Profiles (SBPL Guide)](https://gist.github.com/cedws/b7dd60878a208be09ae7d766e6317061)
- [OSX Sandbox Seatbelt Profiles](https://github.com/s7ephen/OSX-Sandbox--Seatbelt--Profiles)
- [Custom macOS Sandbox Profiles](https://github.com/Ozymandias42/macOS-Sandbox-Profiles)
- [Chromium Mac Sandbox V2 Design Doc](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/sandbox/mac/seatbelt_sandbox_design.md) — Documents Chromium's dependency on `sandbox_init_with_parameters()`
- [Codex CLI issue #215: sandbox-exec deprecation](https://github.com/openai/codex/issues/215) — OpenAI kept it because "it still works very well"
- [HN: sandbox-exec Deprecation Discussion](https://news.ycombinator.com/item?id=44283454) — Community analysis including Apple insider perspective
- [HN: macOS's Little-Known Command-Line Sandboxing Tool](https://news.ycombinator.com/item?id=47101200)
- [Apple Developer Forums thread 661939](https://developer.apple.com/forums/thread/661939) — "How to build a replacement for sandbox-exec?" — No official Apple response
- [macOS 26 Tahoe Release Notes](https://developer.apple.com/documentation/macos-release-notes/macos-26-release-notes) — Silent on sandbox API changes

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

- [Meet Containerization — WWDC 2025 Session 346](https://developer.apple.com/videos/play/wwdc2025/346/)
- [apple/containerization GitHub](https://github.com/apple/containerization) — Swift framework (Apache 2.0)
- [apple/container GitHub](https://github.com/apple/container) — CLI tool (Apache 2.0), v0.11.0 as of March 2025
- [Under the hood with Apple's Containerization — Anil Madhavapeddy](https://anil.recoil.org/notes/apple-containerisation) — Best independent technical analysis; startup benchmarks, architecture breakdown
- [Apple Containers Technical Comparison with Docker — The New Stack](https://thenewstack.io/apple-containers-on-macos-a-technical-comparison-with-docker/)
- [What You Need To Know About Apple's Container Framework — The New Stack](https://thenewstack.io/what-you-need-to-know-about-apples-new-container-framework/)
- [Apple Containerization Deep Dive — kevnu.com](https://www.kevnu.com/en/posts/apple-native-containerization-deep-dive-architecture-comparisons-and-practical-guide)
- [What is Apple Container?](https://medium.com/@kielhyre/what-is-apple-container-a49728ce6c2b)
- [Apple Containers vs Docker Desktop vs OrbStack](https://www.repoflow.io/blog/apple-containers-vs-docker-desktop-vs-orbstack)
- [GitHub Discussion #719: Firewall for container network](https://github.com/apple/container/discussions/719) — Documents network isolation gaps (host gateway reachable, pf doesn't filter vmnet)
- [GitHub Issue #990: Read-only rootfs support](https://github.com/apple/container/issues/990)
- [NanoClaw: Apple Container for AI Agent Sandboxing](https://nanoclaws.io/blog/apple-container-macos-agent-sandbox)
- [SandboxedClaudeCode](https://github.com/CaptainMcCrank/SandboxedClaudeCode) — Uses Apple Container as one isolation option
- [From Lume to Containerization](https://cua.ai/blog/lume-to-containerization) — Lume's integration plans
- [Apple Validates Hypervisor-Isolated Containers — Edera](https://edera.dev/stories/apple-just-validated-hypervisor-isolated-containers-heres-what-that-means)
- [Docker Forum: Apple Container as Docker backend](https://forums.docker.com/t/apple-container-as-a-backend-for-docker-desktop-on-macos-26/149273) — Community request, no Docker response

## Competitive Agent Sandboxing

- [Anthropic: Claude Code Sandboxing Architecture](https://www.anthropic.com/engineering/claude-code-sandboxing) — Built-in sandbox technical design
- [OpenAI: Codex Sandboxing](https://developers.openai.com/codex/concepts/sandboxing) — Three-platform OS-native sandboxing
- [OpenAI: Codex Agent Approvals & Security](https://developers.openai.com/codex/agent-approvals-security)
- [Codex CLI GitHub](https://github.com/openai/codex) — Rust rewrite, 67K+ stars, Apache 2.0
- [Google Gemini CLI](https://github.com/google-gemini/gemini-cli) — Six built-in Seatbelt profiles
- [Cursor: Agent Sandboxing](https://cursor.com/blog/agent-sandboxing) — Seatbelt + Landlock + seccomp, WSL2 on Windows
- [NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) — Linux-focused agent sandbox using Landlock/seccomp
- [NVIDIA OpenShell Developer Blog](https://developer.nvidia.com/blog/run-autonomous-self-evolving-agents-more-safely-with-nvidia-openshell/)
- [UK AISI SandboxEscapeBench](https://www.aisi.gov.uk/blog/can-ai-agents-escape-their-sandboxes-a-benchmark-for-safely-measuring-container-breakout-capabilities) — GPT-5 escapes containers ~50% of the time
- [SandboxEscapeBench Paper (arXiv)](https://arxiv.org/abs/2603.02277)
- [Pillar Security: Cursor Agent Security Paradox](https://www.pillar.security/blog/the-agent-security-paradox-when-trusted-commands-in-cursor-become-attack-vectors)
- [Pillar Security: Hidden Security Risks of SWE Agents](https://www.pillar.security/blog/the-hidden-security-risks-of-swe-agents-like-openai-codex-and-devin-ai)
- [Endor Labs: Cursor Security](https://www.endorlabs.com/learn/cursor-security)

## Claude Code Source Leak (March 31, 2026)

- [VentureBeat: Claude Code Source Leak](https://venturebeat.com/technology/claude-codes-source-code-appears-to-have-leaked-heres-what-we-know) — 512K lines exposed via npm source map
- [VentureBeat: Security Actions After Leak](https://venturebeat.com/security/claude-code-512000-line-source-leak-attack-paths-audit-security-leaders)
- [The Register: Claude Code Source Leak](https://www.theregister.com/2026/04/01/claude_code_source_leak_privacy_nightmare/)
- [Fortune: Anthropic Source Code Leak](https://fortune.com/2026/03/31/anthropic-source-code-claude-code-data-leak-second-security-lapse-days-after-accidentally-revealing-mythos/)

## AI Security M&A (2025-2026)

- [OpenAI acquires Promptfoo](https://openai.com/index/openai-to-acquire-promptfoo/) — AI agent security testing
- [CNBC: OpenAI Acquires Promptfoo](https://www.cnbc.com/2026/03/09/open-ai-cybersecurity-promptfoo-ai-agents.html)
- [TechCrunch: OpenAI Acquires Promptfoo](https://techcrunch.com/2026/03/09/openai-acquires-promptfoo-to-secure-its-ai-agents/)
- [Accenture/Anthropic: Cyber.AI Partnership](https://newsroom.accenture.com/news/2026/accenture-and-anthropic-team-to-help-organizations-secure-scale-ai-driven-cybersecurity-operations)
- [Microsoft: OWASP Top 10 for Agentic AI](https://www.microsoft.com/en-us/security/blog/2026/03/30/addressing-the-owasp-top-10-risks-in-agentic-ai-with-microsoft-copilot-studio/)
- [Microsoft: Securing AI Agents](https://www.microsoft.com/en-us/security/blog/2026/01/23/runtime-risk-realtime-defense-securing-ai-agents/)
- [Software Strategies: Agentic AI Security Startups and M&A](https://softwarestrategiesblog.com/2026/03/28/agentic-ai-security-startups-funding-mna-rsac-2026/) — $96B in security M&A across 400 transactions (2025-2026)
