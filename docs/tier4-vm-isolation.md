# Tier 4: Full VM Isolation

**Effort:** 45 minutes | **Performance:** ~85-95% native | **Cost:** Free

For fully autonomous agents running 24/7 or working on untrusted codebases. This is the strongest practical isolation available on macOS.

All three tools below use Apple's Virtualization.framework on Apple Silicon:
- **Hardware-enforced CPU boundaries** via the hypervisor
- **Separate kernel** per VM (unlike Docker, which shares the host kernel)
- Container escape -> VM escape required to reach host (much higher bar)

## Option A: Lima (Recommended — Free, CNCF Incubating)

The most proven free option. CNCF incubating project (promoted from Sandbox Nov 2025).

### Setup

```bash
brew install lima

cat > claude-vm.yaml << 'EOF'
images:
  - location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img"
    arch: "aarch64"
cpus: 6
memory: "16GiB"
disk: "50GiB"
mounts: []   # CRITICAL: no host mounts
provision:
  - mode: system
    script: |
      curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
      apt-get install -y nodejs git
      npm install -g @anthropic-ai/claude-code
      # Network lockdown
      iptables -P OUTPUT DROP
      iptables -A OUTPUT -o lo -j ACCEPT
      iptables -A OUTPUT -p tcp -d 160.79.104.0/23 --dport 443 -j ACCEPT
      iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
      iptables -A OUTPUT -p tcp -d 140.82.112.0/20 --dport 443 -j ACCEPT
      iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
EOF

limactl create --name=claude-sandbox claude-vm.yaml
limactl start claude-sandbox
limactl shell claude-sandbox
```

### Inside the VM

```bash
export ANTHROPIC_API_KEY="example-anthropic-key"
git clone https://github.com/your/project.git  # clone INSIDE the VM
cd project
claude --dangerously-skip-permissions
```

### Critical Hardening

The `mounts: []` line is essential. Lima's default config mounts your home directory into the VM — SSH keys, cloud credentials, and browser data would be accessible. **Always remove home mounts.**

Additional hardening:
- Clone repos inside the VM rather than using bind mounts (native ext4 performance vs ~3x penalty for cross-VM FS)
- Install Docker inside the VM rather than sharing from host
- Use multi-VM strategy: separate VMs for trusted, experimental, and untrusted workloads

### VS Code Integration

```bash
# Connect VS Code to the VM via SSH
# Note: VS Code's remote docs warn a compromised remote could use the connection back to the host
```

### Specs

- License: Apache 2.0
- Guest OS: Linux (15+ distros)
- Default mounts: Home dir (**remove!**)
- Community: CNCF, large, active

## Option B: Lume (AI-Native, macOS VMs)

Best if you need a macOS guest or want MCP server integration for programmatic VM control. Used by Anthropic's Claude Cowork.

### Setup

```bash
# Install
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/trycua/cua/main/libs/lume/scripts/install.sh)"

# Create macOS VM
lume create claude-sandbox --os macos --ipsw latest

# After setup: SSH in, install Claude Code
ssh user@$(lume get claude-sandbox | grep ip | awk '{print $2}')
npm install -g @anthropic-ai/claude-code

# Create golden snapshot for instant rollback
lume stop claude-sandbox
lume clone claude-sandbox claude-sandbox-golden

# Reset anytime:
lume stop claude-sandbox && lume delete claude-sandbox
lume clone claude-sandbox-golden claude-sandbox

# Run headlessly
lume run claude-sandbox --no-display
```

### Key Features

- HTTP API (`lume serve`) for programmatic VM control
- MCP server for AI agent framework integration
- Automated macOS Setup Assistant handling via VNC + OCR
- OCI registry support for VM image management
- Headless mode for CI/CD and agent workflows

### Specs

- License: MIT
- Guest OS: macOS + Linux
- Default mounts: Configurable
- Apple Silicon only, macOS 13.0+
- Community: Small, new

### Golden Snapshot Pattern (from OpenClaw report)

```bash
# Create a clean baseline
lume stop claude-sandbox
lume clone claude-sandbox claude-sandbox-golden

# After any compromise or destructive action:
lume stop claude-sandbox && lume delete claude-sandbox
lume clone claude-sandbox-golden claude-sandbox
lume run claude-sandbox --no-display
```

This is the single most powerful pattern for autonomous agents — instant rollback to a known-good state.

## Option C: Tart (Best Security Defaults)

Most security-conscious option. Used by "tens of thousands of engineers" for CI/CD.

### Setup

```bash
brew install cirruslabs/cli/tart

# From a base image (no filesystem mounts by default)
tart clone ghcr.io/cirruslabs/ubuntu:latest claude-sandbox
tart run claude-sandbox
```

### Key Features

- No filesystem mounts by default (most secure default)
- Softnet packet filtering for network restrictions
- OCI registry support for VM image distribution
- Mature CI/CD integrations (Cirrus CI, GitHub Actions, GitLab)
- Packer integration for automated VM image building

### Specs

- License: Fair Source (free for personal use, free for orgs under 100 CPU cores)
- Guest OS: macOS + Linux
- Default mounts: None
- Community: Medium, established

## Comparison

| Aspect | Lima | Lume | Tart |
|--------|------|------|------|
| License | Apache 2.0 | MIT | Fair Source (free personal) |
| Guest OS | Linux | macOS + Linux | macOS + Linux |
| Default mounts | Home dir (**remove!**) | Configurable | **None** |
| HTTP API | No | Yes | No |
| MCP Server | No | Yes | No |
| Automated setup | Via cloud-init | Built-in (VNC+OCR) | Via Packer |
| CI/CD integrations | Minimal | Minimal | Extensive |
| Community | CNCF, largest | Smallest, newest | Medium |
| Best for | Free Linux VMs | AI agent integration | Security defaults, CI/CD |

## VM Isolation Strength

The isolation hierarchy (strongest to weakest):

1. **MicroVMs** (dedicated kernel, hardware boundary) — VMs, Firecracker
2. **gVisor** (user-space kernel, ~70 host syscalls)
3. **WebAssembly** (no kernel, explicit imports only)
4. **Docker containers** (shared kernel, ~340 syscalls) — weakest

All three tools here provide level 1 isolation.

## Limitations

- Apple Silicon only (no Intel Macs)
- The VM is not a perfect security boundary — Virtualization.framework shares the hypervisor with the host (stronger than Docker, weaker than a separate physical machine)
- Performance overhead is minimal for CPU but measurable for disk I/O (especially with shared mounts)
- You still need to keep the VM updated

## Community Projects

- **lima-devbox**: Claude Skill for Lima VM setup — [github.com/recodelabs/lima-devbox](https://github.com/recodelabs/lima-devbox)
- **open-cowork**: Open-source Claude Cowork alternative using Lima

## Future: Apple Containers (macOS 26+)

Apple announced native OCI-compliant container support at WWDC 2025. Each container runs in its own lightweight VM (unlike Docker's shared-kernel model). Written in Swift, built on Virtualization.framework. Version 0.1.0 — not yet ready for production use, but the direction Apple is pushing.
