# Problem 3 — Backup/Restore Safety

## Problem Statement

Hazmat automatically snapshots project directories before each session and
supports manual cloud backup/restore of selected project directories. The system uses
Kopia for content-addressed, deduplicated snapshots stored locally and in S3.

The correctness questions are about **data loss prevention** and **operation
ordering**, not Kopia internals:

1. **Restore reversibility** — does every restore path (local and cloud)
   snapshot the current state before overwriting? If not, a stale or wrong
   snapshot permanently destroys user work.

2. **Session non-blocking** — does a snapshot failure ever prevent a session
   from launching? The design requires graceful degradation: warn but proceed.

3. **Repository preconditions** — are snapshot and restore operations always
   preceded by repository initialization? Auto-init must handle the case where
   `hazmat init` was never run.

4. **Cloud config precondition** — do cloud operations always require cloud
   configuration? Operating without credentials should fail immediately, not
   silently.

5. **Snapshot ordering** — is the pre-session snapshot always attempted before
   the sandbox boundary is crossed? The snapshot runs as the host user; it
   cannot work inside the sandbox.

6. **Cloud scope** — do cloud backup and cloud restore operate on the selected
   project directory instead of a canonical workspace root? The old
   `cloudBackupDir=$HOME/workspace` behavior could unexpectedly upload or
   overwrite sibling projects.

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/kopia_wrapper.go` | `initLocalRepo()`, `openLocalRepo()`, `snapshotDir()`, `snapshotProject()`, `runCloudBackup()`, `runCloudRestore()`, `restoreSnapshotTo()` |
| `hazmat/restore.go` | `runProjectRestore()` |
| `hazmat/internal/backupruntime/session.go` | `PreSessionSnapshot()` |
| `hazmat/session.go` | `preSessionSnapshot()` wrapper and session commands (`shell`, `exec`, `claude`, `opencode`) |
| `hazmat/backup.go` | `newBackupCmd()`, `backupBuiltinExcludes` |

## Operation Paths (as implemented)

### Pre-session snapshot
```
session command → preSessionSnapshot(cfg, cmd, skip)
  skip=true  → return (no snapshot)
  skip=false → snapshotProject(projectDir, cmd, ignoreRules...)
    openLocalRepo()  → auto-init if needed
    snapshotDir()    → create snapshot with configured ignore rules
    success → print timing, continue to session
    failure → warn to stderr, continue to session
```

### Local project restore
```
hazmat restore [--session=N]
  openLocalRepo()
  listSnapshots(projectDir) → validate session index
  user confirmation (unless --yes)
  snapshotProject(projectDir, "pre-restore")  ← SAFETY NET
    failure → warn, proceed (current state may not be recoverable)
  restoreSnapshotTo(target, projectDir)
```

### Cloud project restore
```
hazmat restore --cloud [-C projectDir]
  resolveDir(projectDir or cwd)
  openCloudRepo()  → loadCloudConfig() + connect to S3
  listSnapshots(projectDir)
  snapshotProject(projectDir, "pre-cloud-restore")  ← SAFETY NET
    failure → warn, proceed
  restoreSnapshotTo(latest, projectDir)
```

### Cloud project backup
```
hazmat backup --cloud [-C projectDir]
  resolveDir(projectDir or cwd)
  openCloudRepo()  → loadCloudConfig() + connect to S3
  snapshotDir(projectDir, "Hazmat project cloud backup")
