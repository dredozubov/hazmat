# TLA+ Verified Areas — Hazmat

This document is the authoritative record of which subsystems are under formal
verification, what was proved or disproved, and the governance rules that apply
to future changes in those areas.

---

## Governance Rules

### When TLA+ is required

A change to a verified subsystem **must** be accompanied by TLA+ analysis before
committing. Specifically:

1. **Code changes in a verified area** — re-run TLC against the existing spec to
   confirm the invariants still hold after your change. If the spec's model no
   longer matches the new code, update the spec first, prove the new invariants
   with TLC, then update the implementation to match the proved design.

2. **Spec update before implementation** — if you want to change the correctness
   properties (e.g., relax an invariant, add a new one), write and prove the new
   spec first. Only then propagate the design to implementation. Do not implement
   first and update the spec to match.

3. **New setup or rollback steps** — if you add a new resource to the setup
   chain, add it to the TLA+ model first. Prove that the ordering preserves
   all invariants, then implement.

4. **Reordering steps** — any change to the order of setup or rollback steps
   must be modeled and proved before the code change. Step ordering is the
   primary source of bugs in this subsystem.

### What "proved" means here

TLC exhaustively checks all reachable states within the model bounds. A spec is
considered **proved** when TLC reports "No error has been found" with the bounds
listed in each spec's `.md` file. See `USAGE.md` for how to run TLC.

### Spec files

File naming convention: `MC_<slug>.tla` + `MC_<slug>.cfg`.

---

## Verified Subsystems

### 1 — Setup/Rollback State Machine

| Field | Value |
|-------|-------|
| Spec | `tla/01_setup_rollback_state_machine.md` |
| TLA+ files | `tla/MC_SetupRollback.tla`, `tla/MC_SetupRollback.cfg` |
| Governed code | `hazmat/setup.go` — `runSetup()`, all `setupX()` functions |
| Governed code | `hazmat/rollback.go` — `runRollback()`, all `rollbackX()` functions |
| Key invariants | `AgentContained`, `NoOrphanedArtifacts`, `SudoersRequiresHelper`, `AgentDepsRequireUser` |
| Key liveness | `CanAlwaysReachClean`, `SetupEventuallyCompletes` |
| Status | **Fixed** — containment before privilege in both setup and rollback |

**What was found:**

1. **Setup:** sudoers was installed (step 8) before pf firewall (step 9). If
   setup was interrupted between those steps, the agent was launchable without
   firewall containment.

2. **Rollback:** pf firewall was removed (step 2) before sudoers (step 4). If
   rollback was interrupted between those steps, the agent remained launchable
   with the firewall already gone. Mirror image of the setup bug.

**Fixes applied:**

1. **Setup:** Reordered so pf/dns/daemon run before launchHelper and sudoers.
2. **Rollback:** Reordered so sudoers is removed first, before firewall/dns/daemon.

The principle: **grant privilege last, revoke privilege first.**
`AgentContained` now passes across all 26,905 reachable states (<1s).

**Change rules:**
- Any change to setup step ordering must be modeled and proved against
  `AgentContained` before committing.
- Adding a new setup step requires adding the corresponding resource variable
  and updating `SetupStepSucceed` / `RollbackCore` / `RollbackDestructive`.
- Adding a new rollback step (e.g., a new `--delete-X` flag) requires a new
  rollback action in the spec.
- Changes to which resources rollback preserves vs. removes must be reflected
  in `RollbackCore` and checked against `NoOrphanedArtifacts`.

---

### 2 — Seatbelt Policy Structure

| Field | Value |
|-------|-------|
| Spec | `tla/02_seatbelt_policy_structure.md` |
| TLA+ files | `tla/MC_SeatbeltPolicy.tla`, `tla/MC_SeatbeltPolicy.cfg` |
| Governed code | `hazmat/session.go` — `generateSBPL()`, `isWithinDir()` |
| Key invariants | `CredentialReadDenied`, `CredentialWriteDenied`, `ReadDirsNoWrite`, `ProjectDirWritable`, `ReadDirSubsumption`, `ResumeDirNotCredential` |
| Status | **Fixed** — credential denies cover both ops; resume dir + project re-assertion modeled |

