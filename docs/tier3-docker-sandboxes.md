# Tier 3: Docker Sandboxes and Private-Daemon Workflows

**Effort:** 15 minutes | **Performance:** ~90% native | **Cost:** Free (Docker Desktop personal)

Tier 3 is the right fit only when the repo can run inside a **private Docker
daemon**. It is not the answer for shared-daemon local development setups.

## Read This When

Use Tier 3 when any of these are true:

- the project needs `docker build`, `docker run`, or `docker compose`
- the repo uses a devcontainer workflow that can live inside a private daemon
- Tier 2 blocks the workflow because the agent needs Docker control, but the host Docker socket must stay inaccessible
- you want a stronger boundary than "same user, same host daemon" but less overhead than a full VM

Do **not** use Tier 3 when the repo depends on the host daemon for shared
networks, shared volumes, Traefik routing, or other cross-project Docker state.
That case is covered in [shared-daemon-projects.md](shared-daemon-projects.md).

If the project does not need Docker, [setup-option-a.md](setup-option-a.md) and Tier 2 are usually the better default. For the broader selection logic, start with [overview.md](overview.md).

## Design Choices in Tier 3

- Give the agent a dedicated Docker daemon or microVM. Do not share the host daemon.
- Treat Docker networking as its own surface. Deny-mode egress is part of the design, not an optional extra.
- Treat Compose files as security-relevant configuration, not just developer convenience.
- Keep Docker Desktop and Compose versions current because container tooling itself has meaningful CVEs.

## Which Option Should You Pick?

| Option | Use when | Why it is not the default |
|--------|----------|---------------------------|
| **A. Docker Sandboxes** | You want the cleanest UX/security balance | Requires Docker Desktop 4.58+ |
| **B. Official Anthropic devcontainer** | You want Anthropic's reference container workflow | Heavier IDE/container workflow than Docker Sandboxes |
| **C. Trail of Bits devcontainer** | You want a security-focused devcontainer baseline | Still a devcontainer workflow, not the simplest day-to-day UX |
| **D. Compose + Squid proxy** | You need maximum control or want to design your own isolation | Highest setup complexity and easiest to misconfigure |

## Version Checks

Before relying on Tier 3, verify the toolchain:

- Docker Desktop `>= 4.44.3` to avoid CVE-2025-9074
- `docker-compose >= 2.40.2` to avoid CVE-2025-62725

Read [incidents-and-cves.md](research/incidents-and-cves.md) for why these versions matter.

## Option A: Docker Sandboxes (Recommended)

Docker Sandboxes (Docker Desktop 4.58+) run each agent in an isolated runtime
with a **private Docker daemon**. Each sandbox gets its own independent
container system, preventing lateral movement through daemon sharing.
The implementation uses a microVM internally, but the primary security
property for Hazmat's threat model is daemon isolation, not kernel
separation.

### Using Docker Sandboxes with Hazmat

Hazmat manages Docker Sandbox lifecycle automatically. Use `--docker=sandbox`
or let Hazmat auto-route when Docker artifacts are detected **and** the repo
looks compatible with a private daemon:

```bash
hazmat claude --docker=sandbox       # explicit Docker Sandbox mode
hazmat claude                        # auto-routes for private-daemon fits
hazmat explain --docker=sandbox      # preview what the session will do
```

Hazmat creates the sandbox, applies network policy, and prints a session
contract showing the mode, routing reason, project boundaries, and active
integrations before launching the agent.

If Hazmat thinks the repo depends on the **host** daemon instead, it stops and
asks you to choose `--docker=none` for a code-only session or to move the
workflow to Tier 4.

### Manual Setup (without Hazmat)

```bash
# Install Docker Desktop (free for personal use)
# Then:
docker sandbox run claude ~/my-project
```

### Network Isolation

> **Mandatory.** Docker Sandboxes default to allow-all egress. Hazmat
> automatically applies a deny-by-default network policy with an allowlist
> for required services (Anthropic API, GitHub, package registries) before
> every session. If you are running Docker Sandboxes manually outside of
> Hazmat, apply deny-mode yourself:

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

- Private daemon isolation — the agent cannot access the host Docker daemon or other sandboxes
- The agent can build and run Docker containers inside the sandbox without affecting the host
- `--dangerously-skip-permissions` is acceptable here because the microVM is the primary boundary
- Workspace paths stay stable between host and sandbox
- It supports Claude Code, Gemini CLI, Codex, Copilot, and Kiro

## Current Product Fit

Hazmat Tier 3 complements Tier 2 only for **self-contained Docker projects**.
It does not currently provide a smooth "switch modes and keep working" story
for every repo.

Known continuity gaps relative to Tier 2:

- integration env passthrough is not supported in Docker Sandbox mode yet
- `hazmat claude --resume/--continue` uses sandbox-local history instead of the
  host transcript sync used by native containment
- read-only parent directories may be rewritten into sibling mounts because
  Docker mount semantics differ from seatbelt path rules
- some overlapping read-only/read-write extension layouts are still rejected
  rather than remapped automatically
- localhost and service topology differ materially between native containment
  and Docker Sandbox mode

Treat Tier 3 as the right answer for private-daemon Docker workflows, not as a
general continuation path for every Docker-heavy local dev environment.

### Authentication

Set `ANTHROPIC_API_KEY` in `~/.bashrc` or `~/.zshrc` globally, not inline on the command. The daemon runs independently of the current shell session.

## Option B: Official Anthropic Devcontainer

Anthropic provides a reference devcontainer with a default-deny firewall.

