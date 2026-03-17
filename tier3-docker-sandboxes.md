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

> **Required for this threat model.** Docker Sandboxes default to allow-all egress. Switch
> to deny-mode with an explicit allowlist before running any agent session:

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
    user: "1000:1000"           # never run as root
    read_only: true
    tmpfs: [/tmp, /run]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    # Use service-level limits, not deploy.resources — the Compose spec says
    # deploy may be ignored by runtimes that don't support it (e.g. plain
    # `docker compose up` without Swarm mode).
    cpus: "4"
    mem_limit: 8g
    pids_limit: 200
    ulimits:
      nofile: { soft: 1024, hard: 1024 }
      nproc:  { soft: 200,  hard: 200 }

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

## Compose Hardening Reference

When writing or reviewing a `docker-compose.yml` for agent use, apply this checklist:

| Setting | Safe value | Why |
|---|---|---|
| `user` | `"UID:GID"` (non-root) | Root in container = root in VM; files written as root are hard to clean up |
| `read_only: true` | required | Prevents writes to container filesystem; use `tmpfs` for writable scratch |
| `tmpfs` | `[/tmp, /run]` | Writable scratch that is never persisted or shared |
| `cap_drop: [ALL]` | required | Drops all Linux capabilities; add back only what's proven necessary |
| `security_opt: ["no-new-privileges:true"]` | required | Prevents `setuid`/`setgid` privilege escalation |
| `cpus` / `mem_limit` / `pids_limit` | set explicit limits | Prevents fork bombs and resource exhaustion |
| `ulimits` | restrict `nproc`, `nofile` | Second line of defense against fork bombs |
| Port bindings | `"127.0.0.1:PORT:PORT"` only | Avoids exposing ports on `0.0.0.0` to the host network |
| Secrets | `secrets:` block | Mounts secrets as tmpfs files; `environment:` leaks them into `docker inspect` |

### What to ban in agent-facing Compose files

```yaml
# NEVER allow any of these:
privileged: true                    # removes all container isolation
network_mode: host                  # container shares VM network namespace
network_mode: none                  # agent cannot reach Anthropic API
volumes:
  - /var/run/docker.sock:/var/run/docker.sock  # full daemon escape
  - /:/hostfs                       # full VM root access
environment:
  - ANTHROPIC_API_KEY=${KEY}        # prefer secrets: block
```

`use_api_socket: true` (Docker Compose's Buildx shorthand for socket mounting) is equally prohibited.

> **CVE-2025-62725** (docker-compose < 2.40.2, affects >= 2.34.0): When a `docker-compose.yml`
> references a remote OCI artifact as its source, compose resolves annotation paths without
> sanitisation, enabling path traversal to write arbitrary host files. The trigger is remote
> OCI artifact resolution, not a plain local compose file. Ensure docker-compose >= 2.40.2.

## Critical Rules for All Docker Approaches

1. **NEVER share the host Docker socket** — giving the agent access to any Docker daemon it did not exclusively own is a full sandbox escape. `chmod 700 ~/.docker/run/docker.sock` keeps the host daemon inaccessible to other users (enforced by `sandbox setup` and verified by `sandbox test`).
2. **NEVER mount `/var/run/docker.sock`** into a container — if the agent can talk to the daemon, it can create privileged containers and escape completely.
3. **Set deny-mode network policy** on Docker Sandboxes before each session — the default is allow-all.
4. `--dangerously-skip-permissions` inside containers does **not prevent exfiltration** of anything accessible in the container, including Claude Code credentials.
5. Performance overhead on macOS: bind mounts are ~3.5x slower than native for metadata-heavy ops (improved with VirtioFS but still measurable).

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