**What was found:** Credential deny rules only blocked `file-read*`, not
`file-write*`. Two vectors: (a) `ProjectDir = /Users/agent` granted write to
`.ssh`; (b) static `.config` allow covered `.config/gcloud` writes.

**Fixes applied:**

1. Changed deny rules from `(deny file-read* ...)` to
   `(deny file-read* file-write* ...)`. Both reads and writes to all credential
   paths are now denied regardless of user input.

2. Added `ResumeDir` (section 3) — optional read+write allow for the invoking
   user's session directory when `--resume` or `--continue` is used. This path is
   under the invoker's home (e.g., `/Users/dr/.claude/...`), never under agent home,
   so it cannot overlap with credential paths. `ResumeDirNotCredential` verifies this.

3. Added project write re-assertion (section 5) — when a read-only `-R` directory
   is a parent of the project directory, the project's write access is re-asserted
   as the last allow before credential denies.

Policy sections are now: 0=system libs, 1=read dirs, 2=project r+w, 3=resume dir,
4=agent home, 5=project write re-assert, 6=credential denies.

**Change rules:**
- Do not reorder the sections in `generateSBPL()` — credential denies MUST be
  last. Moving any allow after the denies would break `CredentialReadDenied`.
- Adding new credential paths to the deny list requires adding them to
  `CredPaths` in the TLA+ model and re-running TLC.
- Adding new static allow paths (new `AgentHomeSubs`) requires checking whether
  they cover any credential paths — add to the model and re-verify.
- Adding new optional read+write sections (like ResumeDir) requires modeling the
  path and verifying it cannot overlap with `CredPaths`.

---

### 3 — Backup/Restore Safety

| Field | Value |
|-------|-------|
| Spec | `tla/03_backup_restore_safety.md` |
| TLA+ files | `tla/MC_BackupSafety.tla`, `tla/MC_BackupSafety.cfg` |
| Governed code | `hazmat/kopia_wrapper.go` — `openLocalRepo()`, `snapshotProject()`, `runCloudBackup()`, `runCloudRestore()` |
| Governed code | `hazmat/restore.go` — `runProjectRestore()` |
| Governed code | `hazmat/session.go` — `preSessionSnapshot()`, session commands |
| Key invariants | `RestoreReversible`, `RepoBeforeSnapshot`, `CloudRequiresConfig`, `NoOverwriteWithoutAttempt` |
| Key liveness | `SessionEventuallyLaunches`, `RestoreEventuallyCompletes` |
| Status | **Fixed** — cloud restore now takes pre-restore snapshot before overwriting |

**What was found:**

1. **Cloud restore:** `runCloudRestore()` overwrote the entire workspace without
   taking a pre-restore snapshot. If the cloud snapshot was stale or wrong, the
   user's current workspace was permanently lost with no undo. The local restore
   path (`runProjectRestore()`) did this correctly.

**Fix applied:**

1. Added `snapshotProject(sharedWorkspace, "pre-cloud-restore")` to
   `runCloudRestore()` before the overwrite, matching the pattern in
   `runProjectRestore()`. Failure is non-fatal (warn and proceed).

The principle: **every overwrite must be preceded by a snapshot attempt.**
`RestoreReversible` now passes across all 395 distinct states (<1s).

**Change rules:**
- Adding a new restore path (e.g., restore from external drive) must include a
  pre-restore snapshot step. Add the path to the TLA+ model and verify
  `RestoreReversible` still holds.
- Changing when `preSessionSnapshot()` is called relative to sandbox entry must
  preserve the ordering: snapshot before sandbox boundary.
- Adding new snapshot triggers must ensure `openLocalRepo()` auto-init is
  called first (modeled by `RepoBeforeSnapshot`).

---

### 4 — Version Migration and Rollback from Any State

