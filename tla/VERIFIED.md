# TLA+ Verified Areas — Hazmat

This document is the authoritative record of which subsystems are under formal
verification, what was proved or disproved, and the governance rules that apply
to future changes in those areas.

Important scope boundary: the current TLA+ suite governs Hazmat's core
containment, rollback, seatbelt, backup, core version-migration logic,
session-time host permission repair planning/persistence, built-in harness
state recording/rollback cleanup, and host-owned harness secret-store
crash recovery. It still does **not** model curated import file contents,
session-only integration activation/pinning, or future harness plugin systems.
Those should not be implied by the existing proofs.

Important additional scope boundary: the current TLA+ suite now includes the
host-side Tier 3 launch boundary for Docker-capable sessions: mount-planner
exclusions, zero extra env passthrough in the current implementation, backend
readiness gating, and policy-before-launch ordering. It still does **not**
model Docker Sandbox or microVM internals, container runtime behavior after
launch, Compose semantics, or future non-Docker backends.

Important equivalence boundary: the current suite also models a backend-neutral
effective-policy contract shared by Tier 2 and Tier 3. It proves a narrower
core containment equivalence and disproves exact backend identity. The suite
does **not** claim that Seatbelt policy and Docker Sandbox runtime behavior are
identical implementations.

Important launch-boundary addition: the current suite now also models the
native helper's launch-time fd table. It proves Hazmat's native path reaches
`sandbox_init()` with only stdio plus helper-opened policy state, and that the
final agent exec keeps only stdio. The suite still does **not** model `sudo`
internals, Go runtime internals, or kernel behavior beyond that abstract fd
contract.

Important concrete-IO boundary: the current suite models which repair classes
and harness-state transitions Hazmat may plan, apply, preserve, or delete. It
does **not** model the exact `chmod`/ACL syscall effects, concrete filesystem
walk details, agent Git `safe.directory` config mutations, imported file
contents, or timestamp values. Those remain governed by tests and
documentation.

Important hook-activation boundary: the current suite now also models the
host-side repo-local Git hook approval state machine: manifest-backed approval,
immutable approved snapshot execution, `core.hooksPath` pinning, and refusal on
drift or reroute. It still does **not** model exact `hooks.yaml` parsing,
human-readable diff summarization, shell-script contents, or arbitrary direct
invocation of foreign `git` binaries outside Hazmat-managed entrypoints.

Important secret-store boundary: the current suite now also models file-backed
harness auth crash recovery: host-owned primary storage, temporary agent-side
materialization, startup recovery of residue, conflict archive preservation,
harvest, removal, and crash/restart at each phase. It still does **not** model
Keychain-backed auth, exact JSON merge semantics, or concurrent host-store
writes while a session is running.

Important credential-capability boundary: the current suite now also models the
registry-level credential lifecycle: delivery mode matching, session scoping,
adapter-required backend denial, env/broker/external grant cleanup on crash,
and file-backed residue recovery as a precondition to session launch. It still
does **not** model concrete Keychain APIs, git credential-helper bytes, SSH
agent liveness, cloud-provider APIs, or exact integration manifest parsing.

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
| Governed code | `hazmat/init.go` — `runInit()`, all `setupX()` functions |
| Governed code | `hazmat/sudoers.go` — optional agent-maintenance sudoers choice |
| Governed code | `hazmat/rollback.go` — `runRollback()`, all `rollbackX()` functions |
| Key invariants | `AgentContained`, `NoOrphanedArtifacts`, `SudoersRequiresHelper`, `PrivilegeRequiresAgentUser`, `AgentDepsRequireUser` |
| Key liveness | `CanAlwaysReachClean` |
| Status | **Fixed and Re-Proved** — containment before privilege in both setup and rollback, including the optional broader maintenance sudoers rule |

**What was found:**

1. **Setup:** sudoers was installed (step 8) before pf firewall (step 9). If
   setup was interrupted between those steps, the agent was launchable without
   firewall containment.

2. **Rollback:** pf firewall was removed (step 2) before sudoers (step 4). If
   rollback was interrupted between those steps, the agent remained launchable
   with the firewall already gone. Mirror image of the setup bug.

**Fixes applied:**

1. **Setup:** Reordered so pf/dns/daemon run before launchHelper and sudoers.
2. **Optional maintenance privilege:** The broader `agent-maintenance` sudoers rule is modeled explicitly and may only appear after firewall containment is already active. Interactive init may skip it; `hazmat init --yes` installs it by default.
3. **Rollback:** Reordered so all sudoers privilege is removed first, before firewall/dns/daemon.

The principle: **grant privilege last, revoke privilege first.**
`AgentContained` and `CanAlwaysReachClean` now pass across all 33,135 reachable
states (62,148 generated, ~1s with liveness enabled).

