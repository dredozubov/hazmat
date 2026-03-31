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

`~/.hazmat/state.json` records which version last completed init:

```json
{
  "init_version": "0.2.0",
  "init_date": "2026-03-31T19:00:00Z"
}
```

### Migration chain

When `hazmat init` runs and `init_version < binary_version`, it builds a
chain of adjacent migrations and applies them in order before running the
normal idempotent init:

```
v0.1.0 → v0.2.0: remove workspace artifacts, add home dir traverse
v0.2.0 → v0.3.0: add npmrc, pip.conf
```

Skipping a version is NOT allowed. A user upgrading from v0.1.0 to v0.3.0
runs both migrations in sequence.

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
constraints encoded in `CanRemove`. The critical constraint: sudoers must be
removed before pfAnchor (revoke privilege before removing containment). TLC
checks that `AgentContained` holds at every intermediate rollback state,
including rollback of a partially migrated system.

## TLC results

- **44,795 distinct states** explored
- **140,535 state transitions** checked
- **0 errors** found
- **3 seconds** runtime
- Graph depth: 18 (longest path from any initial state to terminal)

## Model bounds

- 3 versions: v0.1.0, v0.2.0, v0.3.0
- Binary version: v0.3.0
- Init from any previous version (including v0.3.0 = already current)
- Failure at any migration step
- Rollback from any state (idle, done, failed, mid-migration)
- Rollback failure and retry

## Running

```bash
cd tla
java -XX:+UseParallelGC -jar ~/workspace/tla2tools.jar -workers auto \
  -lncheck final -config MC_Migration.cfg MC_Migration.tla
```

## Change rules

1. **Adding a new version**: add `V4` constant, `Expected(V4)`,
   `HasMigration(V3, V4)`, update `NextVersion(V3) == V4`. Run TLC.
   It checks all paths from every older version through the new migration,
   AND rollback from every intermediate state.

2. **Changing expected artifacts for a version**: update `Expected(v)`.
   If the change affects an existing version, you need a migration step.

3. **Adding rollback ordering constraints**: update `CanRemove`. If a new
   artifact depends on another for safety, encode the dependency. TLC
   verifies `AgentContained` across all removal orderings.

4. **The `AgentContained` invariant must hold everywhere** — init, migration,
   failure, rollback, partial rollback after partial migration. 44,795 states
   is a lot of "everywhere."
