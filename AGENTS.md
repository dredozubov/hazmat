# Agent Instructions

This project uses **bd (beads)** for issue tracking. Run `bd prime` for current workflow context, or install hooks (`bd hooks install`) for auto-injection.

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work atomically
bd close <id>         # Complete work
```

**This repo intentionally has no Dolt remote for beads.** Beads state is local
only here; do not run `bd dolt pull` or `bd dolt push` during Hazmat closeout
unless a remote is explicitly configured later.

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i` (interactive) mode on some systems, causing the agent to hang indefinitely waiting for y/n input.

**Use these forms instead:**
```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**
- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

## TLA+ Governance

Changes in verified areas must start from the TLA+ model. For setup/init,
rollback, seatbelt policy, credential delivery, session permission repair,
launch fd isolation, and other areas listed in `tla/VERIFIED.md`:

1. Update the relevant `tla/MC_*.tla` model and companion `.md` design note first.
2. Run TLC for the affected spec and confirm "No error has been found."
3. Only then update the implementation/tests to match the proved design.

For `hazmat/init.go` specifically, adding a setup step, changing step order, or
adding a persistent mutation inside an existing setup step requires model work
first. If rollback intentionally preserves the mutation, document that boundary
in the setup/rollback spec.

<!-- BEGIN BEADS INTEGRATION -->
## Issue Tracking

This project uses **bd (beads)** for all issue tracking. Do not use markdown TODOs, task lists, or external trackers.

Run `bd prime` at the start of a session or after context compaction to load the current AI workflow guidance. `bd onboard` intentionally stays minimal and points agents back to `bd prime`.

**Quick reference:**
- `bd ready` - Find unblocked work
- `bd show <id>` - View issue details
- `bd create --title="Title" --description="Context" --type=task --priority=2` - Create issue
- `bd update <id> --claim` - Claim work atomically
- `bd close <id> --reason="Done"` - Complete work
- `bd remember "insight"` - Store durable local project memory for future agents

**Dolt remote policy:** Hazmat intentionally has no Dolt remote configured for
beads. Use local `bd` state and persistent `bd remember` memories; skip
`bd dolt pull` / `bd dolt push` unless a remote is explicitly added later.

**Rules:**
- Create or claim a bead before writing code.
- Link discovered follow-up work with `--deps discovered-from:<parent-id>`.
- Use priority values `0`-`4` or `P0`-`P4`; do not use textual priorities.
- Do not use `bd edit`; it opens an editor and can block non-interactive agents.

<!-- END BEADS INTEGRATION -->

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   # Do not run bd dolt pull/push in this repo; Hazmat has no Dolt remote.
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
