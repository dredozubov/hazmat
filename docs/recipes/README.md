# Recipes

Recipes are practical starting points for real Hazmat workflows.

They are intentionally lighter-weight than core docs:

- focused on one harness + one stack
- explicit about integrations and caveats
- easy for community contributors to improve

Recipes are a **Community** support surface. They are useful, but they are not a substitute for the core trust model docs.

## Starter Recipes

- [Claude + Next.js](claude-nextjs.md)
- [Codex + uv](codex-uv.md)
- [OpenCode + Go](opencode-go.md)
- [Gemini + TLA+](gemini-tla.md)
- [AGENTS.md Hazmat security snippet](agents-md.md)
- [Local MCP servers under Hazmat](mcp-servers.md)

## Database Recipes

These recipes show how to use Hazmat with a real database without granting credentials.

- [Database: SQLite-only tests](database-sqlite.md) — simplest case.
- [Database: ephemeral PostgreSQL / Redis via Tier 3](database-tier3-postgres-redis.md) — when SQLite isn't enough.
- [Database: cloud DBs (RDS, Aurora, Cloud SQL, Supabase, Neon, PlanetScale)](database-cloud-db.md) — keeping production credentials outside the session.

## What a Good Recipe Includes

- which harness to use
- which integrations to activate
- whether the workflow is native-only, Docker-capable, or better suited for Tier 4
- what extra read or write scope is typical
- known caveats and setup friction

If you want to add a new recipe, keep it concrete. "Rust project" is weaker than "Codex + cargo workspace with native containment."