The bounded-retry model does **not** currently prove `SetupEventuallyCompletes`.
If setup and rollback attempts are both exhausted after repeated failures, TLC
can stutter in a partially configured idle state. Hazmat's current checked
liveness bar for this model is recoverable clean exit, not guaranteed eventual
successful completion after arbitrary bounded failures.

**Change rules:**
- Any change to setup step ordering must be modeled and proved against
  `AgentContained` before committing.
- Adding a new setup step requires adding the corresponding resource variable
  and updating `SetupStepSucceed` / `RollbackCore` / `RollbackDestructive`.
- Adding a new privilege-granting artifact requires extending `AgentContained`
  and the rollback-first privilege revocation logic, not just the setup path.
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

Important proof dependency: `CredentialReadDenied` and `CredentialWriteDenied`
reason about SBPL path matching, not already-open inherited kernel handles. The
native launch path now proves that precondition separately in
`MC_LaunchFDIsolation`: `hazmat-launch` must reach `sandbox_init()` with no
inherited credential-bearing fd still alive.

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

1. Added `snapshotProject(cloudBackupDir, "pre-cloud-restore")` to
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
| Governed code | `hazmat/sudoers.go` — optional current-version sudoers artifact |
| Governed code | `hazmat/migrate.go` — migration functions (per-version) |
| Governed code | `hazmat/rollback.go` — `runRollback()`, artifact removal ordering |
| Governed code | `~/.hazmat/state.json` — core init version tracking (`harnesses` metadata is modeled separately by `MC_HarnessLifecycle`) |
| Key invariants | `AgentContained`, `InitComplete`, `VersionConsistent`, `FailureRecoverable`, `MigrationForward`, `RollbackClean`, `RollbackAlwaysAvailable` |
| Key liveness | `EventuallyComplete` |
| Status | **Re-Proved** — 72,442 states, 234,101 transitions, 0 errors (3s) |

**What this verifies:**

1. **Forward migration:** Upgrading from any previous init version (v0.1.0,
   v0.2.0, v0.3.0) to the current binary version (v0.4.0) produces a
   consistent system with exactly the expected artifacts. Migrations are
   sequential — no version is skipped.

2. **Rollback from any state:** The system can reach a clean state (zero
   artifacts) via rollback from any intermediate state: fully initialized,
   mid-migration, or after a migration failure. Rollback respects ordering
   constraints — both sudoers artifacts are removed before pfAnchor (revoke
   privilege before removing containment).

3. **AgentContained everywhere:** Across all 72,442 reachable states —
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
- Adding an optional artifact to the current binary without a version bump
  requires updating `OptionalArtifacts(v)`, `RunInit`, and `InitComplete` so
  the model accepts both the present and absent cases explicitly.
- The `CanRemove` function defines rollback ordering constraints. If a new
  artifact depends on another (like sudoers depends on pfAnchor), add the
  constraint there and re-verify.
- The `AgentContained` invariant must pass across ALL states — init,
  migration, failed, and rollback. This is the non-negotiable property.

---

### 5 — Tier 3 Launch Containment

| Field | Value |
|-------|-------|
| Spec | `tla/05_tier3_launch_containment.md` |
| TLA+ files | `tla/MC_Tier3LaunchContainment.tla`, `tla/MC_Tier3LaunchContainment.cfg` |
| Governed code | `hazmat/sandbox.go` — `buildSandboxLaunchSpec()`, `prepareSandboxLaunch()`, `loadHealthySandboxLaunchBackend()`, `dockerSandboxesBackend.PrepareLaunch()` |
| Governed code | `hazmat/integration_manifest.go` — `isCredentialDenyPath()` |
| Governed code | `hazmat/session.go` — `isWithinDir()` |
| Key invariants | `CredentialPathsNeverMounted`, `ProjectMountedRW`, `PlannedReadDirsMountedRO`, `CoveredReadDirsOmitted`, `NoUnexpectedLaunchEnv`, `BackendValidationBeforeLaunch`, `PolicyBeforeLaunch`, `ApprovalBeforeLaunch`, `IntegrationEnvRejected`, `ShellVersionGate`, `ExtraWorkspaceVersionGate` |
| Status | **Fixed and Proved** — Tier 3 mount planning now rejects credential deny zones, filters covered read-only mounts, and preserves policy-before-launch gating |

**What was found:**

1. The initial Tier 3 Docker Sandboxes path mounted `ProjectDir` and
   `ReadDirs` directly, without a Tier 3 equivalent of the credential deny-zone
   checks already used for integration `read_dirs`.

2. The initial Tier 3 mount path also did not filter read-only directories
   already covered by the project directory or by another broader read-only
   directory, even though Tier 2 already applies that filtering in
   `generateSBPL()`.

**Fixes applied:**

