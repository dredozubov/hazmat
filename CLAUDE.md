# CLAUDE.md — Hazmat

## What this is

Hazmat is a macOS CLI tool that runs AI agents (Claude Code, etc.) inside containment: dedicated system user, seatbelt sandboxing, pf firewall, DNS blocklist. Written in Go, single binary + helper.

## Repository layout

```
hazmat/              Go source (package main, module hazmat)
  cmd/hazmat-launch/ Privileged helper binary (narrow sudo target)
  Makefile           Build targets: hazmat, hazmat-launch
*.md                 Documentation (tiers, threat model, setup guide)
ux-analysis.md       User flow diagrams and UX analysis
art/                 Homer-in-hazmat ASCII art generator
assets/              Brand images
```

## Build and test

```bash
cd hazmat
go build -o hazmat .
go build -o hazmat-launch ./cmd/hazmat-launch
go test ./cmd/hazmat-launch/
./hazmat test --quick    # integration tests (Steps 15-16 = Kopia backup/restore)
```

## Key conventions

- **Apple sandbox-exec references stay as-is.** `sandbox-exec`, `sandbox_init`, `sandboxed`, `same-sandbox`, `SANDBOX_*` env vars — these are Apple API names, not our tool. Never rename them.
- **Agent system identity is separate from tool name.** User `agent`, group `dev`, pf anchor `agent`, sudoers file `agent` — these don't change when the tool is renamed.
- **Setup is unified.** `hazmat setup` chains system config + bootstrap + enroll. The standalone `bootstrap` and `enroll` commands exist for re-running individually.
- **Pre-flight checks run before any mutations.** `preflightChecks()` in setup.go validates all prerequisites before the first `dscl` call.
- **Seatbelt policies are per-session.** Generated dynamically in `generateSBPL()` with literal paths embedded. Written to `/private/tmp/hazmat-<pid>.sb`, cleaned up on exit.

## When making user-facing changes

**Update ux-analysis.md** if you change:
- The setup flow (steps added, removed, or reordered)
- Command names, flags, or help text
- The status checklist phases
- Backup/restore behavior
- Rollback steps
- Any user-visible prompts or error messages

Specifically update:
1. The relevant **flow diagram** (Flow 1-5 in "User Journey Map")
2. The **Command Reference** section if commands/flags changed
3. The **Configuration** table if defaults or overrides changed
4. The **Non-Obvious Behaviors** list if gotchas were added or resolved
5. The **Remaining UX Improvements** backlog if items were completed or added

## Commit message style

```
<area>: <what changed>

<why, in 1-3 lines>
```

Areas: `cloud`, `ux`, `privilege`, `docker`, `docs`, `rename`, `test`
