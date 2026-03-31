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
| `AgentContained` | Sudoers never exists without pf firewall — even during migration |
| `NoSkippedMigrations` | Every adjacent version pair in the chain is applied |
| `MigrationsOrdered` | Migrations go forward only (v1 < v2) |
| `InitComplete` | After init finishes, all expected artifacts for the binary version are present |
| `VersionConsistent` | After init finishes, the recorded version matches the binary |
| `MigrationRecoverable` | A failed migration can always be retried |

## Liveness

`EventuallyComplete`: if failures are transient and the user keeps retrying
init, the system eventually reaches the "done" state with all artifacts
present for the current binary version.

## Model bounds

- 3 versions: v0.1.0, v0.2.0, v0.3.0
- Binary version: v0.3.0
- Init from any previous version (including v0.3.0 = already current)
- Failure at any migration step

## Running

```bash
cd tla
java -jar ~/workspace/tla2tools.jar -workers auto \
  -config MC_Migration.cfg MC_Migration.tla
```

## Change rules

1. **Adding a new version**: add it to `Versions`, `VersionOrder`,
   `ExpectedArtifacts`, and `MigrationExists` in the config. Write the
   migration function in Go. Re-run TLC.

2. **Changing expected artifacts for a version**: update `ExpectedArtifacts`
   in the config. If the change affects an existing version (not just the
   latest), you may need to add a migration step.

3. **The AgentContained invariant must hold during migration**. If a
   migration removes the pf anchor, it must remove sudoers first (or in
   the same step). Model the migration's artifact transformation and verify.
