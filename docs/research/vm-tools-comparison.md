# VM Tools Comparison

Detailed comparison of macOS virtualization tools for sandboxing AI agents.

## Summary Table

| Tool | Free? | License | Isolation | Guest OS | Default Mounts | API | Best For |
|------|-------|---------|-----------|----------|----------------|-----|----------|
| **Lima** | Yes | Apache 2.0 | Hypervisor | Linux | Home dir (remove!) | CLI only | Free Linux VMs |
| **Lume** | Yes | MIT | Hypervisor | macOS + Linux | Configurable | HTTP + MCP | AI agent integration |
| **Tart** | Personal | Fair Source | Hypervisor | macOS + Linux | None | CLI only | Security defaults, CI/CD |
| **UTM** | Yes | Apache 2.0 | Hypervisor | Any | Manual | GUI only | Interactive desktop VMs |
| **OrbStack** | Personal | Proprietary | Shared kernel | Linux | Home dir | CLI + GUI | Docker dev convenience |
| **Apple Containers** | Yes | Open source | MicroVM | Linux | None | Swift API | Future (macOS 26+) |

## Lima

**CNCF incubating project.** The most proven free option for Linux VMs on macOS.

- Uses Virtualization.framework (or QEMU fallback)
- Automatic file sharing via VirtioFS/9p
- Automatic port forwarding
- Built-in containerd support
- 15+ Linux distributions
- CLI-driven: `limactl create`, `limactl start`, `limactl shell`

**Performance:**
- File operations crossing VM boundary: ~3x penalty (8.99s for metadata-heavy ops)
- Cloning repos inside the VM achieves native ext4 performance
- Recommended: 6 CPUs, 16GB RAM allocation