1. Added `buildSandboxLaunchSpec()` as the explicit Tier 3 mount planner. It
   rejects project/read-only mount inputs that resolve to credential deny zones
   and filters read-only mounts already covered by the project or another
   broader reference path.

2. Updated Tier 3 launch compatibility checks and sandbox naming to use the
   effective read-only mount set rather than raw `ReadDirs`, so redundant
   `-R` inputs do not trigger spurious extra-workspace version gates or create
   distinct sandbox identities for the same effective mount plan.

The principle: **Tier 3 must prove its host-side launch boundary explicitly;
it cannot inherit Tier 2's Seatbelt guarantees by implication.** TLC now
passes across all 23,580 reachable states (33,876 generated, depth 9, ~1s).

**Change rules:**
- Any change to Tier 3 mount planning must preserve both properties:
  no credential-zone mounts and no redundant read-only mounts. Update the
  TLA+ model first, then the Go implementation.
- Adding new credential deny paths requires updating both `credentialDenySubs`
  and `CredentialLeaves`/the abstract path model before committing.
- Reordering backend validation, approval, sandbox creation, policy
  application, or launch requires re-running TLC; `PolicyBeforeLaunch` and
  `BackendValidationBeforeLaunch` are load-bearing.
- Introducing any explicit Tier 3 env passthrough (for example launch-time
  API-key delivery) requires updating this spec first. The current proof only
  covers the zero-extra-env launch path in `hazmat/sandbox.go`.

---

### 6 — Tier 2 vs Tier 3 Effective Policy Equivalence

| Field | Value |
|-------|-------|
| Spec | `tla/06_tier2_tier3_effective_policy_equivalence.md` |
| TLA+ files | `tla/MC_TierPolicyEquivalence.tla`, `tla/MC_TierPolicyEquivalence.cfg` |
| Governed code | `hazmat/session.go` — `resolveSessionConfig()`, `generateSBPL()`, `agentEnvPairs()` |
| Governed code | `hazmat/sandbox.go` — `prepareSandboxLaunch()`, `buildSandboxLaunchSpec()` |
| Governed code | `hazmat/integration_manifest.go` — `isCredentialDenyPath()` |
| Key invariants | `CredentialInputsRejectedInBoth`, `IntegrationEnvBreaksExactIdentity`, `ResumeBreaksExactIdentity`, `AncestorRewriteBreaksExactIdentity`, `CanonicalCoreContainmentEquivalent` |
| Status | **Proved** — exact Tier 2/Tier 3 identity is false by design, but the canonical core containment contract is equivalent across both backends |

**What was found:**

1. Exact backend identity is not a valid claim for the current product. The
   model proves three intentional divergence classes: integration env
   passthrough, host-side resume history behavior, and Tier 3 ancestor mount
   rewriting.

2. A real Tier 2 vs Tier 3 mismatch existed in implementation: Tier 3 already
   rejected project/read/write roots that overlapped credential deny zones, but
   native Tier 2 session resolution did not reject the same inputs up front.

**Fix applied:**

1. Added credential-deny validation for explicit project, read-only, and
   read-write roots during native session resolution in
   `hazmat/session.go:resolveSessionConfig()`. Tier 2 now rejects the same
   unsafe inputs Tier 3 rejects.

The principle: **Hazmat may share one path-based containment contract across
tiers, but it must not claim stronger backend identity than the implementation
actually provides.** TLC passes across all 163,840 reachable states (327,680
generated, depth 1, 13s).

**Change rules:**
- Changes to project/read/write root normalization or credential-deny handling
  in either tier require re-running both this spec and the Tier 3 launch
  containment spec.
- Adding Tier 3 integration-env support requires updating this spec first; the
  current proof treats that difference as an intentional exact-identity break.
- Changing resume/continue transcript handling across tiers requires updating
  this spec first; host resume parity is currently outside the equivalent core
  containment contract.
- If Tier 3 ancestor-overlap rewriting changes, update the abstract
  `NeedsAncestorRewrite` model and re-prove the exact-identity break plus the
  canonical comparable subset.

---

### 7 — Session-Time Permission Repairs

| Field | Value |
|-------|-------|
| Spec | `tla/07_session_permission_repairs.md` |
| TLA+ files | `tla/MC_SessionPermissionRepairs.tla`, `tla/MC_SessionPermissionRepairs.cfg` |
| Governed code | `hazmat/session_mutation.go` — native mutation planning/execution |
| Governed code | `hazmat/workspace_acl.go` — project/traverse ACL repair detection and repair |
| Governed code | `hazmat/git_preflight.go` — `.git` metadata repair checks |
| Governed code | `hazmat/integration_resolver.go` — Homebrew tool permission repair planning |
| Governed code | `hazmat/session.go`, `hazmat/explain.go` — preview vs launch mutation behavior |
| Key invariants | `PlannedRepairsMatchSnapshot`, `PreviewIsReadOnly`, `DockerSkipsNativeACLRepairs`, `HomebrewRepairRequiresEligibleCellar`, `LaunchClearsFatalRepairNeeds`, `RollbackPreservesSessionRepairs` |
| Status | **Proved** — explicit host permission repair classes, preview semantics, and non-reverting rollback behavior are now modeled |

