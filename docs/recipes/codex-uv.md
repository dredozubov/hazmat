# Codex + uv

Use this when you want Codex in native containment for a Python project that uses `uv`.

## Recommended Setup

Fastest path:

```bash
hazmat config import codex
hazmat codex --integration python-uv
```

If you revisit the repo often:

```bash
hazmat config set integrations.pin "~/workspace/my-app:python-uv"
```

## Why Import First

Codex is supported in Hazmat, but the first-run picker currently has a known arrow-key issue under containment. The import path avoids that startup friction and gets you to a working session faster.

See [docs/harnesses.md](../harnesses.md) for the current auth details and caveats.

## Caveats

- This is a **works with caveats** path today because Codex startup auth still has known UX rough edges under Hazmat.
- `uv` caches are read-only inputs; if your workflow depends on extra writable host paths, expose them explicitly with `-W`.
- Keep secrets in the project only if you intend the contained agent to read them.

## Typical Commands

```bash
hazmat exec --integration python-uv -- uv run pytest -q
hazmat exec --integration python-uv -- uv run ruff check .
```