**Hardening checklist:**
- [ ] Remove all home directory mounts (`mounts: []`)
- [ ] Clone repos inside the VM
- [ ] Add iptables rules for outbound filtering
- [ ] Install Docker inside the VM (don't share from host)
- [ ] Pass only API key (not SSH keys or cloud credentials)

**Community projects:**
- `lima-devbox`: Claude Skill for Lima VM setup
- `open-cowork`: Open-source Claude Cowork alternative

## Lume

**AI-native VM tool** by Cua (trycua). Anthropic's Claude Cowork uses the same Virtualization.framework.

- Single binary with HTTP API (`lume serve`)
- MCP server for AI agent framework integration
- Automated macOS Setup Assistant via VNC + OCR
- OCI registry support (GHCR, GCS)
- Headless mode (`--no-display`)

**Requirements:**
- Apple Silicon only
- macOS 13.0+
- Minimum 8GB RAM (16GB recommended), 30GB disk

**Unique advantages:**
- Only tool with an MCP server for programmatic VM control
- Only tool with automated macOS guest setup
- HTTP API enables building custom orchestration

**Limitations:**
- Young project (v0.2.x)
- Smaller community than Lima or Tart
- Limited GPU support (paravirtualized, Family 5 only)

## Tart

**Security-focused** VM tool by Cirrus Labs. Used by "tens of thousands of engineers."

- Built on Virtualization.framework
- OCI registry support
- Packer integration for automated builds
- Mature CI/CD integrations (Cirrus CI, GitHub Actions, GitLab)

**Security advantages:**
- **No filesystem mounts by default** (most secure default of any tool)
- Configurable Softnet filtering for network isolation
- Rated "most security-conscious" for CI/CD pipelines (Infralovers analysis)

**Pricing:**
- Personal workstation: free
- Organizations under 100 CPU cores: free
- Gold: $12,000/year (up to 500 cores)
- Platinum: $36,000/year (up to 3,000 cores)

## UTM

General-purpose VM application for macOS, iOS, and visionOS.

- Uses Hypervisor framework for ARM64 guests (near-native speed)
- Uses QEMU for x86/x64 emulation (slower) on Apple Silicon
- GUI-focused with polished macOS interface
- Supports Windows, Linux, macOS guests

**Not recommended for agent sandboxing:**
- GUI-first design — no HTTP API, no CLI-first workflow
- No MCP server, no agent framework integration
- No automated/unattended VM provisioning
- No registry support for VM images

## OrbStack

Lightweight Docker Desktop alternative and Linux VM manager.

- Free for personal, non-commercial use
- Commercial: $8/user/month
- Boots in ~2 seconds, <1GB RAM idle
- Native macOS app (Swift, not Electron)

**Security concerns for agent sandboxing:**
- **Shared kernel model** — container escapes expose the shared VM
- Documentation states: "Linux machines are considered trusted because OrbStack provides integration with macOS"
- Home directory and file mounts enabled by default
- CVE-2025-9074 (CVSS 9.3) demonstrated escape from Docker container
- **Not suitable for untrusted agent isolation**

## Apple Containers

Native OCI-compliant Linux container runtime announced at WWDC 2025 (Session 346, "Meet Containerization"). Open source under Apache 2.0. Latest release: **v0.11.0** (March 31, 2025).

Two components:
- [`apple/containerization`](https://github.com/apple/containerization) — Swift framework providing low-level APIs for image management, VM lifecycle, ext4 filesystem, networking
- [`apple/container`](https://github.com/apple/container) — CLI tool built on the framework (`container run`, `container build`, etc.)

Architecture:
- **Each container runs in its own lightweight VM** via `Virtualization.framework` (unlike Docker's shared-kernel model)
- Boots a Linux kernel (Kata-based, v6.12.28+) with `vminitd` (minimal Swift-compiled static init using musl libc)
- **Linux guests only** — macOS-native binaries (Mach-O) cannot execute inside these containers
- **Apple Silicon only** — no Intel Mac support
- Full networking features require **macOS 26 (Tahoe)**

Performance:
- Startup: **200-700ms** (boots a full Linux kernel per container)
- Alpine image ~733ms measured by [Anil Madhavapeddy](https://anil.recoil.org/notes/apple-containerisation); other sources report 200-400ms
- File I/O: VirtioFS shared mounts, performance comparable to Docker Desktop

Filesystem:
- `--read-only` rootfs supported (PR #999, January 2026)
- Read-only bind mounts via `--mount type=bind,...,readonly`
- **No equivalent to Seatbelt's per-path deny rules** — the model is mount-only (unmounted paths are invisible, but there are no active deny rules for mounted content)

Network isolation (**significant gaps**):
- Each container gets its own IP via vmnet
- Networks can be created and are isolated from each other
- **No built-in per-container firewall** or port/protocol blocking
- Host gateway reachable from `--internal` networks — "any host service bound to 0.0.0.0 is accessible from inside the agent VM without going through the proxy" ([GitHub discussion #719](https://github.com/apple/container/discussions/719))
- **pf does not filter vmnet-bridged traffic** — host-side firewall rules are ineffective
- No native DNS blocking or domain allowlisting (workarounds via Squid proxy on dual-homed network)
- Privileged process inside the VM could bypass guest-side iptables

Security:
- CVE-2026-20613 fixed in v0.9.0 (first reported CVE)
- VM-per-container provides hypervisor-level isolation — strongest tier short of separate physical machines
- But the network isolation gaps are real and documented

**Critical distinction for agent sandboxing:** Apple Containers run **Linux** binaries, not macOS-native processes. Claude Code (Node.js on macOS), Cursor, Gemini CLI, and other macOS-native AI tools cannot run natively inside Apple Containers. You would need Linux builds of these tools — effectively the same tradeoff as Docker. This makes Apple Containers a better Docker, not a replacement for macOS-native sandboxing (Seatbelt, user isolation, pf).

**Not a replacement for sandbox-exec.** Apple Containerization is a completely different mechanism: hypervisor VMs vs. process-level kernel sandbox. It does not address the use case of restricting what a macOS-native CLI tool can do. There is no indication Apple intends it as a sandbox-exec successor.

**Ecosystem adoption:**
- [SandboxedClaudeCode](https://github.com/CaptainMcCrank/SandboxedClaudeCode) offers Apple Container as one of three isolation options (alongside Bubblewrap and Firejail)
- [NanoClaw](https://nanoclaws.io/blog/apple-container-macos-agent-sandbox) uses Apple Container for per-conversation isolated AI agent environments
- Docker has **not** adopted Apple Containerization as a backend; pursuing their own Docker VMM solution
- Lume is positioning to integrate with it ([blog](https://cua.ai/blog/lume-to-containerization))

**References:**
- [Meet Containerization — WWDC 2025 Session 346](https://developer.apple.com/videos/play/wwdc2025/346/)
- [Under the hood with Apple's Containerization — Anil Madhavapeddy](https://anil.recoil.org/notes/apple-containerisation)
- [Apple Containers Technical Comparison with Docker — The New Stack](https://thenewstack.io/apple-containers-on-macos-a-technical-comparison-with-docker/)
- [Apple Containerization Deep Dive — kevnu.com](https://www.kevnu.com/en/posts/apple-native-containerization-deep-dive-architecture-comparisons-and-practical-guide)
- [GitHub Discussion #719: Firewall for container network](https://github.com/apple/container/discussions/719)
- [NanoClaw Apple Container Blog](https://nanoclaws.io/blog/apple-container-macos-agent-sandbox)

## Isolation Strength Ranking

1. **Separate physical machine** — strongest, most expensive
2. **Full VMs** (Lima, Lume, Tart) — hardware-enforced, separate kernel
3. **MicroVMs** (Docker Sandboxes, Apple Containers) — VM per container, but network isolation gaps in Apple Containers
4. **OS-level process sandbox + user isolation** (Hazmat) — kernel MACF hooks + Unix permissions + pf firewall. No VM overhead. Not hypervisor-isolated, but multiple independent layers compensate. See [tier2-user-pf-isolation.md](tier2-user-pf-isolation.md)
5. **OS-level process sandbox** (Seatbelt/Landlock/seccomp wrappers) — single-layer, same user. Claude Code `/sandbox`, Codex CLI, Cursor, NVIDIA OpenShell (Linux)
6. **gVisor** — user-space kernel, ~70 host syscalls
7. **WebAssembly** — no kernel, explicit imports
8. **Docker containers** — shared kernel, ~340 syscalls — **weakest**. UK AISI SandboxEscapeBench: GPT-5 escapes ~50% of misconfigured containers ([Source](https://www.aisi.gov.uk/blog/can-ai-agents-escape-their-sandboxes-a-benchmark-for-safely-measuring-container-breakout-capabilities))

## Recommendation

| Use Case | Tool |
|----------|------|
| Free Linux VM, daily use | Lima |
| AI agent integration, programmatic control | Lume |
| Strongest security defaults | Tart |
| Need macOS guest | Lume or Tart |
| Interactive desktop VM | UTM |
| Docker dev convenience (trusted code only) | OrbStack |
| macOS-native agent containment (no VM) | Hazmat |
| Linux agent sandboxing (Landlock + seccomp) | [NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) |