**What this verifies:**

1. **Preview is pure:** `hazmat explain` shows the same repair classes a real
   session may need, but it does not mutate host permissions.

2. **Mode-specific planning is explicit:** native sessions may plan project,
   traverse, and `.git` ACL repairs; Docker Sandbox sessions do not silently
   inherit those native-only repair classes.

3. **Homebrew repair stays narrow:** the Homebrew toolchain repair path is only
   planned when an eligible Homebrew Cellar path is both in scope and still
   blocked.

4. **Rollback preserves these repairs:** core rollback does not claim to undo
   any already-applied session repair. That persistence is now part of the
   proved contract instead of documentation-only behavior.

TLC passes across all 6,634 reachable states (15,663 generated, depth 7, ~2s).

**Change rules:**
- Adding a new host permission repair class requires updating this spec first:
  define when it is planned, whether preview may show it, whether launch must
  block on it, and whether rollback preserves it.
- Changing native vs Docker mutation planning requires re-running this spec
  before implementation. The current proof intentionally keeps
  project/traverse/git ACL repair native-only.
- Changing whether rollback reverts any of these repairs requires updating this
  spec first. The current proof bar is explicit non-reversion.
- Changing `hazmat explain` so it mutates or omits planned repairs requires
  updating this model first.

---

### 8 — Harness Lifecycle

| Field | Value |
|-------|-------|
| Spec | `tla/08_harness_lifecycle.md` |
| TLA+ files | `tla/MC_HarnessLifecycle.tla`, `tla/MC_HarnessLifecycle.cfg` |
| Governed code | `hazmat/harness.go` — harness state recording |
| Governed code | `hazmat/state.go` — `saveState()`, `updateHarnessState()`, `writeState()` |
| Governed code | `hazmat/bootstrap.go`, `hazmat/bootstrap_codex.go`, `hazmat/bootstrap_opencode.go` — bootstrap flows |
| Governed code | `hazmat/config_import.go`, `hazmat/config_import_opencode.go` — curated import flows |
| Governed code | `hazmat/migrate.go` — rollback cleanup of `~/.hazmat/state.json` |
| Key invariants | `RecordedHarnessVersionsMatchSpec`, `ImportedMetadataCarriesVersion`, `StateFilePresentWhenMetadataExists`, `DryRunLeavesStateUntouched`, `SaveCoreStatePreservesHarnessMetadata`, `RollbackClearsMetadata`, `RollbackWithoutDeleteUserPreservesArtifacts`, `RollbackDeleteUserRemovesArtifacts` |
| Status | **Proved** — harness state recording, dry-run behavior, and rollback cleanup semantics are now modeled separately from core migration |

**What this verifies:**

1. **Known-version recording only:** successful harness recording writes only
   the declared built-in harness state version.

2. **Dry runs are read-only:** dry-run bootstrap/import paths do not mutate
   either `~/.hazmat/state.json` or the agent-home artifact state.

3. **Core state saves preserve harness metadata:** `saveState()` for init and
   migration does not erase or rewrite existing harness metadata.

4. **Rollback cleanup is split correctly:** rollback removes the host-owned
   harness metadata record, but agent-home harness artifacts survive unless the
   user chooses destructive rollback with `--delete-user`.

TLC passes across all 1,564 reachable states (16,064 generated, depth 9, ~2s).

**Change rules:**
- Adding a new built-in harness requires updating this spec first: define
  whether it supports curated import, how it records state, and what rollback
  removes.
- Changing harness dry-run behavior requires updating this spec first. The
  current proof requires dry runs to be read-only.
- Changing how `saveState()` preserves or rewrites harness metadata requires
  updating this spec first.
- Changing rollback semantics for `~/.hazmat/state.json` or agent-home harness
  files requires updating this spec first.

---

### 10 — Git-SSH Routing (Multi-Key)

