# Problem 3 — Backup/Restore Safety

## Problem Statement

Hazmat automatically snapshots project directories before each session and
supports manual cloud backup/restore of the entire workspace. The system uses
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

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/kopia_wrapper.go` | `initLocalRepo()`, `openLocalRepo()`, `snapshotDir()`, `snapshotProject()`, `runCloudBackup()`, `runCloudRestore()`, `restoreSnapshotTo()` |
| `hazmat/restore.go` | `runProjectRestore()` |
| `hazmat/session.go` | `preSessionSnapshot()`, session commands (`shell`, `exec`, `claude`) |
| `hazmat/backup.go` | `newBackupCmd()`, `backupBuiltinExcludes` |

## Operation Paths (as implemented)

### Pre-session snapshot
```
session command → preSessionSnapshot(projectDir, cmd, skip)
  skip=true  → return (no snapshot)
  skip=false → snapshotProject(projectDir, cmd)
    openLocalRepo()  → auto-init if needed
    snapshotDir()    → create snapshot
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

### Cloud restore
```
hazmat restore --cloud
  openCloudRepo()  → loadCloudConfig() + connect to S3
  listSnapshots(sharedWorkspace)
  snapshotProject(sharedWorkspace, "pre-cloud-restore")  ← SAFETY NET (was missing)
    failure → warn, proceed
  restoreSnapshotTo(latest, sharedWorkspace)
```

### Cloud backup
```
hazmat backup --cloud
  openCloudRepo()  → loadCloudConfig() + connect to S3
  snapshotDir(sharedWorkspace, "Hazmat workspace backup")
```

## TLA+ Model

### Variables

- `repoState` — `"absent"` | `"initialized"` — local Kopia repository
- `cloudConfigured` — `BOOLEAN` — cloud credentials configured
- `snapshotCount` — `0..MaxSnapshots` — how many snapshots exist
- `sessionPhase` — `"idle"` | `"snapshot_attempted"` | `"in_session"` — session lifecycle
- `restorePhase` — `"idle"` | `"pre_restore_snap"` | `"restoring"` — restore lifecycle
- `restoreType` — `"none"` | `"local"` | `"cloud"` — which restore path is active
- `dataLost` — `BOOLEAN` — tracks whether an overwrite happened without a prior snapshot attempt

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
- `CloudBackup` — manual cloud backup
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

## What TLC Should Find

### Invariants to verify

| Invariant | Meaning |
|-----------|---------|
| `RestoreReversible` | Every restore (local or cloud) attempts a pre-restore snapshot before overwriting |
| `SessionNonBlocking` | Snapshot failure leads to session proceeding, never blocking |
| `RepoBeforeOps` | Snapshot and restore operations only occur when repo is initialized |
| `CloudRequiresConfig` | Cloud backup and cloud restore only occur when cloud is configured |
| `NoSilentDataLoss` | `dataLost` is never TRUE — overwrites always preceded by snapshot attempt or explicit skip |

### Liveness

| Property | Meaning |
|----------|---------|
| `SessionEventuallyLaunches` | A session command eventually reaches `in_session` state |

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxSnapshots` | 3 | Enough to model: 0 (empty), 1 (one snapshot), 2+ (pre-restore + restore target) |
| `MaxSessions` | 2 | Covers: session creates snapshot, then another session or restore |
| `MaxRestores` | 2 | Covers: restore, then undo-restore |

Expected state space: a few hundred distinct states, <1s runtime.
