# Importing Claude Basics

Hazmat supports a **curated Claude import**, not a full Claude configuration migration.

The product boundary is intentional:

- Hazmat owns the agent runtime
- Hazmat owns the safety controls
- import is for portable conveniences only

That keeps the UX legible and the maintenance burden bounded.

## Commands

```bash
# Preview what Hazmat would import
hazmat config import claude --dry-run

# Run the import
hazmat config import claude

# Resolve conflicts explicitly in non-interactive runs
hazmat config import claude --overwrite
hazmat config import claude --skip-existing
```

`hazmat init` also offers the same flow after bootstrap.

Import is **copy-once**. Hazmat does not sync your host Claude setup continuously. If you want to refresh commands or skills later, rerun the import command. Imported sign-in state is stored in Hazmat's host-owned secret store and only materialized into `/Users/agent` for active Claude sessions.

## What Hazmat Imports

Hazmat currently imports only these categories:

- Sign-in state from Claude's known auth stores, when present
- Git `user.name` and `user.email`
- `~/.claude/commands`
- `~/.claude/skills`

Claude auth lands in `~/.hazmat/secrets/claude/`. Commands and skills are still copied into the agent environment: Hazmat resolves top-level symlinks and copies the resolved file or directory content into `/Users/agent/.claude/...`, not a live link back to your host setup.

If an entry is broken, unreadable, or not a regular file/directory after resolution, Hazmat skips it and reports why.

## What Hazmat Does Not Import

Hazmat does **not** import:

- `~/.claude/settings.json`
- `~/.claude/settings.local.json`
- hooks
- MCP configuration
- plugins
- project-local `.claude/` directories
- session history
- tasks, telemetry, caches, backups, or other runtime state

This is a product decision, not an omission.

Hazmat is not trying to become a compatibility layer for the full Claude home directory. Claude's config surface is large, fast-moving, and full of runtime-specific state that does not transfer cleanly into containment.

## Conflict Handling

Hazmat always scans first.

It classifies import candidates as:

- `new`
- `unchanged`
- `conflict`
- `skipped`

Interactive runs ask once when conflicts exist:

1. overwrite existing items
2. skip existing items
3. cancel

Non-interactive runs do not guess. If conflicts exist, pass an explicit policy:

- `--overwrite`
- `--skip-existing`

## Why MCP Is Manual

If you want MCP migration, do that manually using Claude Code's own documentation.

Hazmat does not import MCP config because MCP state is usually not just "preferences." It is executable integration state with environment-specific assumptions. Common failure modes:

- A server points at an absolute host path like `/Users/dr/...` that does not exist for the agent user.
- A server expects tokens, files, or sockets that only exist in your host environment.
- A filesystem MCP import exposes the wrong directories because the original allowlist assumed host paths, not Hazmat paths.
- A localhost service referenced from the host setup is unavailable or behaves differently under containment.
- A copied hook or wrapper command silently depends on shell setup that Hazmat intentionally does not inherit.

That is why Hazmat keeps the MCP story explicit: use Claude's docs to recreate MCP integrations deliberately inside the contained environment.

## Recommended Workflow

1. Run `hazmat config import claude --dry-run`
2. Import the portable basics
3. Start `hazmat claude`
4. If Claude still needs auth, use `/login`
5. Recreate MCP integrations manually, one by one, inside Hazmat

## Documentation Contract

This document is part of the feature contract.

Any change to Hazmat's Claude import scope, conflict behavior, or migration rules should update:

- this document
- the README summary
- command help text