| Field | Value |
|-------|-------|
| Spec | `tla/10_git_ssh_routing.md` |
| TLA+ files | `tla/MC_GitSSHRouting.tla`, `tla/MC_GitSSHRouting.cfg` |
| Governed code | `hazmat/config.go` — `ValidateProjectSSHConfig()`, `ProjectSSHConfig.NormalizedKeys()`, `runConfigSSHAdd()`, `runConfigSSHRemove()` |
| Governed code | `hazmat/git_ssh.go` — `resolveProjectSSHKeys()`, `prepareSSHIdentityRuntime()`, `buildGitSSHWrapperScript()`, `selectSessionGitSSHKey()` |
| Key invariants | `DeterministicRouting`, `OverlapRejectedAtConfigTime`, `HostsOutsideAllowlistRejected`, `InlineKeysHaveDeclaredHosts`, `SocketsDistinctForPresent`, `NoDanglingProfileRefs`, `NoProfileInlineConflict`, `PresentKeysHaveIdentity`, `IdentitySourceClassified`, `NoCrossKey` |
| Status | **Proved and Implemented** — multi-key routing (sandboxing-vmg1), reusable profile resolution (sandboxing-nm5o), any-host fallback retirement (sandboxing-qq9b), and typed Git SSH identity-source classification are implemented and covered by the routing model |

**What this verifies:**

1. **Deterministic routing:** for any destination host, a ready config
   admits at most one configured key. The wrapper's `case` dispatch in
   `buildGitSSHWrapperScript` matches this one-to-one structure.

2. **Overlap is a config-set error:** a config where two keys match the
   same host is refused at config save time, not at session time.
   `ValidateProjectSSHConfig` enforces the spec's
   `OverlapRejectedAtConfigTime` invariant.

3. **Inline keys must declare hosts (legacy fallback retired):** every
   present inline key declares at least one host. The any-host fallback
   that previously admitted a single inline key with empty declared
   hosts has been removed; pre-migration configs are rejected at load
   with a copy-paste YAML snippet. Profile-referencing keys are
   unaffected — they inherit the profile's `default_hosts` when their
   own declared host list is empty (and may resolve to an empty
   effective set, routing nothing, rather than expanding to all hosts).
   Proved by `InlineKeysHaveDeclaredHosts`.

4. **Per-key identity-agent sockets are distinct:** session-time socket
   allocation derives paths from validated key names and asserts pairwise
   distinctness before entering the wrapper. Two project keys that
   reference the same profile still allocate separate sockets.

5. **Profile references cannot dangle:** every profile name used by a
   project key must exist in `ssh_profiles:`. Dangling references are
   rejected at config load, not session launch. Proved by
   `NoDanglingProfileRefs`.

6. **Profile and inline identity are mutually exclusive:** a key that
   declares both a profile reference and inline `private_key:` is a
   schema-level conflict. The spec models `identitySource` explicitly so
   TLC can reach the conflict state and witness the rejection. Proved by
   `NoProfileInlineConflict`.

7. **No orphan keys:** every present key has an identity source (a profile
   reference or inline material). A present key with neither is rejected.
   Proved by `PresentKeysHaveIdentity`.

8. **Identity sources are classified:** every ready key resolves to exactly
   one identity class: profile-backed, external host-file reference, or
   provisioned secret-store-backed root. Proved by
   `IdentitySourceClassified`.

TLC passes across 1,990,656 distinct states (2,985,984 generated, depth 2, ~16s).

**Scope boundary:**

The spec models the routing relation after glob expansion and the
socket-to-key binding. Glob syntax, shell quoting, signal handling,
ssh-agent liveness, and concrete `IdentityAgent` emission in the wrapper
script remain governed by unit tests rather than TLC.

**Change rules:**
- Changes to overlap detection, legacy normalization, or the keys schema
  must update `MC_GitSSHRouting.tla` first and re-run TLC before the Go
  implementation changes.
- Adding a precedence / override semantics on overlapping host patterns
  requires a spec change; the current proof assumes overlap is rejected,
  not resolved.
- Replacing the wrapper-based routing with a host-side broker (see
  `sandboxing-n1xy`) reuses this spec as-is: the routing relation is
  transport-agnostic. New socket-collision checks or identity-binding
  mechanisms must still preserve `SocketsDistinctForPresent` and
  `NoCrossKey`.

---

### 11 — Git Hook Approval

| Field | Value |
|-------|-------|
| Spec | `tla/11_git_hook_approval.md` |
| TLA+ files | `tla/MC_GitHookApproval.tla`, `tla/MC_GitHookApproval.cfg` |
| Governed code | `hazmat/hook_manifest.go`, `hazmat/hook_approval.go`, `hazmat/hook_runtime.go`, `hazmat/hook_cli.go` |
| Governed code | `hazmat/rollback.go` — repo-local hook cleanup sweep |
| Key invariants | `ApprovedContentOnly`, `HooksPathPinned`, `WrapperRefusesReroute`, `ManagedDispatcherRefusesDrift`, `FallbackDispatcherOnlyRefuses`, `RollbackClearsHookInstall`, `NoImplicitWidening` |
| Status | **Proved and implemented** — repo-local hook approval, immutable snapshot execution, wrapper / dispatcher refusal, and rollback cleanup now ship behind the current hook command surface |

**What this verifies:**

