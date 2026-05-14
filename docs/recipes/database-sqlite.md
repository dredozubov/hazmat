# Database: SQLite-only tests

Use this when your project's tests can run against a fresh SQLite file each run. This is the simplest database story under Hazmat — no daemon, no socket, no credentials.

## Why this works

SQLite stores its database in a single file inside the project directory. The agent already has full read/write access to the project tree, so the test file lives there with no extra setup. The integration manifests for `python-uv`, `python-pip`, `python-poetry`, `node`, `pnpm`, `bun`, and `yarn` all already exclude common test artifacts from snapshots; you don't need any extra integration for SQLite itself.

## Setup

Point your test config at a project-local file:

- Python (pytest): `DATABASE_URL=sqlite:///./test.db` in `conftest.py` or `pytest.ini`
- Node: `DATABASE_URL=file:./test.db` in `vitest.config.ts` / `jest.config.js`
- Rust (sqlx): `DATABASE_URL=sqlite:./test.db` in `.env.test`

Add the database file to `.gitignore`:

```
test.db
test.db-journal
test.db-wal
test.db-shm
```

## Typical commands

```bash
hazmat exec --integration python-uv -- uv run pytest
hazmat exec --integration node -- npm test
hazmat exec --integration rust -- cargo test
```

## What this recipe does NOT do

- It does not grant `DATABASE_URL` via env passthrough. The integration carries no DB-specific env. Put the value in a project file (`.env.test`, `pytest.ini`) so it lives inside the project read-write zone.
- It does not grant access to `~/.pgpass`, `~/.my.cnf`, or any cloud-provider credentials. Those are credential paths and stay denied.
- It does not give the agent a connection to a host-running PostgreSQL/MySQL/Redis daemon. If you need that, use [database-tier3-postgres-redis.md](database-tier3-postgres-redis.md) instead.

## See also

- [docs/compatibility.md](../compatibility.md) — which harness/database combinations are tested.
- [docs/integrations.md](../integrations.md#built-in-integrations) — the language integration you'll combine with this recipe.
