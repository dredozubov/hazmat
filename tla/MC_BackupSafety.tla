---- MODULE MC_BackupSafety ----
\* Backup/restore safety — verifies that every restore path takes a pre-restore
\* snapshot before overwriting, that session snapshot failures never block the
\* session, and that repository preconditions are always met.
\*
\* The model covers three operation paths:
\*   1. Pre-session snapshot (automatic before hazmat claude/exec/shell/opencode)
\*   2. Local project restore (hazmat restore)
\*   3. Cloud restore (hazmat restore --cloud)
\*   4. Cloud backup (hazmat backup --cloud)
\*
\* Pack-specific/configured snapshot ignore rules are intentionally out of model.
\*
\* Each operation can succeed or fail nondeterministically. The key safety
\* properties are about ordering (snapshot before overwrite) and graceful
\* degradation (snapshot failure doesn't block sessions).
\*
\* Expected TLC result: No error has been found. (~200-500 states)
\*
\* Governed code:
\*   hazmat/kopia_wrapper.go — openLocalRepo(), snapshotProject(), runCloudBackup(), runCloudRestore()
\*   hazmat/restore.go       — runProjectRestore()
\*   hazmat/session.go       — preSessionSnapshot(), session commands

EXTENDS Naturals

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    repoState,        \* "absent" | "initialized"
    cloudConfigured,  \* BOOLEAN — cloud credentials set up
    snapshotCount,    \* 0..MaxSnapshots — local snapshots that exist

    \* Session lifecycle
    sessionPhase,     \* "idle" | "snapshot_pending" | "snapshot_done" | "snapshot_failed" | "skipped" | "in_session"
    sessionAttempts,  \* how many session commands have been issued

    \* Restore lifecycle
    restorePhase,     \* "idle" | "pre_snap_pending" | "pre_snap_done" | "pre_snap_failed" | "restoring" | "done"
    restoreType,      \* "none" | "local" | "cloud"
    restoreAttempts,  \* how many restores have been issued

    \* Safety tracking
    preRestoreSnapshotAttempted  \* BOOLEAN — was a pre-restore snapshot attempted before the current restore?

CONSTANTS
    MaxSnapshots,
    MaxSessions,
    MaxRestores

vars == <<repoState, cloudConfigured, snapshotCount,
          sessionPhase, sessionAttempts,
          restorePhase, restoreType, restoreAttempts,
          preRestoreSnapshotAttempted>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ repoState     \in {"absent", "initialized"}
    /\ cloudConfigured \in BOOLEAN
    /\ snapshotCount \in 0..MaxSnapshots
    /\ sessionPhase  \in {"idle", "snapshot_pending", "snapshot_done", "snapshot_failed", "skipped", "in_session"}
    /\ sessionAttempts \in 0..MaxSessions
    /\ restorePhase  \in {"idle", "pre_snap_pending", "pre_snap_done", "pre_snap_failed", "restoring", "done"}
    /\ restoreType   \in {"none", "local", "cloud"}
    /\ restoreAttempts \in 0..MaxRestores
    /\ preRestoreSnapshotAttempted \in BOOLEAN

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ repoState     = "absent"
    /\ cloudConfigured = FALSE
    /\ snapshotCount = 0
    /\ sessionPhase  = "idle"
    /\ sessionAttempts = 0
    /\ restorePhase  = "idle"
    /\ restoreType   = "none"
    /\ restoreAttempts = 0
    /\ preRestoreSnapshotAttempted = FALSE

\* ═══════════════════════════════════════════════════════════════════════════════
\* External actions — model environment changes
\* ═══════════════════════════════════════════════════════════════════════════════

\* User runs `hazmat init` or repo auto-initializes. Idempotent.
InitRepo ==
    /\ repoState' = "initialized"
    /\ UNCHANGED <<cloudConfigured, snapshotCount, sessionPhase, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* User runs `hazmat init cloud` to configure S3 credentials.
ConfigureCloud ==
    /\ ~cloudConfigured
    /\ cloudConfigured' = TRUE
    /\ UNCHANGED <<repoState, snapshotCount, sessionPhase, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Rollback removes the local repository.
RollbackRepo ==
    /\ sessionPhase = "idle"
    /\ restorePhase = "idle"
    /\ repoState' = "absent"
    /\ snapshotCount' = 0
    /\ UNCHANGED <<cloudConfigured, sessionPhase, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Session lifecycle — models preSessionSnapshot() + session launch
\*
\* Session commands: hazmat claude, hazmat exec, hazmat shell, hazmat opencode
\* Each calls preSessionSnapshot() which calls openLocalRepo() (auto-init)
\* then snapshotProject(). On failure, warns but proceeds.
\* ═══════════════════════════════════════════════════════════════════════════════

\* Begin a session command (without --no-backup).
\* openLocalRepo() auto-inits the repo if needed.
BeginSession ==
    /\ sessionPhase = "idle"
    /\ restorePhase = "idle"
    /\ sessionAttempts < MaxSessions
    /\ sessionPhase'  = "snapshot_pending"
    /\ sessionAttempts' = sessionAttempts + 1
    \* Auto-init: openLocalRepo() creates repo if absent
    /\ repoState' = "initialized"
    /\ UNCHANGED <<cloudConfigured, snapshotCount,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Begin a session with --no-backup flag.
BeginSessionNoBackup ==
    /\ sessionPhase = "idle"
    /\ restorePhase = "idle"
    /\ sessionAttempts < MaxSessions
    /\ sessionPhase'  = "skipped"
    /\ sessionAttempts' = sessionAttempts + 1
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Pre-session snapshot succeeds.
PreSessionSnapshotSucceed ==
    /\ sessionPhase = "snapshot_pending"
    /\ repoState = "initialized"
    /\ sessionPhase' = "snapshot_done"
    /\ snapshotCount' = IF snapshotCount < MaxSnapshots THEN snapshotCount + 1 ELSE snapshotCount
    /\ UNCHANGED <<repoState, cloudConfigured, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Pre-session snapshot fails (Kopia error, disk full, etc.).
PreSessionSnapshotFail ==
    /\ sessionPhase = "snapshot_pending"
    /\ sessionPhase' = "snapshot_failed"
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Session launches after snapshot (succeeded, failed, or skipped).
\* This is the key safety property: session ALWAYS proceeds.
EnterSession ==
    /\ sessionPhase \in {"snapshot_done", "snapshot_failed", "skipped"}
    /\ sessionPhase' = "in_session"
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Session ends, return to idle.
EndSession ==
    /\ sessionPhase = "in_session"
    /\ sessionPhase' = "idle"
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionAttempts,
                    restorePhase, restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Local restore lifecycle — models runProjectRestore()
\*
\* Steps: open repo → list snapshots → confirm → pre-restore snapshot → restore
\* Pre-restore snapshot failure is non-fatal (warn, proceed).
\* ═══════════════════════════════════════════════════════════════════════════════

\* Begin local restore. Requires repo initialized and at least one snapshot.
BeginLocalRestore ==
    /\ restorePhase = "idle"
    /\ sessionPhase = "idle"
    /\ restoreAttempts < MaxRestores
    /\ repoState = "initialized"
    /\ snapshotCount > 0
    /\ restorePhase' = "pre_snap_pending"
    /\ restoreType' = "local"
    /\ restoreAttempts' = restoreAttempts + 1
    /\ preRestoreSnapshotAttempted' = FALSE
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionPhase, sessionAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Cloud restore lifecycle — models runCloudRestore()
\*
\* Steps: open cloud repo → list snapshots → pre-restore snapshot → restore
\* Requires cloud configured. Pre-restore snapshot uses LOCAL repo.
\* ═══════════════════════════════════════════════════════════════════════════════

\* Begin cloud restore. Requires cloud configured.
\* Auto-inits local repo for the pre-restore snapshot.
BeginCloudRestore ==
    /\ restorePhase = "idle"
    /\ sessionPhase = "idle"
    /\ restoreAttempts < MaxRestores
    /\ cloudConfigured
    /\ restorePhase' = "pre_snap_pending"
    /\ restoreType' = "cloud"
    /\ restoreAttempts' = restoreAttempts + 1
    /\ preRestoreSnapshotAttempted' = FALSE
    \* Auto-init local repo for pre-restore snapshot
    /\ repoState' = "initialized"
    /\ UNCHANGED <<cloudConfigured, snapshotCount, sessionPhase, sessionAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Shared restore steps — pre-restore snapshot + overwrite
\* Both local and cloud restore follow the same pattern.
\* ═══════════════════════════════════════════════════════════════════════════════

\* Pre-restore snapshot succeeds.
PreRestoreSnapshotSucceed ==
    /\ restorePhase = "pre_snap_pending"
    /\ repoState = "initialized"
    /\ restorePhase' = "pre_snap_done"
    /\ preRestoreSnapshotAttempted' = TRUE
    /\ snapshotCount' = IF snapshotCount < MaxSnapshots THEN snapshotCount + 1 ELSE snapshotCount
    /\ UNCHANGED <<repoState, cloudConfigured, sessionPhase, sessionAttempts,
                    restoreType, restoreAttempts>>

\* Pre-restore snapshot fails. Restore proceeds with warning.
PreRestoreSnapshotFail ==
    /\ restorePhase = "pre_snap_pending"
    /\ restorePhase' = "pre_snap_failed"
    /\ preRestoreSnapshotAttempted' = TRUE
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionPhase, sessionAttempts,
                    restoreType, restoreAttempts>>

\* Begin the actual restore (overwrite destination).
\* Only proceeds after pre-restore snapshot was attempted (success or failure).
BeginOverwrite ==
    /\ restorePhase \in {"pre_snap_done", "pre_snap_failed"}
    /\ restorePhase' = "restoring"
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionPhase, sessionAttempts,
                    restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Restore completes. Return to idle.
RestoreComplete ==
    /\ restorePhase = "restoring"
    /\ restorePhase' = "done"
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionPhase, sessionAttempts,
                    restoreType, restoreAttempts, preRestoreSnapshotAttempted>>

\* Restore done, return to idle.
RestoreDone ==
    /\ restorePhase = "done"
    /\ restorePhase' = "idle"
    /\ restoreType'  = "none"
    /\ preRestoreSnapshotAttempted' = FALSE
    /\ UNCHANGED <<repoState, cloudConfigured, snapshotCount, sessionPhase, sessionAttempts,
                    restoreAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Cloud backup — models runCloudBackup()
\* Requires cloud configured. Creates a snapshot in the cloud repo.
\* ═══════════════════════════════════════════════════════════════════════════════

CloudBackup ==
    /\ sessionPhase = "idle"
    /\ restorePhase = "idle"
    /\ cloudConfigured
    /\ UNCHANGED vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Terminal — allow stuttering when all attempts exhausted.
\* ═══════════════════════════════════════════════════════════════════════════════

Done ==
    /\ sessionPhase = "idle"
    /\ restorePhase = "idle"
    /\ sessionAttempts = MaxSessions
    /\ restoreAttempts = MaxRestores
    /\ UNCHANGED vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Next-state relation
\* ═══════════════════════════════════════════════════════════════════════════════

Next ==
    \/ InitRepo
    \/ ConfigureCloud
    \/ RollbackRepo
    \/ BeginSession
    \/ BeginSessionNoBackup
    \/ PreSessionSnapshotSucceed
    \/ PreSessionSnapshotFail
    \/ EnterSession
    \/ EndSession
    \/ BeginLocalRestore
    \/ BeginCloudRestore
    \/ PreRestoreSnapshotSucceed
    \/ PreRestoreSnapshotFail
    \/ BeginOverwrite
    \/ RestoreComplete
    \/ RestoreDone
    \/ CloudBackup
    \/ Done

Fairness ==
    /\ WF_vars(PreSessionSnapshotSucceed)
    /\ WF_vars(PreSessionSnapshotFail)
    /\ WF_vars(EnterSession)
    /\ WF_vars(EndSession)
    /\ WF_vars(PreRestoreSnapshotSucceed)
    /\ WF_vars(PreRestoreSnapshotFail)
    /\ WF_vars(BeginOverwrite)
    /\ WF_vars(RestoreComplete)
    /\ WF_vars(RestoreDone)

Spec == Init /\ [][Next]_vars /\ Fairness

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- Restore reversibility: every restore overwrites ONLY after a pre-restore
\* snapshot was attempted (success or failure). ---
\* This is the property that caught the cloud restore bug: runCloudRestore()
\* was missing the pre-restore snapshot entirely.
RestoreReversible ==
    restorePhase = "restoring" => preRestoreSnapshotAttempted

\* --- Session non-blocking: snapshot failure always leads to session launch,
\* never blocks. Enforced structurally: from snapshot_failed, only EnterSession
\* is enabled. Combined with WF_vars(EnterSession) fairness, TLC verifies that
\* SessionEventuallyLaunches holds even when the snapshot fails.

\* --- Repo precondition: snapshots only happen when repo is initialized. ---
\* openLocalRepo() auto-inits, so BeginSession sets repoState = "initialized".
\* This verifies that the auto-init happens before the snapshot attempt.
RepoBeforeSnapshot ==
    sessionPhase = "snapshot_pending" => repoState = "initialized"

\* --- Cloud precondition: cloud operations require cloud configuration. ---
CloudRequiresConfig ==
    restoreType = "cloud" => cloudConfigured

\* --- No overwrite without snapshot attempt ---
\* Stronger than RestoreReversible: the restoring phase is unreachable
\* without first passing through pre_snap_pending.
NoOverwriteWithoutAttempt ==
    restorePhase \in {"restoring", "done"} => preRestoreSnapshotAttempted

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness properties
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- Sessions always eventually launch (given fairness). ---
\* If we start a session attempt, we eventually reach in_session.
SessionEventuallyLaunches ==
    (sessionPhase = "snapshot_pending") ~> (sessionPhase = "in_session")

\* --- Restores always eventually complete (given fairness). ---
RestoreEventuallyCompletes ==
    (restorePhase = "pre_snap_pending") ~> (restorePhase = "idle")

====
