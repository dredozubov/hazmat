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

## Apple Containers (Future)

Native OCI-compliant Linux container support announced at WWDC 2025.

- Written in Swift, built on Virtualization.framework
- **Each container runs in its own lightweight VM** (unlike Docker's shared-kernel)
- Optimized for Apple Silicon
- Open source on GitHub
- Version 0.1.0 — not yet ready for production

**When mature, this could become the gold standard** — it provides VM-level isolation with container-level ease of use. Lume is already positioning to integrate with it.

## Isolation Strength Ranking

1. **Separate physical machine** — strongest, most expensive
2. **Full VMs** (Lima, Lume, Tart) — hardware-enforced, separate kernel
3. **MicroVMs** (Docker Sandboxes, Apple Containers) — VM per container
4. **gVisor** — user-space kernel, ~70 host syscalls
5. **WebAssembly** — no kernel, explicit imports
6. **Docker containers** — shared kernel, ~340 syscalls — **weakest**

## Recommendation

| Use Case | Tool |
|----------|------|
| Free Linux VM, daily use | Lima |
| AI agent integration, programmatic control | Lume |
| Strongest security defaults | Tart |
| Need macOS guest | Lume or Tart |
| Interactive desktop VM | UTM |
| Docker dev convenience (trusted code only) | OrbStack |