```

## TLA+ Model

### Variables

- `repoState` — `"absent"` | `"initialized"` — local Kopia repository
- `cloudConfigured` — `BOOLEAN` — cloud credentials configured
- `snapshotCount` — `0..MaxSnapshots` — how many snapshots exist
- `sessionPhase` — `"idle"` | `"snapshot_attempted"` | `"in_session"` — session lifecycle
- `restorePhase` — `"idle"` | `"pre_restore_snap"` | `"restoring"` — restore lifecycle
- `restoreType` — `"none"` | `"local"` | `"cloud"` — which restore path is active
- `preRestoreSnapshotAttempted` — `BOOLEAN` — tracks whether an overwrite is preceded by a pre-restore snapshot attempt
- `restoreTarget` — `"none"` | `"project"` | `"workspace"` — target currently being restored
- `preRestoreSnapshotTarget` — `"none"` | `"project"` | `"workspace"` — target snapshotted before restore
- `lastCloudBackupTarget` — `"none"` | `"project"` | `"workspace"` — target used by the latest cloud backup

### Actions

- `InitRepo` — initialize local repository (idempotent)
- `BeginSession` — start a session (with or without --no-backup)
- `PreSessionSnapshotSucceed` — pre-session snapshot succeeds
- `PreSessionSnapshotFail` — pre-session snapshot fails, session continues
- `SkipSnapshot` — --no-backup flag, session proceeds without snapshot
- `EnterSession` — cross sandbox boundary (session now running)
- `BeginLocalRestore` — start local project restore
- `BeginCloudRestore` — start cloud restore
- `PreRestoreSnapshotSucceed` — pre-restore snapshot succeeds
- `PreRestoreSnapshotFail` — pre-restore snapshot fails, restore continues
- `RestoreComplete` — restore overwrites destination
- `CloudBackup` — manual project-scoped cloud backup
- `RollbackRepo` — remove local repository (during hazmat rollback)

### Key Design Choices

1. **Auto-init is idempotent.** `openLocalRepo()` calls `initLocalRepo()` if
   the config file doesn't exist. `initLocalRepo()` returns nil if already
   initialized. This means snapshot/restore operations never fail due to
   missing repo — they create it on the fly.

2. **Pre-restore snapshot failure is non-fatal.** Both local and cloud restore
   warn but proceed if the pre-restore snapshot fails. This is deliberate:
   a broken repo shouldn't prevent recovery from a known-good snapshot.

3. **Session snapshot failure is non-fatal.** Same principle: a broken backup
   system shouldn't prevent the user from working.

4. **Cloud operations require config.** `loadCloudConfig()` fails immediately
   with a helpful error if the config file doesn't exist.

5. **Snapshot contents are out of model.** Config-level exclude rules and
   session-only integration excludes change what gets snapshotted, but this
   spec models only ordering and non-blocking behavior, not exact file sets.

6. **Cloud operations are project-scoped.** Manual cloud backup and cloud
   restore resolve the same selected project directory convention as local
   restore: `-C` when provided, otherwise the current working directory. The
   TLA+ model includes `"workspace"` as a possible target value only so the
   `CloudTargetsProject` invariant can catch reintroducing the retired
   workspace target.

## What TLC Should Find

### Invariants to verify

| Invariant | Meaning |
|-----------|---------|
| `RestoreReversible` | Every restore (local or cloud) attempts a pre-restore snapshot before overwriting |
| `RepoBeforeSnapshot` | Pre-session snapshot attempts only occur after local repo initialization |
| `CloudRequiresConfig` | Cloud backup and cloud restore only occur when cloud is configured |
| `NoOverwriteWithoutAttempt` | Restore overwrites are always preceded by a pre-restore snapshot attempt |
| `CloudTargetsProject` | Cloud backup and cloud restore use the selected project target, not a workspace target |
| `PreRestoreSnapshotMatchesTarget` | The pre-restore safety snapshot covers the same target that restore overwrites |

### Liveness

| Property | Meaning |
|----------|---------|
| `SessionEventuallyLaunches` | A session command eventually reaches `in_session` state |
| `RestoreEventuallyCompletes` | A restore that starts eventually reaches completion under the checked fairness assumptions |

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxSnapshots` | 3 | Enough to model: 0 (empty), 1 (one snapshot), 2+ (pre-restore + restore target) |
| `MaxSessions` | 2 | Covers: session creates snapshot, then another session or restore |
| `MaxRestores` | 2 | Covers: restore, then undo-restore |

Expected state space: under 1,000 distinct states, <1s runtime.
