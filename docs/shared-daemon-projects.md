# Shared-Daemon Docker Projects

Some repos use Docker in a way that depends on the **host** Docker daemon, not
just "Docker as a tool." Common signals:

- external Docker networks such as `proxy`
- Traefik Docker labels and `traefik.docker.network`
- shared containers such as `shared-postgres`
- scripts that boot another Compose project first

Examples include local setups where one repo runs shared infra and another repo
joins that network for application development.

## What Hazmat Supports

Hazmat containment supports two Docker shapes:

- **Private-daemon Docker**: the agent runs in Docker Sandbox mode with its own
  daemon or microVM. This is Tier 3.
- **Code-only native sessions**: the agent stays in Tier 2 native containment
  with `--docker=none`. Docker commands do not work in the session.

Hazmat does **not** support shared-daemon Docker access inside containment. The
host Docker socket is treated as a full sandbox escape.

## Code-Only Fallback

If the repo depends on a shared daemon but you only need the agent to edit code
and hit already-running local services:

```bash
hazmat claude --docker=none -C ~/workspace/my-project
hazmat config docker none -C ~/workspace/my-project
```

This is a fallback, not a full replacement for agent-managed Docker workflows.

## What Changes In `--docker=none`

- The agent can still read and write project files.
- The agent can still reach host-published services on allowed ports.
- The agent cannot run `docker`, `docker compose`, `docker exec`, or restart
  containers.

Use host-published ports from inside Hazmat, not container DNS names. For
example, use `localhost:5433` instead of `postgres:5432`.

## Important Boundary Notes

- **Mounted source still matters.** If your Compose setup watches or mounts the
  project directory, file edits from Hazmat become running code inside
  containers automatically.
- **Localhost is inside the blast radius.** Admin surfaces, dev databases,
  debug endpoints, and local dashboards exposed on allowed ports remain
  reachable from the session.
- **Tier 2 and Tier 3 are not smooth mode switches today.** Pack env
  passthrough, resume behavior, read-only mount semantics, and network/runtime
  topology differ between native containment and Docker Sandboxes.

## When Tier 4 Is The Right Answer

If the agent needs to debug the live Docker topology, restart services, inspect
logs, run `docker exec`, or otherwise stay inside the shared Docker loop, Tier 2
code-only mode is the wrong fit. Use Tier 4 or another VM-based workflow where
the shared daemon lives inside the VM boundary instead of on your host.
