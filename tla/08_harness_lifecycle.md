# Problem 08 — Harness Lifecycle

## Problem Statement

Hazmat now stores explicit per-harness metadata under `~/.hazmat/state.json`
while the actual harness files live in the agent home. That creates a separate
state machine from core init/migration:

1. successful harness bootstrap should record the harness state version
2. successful curated import should record both the state version and import
   metadata
3. dry-run bootstrap/import must not mutate either the state file or the agent
   home
4. `saveState()` for core init/migration must preserve existing harness
   metadata
5. explicit harness uninstall removes only Hazmat-owned harness code artifacts
   and the host-owned metadata entry for that harness by default
6. uninstall dry-run must not mutate either the state file or the agent home
7. uninstall preserves imported/auth/profile-like agent-home artifacts by
   default; destructive purge semantics require a separate model change
8. rollback always removes the host-owned `state.json` record, but agent-home
   harness artifacts survive unless `--delete-user` is passed

The core migration proof intentionally did not model these rules. This spec
gives them a dedicated home.

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/harness.go` | `RecordInstalled()`, `RecordBasicsImported()`, `recordHarnessInstalled()`, `recordHarnessImportRun()` |
| `hazmat/state.go` | `loadState()`, `saveState()`, `updateHarnessState()`, `writeState()` |
| `hazmat/migrate.go` | `saveState()`, `runDownMigrations()` |
| `hazmat/bootstrap.go` | Claude bootstrap path |
| `hazmat/bootstrap_codex.go` | Codex bootstrap path |
| `hazmat/bootstrap_opencode.go` | OpenCode bootstrap path |
| `hazmat/bootstrap_gemini.go` | Gemini bootstrap path |
| `hazmat/bootstrap_hermes.go` | Hermes verification and managed profile reset |
| `hazmat/bootstrap_qwen.go` | Qwen Code npm bootstrap path |
| `hazmat/harness_lifecycle.go` | Harness lifecycle CLI status/update/uninstall |
| `hazmat/config_import.go` | Claude basics import |
| `hazmat/config_import_opencode.go` | OpenCode basics import |
| `hazmat/config_import_codex.go` | Codex basics import |
| `hazmat/config_import_gemini.go` | Gemini basics import |

## TLA+ Model

The model tracks seven built-in harnesses:

- `claude`
- `codex`
- `opencode`
- `gemini`
- `hermes`
- `qwen`
- `cursor-agent`

and the importable subset:

- `claude`
- `codex`
- `opencode`
- `gemini`

Hermes, Qwen, and Cursor Agent are deliberately not in the importable subset for
Phase 1. They have managed foreground harness plans, but no curated
host-profile import path:
Hermes host `~/.hermes` state and Qwen host `~/.qwen` settings, extensions,
auth, sessions, and MCP configuration remain outside the import lifecycle until
a typed import design exists. Cursor Agent host IDE/auth/profile state likewise
stays outside the import lifecycle until Hazmat has a typed import design.

State is split into two layers:

- **agent-home artifacts**: bootstrap and imported basics that live under
  `/Users/agent`
- **Hazmat-owned code artifacts**: the executable/package files that Hazmat's
  harness bootstrap installed or verified as its managed launch target
- **host-owned metadata**: the `~/.hazmat/state.json` harness map

Explicit uninstall is intentionally narrower than rollback:

- code artifacts for the selected harness are removed when Hazmat owns them
- the selected harness metadata entry is removed from `state.json`
- imported basics, auth files, profile roots, sessions, provider state, and
  other user data in the agent home are preserved

Hermes remains special: Hazmat verifies a manually installed binary for Phase 1
and does not own the upstream Hermes executable. Its lifecycle status can show
that binary, but default uninstall only clears Hazmat metadata and leaves the
manual executable and managed profile roots alone.

The model also tracks:

- whether the core system is ready for harness commands
- whether `state.json` exists
- whether the core init version has been recorded
- snapshots used to prove dry-run and rollback-preservation properties

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `RecordedHarnessVersionsMatchSpec` | Recorded harness entries always use the current declared harness state version |
| `ImportedMetadataCarriesVersion` | Any recorded import timestamp implies the harness also has a recorded state version |
| `StateFilePresentWhenMetadataExists` | Harness or init metadata never exists without `state.json` |
| `DryRunLeavesStateUntouched` | Dry-run bootstrap/import/uninstall never mutates metadata or agent-home artifacts |
| `UninstallRemovesOnlyCodeAndMetadata` | Explicit uninstall removes selected harness code + metadata while preserving imported artifacts |
| `SaveCoreStatePreservesHarnessMetadata` | Core `saveState()` preserves all existing harness metadata and artifacts |
| `RollbackClearsMetadata` | Rollback removes the host-owned harness metadata record |
| `RollbackWithoutDeleteUserPreservesArtifacts` | Rollback without `--delete-user` keeps all agent-home harness artifacts |
| `RollbackDeleteUserRemovesArtifacts` | Rollback with `--delete-user` removes all agent-home harness artifacts |

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers auto \
  -config MC_HarnessLifecycle.cfg \
  MC_HarnessLifecycle.tla
```

Observed result from the 2026-06-03 package-split refactor confirmation run:

- `Model checking completed. No error has been found.`
- `25,164,502 states generated`
- `633,107 distinct states found`
- `depth 18`
- `Finished in 2m45s`

## Interpretation

The useful product conclusion is not "Hazmat models every imported file."
Instead, it proves the lifecycle contract around the explicit harness boundary:

- dry runs are read-only
- successful recording writes only known harness versions
- explicit uninstall clears a selected harness's code and metadata boundary
  without erasing imported/auth/profile data
- core init-state saves do not erase harness metadata
- rollback drops the host-owned metadata record
- agent-home harness files survive ordinary rollback and only disappear on
  destructive rollback

That is the state-machine behavior users and developers actually need to reason
about when editing harness bootstrap/import flows.

## Change Rules

1. **Adding a new built-in harness**: add it to the harness set, define whether
   it supports curated import, and update the recording invariants before code.
2. **Changing what rollback removes**: update this model first. The current
   proof intentionally distinguishes host-owned metadata cleanup from
   `--delete-user` agent-home deletion.
3. **Changing what explicit uninstall removes**: update this model first if
   uninstall starts deleting imported basics, auth/profile state, sessions, or
   any user data beyond declared Hazmat-owned code artifacts.
4. **Changing dry-run behavior**: if any harness dry-run starts writing state,
   update this model and revisit `DryRunLeavesStateUntouched`.
5. **Changing how `saveState()` rewrites `~/.hazmat/state.json`**: update this
   model first. The current proof requires harness metadata preservation.