1. **Approved execution uses immutable snapshot bytes only:** a host-side hook
   run that succeeds must execute content from the approved snapshot record,
   not the live repo copy.

2. **`core.hooksPath` reroute is a refusal path:** the primary wrapper boundary
   must refuse if the effective `core.hooksPath` drifts away from the
   Hazmat-managed path.

3. **Managed dispatcher drift is fatal, not advisory:** repo drift, approval
   drift, snapshot drift, or managed-layout drift all resolve to refusal rather
   than best-effort execution.

4. **Fallback `.git/hooks` is detection-only:** reaching the default hook path
   is modeled as a refusal path, not an alternate approved execution channel.

5. **Hook approval does not widen session policy:** the proof boundary for hook
   activation does not grant future filesystem or network capability beyond the
   existing session contract.

TLC passes across 2,179,200 distinct states (127,229,656 generated, depth 9,
~3m).

**Scope boundary:**

The spec models Hazmat-managed host-side entrypoints only: the Git wrapper
Hazmat installs, the managed dispatcher path, and the fallback `.git/hooks`
drift detector. It does **not** claim correctness for arbitrary direct
invocation of a foreign `git` binary outside that managed path.

**Change rules:**
- Changes to repo-local hook approval semantics, approved snapshot execution,
  `core.hooksPath` pinning, or fallback-dispatcher refusal behavior must update
  `MC_GitHookApproval.tla` first and re-run TLC before the Go implementation
  changes.
- Expanding v1 scope beyond repo-local `pre-commit`, `pre-push`, and
  `commit-msg` requires updating this model first.
- Replacing the wrapper + dual-dispatcher design with a different activation
  primitive requires a spec update first. The current proof assumes wrapper
  validation is the primary defense and fallback `.git/hooks` dispatchers are
  refusal-only.
- Human-facing prompt text, manifest diff presentation, exact shell quoting,
  and other UX details remain governed by tests and docs rather than TLC.

---

### 12 — Secret Store Crash Recovery

| Field | Value |
|-------|-------|
| Spec | `tla/12_secret_store_recovery.md` |
| TLA+ files | `tla/MC_SecretStoreRecovery.tla`, `tla/MC_SecretStoreRecovery.cfg` |
| Governed code | `hazmat/harness_auth_runtime.go` — startup recovery, materialization, harvest, conflict archive |
| Governed code | `hazmat/secret_store.go` — host/agent secret file read/write/remove helpers |
| Key invariants | `LatestValueNeverSilentlyLost`, `CleanRecoveredStateHasNoAgentResidue`, `CleanRecoveredStateKeepsLatestHostOwned`, `NoCrossHarnessAgentExposure`, `LaunchOnlyAfterRecovery`, `IdleClearsSessionBaseline` |
| Status | **Proved and implemented** — file-backed harness auth survives crash/restart interleavings without silently losing the latest known value or leaving recovered idle state dependent on `/Users/agent` residue |

**What this verifies:**

1. **Startup recovery precedes launch:** materialization and session execution
   cannot start until leftover agent-side auth residue has been reconciled.

2. **Crash residue is promoted, not ignored:** if a prior session refreshed auth
   and Hazmat died before cleanup, the next launch promotes that agent-side
   value into the host store.

3. **Divergence is archived before overwrite:** if both host and agent copies
   exist and differ, the previous host copy is preserved in a host-owned
   conflict archive before the agent residue becomes primary.

4. **Recovered idle state is host-owned:** after recovery completes, the latest
   known auth value is in the host primary store or conflict archive, not only
   under `/Users/agent`.

5. **No cross-harness materialization:** while one harness session is active,
   the model never exposes another harness's auth artifact under the agent
   home.

TLC passes across 9,238 distinct states (34,723 generated, depth 28, <1s).

**Scope boundary:**

The proof is content-level and crash/restart focused. It does not model exact
Claude JSON merge semantics, Keychain-backed auth, concrete filesystem
permission syscalls, or concurrent writes to the same host secret while a
session is running. Proving the concurrent-host-write case requires revision or
epoch metadata; content equality alone cannot distinguish an unchanged
baseline from a same-content rewrite.

**Change rules:**
- Changes to `migrateHarnessAuthArtifact()`, `materializeHarnessAuthArtifact()`,
  or `harvestHarnessAuthArtifact()` must update `MC_SecretStoreRecovery.tla`
  first and re-run TLC before the Go implementation changes.
- Any new harness file-backed auth artifact must be representable by the
  host/agent/conflict archive state machine before it participates in session
  materialization.
- Any path that overwrites host-owned auth with agent-side auth must either
  prove the host value is the expected session baseline or preserve the
  divergent host value first.
- Stronger guarantees for concurrent host-store writes require explicit
  revision metadata in the model and implementation.

---

### 13 — Credential Capability Lifecycle

