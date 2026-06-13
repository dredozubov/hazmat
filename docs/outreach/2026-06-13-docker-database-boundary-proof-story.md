# Docker / Database Boundary Proof Story

**Date:** 2026-06-13
**Bead:** `sandboxing-u0zt`
**Status:** owned boundary story, not a live Docker Sandbox smoke claim
**Recipe:** [Database: ephemeral PostgreSQL / Redis via Tier 3](../recipes/database-tier3-postgres-redis.md)
**Boundary docs:** [Tier 3 Docker Sandboxes](../tier3-docker-sandboxes.md),
[Shared-Daemon Docker Projects](../shared-daemon-projects.md)

## Short Version

Docker is not just another binary in an agent session. A host Docker socket is
authority to ask a more privileged daemon to mount files, start containers, and
join networks. Hazmat's native containment therefore refuses host Docker socket
shortcuts and routes private-daemon Docker workflows through Tier 3 Docker
Sandbox mode.

For database-heavy tests, the strongest simple story is ephemeral PostgreSQL or
Redis inside a private Docker Sandbox daemon. The agent can work with a real
database for the session without reaching the host's primary Docker daemon or
host-running databases.

## Audience

Use this story for:

- developers whose agents need PostgreSQL, Redis, Compose, or service stacks;
- security and platform engineers evaluating whether Hazmat treats Docker as a
  privileged boundary;
- teams deciding between native containment, Docker Sandbox, and full VM/Tier 4
  workflows.

Do not use it for:

- claims that Docker support is transparent or automatic;
- shared host Docker daemon workflows;
- production database credential demos;
- live pass claims without an approved Docker Sandbox smoke transcript.

## Proof Path

Owned artifacts:

- [README](../../README.md) for the account-boundary wedge and caveated Docker
  route.
- [Database Tier 3 recipe](../recipes/database-tier3-postgres-redis.md) for an
  ephemeral PostgreSQL/Redis shape.
- [Tier 3 Docker Sandboxes](../tier3-docker-sandboxes.md) for the private-daemon
  model and continuity gaps.
- [Shared-daemon projects](../shared-daemon-projects.md) for the refused
  host-daemon boundary.
- [Stack coverage](../STACKS.md) for the `docker` integration warning.
- [Testing matrix](../testing.md) for approval-gated smoke rules.

Safe command shapes from the recipe:

```bash
hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml up -d
hazmat exec --docker=sandbox -- pytest
hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml down
```

Session-contract angle:

- native mode stays code-only and does not expose the host Docker socket;
- `--docker=sandbox` opts into a separate Tier 3 boundary;
- database containers live inside the private daemon/session boundary;
- project files are still the agent's work area;
- host cloud/database credentials stay denied unless explicitly granted by a
  separate modeled capability;
- shared-daemon projects route to Tier 4 or native code-only mode.

## Caveats To Keep In The Copy

- Do not describe Docker Sandbox as a transparent fallback from native
  containment. It is a different boundary with different runtime semantics.
- Do not claim a live Docker Sandbox pass without an approved command
  transcript from the current machine.
- Shared host Docker daemon access remains unsupported inside containment.
- Database service addresses in the recipe are test endpoints, not production
  credentials.
- Tier 3 still has continuity gaps compared with native containment:
  integration env passthrough, managed Git SSH, resume behavior, read-only
  mount semantics, and localhost/service topology differ.

## Post Template

```text
Docker is a boundary, not a checkbox.

Hazmat native containment refuses host Docker socket shortcuts. If an agent
needs a real PostgreSQL or Redis for tests, use a private-daemon Docker Sandbox
workflow instead:

    hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml up -d
    hazmat exec --docker=sandbox -- pytest
    hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml down

What this proves:
- the host Docker daemon is not the native-session escape hatch
- database services can be session-local test infrastructure
- Docker-heavy workflows need an explicit boundary decision

Proof:
- README: <link>
- Database Tier 3 recipe: <link>
- Shared-daemon caveat: <link>

Caveat:
- This is an educational boundary story unless an approved Docker Sandbox smoke
  transcript is linked. Shared host-daemon projects need Tier 4 or native
  code-only mode, not a socket passthrough.
```

## README Link Path

The public path should be:

1. README first screen for the wedge and Docker caveat.
2. `docs/overview.md` for tier selection.
3. `docs/tier3-docker-sandboxes.md` for private-daemon workflows.
4. `docs/shared-daemon-projects.md` for the refused host-daemon case.
5. `docs/recipes/database-tier3-postgres-redis.md` for the database recipe.
6. `docs/testing.md` for approval-gated live smoke policy.

## Acceptance Checklist For Reuse

Before using this externally:

- Say "native containment refuses host Docker socket shortcuts."
- Say "Docker Sandbox is a separate private-daemon boundary."
- Include the shared-daemon caveat near the main claim.
- Avoid "Docker support" as a standalone phrase.
- Do not claim a live pass unless the exact approved transcript exists.
