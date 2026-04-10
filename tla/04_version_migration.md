# Spec 04 — Version Migration

## Problem

Hazmat modifies system state during `hazmat init`: it creates users, groups,
firewall rules, seatbelt wrappers, config files, and more. When the hazmat
binary is upgraded, the new version may expect different artifacts than the
old version created. Without a formal migration path:

- Old artifacts are left behind (workspace symlinks, stale ACLs)
- New artifacts are missing (npmrc, home dir traverse ACL)
- The `AgentContained` invariant may be violated during the transition

## Design

### Version tracking

`~/.hazmat/state.json` records which version last completed init. It may also
contain opaque harness metadata, but the current migration spec models only the
core `init_version` / `init_date` fields:

```json
{
  "init_version": "0.2.0",
  "init_date": "2026-03-31T19:00:00Z",
  "harnesses": {
    "claude": {
      "state_version": "1",
      "last_import_run_at": "2026-04-01T11:00:00Z"
    }
  }
}
```

### Migration chain

When `hazmat init` runs and `init_version < binary_version`, it builds a
chain of adjacent migrations and applies them in order before running the
normal idempotent init:

```
v0.1.0 → v0.2.0: remove workspace artifacts, add home dir traverse
v0.2.0 → v0.3.0: add npmrc, pip.conf
v0.3.0 → v0.4.0: add zsh completions
```

Skipping a version is NOT allowed. A user upgrading from v0.1.0 to v0.4.0
runs each adjacent migration in sequence.

### Failure handling

If a migration step fails (power loss, error), the system is in an
intermediate state. The user re-runs `hazmat init`, which retries from
the failed step. The spec proves that `RecoverFromFailure` is always enabled
when `phase = "failed"`.

## Invariants

| Invariant | What it ensures |
|-----------|----------------|
| `AgentContained` | Sudoers never exists without pf firewall — during init, migration, failure, AND rollback |
| `InitComplete` | After init finishes, all expected artifacts for the binary version are present |
| `VersionConsistent` | After init finishes, the recorded version matches the binary |
| `FailureRecoverable` | Any failed state can retry init or start rollback |
| `MigrationForward` | Migrations go forward only (v1 < v2) |
| `RollbackClean` | After rollback completes, zero artifacts remain |
| `RollbackAlwaysAvailable` | From idle, done, or failed — rollback can always start if artifacts exist |

## Liveness

`EventuallyComplete`: under strong fairness (transient failures), the system
reaches either "done" (fully initialized) or "clean" (fully rolled back).

## Rollback from any state

Rollback is modeled as removing one artifact at a time, respecting ordering
constraints encoded in `CanRemove`. The critical constraint: both Hazmat
sudoers artifacts (`sudoers` and the optional `agentMaintenanceSudoers`) must
be removed before pfAnchor (revoke privilege before removing containment). TLC
checks that `AgentContained` holds at every intermediate rollback state,
including rollback of a partially migrated system.

## TLC results

- **72,442 distinct states** explored
- **234,101 state transitions** checked
- **0 errors** found
- **3 seconds** runtime
- Graph depth: 18 (longest path from any initial state to terminal)

## Model bounds

- 4 versions: v0.1.0, v0.2.0, v0.3.0, v0.4.0
- Binary version: v0.4.0
- Init from any previous version (including v0.4.0 = already current)
- Failure at any migration step
- Rollback from any state (idle, done, failed, mid-migration)
- Rollback failure and retry

## Running

```bash
cd tla/
./run_tlc.sh -workers auto -lncheck final \
  -config MC_Migration.cfg MC_Migration.tla
```

## Change rules

1. **Adding a new version**: add the next version constant, `Expected(Vn)`,
   `HasMigration(Vn-1, Vn)`, and update `NextVersion`. Run TLC.
   It checks all paths from every older version through the new migration,
   AND rollback from every intermediate state.

2. **Changing expected artifacts for a version**: update `Expected(v)`.
   If the change affects an existing version, you need a migration step.
   If the current binary gains an optional artifact without a version bump,
   model it in `OptionalArtifacts(v)` and keep `RunInit`/`InitComplete`
   consistent with both outcomes. This covers real CLI branches such as
   interactive init skipping an optional artifact while `hazmat init --yes`
   installs it by default.

3. **Adding rollback ordering constraints**: update `CanRemove`. If a new
   artifact depends on another for safety, encode the dependency. TLC
   verifies `AgentContained` across all removal orderings.

4. **The `AgentContained` invariant must hold everywhere** — init, migration,
   failure, rollback, partial rollback after partial migration. 72,442 states
   is a lot of "everywhere."

## Known spec-vs-implementation divergences

**Harness metadata is modeled separately, not here.** The Go implementation
persists per-harness metadata under `state.harnesses`, and that lifecycle now
has its own spec in `MC_HarnessLifecycle.tla`. `MC_Migration.tla` continues to
model only the core init-version migration chain because harness metadata does
not currently participate in Hazmat's versioned host-artifact migration logic.

**Detection heuristic for v0.1.0 installs.** The spec models `Init` as
starting from `Expected(v)` for any version — a complete, consistent artifact
set. The Go implementation (`detectV010Artifacts()` in `migrate.go`) only
checks for the agent workspace symlink and workspace ACL to determine that a
v0.1.0 install exists. A system with a partial v0.1.0 install is detected as
v0.1.0 even though not all 14 artifacts in `Expected(V1)` are present.

This is acceptable because:
- The migration up function is best-effort (removes what exists, skips what doesn't)
- The idempotent init steps after migration create anything that's missing
- The spec proves the *ideal* migration path is safe; the Go code handles the messy reality
- A partial v0.1.0 install has fewer artifacts than `Expected(V1)`, so the migration is strictly easier (fewer things to remove)