```bash
# Clone the reference
git clone https://github.com/anthropics/claude-code .devcontainer-ref
# Copy .devcontainer/ into your project
# Open in VS Code -> "Reopen in Container"
```

**Included security:**

- default-deny firewall (`init-firewall.sh`)
- allowlist for npm, GitHub, Claude API, and Sentry
- DNS and SSH outbound permitted
- `--dangerously-skip-permissions` is safe inside the devcontainer

Source: [github.com/anthropics/claude-code/tree/main/.devcontainer](https://github.com/anthropics/claude-code/tree/main/.devcontainer)

## Option C: Trail of Bits Devcontainer

```bash
git clone https://github.com/trailofbits/claude-code-devcontainer
```

**Features:**

- Ubuntu 24.04, Node.js 22, Python 3.13
- read-only host mounts for git config
- persistent volumes for command history, Claude config, and GitHub CLI auth
- does not mount the Docker socket
- optional iptables rules for network restriction

## Option D: Docker Compose + Squid Proxy

Use this path only when you want to design the isolation yourself. It offers the most control and the most room to make a mistake.

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
    user: "1000:1000"
    read_only: true
    tmpfs: [/tmp, /run]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    cpus: "4"
    mem_limit: 8g
    pids_limit: 200
    ulimits:
      nofile: { soft: 1024, hard: 1024 }
      nproc: { soft: 200, hard: 200 }

networks:
  isolated:
    internal: true
  internet:
    driver: bridge
```

### Why This Example Looks Like This

- `HTTPS_PROXY` stays in `environment:` because it is operational config, not secret material.
- `ANTHROPIC_API_KEY` also remains in `environment:` because Claude Code expects it there.
- Other credentials should move to Compose `secrets:` rather than env vars whenever the tool supports it.
- Service-level limits are used instead of `deploy.resources` because the Compose spec allows non-Swarm runtimes to ignore `deploy`.

### Squid Config (`squid.conf`)

```conf
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

When writing or reviewing a Compose file for agent use, apply this checklist.
These are strongly recommended practices for defense-in-depth. Hazmat's primary
enforcement point is mount scope, environment isolation, and network policy —
not Compose file validation.

| Setting | Safe value | Why |
|---------|------------|-----|
| `user` | `"UID:GID"` (non-root) | Root in container means root inside the Docker VM and causes awkward file ownership |
| `read_only: true` | required | Prevents writes to the container filesystem; use `tmpfs` for scratch space |
| `tmpfs` | `[/tmp, /run]` | Writable scratch that is not persisted or shared |
| `cap_drop: [ALL]` | required | Drops all Linux capabilities; add back only what is proven necessary |
| `security_opt: ["no-new-privileges:true"]` | required | Prevents `setuid` and `setgid` escalation paths |
| `cpus`, `mem_limit`, `pids_limit` | set explicit limits | Reduces resource-exhaustion and fork-bomb risk |
| `ulimits` | restrict `nproc`, `nofile` | Secondary line of defense against abuse |
| Port bindings | `"127.0.0.1:PORT:PORT"` only | Avoids exposing services broadly on the host network |
| Secrets | `secrets:` block when supported | Keeps secrets out of `docker inspect`; `ANTHROPIC_API_KEY` is the main practical exception for Claude Code |

### What to Ban in Agent-Facing Compose Files

```yaml
# NEVER allow any of these:
privileged: true
network_mode: host
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
  - /:/hostfs
use_api_socket: true
environment:
  - AWS_SECRET_ACCESS_KEY=...
  - DATABASE_URL=...
```

`network_mode: none` is also usually wrong for Claude Code because it breaks access to the Anthropic API and package registries.

> **CVE-2025-62725** (docker-compose < 2.40.2, affects >= 2.34.0): when a Compose file references a remote OCI artifact, unsanitized annotation paths can lead to host-file writes. The trigger is remote OCI artifact resolution, not every plain local Compose file.

## What Tier 3 Does Not Solve by Itself

- It does not stop exfiltration of anything already reachable inside the sandbox.
- It does not make broad domain allowlists safe.
- It does not make old Docker or Compose versions safe.
- It does not make insecure Compose files safe unless you apply the hardening guidance.
- It does not support shared-daemon projects. Never share the host Docker socket.

## Critical Rules for All Docker Approaches

1. **Never share the host Docker socket.** Giving the agent access to any Docker daemon it does not exclusively own is a full sandbox escape.
2. **Never mount `/var/run/docker.sock` into a container.** If the agent can talk to the daemon, it can create privileged containers and escape.
3. **Apply deny-mode network policy before each session.** Hazmat does this automatically; if running sandboxes manually, the default is allow-all and you must apply policy yourself.
4. `--dangerously-skip-permissions` inside containers does not prevent exfiltration of anything accessible in the container, including Claude Code credentials.
5. Performance overhead on macOS is real. Bind mounts are still slower than native for metadata-heavy workloads, even with modern virtualization improvements.

## Docker Desktop Pricing

- **Personal:** Free (individuals, education, open source, small businesses with fewer than 250 employees and less than $10M revenue)
- **Pro:** $9/month
- **Team:** $15/user/month
- **Business:** $21/user/month

## Free Alternative: Colima

If you want Docker without Docker Desktop:

```bash
brew install colima docker
colima start --cpu 4 --memory 8 --disk 50
```

- completely free (MIT license)
- lower idle RAM than Docker Desktop
- CLI-only
- no Docker Sandbox feature and less integrated UX than Docker Desktop

## Free Alternative: Podman

```bash
brew install podman
podman machine init
podman machine start
```

- completely free (Apache 2.0)
- rootless by default
- daemonless
- some Docker Compose workflows need adjustment
