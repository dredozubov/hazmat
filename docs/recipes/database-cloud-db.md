# Database: cloud DBs (RDS, Aurora, Cloud SQL, Supabase, Neon, PlanetScale)

Use this when your project needs to read from a managed cloud database. The agent does NOT get cloud credentials. The recipe is about how you set up your project so the agent can do useful work against schema, migrations, and read-only views without ever holding the keys.

## The boundary

Hazmat refuses to:
- Pass `DATABASE_URL`, `AWS_*`, `GCP_*`, `AZURE_*`, `SUPABASE_*`, `NEON_API_KEY`, or any `*_API_KEY` / `*_ACCESS_TOKEN` / `*_PASSWORD` env vars into the session. The credential-shape rejection in `integration_manifest.go` (`rejectCredentialGrantEnvKey`) blocks them at the integration boundary.
- Grant access to `~/.aws`, `~/.azure`, `~/.config/gcloud`, `~/.oci`, `~/.netrc`, `~/.pgpass`, `~/.kube`. All are on the credential deny list.
- Bridge to a host-running cloud-tunnel proxy unless you explicitly mount it via `-W` or expose it through Tier 3.

This is intentional. A cloud DB credential typically grants read AND write to production data. An agent loop running `psql` against production is not a sandbox failure mode hazmat designs for.

## Pattern: read-only schema + migrations work

Most agent work against a cloud DB is one of:
1. Reading the schema and writing migrations.
2. Running tests against a temporary database that mirrors production schema.
3. Investigating a query plan from a production trace.

None of these require the agent to hold production credentials.

**Recommended setup:**

```bash
# Dump the live schema from outside the session, one time.
pg_dump --schema-only --no-owner --no-acl "$PROD_DATABASE_URL" > db/schema.sql

# Run a project-local ephemeral postgres for tests via Tier 3
#  (see database-tier3-postgres-redis.md)
hazmat exec --docker=sandbox -- docker compose -f docker-compose.test.yml up -d

# Apply the schema and run tests
hazmat exec --docker=sandbox -- psql -h postgres -U postgres -d app_test -f db/schema.sql
hazmat exec --docker=sandbox -- pytest
```

The agent sees schema, runs migrations against an ephemeral mirror, and never touches production. The credential lives in your shell history outside the session, not in the integration manifest.

## Pattern: read-only replica for analysis

If you genuinely need the agent to query real data (e.g. investigating a slow query plan):

1. Create a read-only replica or a time-bounded credential scoped to a single dataset.
2. Open a host-side proxy on `127.0.0.1:5433` *outside* the session.
3. Use `--docker=sandbox` and point your test config at `host.docker.internal:5433`.
4. Tear the proxy down when done.

This way the credential lives on the host process, the agent talks to a forwarded socket via Tier 3 networking, and revoking access is just killing the proxy.

## What this recipe does NOT do

- It does not pass `DATABASE_URL` into the session. Put a sanitized `DATABASE_URL` pointing at the ephemeral DB in `.env.test`, not the production one in env.
- It does not grant access to `~/.pgpass`, `~/.aws`, or any cloud SDK config. The agent has none of those.
- It does not run cloud-provider CLIs (`aws`, `gcloud`, `az`) inside the session against your real account. Those need credentials hazmat won't provide. If you need cloud CLI work, do it outside the session.

## See also

- [database-sqlite.md](database-sqlite.md) — simplest case.
- [database-tier3-postgres-redis.md](database-tier3-postgres-redis.md) — for real-daemon ephemeral DBs.
- [docs/shared-daemon-projects.md](../shared-daemon-projects.md) — why localhost daemons are not auto-granted.

## Compatibility report prompts

If you exercise this recipe, please file a compatibility note with:
- harness (claude / codex / opencode / gemini / hermes / qwen / cursor-agent)
- cloud DB provider (RDS / Aurora / Cloud SQL / Supabase / Neon / PlanetScale)
- containment mode (native / Tier 3 sandbox / Tier 4)
- test command used
- known friction
