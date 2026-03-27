# CLAUDE.md — Hazmat

## What this is

Hazmat is a macOS CLI tool that runs AI agents (Claude Code, etc.) inside containment: dedicated system user, seatbelt sandboxing, pf firewall, DNS blocklist. Written in Go, single binary + helper.

## Repository layout

```
hazmat/                  Go source (package main, module hazmat)
  cmd/hazmat-launch/     Privileged helper binary (narrow sudo target)
  Makefile               Build targets: hazmat, hazmat-launch
tla/                     TLA+ formal verification specs (see tla/VERIFIED.md)
docs/                    User-facing documentation
  usage.md               Complete user guide
  overview.md            Tier selection and design choices
  design-assumptions.md  Explicit design assumptions and security tradeoffs
  threat-matrix.md       Risk-by-risk coverage analysis
  setup-option-a.md      Tier 2 setup walkthrough
  tier3-docker-sandboxes.md  Docker project guide
  tier4-vm-isolation.md  VM isolation guide
  research/              Internal research and reference material
    ux-analysis.md       User flow diagrams and UX analysis
    attack-surface-deep-dive.md  Escape and exfiltration path analysis
    security-evidence.md Incidents, CVEs, and academic sources
    ...                  Tier research, seatbelt reference, pf design, etc.
art/                     Homer-in-hazmat ASCII art generator
assets/                  Brand images
```

## Build and test

```bash
cd hazmat
go build -o hazmat .
go build -o hazmat-launch ./cmd/hazmat-launch
go test ./cmd/hazmat-launch/
./hazmat init check      # integration tests (Steps 15-16 = Kopia backup/restore)
```

## Key conventions

- **Apple sandbox-exec references stay as-is.** `sandbox-exec`, `sandbox_init`, `sandboxed`, `same-sandbox`, `SANDBOX_*` env vars — these are Apple API names, not our tool. Never rename them.
- **Agent system identity is separate from tool name.** User `agent`, group `dev`, pf anchor `agent`, sudoers file `agent` — these don't change when the tool is renamed.
- **`hazmat init` is the single entry point for all static setup.** It chains system config + bootstrap + enroll. Subcommands (`init check`, `init rollback`, `init enroll`, `init cloud`) handle individual concerns. `bootstrap` is an internal step, not a user-facing command.
- **Pre-flight checks run before any mutations.** `preflightChecks()` in init.go validates all prerequisites before the first `dscl` call.
- **Seatbelt policies are per-session.** Generated dynamically in `generateSBPL()` with literal paths embedded. Written to `/private/tmp/hazmat-<pid>.sb`, cleaned up on exit.

## When changing setup or rollback

**Check TLA+ specs first.** Setup/rollback step ordering is formally verified.
See `tla/VERIFIED.md` for the authoritative rules. In short:

1. **Adding, removing, or reordering setup/rollback steps** — update the TLA+
   spec (`tla/MC_SetupRollback.tla`) first, run TLC, prove invariants pass,
   then implement in Go.
2. **Run TLC** after any change to `init.go` or `rollback.go`:
   ```bash
   cd tla && java -jar ~/workspace/tla2tools.jar -workers auto \
     -config MC_SetupRollback.cfg MC_SetupRollback.tla
   ```
3. **Key invariant: `AgentContained`** — the agent must never be launchable
   (sudoers exists) without firewall containment (pfAnchor active).

## When making security-relevant changes

**Update docs/design-assumptions.md** if you change:
- The seatbelt credential deny list
- Network policy (pf rules or DNS blocklist)
- The trust model or containment boundaries
- Credential storage or handling
- Any assumption about what the agent can or cannot access

## When making user-facing changes

**Update docs/research/ux-analysis.md** if you change:
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
