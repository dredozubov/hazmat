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
5. rollback always removes the host-owned `state.json` record, but agent-home
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
| `hazmat/config_import.go` | Claude basics import |
| `hazmat/config_import_opencode.go` | OpenCode basics import |

## TLA+ Model

The model tracks three built-in harnesses:

- `claude`
- `codex`
- `opencode`

and the importable subset:

- `claude`
- `opencode`

State is split into two layers:

- **agent-home artifacts**: bootstrap and imported basics that live under
  `/Users/agent`
- **host-owned metadata**: the `~/.hazmat/state.json` harness map

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
| `DryRunLeavesStateUntouched` | Dry-run bootstrap/import never mutates metadata or agent-home artifacts |
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

Observed result:

- `Model checking completed. No error has been found.`
- `16,064 states generated`
- `1,564 distinct states found`
- `depth 10`
- `Finished in 2s`

## Interpretation

The useful product conclusion is not "Hazmat models every imported file."
Instead, it proves the lifecycle contract around the explicit harness boundary:

- dry runs are read-only
- successful recording writes only known harness versions
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
3. **Changing dry-run behavior**: if any harness dry-run starts writing state,
   update this model and revisit `DryRunLeavesStateUntouched`.
4. **Changing how `saveState()` rewrites `~/.hazmat/state.json`**: update this
   model first. The current proof requires harness metadata preservation.
