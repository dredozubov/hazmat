# Database: ephemeral PostgreSQL / Redis via Tier 3 Docker Sandbox

Use this when your tests need a real PostgreSQL or Redis (not SQLite), but you don't want the agent reaching your host's primary database. The Tier 3 Docker Sandbox mode runs a private Docker daemon per session, isolated from the host's Docker.

This recipe assumes you have the `docker` integration approved for the project.

## Why Tier 3, not native

PostgreSQL and Redis need a long-lived daemon listening on a socket. Hazmat's native containment intentionally does not expose the host's Docker socket or host-running database sockets to the agent — that would let the agent touch your production DB. Tier 3 Docker Sandbox mode is the supported way to give the agent a real database that lives and dies with the session.

See [docs/tier3-docker-sandboxes.md](../tier3-docker-sandboxes.md) for the full Tier 3 model and [docs/shared-daemon-projects.md](../shared-daemon-projects.md) for why hazmat refuses to share host daemons.

## Project layout

A `docker-compose.test.yml` in the project root scoped to test services only:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: test
      POSTGRES_DB: app_test
    tmpfs:
      - /var/lib/postgresql/data
  redis:
    image: redis:7-alpine
    tmpfs:
      - /data
```

`tmpfs` keeps the data ephemeral; nothing persists past the session.

## Setup

```bash
# Activate the docker integration once per project (or use --integration docker)
mkdir -p .hazmat
printf 'integrations:\n  - docker\n' > .hazmat/integrations.yaml
```

## Typical commands

```bash
hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml up -d
hazmat exec --docker=sandbox -- pytest                # or vitest, cargo test, etc.
hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml down
```

`--docker=sandbox` is required. Hazmat does not auto-switch to Tier 3 even when a Dockerfile is present (the docker integration warning documents this).

## Connection from inside the session

The test container DNS names (`postgres`, `redis`) resolve inside the sandbox's private Docker network. Point your app at:

- `DATABASE_URL=postgresql://postgres:test@postgres:5432/app_test`
- `REDIS_URL=redis://redis:6379`

Put these in `.env.test` inside the project — they are not secrets in this layout, just service addresses.

## What this recipe does NOT do

- It does not grant `KUBECONFIG`, cloud-provider credentials, or any path under `~/.aws`, `~/.azure`, `~/.oci`, `~/.kube`, or `~/.config/gcloud`. Those are still denied.
- It does not bridge the sandbox to your host's Docker daemon or host's PostgreSQL. The Tier 3 daemon is private to the session.
- It does not auto-suggest `--docker=sandbox`. The user must opt in explicitly — auto-switching containment modes based on a Dockerfile would surprise users about their effective trust model.

## See also

- [database-sqlite.md](database-sqlite.md) — for simpler workflows that don't need a real daemon.
- [database-cloud-db.md](database-cloud-db.md) — for production cloud DBs (RDS, Aurora, Cloud SQL).
- [docs/tier3-docker-sandboxes.md](../tier3-docker-sandboxes.md) — the full Tier 3 model.
- [docs/shared-daemon-projects.md](../shared-daemon-projects.md) — why hazmat refuses to share host daemons.
