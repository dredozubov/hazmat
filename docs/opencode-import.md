# Importing OpenCode Basics

Hazmat supports a **curated OpenCode import**, not a full OpenCode configuration migration.

The product boundary is intentional:

- Hazmat owns the agent runtime
- Hazmat owns the safety controls
- import is for portable conveniences only

That keeps the UX legible and the maintenance burden bounded.

## Commands

```bash
# Preview what Hazmat would import
hazmat config import opencode --dry-run

# Run the import
hazmat config import opencode

# Resolve conflicts explicitly in non-interactive runs
hazmat config import opencode --overwrite
hazmat config import opencode --skip-existing
```

Import is **copy-once**. Hazmat does not sync your host OpenCode setup continuously. If you want to refresh commands, agents, or skills later, rerun the import command.

## What Hazmat Imports

Hazmat currently imports only these categories:

- Sign-in state from `~/.local/share/opencode/auth.json`, when present
- Git `user.name` and `user.email`
- `~/.config/opencode/commands`
- `~/.config/opencode/agents`
- `~/.config/opencode/skills`

For commands, agents, and skills, Hazmat resolves top-level symlinks and copies the resolved file or directory content into the agent environment. The imported result is a regular file or directory tree inside `/Users/agent/.config/opencode/...`, not a live link back to your host setup.

If an entry is broken, unreadable, or not a regular file/directory after resolution, Hazmat skips it and reports why.

## What Hazmat Does Not Import

Hazmat does **not** import:

- `~/.config/opencode/opencode.json`
- plugins
- tools
- themes
- modes
- project-local `.opencode/` directories
- runtime sessions, caches, logs, or other volatile state

This is a product decision, not an omission.

Hazmat is not trying to become a compatibility layer for the full OpenCode home directory. Runtime settings, plugin surfaces, model/provider wiring, and project-local behavior are environment-specific enough that copying them blindly into containment is more confusing than helpful.

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

## Recommended Workflow

1. Run `hazmat bootstrap opencode`
2. Run `hazmat config import opencode --dry-run`
3. Import the portable basics
4. Start `hazmat opencode`
5. If auth still needs attention, use OpenCode's own auth/login flow and config docs

## Documentation Contract

This document is part of the feature contract.

Any change to Hazmat's OpenCode import scope or conflict behavior should update:

- this document
- the README summary
- command help text