| Field | Value |
|-------|-------|
| Spec | `tla/13_credential_capability_lifecycle.md` |
| TLA+ files | `tla/MC_CredentialCapabilityLifecycle.tla`, `tla/MC_CredentialCapabilityLifecycle.cfg` |
| Governed code | `hazmat/credential_registry.go` — credential IDs, backends, delivery modes, support status, harness scope |
| Governed code | `hazmat/harness_auth_runtime.go` — file-backed materialization, harvest, crash recovery precondition |
| Governed future code | Git HTTPS broker, cloud credentials, SSH identity refs, and integration/env credential grants |
| Key invariants | `NonHostBackendsHaveNoHostStore`, `DeliveryMatchesRegistry`, `AdapterRequiredNeverExposed`, `NoCrossHarnessExposure`, `NoSessionExposureOutsideActivePhase`, `LaunchOnlyAfterRecovery`, `CleanRecoveredStateHasNoAgentResidue`, `LatestValueNeverSilentlyLost`, `CleanRecoveredStateKeepsLatestHostOwned`, `IdleClearsSessionState` |
| Status | **Proved** — registry entries cannot be delivered through the wrong mechanism, adapter-required credentials remain unexposed, and crash/restart clears session-only grants while preserving file-backed recovery invariants |

**What this verifies:**

1. **Delivery mode is authoritative:** a file credential may create agent-side
   materialization, an env credential may only appear in env grants, a brokered
   credential may only appear in broker grants, and external references may only
   appear as external grants.

2. **Adapter-required backends are inert:** a credential like Gemini Keychain
   OAuth cannot become active, delivered, materialized, env-granted,
   broker-granted, or externally granted until an adapter is modeled.

3. **Harness scope is enforced:** active-session exposure must either belong to
   the active harness or be explicitly global, which is the shape expected for
   future Git HTTPS broker credentials.

4. **Crash clears session-only grants:** env, broker, and external grants do not
   survive a crash/restart transition. File residue may survive, but launch is
   blocked until recovery reconciles it.

5. **Host ownership remains the recovered state:** after recovery, the latest
   known managed value is in host primary storage or a host-owned conflict
   archive, not only in `/Users/agent`.

TLC passes across 63,681 distinct states (225,105 generated, depth 32, ~4s).

**Scope boundary:**

This is a registry-level proof. It does not model exact concrete file paths,
filesystem permissions, JSON merge semantics, Keychain authorization prompts,
git credential-helper protocol bytes, cloud provider behavior, SSH agent socket
behavior, or integration manifest parsing. Those remain governed by narrower
future specs, tests, and docs.

**Change rules:**
- Adding a credential delivery mode, support status, or secret-exposing backend
  requires updating `MC_CredentialCapabilityLifecycle.tla` first.
- Adapter-required credentials must remain undeliverable until their adapter is
  represented in this model and TLC proves the intended invariants.
- Git HTTPS, cloud backup, Git SSH, and integration/env credential work must
  preserve the model's delivery-mode and session-scope invariants.
- Any future path that creates durable `/Users/agent` credential material must
  be modeled as file delivery and must preserve recovery-before-launch.

---

### 9 — Launch FD Isolation

| Field | Value |
|-------|-------|
| Spec | `tla/09_launch_fd_isolation.md` |
| TLA+ files | `tla/MC_LaunchFDIsolation.tla`, `tla/MC_LaunchFDIsolation.cfg` |
| Governed code | `hazmat/agent_launch.go` — native sudo + helper launch construction |
| Governed code | `hazmat/session.go` — `runAgentSeatbeltScriptWithUI()`, policy-file generation |
| Governed code | `hazmat/cmd/hazmat-launch/main.go` — inherited-fd cleanup, policy read, `sandbox_init()`, final `exec` |
| Key invariants | `HelperFDTableAllowlistedAtSandbox`, `NoInheritedShellFDsAtSandbox`, `CredentialFDsGoneBeforeSandbox`, `AgentFDTableAllowlisted`, `StdioSurvivesToAgent` |
| Status | **Proved and Implemented** — the native helper now sanitizes inherited fds before sandboxing and keeps the final agent exec to stdio only |

**What this verifies:**

1. **Helper-side cleanup is mandatory:** the checked design does not rely on
   Go's current `exec` behavior or `sudo`'s current fd cleanup to keep
   inherited descriptors out of the helper.

2. **Sandboxing starts from a curated fd table:** once `sandbox_init()` is
   called, the helper holds only stdio plus its helper-opened policy file.

3. **Credential-bearing inherited fds are gone before Seatbelt matters:** path
   denies are only meaningful if no already-open credential handle survived
   into the helper.

4. **The final agent exec is stdio-only:** helper-opened policy state is
   `CLOEXEC`, so it cannot leak into the actual agent process.

