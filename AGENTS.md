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

## Sudo-Adjacent Command Consent

Ask the user for explicit approval before running any sudo-adjacent command,
and name the exact command you want to run. This applies to more than literal
`sudo`: `hazmat check`, `hazmat doctor --fix`, native helper-backed smokes,
live harness probes, Codex desktop attach probes, `launchctl`/`pf` paths,
DTrace/dtruss-style probes, and `git push` when hooks may invoke these gates.
If approval is needed, ask first; do not try the command speculatively.

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

**When ending a work session**, complete all local closeout steps below. Remote
sync is required for a fully landed session, but `git push` is sudo-adjacent in
this repo because hooks may invoke gated checks. Do not run `git push` unless
the user explicitly approves that exact command.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Remote sync (approval-gated)** - After explicit approval for `git push`,
   run:
   ```bash
   git pull --rebase
   # Do not run bd dolt pull/push in this repo; Hazmat has no Dolt remote.
   git push
   git status  # MUST show "up to date with origin"
   ```
   If approval is absent, stop after local commits/status and state:
   "Approval needed for exact command: `git push`." Do not claim remote
   completion.
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed, and pushed only when the approved push succeeds
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Do not run `git push` without explicit approval for the exact command.
- Do not say work is remotely complete until `git push` succeeds.
- If push approval is absent, report the local commit/status and the exact
  approval needed: `git push`.
- If an approved push fails, resolve and retry only commands that are not
  approval-gated; ask again before rerunning any sudo-adjacent command.