| Field | Value |
|-------|-------|
| Spec | `tla/04_version_migration.md` |
| TLA+ files | `tla/MC_Migration.tla`, `tla/MC_Migration.cfg` |
| Governed code | `hazmat/init.go` — migration dispatch, `runInit()` |
| Governed code | `hazmat/migrate.go` — migration functions (per-version) |
| Governed code | `hazmat/rollback.go` — `runRollback()`, artifact removal ordering |
| Governed code | `~/.hazmat/state.json` — version tracking |
| Key invariants | `AgentContained`, `InitComplete`, `VersionConsistent`, `FailureRecoverable`, `MigrationForward`, `RollbackClean`, `RollbackAlwaysAvailable` |
| Key liveness | `EventuallyComplete` |
| Status | **Proved** — 44,795 states, 140,535 transitions, 0 errors (3s) |

**What this verifies:**

1. **Forward migration:** Upgrading from any previous init version (v0.1.0,
   v0.2.0) to the current binary version (v0.3.0) produces a consistent
   system with exactly the expected artifacts. Migrations are sequential —
   no version is skipped.

2. **Rollback from any state:** The system can reach a clean state (zero
   artifacts) via rollback from any intermediate state: fully initialized,
   mid-migration, or after a migration failure. Rollback respects ordering
   constraints — sudoers is removed before pfAnchor (revoke privilege
   before removing containment).

3. **AgentContained everywhere:** Across all 44,795 reachable states —
   including partial migrations, failed states, and partial rollbacks — the
   agent is never launchable without firewall containment.

4. **Failure recovery:** From any failed state, the user can either retry
   init (resume migration) or start rollback. No state is permanently stuck.

**What was found during spec development:**

1. **Liveness violation:** The first version used weak fairness on
   `MigrateSucceed`, which allowed an infinite fail → recover → fail loop
   without progress. TLC caught this. Fixed with strong fairness (models
   the assumption that transient failures eventually clear).

**Change rules:**
- Adding a new hazmat version requires adding it to `MC_Migration.tla`:
  new `V4` constant, `Expected(V4)` definition, `HasMigration(V3, V4)`,
  and `NextVersion(V3) == V4`. Run TLC — it checks all paths from every
  older version through the new migration, including rollback.
- The `CanRemove` function defines rollback ordering constraints. If a new
  artifact depends on another (like sudoers depends on pfAnchor), add the
  constraint there and re-verify.
- The `AgentContained` invariant must pass across ALL states — init,
  migration, failed, and rollback. This is the non-negotiable property.

---

## Quick Reference: Spec → Code Mapping

| Spec | Files governed |
|------|---------------|
| `01_setup_rollback_state_machine` | `hazmat/init.go:runInit()`, all `setupX()`; `hazmat/rollback.go:runRollback()`, all `rollbackX()` |
| `02_seatbelt_policy_structure` | `hazmat/session.go:generateSBPL()`, `isWithinDir()` |
| `03_backup_restore_safety` | `hazmat/kopia_wrapper.go:runCloudRestore()`, `snapshotProject()`; `hazmat/restore.go:runProjectRestore()`; `hazmat/session.go:preSessionSnapshot()` |
| `04_version_migration` | `hazmat/init.go` migration dispatch; `hazmat/migrate.go` migration functions |

---

## Workflow: Updating a Spec and Propagating to Code

```
1. Identify which spec governs the code you want to change.
   → See "Quick Reference" table above.

2. Write or update the .tla spec to model your intended design.
   → Use the skeleton in USAGE.md.
   → All new actions, variables, and transitions go in the spec first.

3. Run TLC to prove the invariants hold.
   → See USAGE.md for the exact command.
   → TLC must exit 0 ("No error has been found") with the model bounds.

4. If TLC finds a violation, revise the design (not the invariant) until it passes.

5. Implement the proved design in Go.

6. Update this file (VERIFIED.md): bump the status, add the commit ref,
   and note any change rules that were added or removed.
```

---

## Adding a New Verified Area

If you identify a new correctness hazard:

1. Write a new `NN_<slug>.md` in `tla/` following the existing format.
2. Add it to the table in `README.md`.
3. Add a row to the "Verified Subsystems" section above.
4. Write the `.tla` / `.cfg` files, run TLC, record the result here.