TLC passes across all 112 reachable states (128 generated, depth 7, <1s).

During design, a temporary negative config with
`HelperClosesInheritedFDs = FALSE` immediately produced a counterexample where
an inherited non-stdio fd survived into `sandbox_init()`. That is why helper-
side cleanup is now a proved design rule instead of an implementation detail.

**Change rules:**
- Any change to the native `sudo -> hazmat-launch -> sandbox_init() -> exec`
  chain must preserve both boundaries: no inherited non-stdio fd at
  `sandbox_init()`, and stdio-only final agent exec.
- Replacing helper-side fd cleanup with reliance on upstream `sudo` or Go
  behavior requires updating this spec first. The current proof assumes Hazmat
  owns that boundary itself.
- Any helper-opened fd that may remain live across the final `exec` must be
  modeled here first. The current proof assumes helper-opened policy state is
  explicitly `CLOEXEC`.

---

## Quick Reference: Spec → Code Mapping

| Spec | Files governed |
|------|---------------|
| `01_setup_rollback_state_machine` | `hazmat/init.go:runInit()`, all `setupX()`; `hazmat/rollback.go:runRollback()`, all `rollbackX()` |
| `02_seatbelt_policy_structure` | `hazmat/session.go:generateSBPL()`, `isWithinDir()` |
| `03_backup_restore_safety` | `hazmat/kopia_wrapper.go:runCloudRestore()`, `snapshotProject()`; `hazmat/restore.go:runProjectRestore()`; `hazmat/session.go:preSessionSnapshot()` |
| `04_version_migration` | `hazmat/init.go` migration dispatch; `hazmat/migrate.go` migration functions |
| `05_tier3_launch_containment` | `hazmat/sandbox.go:buildSandboxLaunchSpec()`, `prepareSandboxLaunch()`, `loadHealthySandboxLaunchBackend()`, `dockerSandboxesBackend.PrepareLaunch()`; `hazmat/integration_manifest.go:isCredentialDenyPath()`; `hazmat/session.go:isWithinDir()` |
| `06_tier2_tier3_effective_policy_equivalence` | `hazmat/session.go:resolveSessionConfig()`, `generateSBPL()`, `agentEnvPairs()`; `hazmat/sandbox.go:prepareSandboxLaunch()`, `buildSandboxLaunchSpec()`; `hazmat/integration_manifest.go:isCredentialDenyPath()` |
| `07_session_permission_repairs` | `hazmat/session_mutation.go`; `hazmat/workspace_acl.go`; `hazmat/git_preflight.go`; `hazmat/integration_resolver.go`; `hazmat/session.go`; `hazmat/explain.go` |
| `08_harness_lifecycle` | `hazmat/harness.go`; `hazmat/state.go`; `hazmat/bootstrap*.go`; `hazmat/config_import*.go`; `hazmat/migrate.go` |
| `09_launch_fd_isolation` | `hazmat/agent_launch.go`; `hazmat/session.go:runAgentSeatbeltScriptWithUI()`; `hazmat/cmd/hazmat-launch/main.go` |
| `10_git_ssh_routing` | `hazmat/config.go:ValidateProjectSSHConfig()`, `NormalizedKeys()`, `runConfigSSHAdd()`, `runConfigSSHRemove()`; `hazmat/git_ssh.go:resolveProjectSSHKeys()`, `prepareSSHIdentityRuntime()`, `buildGitSSHWrapperScript()`, `selectSessionGitSSHKey()` |
| `11_git_hook_approval` | Repo-local hook approval command surface, snapshot execution helpers, and rollback cleanup under `hazmat/` |
| `12_secret_store_recovery` | `hazmat/harness_auth_runtime.go`; `hazmat/secret_store.go` |
| `13_credential_capability_lifecycle` | `hazmat/credential_registry.go`; `hazmat/harness_auth_runtime.go`; future credential backend implementations |

---

## Not Yet Formally Modeled

- Exact curated import file contents, conflict-resolution behavior, and merged JSON/file payload semantics
- Concurrent writes to the same host secret while a harness session is running; the current secret-store proof is crash/restart recovery, not multi-writer synchronization
- Integration activation, project pinning, and integration-specific snapshot ignore rules
- Exact `hooks.yaml` parsing behavior, human-readable diff/summary generation, and foreign raw-`git` entrypoints outside Hazmat-managed wrapper paths
- Exact ACL/chmod filesystem walk semantics for session-time permission repairs
- Reworked setup-completion liveness under the current bounded setup/rollback retry model
- Docker Sandbox or microVM runtime internals after the host-side Tier 3 launch boundary
- Concrete Keychain APIs, git credential-helper protocol bytes, SSH agent socket
  behavior, cloud provider APIs, and integration manifest parsing

These areas remain governed by tests and documentation rather than the current
TLC proofs.

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
