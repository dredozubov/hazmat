# Tier 3: Docker Sandboxes & Devcontainers

**Effort:** 15 minutes | **Performance:** ~90% native | **Cost:** Free (Docker Desktop personal)

## Option A: Docker Sandboxes (Simplest, Strongest Desktop Isolation)

Docker Sandboxes (Docker Desktop 4.58+) run each agent in a **dedicated microVM** — not a regular container. Each gets its own Docker daemon and kernel.

### Setup

```bash
# Install Docker Desktop (free for personal use)
# Then:
docker sandbox run claude ~/my-project
```

### Network Isolation

```bash
docker sandbox network proxy claude-myproject --allow-host "api.anthropic.com"
docker sandbox network proxy claude-myproject --allow-host "github.com"
docker sandbox network proxy claude-myproject --allow-host "registry.npmjs.org"
docker sandbox network proxy claude-myproject --deny-host "*"
```

### Management

```bash
docker sandbox ls                        # list sandboxes
docker sandbox exec -it <name> bash      # shell into sandbox
docker sandbox rm <name>                 # delete sandbox
docker sandbox run claude ~/project ~/docs:ro  # read-only extra mounts
```

### Why This Is the Best Balance

- MicroVM-based hard security boundary (not just container namespaces)
- Agent can build/run Docker containers inside the sandbox without affecting host
- `--dangerously-skip-permissions` is safe because the microVM is the boundary
- Workspace syncs between host and sandbox at the same absolute path
- Supports Claude Code, Gemini CLI, Codex, Copilot, Kiro

### Authentication

Set `ANTHROPIC_API_KEY` in `~/.bashrc` or `~/.zshrc` globally (not inline — the daemon runs independently of the current shell).

## Option B: Official Anthropic Devcontainer

Anthropic provides a reference devcontainer with a default-deny firewall.

```bash
# Clone the reference
git clone https://github.com/anthropics/claude-code .devcontainer-ref
# Copy .devcontainer/ into your project
# Open in VS Code -> "Reopen in Container"
```

**Included security:**
- Default-deny firewall (`init-firewall.sh`)
- Allowlist: npm registry, GitHub, Claude API, Sentry only
- DNS and SSH outbound permitted
- `--dangerously-skip-permissions` is safe inside

Source: [github.com/anthropics/claude-code/tree/main/.devcontainer](https://github.com/anthropics/claude-code/tree/main/.devcontainer)

## Option C: Trail of Bits Devcontainer (Security-Focused)

```bash
git clone https://github.com/trailofbits/claude-code-devcontainer
```

**Features:**
- Ubuntu 24.04, Node.js 22, Python 3.13
- Read-only host mounts for git config
- Persistent volumes for command history, Claude config, and GitHub CLI auth
- Does NOT mount the Docker socket
- Optional iptables rules for network restriction

## Option D: Docker Compose + Squid Proxy (Maximum Control)

```yaml
services:
  proxy:
    image: ubuntu/squid
    volumes:
      - ./squid.conf:/etc/squid/squid.conf:ro
    networks: [isolated, internet]

  agent:
    build: .
    environment:
      - HTTPS_PROXY=http://proxy:3128
      - ANTHROPIC_API_KEY
    volumes:
      - ./workspace:/workspace
    networks: [isolated]
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    deploy:
      resources:
        limits:
          cpus: "4"
          memory: "8g"

networks:
  isolated:
    internal: true   # no internet access
  internet:
    driver: bridge
```

### Squid Config (squid.conf)

```
acl allowed_domains dstdomain .anthropic.com
acl allowed_domains dstdomain .github.com
acl allowed_domains dstdomain registry.npmjs.org
acl allowed_domains dstdomain .sentry.io
acl allowed_domains dstdomain .statsigapi.net

http_access allow allowed_domains
http_access deny all

http_port 3128
```

### Dockerfile

```dockerfile
FROM node:22-slim
RUN npm install -g @anthropic-ai/claude-code
RUN apt-get update && apt-get install -y git curl ca-certificates && rm -rf /var/lib/apt/lists/*
USER node
WORKDIR /workspace
```

## Critical Rules for All Docker Approaches

1. **NEVER mount `/var/run/docker.sock`** into the container — if the agent can talk to the Docker daemon, it can create privileged containers and escape completely
2. `--dangerously-skip-permissions` inside containers does **not prevent exfiltration** of anything accessible in the container, including Claude Code credentials
3. Performance overhead on macOS: bind mounts are ~3.5x slower than native for metadata-heavy ops (improved with VirtioFS but still measurable)

## Docker Desktop Pricing

- **Personal:** Free (individuals, education, open source, small businesses <250 employees AND <$10M revenue)
- **Pro:** $9/month
- **Team:** $15/user/month
- **Business:** $21/user/month

## Free Alternative: Colima

If you want Docker without Docker Desktop:

```bash
brew install colima docker
colima start --cpu 4 --memory 8 --disk 50
```

- Completely free (MIT license)
- ~400MB RAM idle vs Docker Desktop's 2GB+
- CLI-only (no GUI)
- 24,000+ GitHub stars

## Free Alternative: Podman

```bash
brew install podman
podman machine init
podman machine start
```

- Completely free (Apache 2.0)
- Rootless by default (more secure than Docker)
- Daemonless (no privileged background service)
- Some Docker Compose features may need adjustment
