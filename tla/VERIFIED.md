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

Important Apple Container boundary: the current suite now includes a design
model for the planned `apple-container` backend's host-side launch boundary:
mount-planner credential exclusions, forbidden-feature rejection, admission
and network fail-closed gating, and session-scoped credential artifact
cleanup accounting. It does **not** model Apple Container VM internals,
VirtioFS ownership mapping, `container machine` persistent mode, or network
allowlist profiles. An experimental runtime follows the modeled ordering
behind an explicit gate (exec-only, invoking-user identity, no host account
isolation claim); the main session pipeline still treats the backend as
plan-only.

Important Linux-native boundary: the current suite now includes a design model
for the future experimental Linux native helper. It proves launch ordering
across spec validation, fd cleanup, namespace/mount/network setup, privilege
drop, `no_new_privs`, Landlock/seccomp decisions, metadata emission, and exec.
It does **not** model concrete Linux syscalls, mount propagation, seccomp
filter contents, Landlock ruleset shape, or runtime behavior after exec.

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

Important service-harness/proxy boundary: the current suite now includes a
design model for future OpenHands-style service harnesses and service-shaped
proxy frontends such as the local API proxy and future HTTP MCP proxy:
prior-residue recovery, fail-closed unsupported features,
metadata-before-side-effects, metadata-before-bind, readiness before attach,
local-only attach authority, localhost token policy, typed credential
materialization, and terminal cleanup accounting. It does **not** model
OpenHands internals, HTTP request bodies, browser automation, Docker or VM
internals, provider APIs, MCP payload semantics, or live service protocol
behavior. Stdio MCP proxying remains a foreground child-process shape governed
by launch/fd-isolation rather than this service lifecycle.

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

### Proof obligation ownership

`proof_ownership.tsv` maps every invariant/property listed in promoted
`MC_*.cfg` files to the owning verified subsystem section below and to the
companion design note. Run `bash proof_ownership_check.sh` after adding,
removing, or renaming a checked obligation. The check prevents phantom proof
claims by failing when the `.cfg` files, promoted suite, `VERIFIED.md`, and
per-spec design notes drift apart.

`promoted_specs.tsv` is the canonical roster for the promoted suite. It records
the expected `MC_*` specs and liveness setting for each spec, and the fast proof
hygiene checks compare it against `check_suite.sh`, `.tla` files, and `.cfg`
files. Removing a promoted spec or changing a liveness setting must be an
explicit roster change, not just a coordinated deletion from the live suite.

### TLC trace artifacts

Raw TLC `_TTrace_` modules, `.bin` files, and `tla/states/` files are local
generated debugging output. They are ignored by Git, may be deleted, and are not
proof sources. `TRACE_ARTIFACTS.md` records the retention policy, and
`trace_artifact_check.sh` fails if raw generated trace/state artifacts become
tracked.

### CI proof tiers

CI separates fast proof hygiene from deep model checking, but the split must not
weaken promoted-spec coverage. The fast `TLA+ proof hygiene` job checks the
ownership ledger, trace-artifact policy, canonical roster, and
`proof_audit.sh --fail-on-drift` inventory/config drift. The deep `TLA+ model
checking` job remains mandatory for every promoted spec listed in
`check_suite.sh`, including liveness checks configured there. The 2026-06-04
baseline run `26942387327` measured the deep TLA+ job at about 24 minutes, with
the verified TLC suite step at about 23m52s; the split exists for earlier drift
feedback, not for skipping promoted proofs.

---

## Verified Subsystems

### 1 — Setup/Rollback State Machine

| Field | Value |
|-------|-------|
| Spec | `tla/01_setup_rollback_state_machine.md` |
| TLA+ files | `tla/MC_SetupRollback.tla`, `tla/MC_SetupRollback.cfg` |
| Governed code | `hazmat/init.go` — `runInit()` and remaining root setup resource callbacks not yet split from `package main` |
| Governed code | `hazmat/internal/setup/*.go` — setup/rollback resource labels, ordering, orchestration, setup verification order, home traversal ACL resource logic, local snapshot repository resource flow, sudoers entry construction/install runtime, managed shell blocks, hardening runtime, zsh completion install/rollback runtime, Git safe.directory setup/rollback flow, and tooling wrapper setup/rollback runtime |
| Governed code | `hazmat/internal/setup/darwin/*.go` — Darwin account, firewall/DNS/LaunchDaemon/launch-helper, and sudoers-removal setup/rollback runtime effects |
| Governed code | `hazmat/native_account*.go`, `hazmat/native_service*.go` — platform backend adapters and unsupported-platform fail-closed stubs |
| Governed code | `hazmat/sudoers.go` — optional agent-maintenance sudoers choice, config command, and compatibility wrappers for sudoers runtime |
| Governed code | `hazmat/rollback.go` — `runRollback()` and remaining root rollback resource callbacks not yet split from `package main` |
| Key invariants | `AgentContained`, `LinuxPrivilegeRequiresContainment`, `NoOrphanedArtifacts`, `SudoersRequiresHelper`, `PrivilegeRequiresAgentUser`, `AgentDepsRequireUser`, `AgentWritableSetupParentsOwned`, `LinuxAgentUserSetupGraph`, `LinuxAgentUserRollbackRevokesPrivilegeFirst`, `LinuxAgentUserDestructiveRollbackBoundary` |
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
`AgentContained` and `CanAlwaysReachClean` now pass across all 35,005 reachable
states (65,662 generated, ~3s with liveness enabled).

**2026-06-03 package-split confirmation:** Phase K moved setup/rollback
resource ordering, setup verification order, home traversal ACL resource
logic, local snapshot repository resource flow, sudoers entry runtime, managed
shell-block rendering, host credential hardening, zsh completion
install/rollback runtime, Git safe.directory setup/rollback flow,
seatbelt/tooling wrapper setup, and wrapper/umask rollback runtime into
`internal/setup`, and moved Darwin account plus service runtime effects into
`internal/setup/darwin` behind root adapters. No modeled resource order or
rollback preservation semantics changed. `MC_SetupRollback` was re-run with TLC
and reported "No error has been found" across the same state space: 65,662
generated states, 35,005 distinct states, depth 56.

**2026-06-12 Amp/Devin/external-agent/Goose credential-state expansion:** Host
credential hardening now includes Amp config/plugin roots, Devin
config/auth-adjacent state, external agent roots for Kilo, Kimi, Kiro, Vibe,
Trae, Pi, Crush, OpenClaw, Qoder, GitHub Copilot CLI, CodeWhale/DeepSeek TUI,
Grok Build, OpenHands, and Goose config/session/log roots alongside the existing
credential deny floor.
This does not add a setup step; it expands the concrete path set covered by the
existing persistent `hostCredentialModes` resource. `MC_SetupRollback` was
re-run with TLC and reported "No error has been found" across the same state
space: 65,662 generated states, 35,005 distinct states, depth 56.

The bounded-retry model does **not** currently prove `SetupEventuallyCompletes`.
If setup and rollback attempts are both exhausted after repeated failures, TLC
can stutter in a partially configured idle state. Hazmat's current checked
liveness bar for this model is recoverable clean exit, not guaranteed eventual
successful completion after arbitrary bounded failures.

**2026-06-14 Linux setup/rollback interpretation:** `MC_SetupRollback` now runs
with `Platform = "linux"` and checks `LinuxPrivilegeRequiresContainment`: Linux
sudoers privilege requires the firewall policy, resolver policy, and
service-manager persistence resources to all be active. TLC reported "No error
has been found" with liveness enabled across 65,662 generated states, 35,005
distinct states, depth 56. This proves the resource-ordering boundary for future
Linux setup/rollback design only; concrete Linux systemd/nftables/resolver
mechanics and disposable-host lifecycle tests are still required before Linux
install or release artifacts can be enabled.

**2026-06-28 Linux agent-user setup graph:** `MC_SetupRollback` now names the
Linux multi-user resource projections from the two-lane design and checks
identity, shared-group, workspace, helper, sudoers, cgroup/service-manager,
distro-profile, and tool-home ordering. TLC reported "No error has been found"
with liveness enabled across 65,662 generated states, 35,005 distinct states,
depth 56. A shared group may survive without the dedicated user only as
unprivileged rollback residue; workspace access, tool-home state, and sudoers
privilege must be absent in that state.

**Change rules:**
- Any change to setup step ordering must be modeled and proved against
  `AgentContained` before committing.
- Adding Linux setup/rollback mechanics must preserve
  `LinuxPrivilegeRequiresContainment`, not just the generic firewall-only
  `AgentContained` invariant.
- Adding Linux agent-user resources must preserve
  `LinuxAgentUserSetupGraph`, `LinuxAgentUserRollbackRevokesPrivilegeFirst`,
  and `LinuxAgentUserDestructiveRollbackBoundary`.
- Adding a new setup step requires adding the corresponding resource variable
  and updating `SetupStepSucceed` / `RollbackCore` / `RollbackDestructive`.
- Adding a new persistent mutation inside an existing setup step still requires
  updating the model first when the step is in governed `hazmat/init.go`.
  If rollback intentionally preserves the mutation, document that boundary in
  the setup/rollback spec instead of treating it as an unmodeled side effect.
- Adding credential paths to the deny floor must keep
  `HostCredentialHardeningSpecs` in sync unless a separate model/design note
  justifies a deny-only exception.
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
| Governed code | `hazmat/native_session_policy.go` — `buildNativeSessionPolicy()` native session contract construction |
| Governed code | `hazmat/session_policy_sbpl.go` — `compileDarwinSBPLChecked()` compiler adapter from native policy to Darwin SBPL |
| Governed code | `hazmat/containment/darwin/sbpl.go` — Darwin SBPL compiler and rule ordering |
| Governed code | `hazmat/containment/agent_home_manifest.go` — explicit durable agent-home path manifest projected into section 4 grants |
| Key invariants | `CredentialReadDenied`, `CredentialWriteDenied`, `AttestationKeyReadDenied`, `AttestationKeyWriteDenied`, `AgentKeychainExceptionScoped`, `ReadDirsNoWrite`, `NoBroadAgentHomeAllow`, `AgentHomeSubsUsable`, `AgentHomeFilesUsable`, `SessionHomeUsableWhenActive`, `SessionHomeSeparateFromCredentials`, `PersistentAgentHomeNotImplicitlyExposedWhenSessionHome`, `UnlistedAgentHomeNotImplicitlyReadable`, `UnlistedAgentHomeNotImplicitlyWritable`, `UnlistedAgentHomeNotImplicitlyExecutable`, `ProjectDirWritable`, `ReadDirSubsumption`, `ResumeDirNotCredential`, `HostTempNotImplicitlyReadable`, `HostTempNotImplicitlyWritable`, `HostTempNotImplicitlyExecutable`, `SessionTempWritable`, `ClaudeRuntimeTempScoped`, `TempSocketsDenied`, `NetworkNoneDeniesOutbound`, `NetworkNoneDeniesDNS`, `NetworkDefaultAllowsOutbound` |
| Status | **Fixed and Re-Proved** — credential denies cover both ops; resume dir, project re-assertion, explicit agent-home subtrees/files, planned session-local HOME layout, native network-none mode, host-temp narrowing, and Claude's exact agent login keychain exception are modeled |

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

3. Added project write re-assertion (section 6) — when a read-only `-R` directory
   is a parent of the project directory, the project's write access is re-asserted
   as the last allow before credential denies.

4. Added native per-session `--network none` modeling. Default native sessions
   keep outbound network and DNS authority; network-none sessions emit neither
   grant, with no global firewall state.

5. Added host temp narrowing to the model. Broad `/private/tmp` and
   `/private/var/folders` access is no longer implicit; an agent-owned
   per-session temp root remains readable, writable, and executable for build
   artifacts; Codex App temp/control socket paths are denied after user grants.

6. Added a conditional Claude agent login keychain exception after credential
   denies. The model allows only the exact `/Users/agent/Library/Keychains/login.keychain-db`
   representative files and sidecars when native Claude OAuth requests Keychain
   compatibility; the broader Keychains directory remains denied.

7. Replaced the blanket section-4 `/Users/agent` allow with explicit agent-home
   state/tooling subtrees while keeping `HOME=/Users/agent`. `NoBroadAgentHomeAllow`
   proves section 4 does not emit a broad home rule, `AgentHomeSubsUsable`
   preserves modeled durable paths, and the `UnlistedAgentHomeNotImplicitly*`
   invariants keep unrelated home content denied unless the user explicitly
   selects it through the project/read surfaces.

Policy sections are now: 0=system libs, 1=read dirs, 2=project r+w, 3=resume dir,
4=explicit agent-home state/tooling, 5=session temp, 6=project write re-assert, 7=temp socket denies,
8=credential denies, 9=optional exact Claude agent login keychain exception.

**2026-06-02 modular refactor confirmation:** Phase-1 package refactors moved
native policy construction through validated `pathpolicy`, `containment`, and
`sessionplanner` packages without changing the modeled SBPL section order or
credential-deny boundary. `MC_SeatbeltPolicy` was re-run with TLC and reported
"No error has been found" across 13,824 generated states, 12,672 distinct
states, depth 11.

**2026-06-09 attestation-key containment addition (Beadpost broker):** The spec now
models the dr-owned Beadpost broker HMAC signing key as a host-authority deny target
(`HostAuthorityPaths` / `HostAuthorityTargets`), kept deliberately separate from
`CredPaths` / `CredentialTargets` so the Claude keychain exception can never apply to
it. Section 8 denies read+write for `CredPaths \cup HostAuthorityPaths`, and there is
NO section-9 re-allow for host-authority paths. `AttestationKeyReadDenied` and
`AttestationKeyWriteDenied` prove the key directory and file stay denied even when the
key directory is chosen as `ProjectDir` or a `ReadDir` — both are now in
`ProjectChoices` / `ReadChoices`, so the case is exercised, not vacuous. Re-run with
TLC: "No error has been found" across 32,256 generated states, 29,568 distinct, depth
11. This is Part 1 of 3 for the contained-agent submitter + dr-owned host broker
attestation boundary (see `docs/plans/2026-06-09-beadpost-attestation-spec-plan.md`).

**2026-06-12 explicit agent-home grant addition:** The spec now models explicit
agent-home compatibility subtrees plus an unlisted home file. TLC proves
`NoBroadAgentHomeAllow`, `AgentHomeSubsUsable`, and the
`UnlistedAgentHomeNotImplicitly*` invariants with "No error has been found"
across the same 32,256 generated states, 29,568 distinct states, depth 11.
The concrete durable-path inventory now lives in
`hazmat/containment/agent_home_manifest.go`, so the current SBPL allowlist and
the future session-local HOME assembly audit share one manifest seed. This is a
behavior-preserving projection change; `HOME` still remains `/Users/agent`.

**2026-06-12 session-local HOME model extension:** The spec now models two home
layout modes. `persistent` preserves the current explicit `/Users/agent`
durable subtree grants; `session` grants a disposable session-local HOME root
under `/private/tmp/hazmat-home/<session-id>/home` while persistent agent-home
subtrees lose implicit section-4 exposure. TLC proves
`SessionHomeUsableWhenActive`, `SessionHomeSeparateFromCredentials`, and
`PersistentAgentHomeNotImplicitlyExposedWhenSessionHome` with "No error has
been found" across 147,456 generated states, 135,168 distinct states, depth 11.

**2026-06-12 Amp/Devin/external-agent/Goose credential-state expansion:** The credential
model now includes representative `ampConfigDir`, `devinConfigDir`,
`agentCliStateDir`, and `gooseStateDir` leaves after the concrete deny floor
added `~/.config/amp`, `~/.config/devin`, `~/.config/kilo`, `~/.kimi-code`,
legacy `~/.kimi`, `~/.kiro`, `~/.vibe`, `~/.traecli`, `~/.pi/agent`,
`~/.config/crush`, `~/.local/share/crush`, `~/.openclaw`, `~/.qoder`,
`~/.copilot`, `~/.deepseek`, `~/.codewhale`, `~/.grok`, `~/.openhands`,
`~/.config/goose`, `~/.local/share/goose`, and `~/.local/state/goose`.
`agentCliStateDir` is the
representative finite-model leaf for external agent CLI/service state roots so
the proof does not grow one dimension per vendor.
`MC_SeatbeltPolicy` was re-run with TLC and reported "No error has been found"
across 7,667,712 generated states, 7,028,736 distinct states, depth 11.

**2026-06-24 Antigravity (agy) routed to the existing Security framework variant:**
The gemini→antigravity migration replaced a Node harness (which bundles its own
CA roots and never touches the macOS trust store) with `agy`, a flat native binary
that verifies TLS through Apple's Security framework. `harnessUsesMacOSSecurityFramework`
now returns true for `HarnessAntigravity`, so its sessions emit the same
`MacOSSecurityFramework` SBPL surface Codex already uses (configd, trustd.agent,
SecurityServer, AF_SYSTEM control socket, the trust read paths, and a read-only
re-allow of the agent's empty login keychain). Without it, trust evaluation failed
on Sequoia+ (errSecNoSuchKeychain −25291), so every HTTPS request — including the
Google OAuth token exchange — died with `tls: failed`. **No spec change was needed:**
`MC_SeatbeltPolicy` quantifies over abstract config booleans (`agentKeychainAccess`
and the modeled surface), not harness identity, and the `MacOSSecurityFramework=true,
agentKeychainAccess=false` policy shape is already in the verified state space via
Codex. `agentKeychainAccess` stays false for antigravity, so the keychain OAuth item
remains the adapter-required external boundary. The full suite (`check_suite.sh`)
re-ran green (exit 0); `MC_SeatbeltPolicy` alone reported "No error has been found"
across the same 7,667,712 generated / 7,028,736 distinct states, depth 11.

**2026-07-29 Security framework/keychain-read gate separation:**
The Security framework compatibility gate and the post-deny agent login
keychain exception are separate policy inputs. In particular, Antigravity may
retain the configd/trustd/Security framework TLS surface without receiving the
login-keychain file exception, because its durable OAuth state is stored in
that keychain. `MC_SeatbeltPolicy` models the keychain exception as an
independent boolean rather than deriving it from TLS compatibility.

**2026-06-26 Claude state file grant:** The spec now distinguishes section-4
agent-home directory grants (`AgentHomeSubs`) from literal file grants
(`AgentHomeFiles`) and models `/Users/agent/.claude.json` as durable Claude
harness state. `AgentHomeFilesUsable` proves that explicit literal state files
remain readable and writable in persistent-home mode without receiving implicit
execute access, while `NoBroadAgentHomeAllow` and the unlisted-home invariants
continue to block unrelated home content. `MC_SeatbeltPolicy` was re-run with
TLC and reported "No error has been found" across 7,667,712 generated states,
7,028,736 distinct states, depth 11.

Important proof dependency: `CredentialReadDenied` and `CredentialWriteDenied`
reason about SBPL path matching, not already-open inherited kernel handles. The
native launch path now proves that precondition separately in
`MC_LaunchFDIsolation`: `hazmat-launch` must reach `sandbox_init()` with no
inherited credential-bearing fd still alive.

**Change rules:**
- Do not reorder the sections emitted by `compileDarwinSBPLChecked()` /
  `hazmat/containment/darwin.Compile()` — credential denies MUST be
  the final broad credential boundary. Only the modeled exact Claude agent
  login keychain exception may appear after them.
- Adding new credential paths to the deny list requires adding them to
  `CredPaths` in the TLA+ model and re-running TLC.
- Changing the Claude keychain exception paths requires updating
  `AgentKeychainExceptionPaths` and re-running TLC.
- Adding new static allow paths (new `AgentHomeSubs` or `AgentHomeFiles`)
  requires checking whether they cover any credential paths — add to the model
  and re-verify.
- Adding new optional read+write sections (like ResumeDir) requires modeling the
  path and verifying it cannot overlap with `CredPaths`.
- Changing native temp grants or temp socket deny paths requires updating this
  model and re-proving host temp denial, session temp usability, and temp socket
  denial.
- Changing native network grants requires updating this model and re-proving
  both default outbound behavior and network-none denial.

---

### 3 — Backup/Restore Safety

| Field | Value |
|-------|-------|
| Spec | `tla/03_backup_restore_safety.md` |
| TLA+ files | `tla/MC_BackupSafety.tla`, `tla/MC_BackupSafety.cfg` |
| Governed code | `hazmat/kopia_wrapper.go` — `openLocalRepo()`, `snapshotProject()`, `runCloudBackup()`, `runCloudRestore()` |
| Governed code | `hazmat/restore.go` — `runProjectRestore()` |
| Governed code | `hazmat/internal/backupruntime/session.go` — `PreSessionSnapshot()` |
| Governed code | `hazmat/session.go` — `beginPreparedSession()` and `runSessionStartupPhases()` session command ordering |
| Governed code | `hazmat/exec_apple_container.go` — `runAppleContainerExecSession()` pre-launch snapshot trigger |
| Key invariants | `RestoreReversible`, `RepoBeforeSnapshot`, `CloudRequiresConfig`, `NoOverwriteWithoutAttempt`, `CloudTargetsProject`, `PreRestoreSnapshotMatchesTarget` |
| Key liveness | `SessionEventuallyLaunches`, `RestoreEventuallyCompletes` |
| Status | **Fixed and Re-Proved** — cloud restore takes a pre-restore snapshot before overwriting, cloud backup/restore target the selected project instead of a workspace root, and the pre-session snapshot trigger package split preserves backup ordering |

**What was found:**

1. **Cloud restore:** `runCloudRestore()` overwrote the entire workspace without
   taking a pre-restore snapshot. If the cloud snapshot was stale or wrong, the
   user's current workspace was permanently lost with no undo. The local restore
   path (`runProjectRestore()`) did this correctly.

2. **Cloud scope:** cloud backup/restore retained a hardcoded
   `cloudBackupDir=$HOME/workspace` target after the session model had moved to
   arbitrary project directories.

**Fix applied:**

1. Added `snapshotProject(projectDir, "pre-cloud-restore")` to
   `runCloudRestore(projectDir)` before the overwrite, matching the pattern in
   `runProjectRestore()`. Failure is non-fatal (warn and proceed).

2. Removed the hardcoded cloud workspace target. `hazmat backup --cloud` and
   `hazmat restore --cloud` now resolve the selected project directory (`-C` or
   the current working directory) and pass that target into the cloud runtime.

The principle: **every overwrite must be preceded by a snapshot attempt.**
`MC_BackupSafety` passed on 2026-06-09 with "No error has been found" across
1,809 generated states and 657 distinct states (<1s).

**Change rules:**
- Adding a new restore path (e.g., restore from external drive) must include a
  pre-restore snapshot step. Add the path to the TLA+ model and verify
  `RestoreReversible` still holds.
- Changing when `runSessionStartupPhases()` or `runAppleContainerExecSession()`
  calls `PreSessionSnapshot()` relative to sandbox entry must preserve the
  ordering: snapshot before sandbox boundary.
- Adding new snapshot triggers must ensure `openLocalRepo()` auto-init is
  called first (modeled by `RepoBeforeSnapshot`).

---

### 4 — Version Migration and Rollback from Any State

| Field | Value |
|-------|-------|
| Spec | `tla/04_version_migration.md` |
| TLA+ files | `tla/MC_Migration.tla`, `tla/MC_Migration.cfg` |
| Governed code | `hazmat/init.go` — migration dispatch, `runInit()` |
| Governed code | `hazmat/internal/setup/completion.go`, `hazmat/internal/setup/git_safe_directory.go`, `hazmat/internal/setup/rollback.go`, `hazmat/internal/setup/local_repo.go`, `hazmat/internal/setup/tooling.go` — rollback resource ordering and moved artifact removal after migration rollback dispatch |
| Governed code | `hazmat/internal/setup/sudoers.go`, `hazmat/sudoers.go` — optional current-version sudoers artifact |
| Governed code | `hazmat/migrate.go` — migration functions (per-version) |
| Governed code | `hazmat/rollback.go` — `runRollback()`, artifact removal ordering |
| Governed code | `hazmat/internal/state/state.go`, `hazmat/state.go` — core init version persistence for `~/.hazmat/state.json` (`harnesses` metadata is modeled separately by `MC_HarnessLifecycle`) |
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

**2026-06-03 package-split confirmation:** Phase K moved rollback resource
ordering plus local snapshot repository, completion, Git safe.directory,
wrapper, and umask artifact removal after migration rollback dispatch into
`internal/setup` while keeping `runDownMigrations()` before core rollback. No
migration version graph, artifact set, or state removal semantics changed.
`MC_Migration` was re-run with TLC and reported "No error has been found"
across the same state space: 234,101 generated states, 72,442 distinct states,
depth 18.

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
| Governed code | `hazmat/internal/runtime/docker/admission.go` — `PrepareLaunchAdmission()` launch-admission ordering |
| Governed code | `hazmat/sandbox.go` — `buildSandboxLaunchSpecWithPlan()`, `prepareSandboxLaunchWithPlan()`, `loadHealthySandboxLaunchBackend()`, `dockerSandboxesBackend.PrepareLaunch()` |
| Governed code | `hazmat/path_policy.go` — `isCredentialDenyPath()` |
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
   `compileDarwinSBPLChecked()`.

**Fixes applied:**

1. Added `buildSandboxLaunchSpecWithPlan()` as the explicit Tier 3 mount planner. It
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

**2026-06-02 modular refactor confirmation:** Re-run as the companion check for
the validated path constructor move and root/credential-deny handling review.
`MC_Tier3LaunchContainment` reported "No error has been found" across 33,876
generated states, 23,580 distinct states, depth 9.

**2026-06-12 Amp/Devin/external-agent/Goose credential-state expansion:** The abstract
launch model now includes `ampConfigDir`, `devinConfigDir`, `agentCliStateDir`, and
`gooseStateDir` as credential leaves, matching the concrete Amp config, Devin
config, external agent config/session roots, and Goose config/session/log deny
roots.
`MC_Tier3LaunchContainment` was re-run with TLC and reported "No error has
been found" across 3,270,228 generated states, 1,623,068 distinct states,
depth 9.

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
| Governed code | `hazmat/session.go` — `resolveSessionConfig()` |
| Governed code | `hazmat/native_session_policy.go` — `buildNativeSessionPolicy()` |
| Governed code | `hazmat/session_policy_sbpl.go` — `compileDarwinSBPLChecked()` |
| Governed code | `hazmat/native_launch.go` — `agentEnvPairs()` |
| Governed code | `hazmat/sandbox.go` — `prepareSandboxLaunchWithPlan()`, `buildSandboxLaunchSpecWithPlan()` |
| Governed code | `hazmat/path_policy.go` — `isCredentialDenyPath()` |
| Key invariants | `CredentialInputsRejectedInBoth`, `IntegrationEnvBreaksExactIdentity`, `NetworkNoneBreaksExactIdentity`, `ResumeBreaksExactIdentity`, `AncestorRewriteBreaksExactIdentity`, `CanonicalCoreContainmentEquivalent` |
| Status | **Proved** — exact Tier 2/Tier 3 identity is false by design, but the canonical core containment contract is equivalent across both backends |

**What was found:**

1. Exact backend identity is not a valid claim for the current product. The
   model proves four intentional divergence classes: integration env
   passthrough, native-only network-none mode, host-side resume history behavior,
   and Tier 3 ancestor mount rewriting.

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
actually provides.** TLC passes across all 327,680 reachable states (655,360
generated, depth 1, 17s).

**2026-06-02 modular refactor confirmation:** The deny-zone rejection move from
`resolveSessionConfig()` into typed path/request constructors preserved the
modeled rejected-input set. Before the move, `resolveSessionConfig()` called
`resolveDir()` / `resolveReadDirs()`, which delegated to
`pathpolicy.ResolveDir()` -> `Canonicalize()` -> `filepath.EvalSymlinks()`
before `isCredentialDenyPath()` / `isHostStateDenyPath()` ran. After the move,
`ResolveProjectRoot`, `ResolveReadOnlyGrant`, and `ResolveReadWriteGrant`
follow the same canonicalization path before `DenyPolicy.ValidateAllowedPath()`.
The additional zero-value `DenyPolicy` rejection is a fail-closed constructor
guard outside the modeled credential-input set. `MC_TierPolicyEquivalence` was
re-run with TLC and reported "No error has been found" across 655,360 generated
states, 327,680 distinct states, depth 1.

**2026-06-12 Amp/Devin/external-agent/Goose credential-state expansion:** The equivalence
model now includes `ampConfigDir`, `devinConfigDir`, `agentCliStateDir`, and
`gooseStateDir` as credential leaves so Tier 2 and Tier 3 reject the new Amp,
Devin, external agent, and Goose credential-state roots consistently.
`MC_TierPolicyEquivalence` was re-run with TLC and reported "No error has been
found" across 18,874,368 generated states, 9,437,184 distinct states, depth 1.

**Change rules:**
- Changes to project/read/write root normalization or credential-deny handling
  in either tier require re-running both this spec and the Tier 3 launch
  containment spec.
- Adding Tier 3 integration-env support requires updating this spec first; the
  current proof treats that difference as an intentional exact-identity break.
- Adding a Docker Sandbox equivalent of native `--network none` or changing
  how native network-none requests route requires updating this spec first.
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
| Key invariants | `PlannedRepairsMatchSnapshot`, `ValidatedCacheRequiresFreshMetadata`, `PreviewIsReadOnly`, `DockerSkipsNativeACLRepairs`, `HomebrewRepairRequiresEligibleCellar`, `LaunchClearsFatalRepairNeeds`, `RollbackPreservesSessionRepairs`, `BackfillIsOutsideStartupPlan` |
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

5. **Project backfill is not startup work:** project ACL startup repair is
   modeled as a bounded launch repair. Historical full-tree backfill remains
   outside the automatic launch plan.

6. **ACL cache hits are metadata-validated:** startup ACL cache entries may be
   used only when their validation metadata is still fresh; cached `.git`
   metadata health follows the same rule and falls back to the fresh permission
   probe on any missing or stale path fingerprint.
   used only while path identity and metadata remain fresh. The implementation
   may evict unrelated entries to bound startup cost; missing entries fall back
   to fresh probes.

TLC passes across all 79,098 reachable states (194,023 generated, depth 7, 5s).

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
| Governed code | `hazmat/harnesses/harnesses.go` — built-in harness IDs, declared state versions, launch/import metadata |
| Governed code | `hazmat/harness.go` — harness state recording and runtime registry compatibility wrappers |
| Governed code | `hazmat/internal/harnessruntime/state.go`, `hazmat/internal/harnessruntime/artifact.go`, `hazmat/internal/harnessruntime/uninstall.go`, `hazmat/internal/harnessruntime/install.go` — harness lifecycle state transition rules and managed artifact install/uninstall rules |
| Governed code | `hazmat/internal/state/state.go` — host-owned state-file schema, persistence, and harness runtime store implementation |
| Governed code | `hazmat/state.go` — root compatibility wrappers and migration helper entrypoints |
| Governed code | `hazmat/bootstrap.go`, `hazmat/bootstrap_codex.go`, `hazmat/bootstrap_opencode.go`, `hazmat/bootstrap_antigravity.go`, `hazmat/bootstrap_qwen.go` — bootstrap flows |
| Governed code | `hazmat/config_import.go`, `hazmat/config_import_codex.go`, `hazmat/config_import_opencode.go`, `hazmat/config_import_harness.go` — curated import flows (shared engine in `config_import_harness.go`) |
| Governed code | `hazmat/migrate.go` — rollback cleanup of `~/.hazmat/state.json` |
| Key invariants | `RecordedHarnessVersionsMatchSpec`, `ImportedMetadataCarriesVersion`, `StateFilePresentWhenMetadataExists`, `DryRunLeavesStateUntouched`, `SaveCoreStatePreservesHarnessMetadata`, `UninstallRemovesOnlyCodeAndMetadata`, `RollbackClearsMetadata`, `RollbackWithoutDeleteUserPreservesArtifacts`, `RollbackDeleteUserRemovesArtifacts` |
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

The model includes Claude, Codex, OpenCode, Antigravity, Hermes, Qwen, and Cursor
Agent. Hermes, Qwen, and Cursor Agent are modeled as built-in harnesses but are
deliberately not importable in Phase 1.

TLC was re-run on 2026-06-03 for the package-split refactor and reported "No
error has been found" across 633,107 distinct states (25,164,502 generated,
depth 18, ~1m47s).

**2026-06-25 curated-import consolidation (no spec change):** the three curated
importers (Claude, Codex, OpenCode) were collapsed onto one shared type set
(`importItem`/`importPlan`/`importApplyResult`/`importOptions`/`importStatus`/
`importConflictPolicy`/`importKind`) and a shared engine (`runBasicsImport`,
`applyImportPlan`, one cobra builder) in `config_import_harness.go`; only the
per-harness env paths, scan set, per-item apply, and display wording stay
distinct. This is a Go-side structural change only. `MC_HarnessLifecycle`
quantifies over abstract membership — `ImportableHarnesses = {claude, codex,
opencode}` and `ImportBasics(h)` recording into `recordedImported`/
`importedArtifacts` — not import mechanics; the same three harnesses remain
importable and the `recordHarnessImportRun` call site (`importHarnessBasicsRecord`)
is unchanged, so no modeled state moved and no spec edit was required (same
reasoning as the Antigravity precedent). The governed-code list above was
updated to add `config_import_harness.go`; the suite was re-run green.

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
| Governed code | `hazmat/configmodel/ssh.go` — `ValidateProjectSSHConfig()`, `ProjectSSHConfig.NormalizedKeys()`, `ValidateProjectSSHProfileRefs()`, `DetectLegacyFlatSSH()` |
| Governed code | `hazmat/config.go` — `runConfigSSHAdd()`, `runConfigSSHRemove()` |
| Governed code | `hazmat/git_ssh.go` — `resolveProjectSSHKeys()`, `prepareGitSSHRuntime()`, `startGitSSHTransportBroker()`, `runGitSSHTransportHelper()`, `selectSessionGitSSHKey()` |
| Key invariants | `DeterministicRouting`, `OverlapRejectedAtConfigTime`, `HostsOutsideAllowlistRejected`, `InlineKeysHaveDeclaredHosts`, `SocketsDistinctForPresent`, `NoDanglingProfileRefs`, `NoProfileInlineConflict`, `PresentKeysHaveIdentity`, `IdentitySourceClassified`, `NoCrossKey` |
| Status | **Proved and Implemented** — multi-key routing (sandboxing-vmg1), reusable profile resolution (sandboxing-nm5o), any-host fallback retirement (sandboxing-qq9b), and typed Git SSH identity-source classification are implemented and covered by the routing model |

**What this verifies:**

1. **Deterministic routing:** for any destination host, a ready config
   admits at most one configured key. The transport broker's per-host key
   selection in `selectSessionGitSSHKey()` matches this one-to-one structure.

2. **Overlap is a config-set error:** a config where two keys match the
   same host is refused at config save time, not at session time.
   `configmodel.ValidateProjectSSHConfig` enforces the spec's
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
| Governed code | `hazmat/hook_manifest.go`, `hazmat/hook_approval.go`, `hazmat/hook_runtime.go`, `hazmat/internal/hookruntime/commands.go`, `hazmat/hook_cli.go` |
| Governed code | `hazmat/rollback.go` — repo-local hook cleanup sweep |
| Key invariants | `ApprovedContentOnly`, `HooksPathPinned`, `WrapperRefusesReroute`, `ManagedDispatcherRefusesDrift`, `ComposedDispatcherRefusesDrift`, `FallbackDispatcherOnlyRefuses`, `RollbackClearsHookInstall`, `NoImplicitWidening` |
| Status | **Proved, implemented, and re-proved** — repo-local hook approval, immutable snapshot execution, wrapper / dispatcher refusal, explicit composed hooksPath ownership, and rollback cleanup ship behind the current hook command surface, with hook hidden command shells housed in `internal/hookruntime` |

**What this verifies:**

1. **Approval state remains well-formed:** `ApprovalStateWellFormed` keeps the
   approval, snapshot, wrapper, dispatcher, and fallback state internally
   consistent.

2. **Approved execution uses immutable snapshot bytes only:** a host-side hook
   run that succeeds must execute content from the approved snapshot record,
   not the live repo copy.

3. **`core.hooksPath` reroute is a refusal path:** the primary wrapper boundary
   must refuse if the effective `core.hooksPath` drifts away from the approved
   Hazmat-managed path or an explicitly composed hooksPath owner.

4. **Managed and composed dispatcher drift is fatal, not advisory:** repo
   drift, approval drift, snapshot drift, managed-layout drift, or composed
   hook block drift all resolve to refusal rather than best-effort execution.

5. **Fallback `.git/hooks` is detection-only:** reaching the default hook path
   is modeled as a refusal path, not an alternate approved execution channel.

6. **Hook approval does not widen session policy:** the proof boundary for hook
   activation does not grant future filesystem or network capability beyond the
   existing session contract.

`MC_GitHookApproval` passed on 2026-06-12 with "No error has been found" across
8,601,760 distinct states (520,417,388 generated, depth 10, 5m25s).

**Scope boundary:**

The spec models Hazmat-managed host-side entrypoints only: the Git wrapper
Hazmat installs, the managed dispatcher path, the Hazmat-managed block inside
an explicitly composed hooksPath owner, and the fallback `.git/hooks` drift
detector. It does **not** claim correctness for arbitrary direct invocation of
a foreign `git` binary outside that managed path, or safety of the external
hooksPath owner's own code.

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
| Governed code | `hazmat/secret_store.go`, `hazmat/internal/credentialruntime/store.go` — host/agent secret file read/write/remove helpers |
| Governed code | `hazmat/claude_keychain.go` — agent login keychain preparation |
| Governed code | `hazmat/claude_keychain_harvest.go` — agent keychain read/clear for keychain-backed OAuth rotation; `reconcileKeychainResidueIntoAgentFile` in `hazmat/harness_auth_runtime.go` folds the keychain value into the file copy for uniform harvest/recovery |
| Key invariants | `LatestValueNeverSilentlyLost`, `AgentKeychainNeverBothLive`, `CleanRecoveredStateHasNoAgentResidue`, `CleanRecoveredStateKeepsLatestHostOwned`, `NoCrossHarnessAgentExposure`, `LaunchOnlyAfterRecovery`, `IdleClearsSessionBaseline` |
| Status | **Proved and implemented** — file-backed harness auth survives crash/restart interleavings without silently losing the latest known value; the model also covers keychain-backed OAuth rotation (rotated token lands in the agent login keychain while the file copy is emptied), and harvest/recovery promote whichever runtime sink is live. Implemented in `hazmat/claude_keychain_harvest.go` + `reconcileKeychainResidueIntoAgentFile` in `hazmat/harness_auth_runtime.go`; covered by `TestHermeticClaudeKeychainRotationLogout` (`hazmat/harness_claude_keychain_rotation_test.go`). |

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

6. **Keychain-backed rotation is captured, not stranded:** when a harness
   refreshes its OAuth token into the agent login keychain and rewrites the
   materialized file to the logged-out empty shape, harvest and crash recovery
   promote the keychain value into the host store. The model proves the file
   copy and the keychain are never both live at once (`AgentKeychainNeverBothLive`),
   so a single runtime value is always unambiguous, and cleanup clears the
   keychain residue between sessions. Omitting the keychain promotion (the
   pre-fix behavior) violates `LatestValueNeverSilentlyLost` — the host store
   retains a server-invalidated refresh token and the next session is logged
   out.

TLC passes across 17,002 distinct states (72,026 generated, depth 29, 1s on the
local single-worker run).

**Scope boundary:**

The proof is content-level and crash/restart focused. It models the two runtime
credential sinks (materialized file and agent login keychain) but not exact
Claude JSON merge semantics, the macOS `security` keychain syscalls themselves,
concrete filesystem permission syscalls, or concurrent writes to the same host
secret while a session is running. Proving the concurrent-host-write case
requires revision or epoch metadata; content equality alone cannot distinguish
an unchanged baseline from a same-content rewrite. The `AgentKeychainNeverBothLive`
invariant assumes the implementation clears the agent keychain residue at
cleanup/recovery, the same way it removes the materialized file copy.

The host-store↔host-Keychain reconciliation in `syncHostKeychainBeforeLaunch`
(host login Keychain vs `~/.hazmat/secrets`) arbitrates divergence by
modification time and is likewise out of model — it is the host-side
counterpart of `ExternalStoreUpdate`'s excluded concurrent-host-write case. The
model resolves divergence by archiving conflicts, never by inferring freshness;
the implementation only reaches for timestamps at this unmodeled boundary, and
when a timestamp is indeterminate (e.g. the Keychain item's `mdat` cannot be
parsed) it **fails closed** — refusing the sync rather than letting the
timestamped side silently overwrite a possibly-newer value — so the
no-silent-loss intent of `LatestValueNeverSilentlyLost` is preserved at the
boundary. Covered by `TestPrepareHarnessAuthRuntimeRejectsUnknownTimeHostKeychainConflict`.

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
- Any harness that can rotate its token into a non-file runtime sink (e.g. the
  macOS Keychain) must have that sink reconciled by harvest and crash recovery,
  and cleared at cleanup, so `AgentKeychainNeverBothLive` stays inductive and
  the live value reaches the host store.
- Stronger guarantees for concurrent host-store writes require explicit
  revision metadata in the model and implementation.

---

### 13 — Credential Capability Lifecycle

| Field | Value |
|-------|-------|
| Spec | `tla/13_credential_capability_lifecycle.md` |
| TLA+ files | `tla/MC_CredentialCapabilityLifecycle.tla`, `tla/MC_CredentialCapabilityLifecycle.cfg` |
| Governed code | `hazmat/credentials/registry.go`, `hazmat/credential_registry.go` — credential IDs, backends, delivery modes, support status, harness scope |
| Governed code | `hazmat/harness_auth_runtime.go` — file-backed materialization, harvest, crash recovery precondition |
| Governed future code | Git HTTPS broker, cloud credentials, SSH identity refs, and integration/env credential grants |
| Key invariants | `NonHostBackendsHaveNoHostStore`, `DeliveryMatchesRegistry`, `AdapterRequiredNeverExposed`, `NoDeliveryNeverExposed`, `NoCrossHarnessExposure`, `NoSessionExposureOutsideActivePhase`, `LaunchOnlyAfterRecovery`, `CleanRecoveredStateHasNoCredentialResidue`, `LatestValueNeverSilentlyLost`, `CleanRecoveredStateKeepsLatestHostOwned`, `IdleClearsSessionState` |
| Status | **Proved** — registry entries cannot be delivered through the wrong mechanism, syncable Keychain caches reconcile through the host-owned store before launch/after harvest, contained-only profile state remains outside credential sync, adapter-required credentials remain unexposed, and crash/restart clears session-only grants while preserving recovery invariants |

**What this verifies:**

1. **Delivery mode is authoritative:** a file credential may create agent-side
   materialization, an env credential may only appear in env grants, a brokered
   credential may only appear in broker grants, and external references may only
   appear as external grants.

2. **Adapter-required backends are inert:** an adapter-required credential (modeled
   by the generic `adapter_keychain` representative) cannot become active, delivered,
   materialized, env-granted, broker-granted, or externally granted until an adapter
   is modeled. (The Antigravity Keychain OAuth credential was reclassified from
   adapter-required to a non-syncable external reference once Hazmat shipped its agent
   login keychain adapter — see the 2026-06-25 note below.)

3. **Syncable Keychain references stay narrow:** Claude-style Keychain OAuth
   can be represented as an external-reference credential with a host-user
   Keychain cache, an agent-user Keychain cache, and Hazmat's host-owned store
   as the neutral exchange point. Launch is blocked until the host cache and
   store are reconciled, and harvest publishes agent-side rotations back to the
   store and host cache.

4. **Contained-only profile state is not credential sync:** no-delivery entries
   represent broad harness profile homes such as Hermes/Qwen/Cursor/Pi-style
   state. They cannot become active credentials or be exposed by file/env/broker
   or external-reference grants.

5. **Harness scope is enforced:** active-session exposure must either belong to
   the active harness or be explicitly global, which is the shape expected for
   future Git HTTPS broker credentials.

6. **Crash clears session-only grants:** env, broker, and external grants do not
   survive a crash/restart transition. File residue may survive, but launch is
   blocked until recovery reconciles it. Agent-user Keychain residue for
   syncable Keychain credentials is also recovered before launch.

7. **Host ownership remains the recovered state:** after recovery, the latest
   known managed value is in host primary storage, a host-user Keychain cache,
   or a host-owned conflict archive, not only in `/Users/agent`.

TLC passes across 5,335,005 distinct states (22,243,319 generated, depth 40) after
the Antigravity Keychain adapter reclassification (see the 2026-06-25 note below);
the pre-adapter baseline was 5,182,905 distinct states (21,747,980 generated, depth
40, 26m56s on the local 10-worker run).

**Scope boundary:**

This is a registry-level proof. It does not model exact concrete file paths,
filesystem permissions, JSON merge semantics, Keychain authorization prompts,
git credential-helper protocol bytes, cloud provider behavior, SSH agent socket
behavior, OAuth provider refresh timing, token-validity checks, or integration
manifest parsing. Those remain governed by narrower future specs, tests, and
docs.

**Change rules:**
- Adding a credential delivery mode, support status, or secret-exposing backend
  requires updating `MC_CredentialCapabilityLifecycle.tla` first.
- Adapter-required credentials must remain undeliverable until their adapter is
  represented in this model and TLC proves the intended invariants.
- Syncable Keychain credentials must model both user-owned keychain caches, the
  neutral store, launch-time reconciliation, harvest publish-back, and cleanup.
- Contained-only profile state must remain no-delivery unless a future narrow
  credential surface is modeled first.
- Git HTTPS, cloud backup, Git SSH, and integration/env credential work must
  preserve the model's delivery-mode and session-scope invariants.
- Any future path that creates durable `/Users/agent` credential material must
  be modeled as file delivery and must preserve recovery-before-launch.

**2026-06-25 Antigravity (agy) Keychain OAuth adapter shipped:** agy is a flat
native binary whose interactive Google sign-in stores its OAuth token in the macOS
Keychain. Hazmat now bridges this with the agent login keychain adapter (the same
empty-password agent login keychain it prepares for Claude), so the credential moved
from adapter-required (inert) to an **external, non-syncable** keychain reference.
In the model: `gemini`/`gemini_keychain` were renamed to `antigravity`/
`antigravity_keychain` (the gemini→antigravity migration had updated
`MC_HarnessLifecycle` but not this spec); `antigravity_keychain` moved from
`AdapterRequiredSupportCreds` to `ExternalSupportCreds` while staying OUT of
`SyncableKeychainCreds` — so `DeliverExternal` exposes it as an `externalGranted`
reference with no modeled secret bytes and no host harvest/writeback (Hazmat does
not extract agy's Keychain item). A synthetic `adapter_keychain` cred was added to
keep `AdapterRequiredNeverExposed` and the `RegistryWellFormed` adapter clause
non-vacuous, since the `SupportAdapterRequired` enum and its generic Go machinery
(diagnostics, session-home blocker, rollback receipts) remain live with no other
built-in adapter-required credential. Go side: `HarnessAntigravityKeychain` is now
`SupportExternal`; the session grants the scoped `MacOSAgentKeychainAccess` SBPL
re-allow (modeled in `MC_SeatbeltPolicy` by the free `agentKeychainAccess` boolean —
no model change, comments generalized to "Claude / Antigravity OAuth") and unlocks
the agent login keychain before launch. `MC_CredentialCapabilityLifecycle` re-ran
with TLC: "No error has been found" across 5,335,005 distinct states (22,243,319
generated, depth 40). `MC_SeatbeltPolicy` re-ran green (comment-only change).

---

### 9 — Launch FD Isolation

| Field | Value |
|-------|-------|
| Spec | `tla/09_launch_fd_isolation.md` |
| TLA+ files | `tla/MC_LaunchFDIsolation.tla`, `tla/MC_LaunchFDIsolation.cfg` |
| Governed code | `hazmat/agent_launch.go` — native sudo + helper launch construction |
| Governed code | `hazmat/session.go` — `runPreparedAgentSeatbeltScriptWithUI()`, `runAgentSeatbeltScriptWithPlan()`, policy-file generation |
| Governed code | `hazmat/cmd/hazmat-launch/main.go` — inherited-fd cleanup, policy read, `sandbox_init()`, final `exec` |
| Governed code | `hazmat/cmd/hazmat-launch-fast/main.c` — experimental lower-level broker child helper for profiling the same inherited-fd cleanup, policy read, `sandbox_init()`, and final `exec` boundary |
| Governed code | `hazmat/internal/runtime/launchbroker/*.go` — authenticated broker request, verified launch request, child-plan fd cleanup contract, helper command plan, buffered helper executor, service lifecycle wrapper |
| Governed code | `hazmat/internal/runtime/darwin/runtime.go` — shared helper argv builder used by both sudo and broker command paths |
| Governed code | `hazmat/internal/agententry/commands.go`, `hazmat/launch_broker_agent_entry.go` — hidden agent-side launch broker command and signal-aware service runner |
| Governed code | `hazmat/launch_broker_supervisor.go` — host-side broker startup command construction through `hazmat-launch exec` and fake-startable supervision |
| Governed code | `hazmat/native_launch_broker.go`, `hazmat/session.go` — opt-in host-side broker client path for buffered non-interactive launches |
| Governed future code | persistent native launch broker executor wiring — forkserver or equivalent lower-level executor, interactive stdio/session transport, and default persistent broker lifecycle |
| Key invariants | `BrokerLaunchRequiresAuthenticatedPeer`, `BrokerFDTableDropsHostInheritedFDs`, `ForkserverFDTableAllowlistedWhenReady`, `HelperFDTableAllowlistedAtSandbox`, `NoInheritedShellFDsAtSandbox`, `CredentialFDsGoneBeforeSandbox`, `AgentFDTableAllowlisted`, `StdioSurvivesToAgent`, `BrokerStartsOnlyAfterSandboxConfirmed`, `TokenMintedOnlyAfterSandboxConfirmed`, `AgentFDTableDoesNotCarryAuthority` |
| Status | **Proved and Partly Implemented** — the native helper now sanitizes inherited fds before sandboxing and keeps the final agent exec to stdio only; the broker transport boundary authenticates peers, constructs only fd-cleanup child plans, plans direct helper invocation without sudo, has a buffered helper executor for non-interactive launches, has a typed service lifecycle wrapper, has a host-side start plan that routes long-lived broker startup through `hazmat-launch exec`, has opt-in host-side buffered launch client wiring, and has a proved forkserver/control-fd alternative for future lower-level executor work; interactive stdio/session transport and default persistent broker lifecycle remain pending |

**What this verifies:**

1. **Launch-child cleanup is mandatory:** the checked design does not rely on
   Go's current `exec` behavior, `sudo`'s current fd cleanup, or a persistent
   broker's steady-state fd hygiene to keep inherited descriptors out of
   `sandbox_init()`.

2. **Sandboxing starts from a curated fd table:** once `sandbox_init()` is
   called, the launch executor holds only stdio plus its helper-opened policy
   file.

3. **Credential-bearing inherited fds are gone before Seatbelt matters:** path
   denies are only meaningful if no already-open credential handle survived
   into the helper.

4. **The final agent exec is stdio-only:** helper-opened policy state is
   `CLOEXEC`, so it cannot leak into the actual agent process.

5. **Brokered launch requires peer authentication:** the persistent broker path
   cannot fork or enter the launch executor path until the host request is
   authenticated.

6. **Broker startup has its own fd cleanup boundary:** the long-lived broker
   must not enter its listening/serving state while retaining host-origin
   non-stdio descriptors inherited from the supervisor startup chain.

7. **A forkserver optimization has the same child cleanup obligation:** a
   persistent lower-level executor may retain only stdio plus a private
   broker/executor control fd in its parent, and every launch child must close
   that control fd before policy validation, `sandbox_init()`, and final exec.

**2026-06-09 Beadpost broker ordering + authority-fd hygiene (minimal addition):**
The spec now also proves the launch-order facts for the contained-agent submitter
+ dr-owned host broker design. `BrokerStartsOnlyAfterSandboxConfirmed` and
`TokenMintedOnlyAfterSandboxConfirmed` establish the chain *sandbox_init success →
confirmed-containment metadata → broker active → attestation minted*, so a pre-sudo
"prepared launch" (which is not confirmed containment) can never mint authority.
`AgentFDTableDoesNotCarryAuthority` adds an `authority` fd-target class and an
adversarially inherited authority-bearing fd (a leaked broker signing-key fd) and
proves it is sanitized out before the final agent exec — non-vacuously, mirroring
`CredentialFDsGoneBeforeSandbox`. Request routing is deliberately out of scope here
(see `MC_BeadpostBrokerBoundary`). Part 2 of 3 for the attestation boundary; see
`docs/plans/2026-06-09-beadpost-attestation-spec-plan.md`.

TLC passes with the addition across 416 reachable states (608 generated, depth 10,
<1s); the original fd-isolation core was 112 reachable states (128 generated, depth 7).

**2026-06-15 persistent launch broker path:** The spec now includes a brokered
steady-state launch mode alongside `sudo -> hazmat-launch`. It models a
long-lived agent broker startup chain that may inherit host-origin
credential/authority fds, proves those are dropped before the broker listens
(`BrokerFDTableDropsHostInheritedFDs`), and still models each launch child
inheriting broker-owned listener/request descriptors before child-side cleanup.
The same launch-child fd cleanup must remove those descriptors before
`sandbox_init()`, and `BrokerLaunchRequiresAuthenticatedPeer` proves a brokered
launch cannot reach that child path before host-peer authentication. TLC passes
across 928 reachable states (1,312 generated, depth 13, <1s on the local run).

The concrete broker request boundary now lives in
`hazmat/internal/runtime/launchbroker`: Unix peer authentication precedes
request verification, `VerifiedLaunchRequest` is required to construct a
`ChildPlan`, and `ChildPlan` construction requires
`ChildFDPolicyCloseInherited`. It now also builds the direct `hazmat-launch`
helper command for an authenticated child path using only
`SUDO_UID=<authenticated-peer>` in the helper process environment. The buffered
helper executor returns exit code/stdout/stderr and fails closed if requested
confirmed-containment metadata is not observed on helper stderr. The service
wrapper owns Unix socket readiness, cancellation, and cleanup while wiring the
helper executor as the default handler. The hidden `_launch_broker` agent-entry
command starts that service with a signal-aware context. The host-side broker
start plan/supervisor starts `_launch_broker` through `hazmat-launch exec`,
reusing the proved helper fd-cleanup boundary before the long-lived broker opens
its socket. The host-side broker client path can now route buffered
non-interactive launches through a configured broker socket, preserving
confirmed-containment metadata replay, stdout/stderr replay, nonzero exit
status, and post-session repair/denial recording while avoiding per-launch
`sudo` when a broker is already running. The experimental default path can start
the per-uid broker once through that same proved startup boundary and retry the
request; explicitly configured sockets remain fail-fast. Broker direct-exec
requests now carry argv plus working directory without also carrying the shell
script field, preserving request validation's direct-exec/shell mutual
exclusion while allowing capable helpers to skip the shell wrapper. The
experimental default path may also choose the existing direct helper path for a
narrow one-shot absolute-command cold launch when no default broker is
listening; this is the already-proved direct launch mode, not a new fd boundary,
and explicitly configured sockets remain fail-fast. The
host-side start plan can now split the startup helper from the child launch
helper: broker startup still uses the sudo-authorized helper for the proved
fd-cleaning `hazmat-launch exec` boundary, while the agent-owned broker may use
a newer checkout-built helper for per-launch child execution. If the broker path
falls back to sudo and the sudo helper cannot create helper-managed session
temp, Hazmat creates the agent temp dir before fallback. The supervisor removes
stale socket path residue before startup, but refuses live sockets, symlinks,
and non-socket paths so crash leftovers do not force a sudo fallback while
active or suspicious paths are not clobbered. The default per-uid broker runtime
directory is revalidated through the same agent-side shared-directory
preparation even when it already exists, repairing mode/group drift before
broker startup so the experimental path stays on the proved broker startup
boundary instead of silently falling back to per-launch `sudo`. Helper
capability detection is cached by helper path, size, mode, mtime, device, and
inode so compatible helpers can avoid repeated binary scans; helper replacement
forces a fresh bounded scan. Interactive
stdio/session transport and default-on lifecycle remain future governed work
under this same model.

**2026-06-15 forkserver executor alternative:** The spec now distinguishes the
current broker child executor (`exec_helper`) from a future persistent
`forkserver`. The forkserver starts from the already-clean agent broker and may
hold a private socketpair-style control fd, but
`ForkserverFDTableAllowlistedWhenReady` proves the forkserver parent retains
only stdio plus that control fd before it accepts launch work. The same launch
child path proves the forkserver child closes the inherited control fd before
policy open, `sandbox_init()`, confirmed-containment metadata, and final agent
exec. This is the proof boundary for replacing per-launch helper exec with a
Zig/C forkserver or equivalent lower-level executor. TLC passes across 1,920
reachable states (2,688 generated, depth 15, <1s on the local run).

During design, a temporary negative config with
`HelperClosesInheritedFDs = FALSE` immediately produced a counterexample where
an inherited non-stdio fd survived into `sandbox_init()`. That is why helper-
side cleanup is now a proved design rule instead of an implementation detail.

**Change rules:**
- Any change to the native `sudo -> hazmat-launch -> sandbox_init() -> exec`
  chain, or replacement with `hazmat -> persistent broker -> launch child ->
  sandbox_init() -> exec`, must preserve both boundaries: no inherited non-stdio
  fd at `sandbox_init()`, and stdio-only final agent exec.
- Replacing helper-side fd cleanup with reliance on upstream `sudo` or Go
  behavior requires updating this spec first. The current proof assumes Hazmat
  owns that boundary itself.
- Any helper-opened fd that may remain live across the final `exec` must be
  modeled here first. The current proof assumes helper-opened policy state is
  explicitly `CLOEXEC`.
- A persistent launch broker must authenticate the host peer before it forks a
  launch child or accepts a policy/command request for execution.
- A persistent launch broker must be started through `hazmat-launch exec` or an
  equivalent fd-cleaning boundary before it listens; relying on Go or sudo fd
  inheritance behavior is not sufficient.
- A persistent forkserver or lower-level executor may retain a private
  broker/executor control fd only in the parent. Every forked launch child must
  close that control fd before policy validation and `sandbox_init()`.
- The Beadpost broker must not activate, and attestation authority must not be
  minted, before confirmed containment (`sandbox_init` + emitted metadata). Any
  change that lets the broker/mint precede confirmation, or that lets an
  authority-bearing fd reach the agent, must update this spec first.

---

### 14 — Linux Native Launch Ordering

| Field | Value |
|-------|-------|
| Spec | `tla/14_linux_native_launch.md` |
| TLA+ files | `tla/MC_LinuxNativeLaunch.tla`, `tla/MC_LinuxNativeLaunch.cfg` |
| Governed code | `hazmat/containment/linux` — plan-only launch spec compiler |
| Governed code | future Linux native helper — validation, fd cleanup, namespace/mount/network setup, privilege drop, LSM/seccomp, metadata, final `exec` |
| Key invariants | `SpecValidatedBeforeSideEffects`, `FDsClosedBeforeNamespaces`, `MountsAfterNamespaces`, `NetworkNoneDeniedBeforeMetadata`, `PrivilegeDropBeforeLSMAndMetadata`, `MetadataAfterEnforcement`, `ExecAfterMetadata`, `NoExecOnFailure`, `NoExecWithMissingRequiredFeature` |
| Status | **Design Proved, Implementation Pending** — Linux native launch remains unsupported until the helper implementation follows this ordering and passes Linux smoke/manual gates |

**What this verifies:**

1. **Spec validation comes first:** no fd cleanup, namespace creation, mount,
   network, privilege, metadata, or exec fact can appear before a valid launch
   spec and mount plan.

2. **Inherited fd cleanup precedes namespace work:** Linux native launch keeps
   the same design rule as the Darwin helper: ambient descriptors are removed
   before enforcement setup continues.

3. **Network-none is enforced before metadata:** a `--network none` launch must
   create the network namespace and record the deny state before metadata or
   exec.

4. **Privilege drop and `no_new_privs` precede LSM/seccomp decisions:** metadata
   cannot be emitted until privilege drop, `no_new_privs`, Landlock, and
   seccomp states are settled.

5. **Capability gaps fail closed unless explicitly accepted:** missing
   namespace support always fails; missing Landlock/seccomp may continue only
   when the launch spec explicitly accepts that gap.

TLC passes across all 2,842 reachable states (3,866 generated, depth 11, <1s).

**Change rules:**
- The Linux helper implementation must follow the modeled order before it can
  be considered supported.
- Any relaxation that emits metadata before enforcement, execs without
  metadata, or treats a missing required feature as success requires updating
  `MC_LinuxNativeLaunch.tla` first and re-running TLC.
- Changes to Linux launch-spec capability gap semantics must update both this
  model and the companion design note before implementation.
- Concrete syscall behavior, seccomp filter contents, Landlock rules, and mount
  propagation details remain test/VM-smoke obligations in addition to this
  abstract proof.

---

### 15 — Beadpost Attestation-Boundary Broker

| Field | Value |
|-------|-------|
| Spec | `tla/15_beadpost_broker_boundary.md` |
| TLA+ files | `tla/MC_BeadpostBrokerBoundary.tla`, `tla/MC_BeadpostBrokerBoundary.cfg` |
| Governed code | `hazmat/hostbroker/session.go` — `Open()`, `confirmSandboxBoundary()`, `allocateBrokerSocket()`, `deriveAuthorityFromLaunchFacts()`, `invokeDelivery()`, `Close()` |
| Key invariants | `BrokerSocketOnlyAfterConfirmedSession`, `AcceptedRequestHasConfirmedSession`, `AgentCannotSupplyAuthorityFields`, `AcceptedAuthorityEqualsLaunchFacts`, `NoCrossSessionRequest`, `NoRequestAfterSessionClose`, `HostAuthorityNeverAgentReadable`, `DeliveryOnlyFromAcceptedRequest` |
| Status | **Design Proved; Implemented (sandboxing-x74u.6)** — the contained-agent submitter + dr-owned host broker membrane. Real implementation behind `//go:build beadpost_hostbroker`; the default/public build ships dependency-free fail-closed stubs and never links the contract module. |

**What this verifies:**

1. **Confirmation gates the membrane:** a broker socket exists, and a request is
   accepted, only for a session whose containment was confirmed. (The launch-time
   confirmation ordering — `sandbox_init` then metadata — is proved in
   `MC_LaunchFDIsolation`; this spec treats "confirmed" as the entry gate.)

2. **Authority is derived, never supplied:** the agent submits closed request
   *content* only. The broker stamps `deliveredAuthority := launchFacts[s]`
   unconditionally, so authority is never a function of agent input
   (`AgentCannotSupplyAuthorityFields`, `AcceptedAuthorityEqualsLaunchFacts`).

3. **Host authority is write-once:** no action mutates `launchFacts`; the genesis
   snapshot witnesses immutability (`HostAuthorityNeverAgentReadable`).

4. **Deterministic per-session binding:** no two sessions share a broker socket
   (`NoCrossSessionRequest`).

5. **Clean teardown:** a closed session retains no socket, content, authority, or
   acceptance (`NoRequestAfterSessionClose`); delivery only follows an accepted
   request (`DeliveryOnlyFromAcceptedRequest`).

TLC passes across all 1,088 reachable states (3,104 generated, depth 9, <1s) with
2 sessions, 2 projects, 2 tiers, 2 sockets.

**Scope boundary:** `HostAuthorityNeverAgentReadable` is design separation, not
OS memory/key isolation — that is proved by `MC_SeatbeltPolicy` (attestation-key
deny) and `MC_LaunchFDIsolation` (authority-fd hygiene). Replay defense, strong-tier
enforcement, and cross-host token theft are Beadpost-side obligations (tracked
under `bp-fyg`), not modeled here. Part 3 of 3 for the attestation boundary; see
`docs/plans/2026-06-09-beadpost-attestation-spec-plan.md`.

**Change rules:**
- Any change letting agent content influence delivered authority must update
  `AgentCannotSupplyAuthorityFields` first and re-run TLC.
- Removing the confirmed-session gate before socket allocation or acceptance
  requires re-proving the gating invariants (they will fail — this is the firewall).
- Adding authority fields beyond `(project, tier)` requires extending
  `AcceptedAuthorityEqualsLaunchFacts`.

---

### 16 — Apple Container Launch Containment

| Field | Value |
|-------|-------|
| Spec | `tla/16_apple_container_launch_containment.md` |
| TLA+ files | `tla/MC_AppleContainerLaunch.tla`, `tla/MC_AppleContainerLaunch.cfg` |
| Governed code | `hazmat/containment/applecontainer/spec.go` — launch spec compiler: `Compile()`, `Argv()`, forbidden-feature rejection, network fail-closed, cleanup accounting |
| Governed code | `hazmat/internal/runtime/applecontainer/runtime.go` — experimental runtime: `ProbeHost()` admission, `Run()` launch + exact-name cleanup with recorded failures |
| Governed code | `hazmat/exec_apple_container.go` — gated `hazmat exec --backend=apple-container` session path |
| Governed code | `hazmat/explain_apple_container.go` — plan-only preview surface |
| Key invariants | `CredentialPathsNeverMounted`, `InvokerHomeNeverMounted`, `AgentHomeNeverMountedWholesale`, `ProjectMountedRW`, `PlannedReadDirsMountedRO`, `CoveredReadDirsOmitted`, `NoUnexpectedLaunchEnv`, `IntegrationEnvRejected`, `SSHForwardingRejected`, `SocketPublishingRejected`, `AdmissionBeforeLaunch`, `UnsupportedNetworkFailsClosed`, `CredentialMaterializationGated`, `CredentialArtifactSessionScoped`, `TerminalCredResidueHandled`, `TerminalContainerHandled`, `ForeignContainersUntouched` |
| Status | **Proved and Implemented (experimental)** — the runtime follows the modeled ordering behind the `HAZMAT_EXPERIMENTAL_APPLE_CONTAINER=1` gate, exec-only, with gated macOS 26 smoke coverage (`internal/runtime/applecontainer/smoke_test.go`, green 2026-06-10 on container 1.0.0). Identity model revised 2026-06-10: the CLI runs as the invoking user (spike F1); the contract states bluntly that host account isolation is not provided by this backend |

**What this verifies:**

1. **Credential deny zones are never mounted:** the mount plan rejects
   credential paths and parents of credential paths — including the invoking
   user's home wholesale and the `agent` user's home wholesale — using the
   same deny-parent posture as the Tier 3 Docker Sandbox planner.

2. **Forbidden launch features fail first:** integration env passthrough,
   SSH agent forwarding (`--ssh`), and socket publishing are rejected before
   admission, mount planning, or any credential materialization.

3. **Admission gates launch:** macOS 26+ Apple silicon, approved CLI path,
   healthy API server reachable for the invoking user, supported version,
   and an approved image are modeled as an abstract admission conjunction
   that must pass before launch. (Identity model revised 2026-06-10: the CLI
   runs as the invoking user; host account isolation is explicitly not
   claimed by this backend.)

4. **Unsupported network policies fail closed:** `--network none` and
   allowlist requests cannot launch with a weaker-than-claimed policy; only
   `default` is a supported mode in the MVP model.

5. **Credential artifacts are session-scoped and accounted for:** generated
   env/secret files exist only after admission and network gating, are
   session-scoped by construction, and at session end are removed or the
   cleanup failure is recorded — including when `container run` fails after
   materialization (a mutation test on that path violates
   `TerminalCredResidueHandled`, confirming non-vacuity).

6. **Cleanup never prunes:** a foreign container chosen at `Init` survives
   every action (genesis-witness style), so prune-style sweeps cannot be
   added without breaking the proof.

TLC passes across all 134,720 distinct states (246,528 generated, depth 10,
~4s).

**2026-06-12 Amp/Devin/external-agent/Goose credential-state expansion:** The Apple
Container launch model now includes `ampConfigDir`, `devinConfigDir`,
`agentCliStateDir`, and `gooseStateDir` as credential leaves, matching the concrete
Amp config, Devin config, external agent config/session roots, and Goose
config/session/log deny roots.
`MC_AppleContainerLaunch` was re-run with TLC and reported "No error has been
found" across 18,487,296 generated states, 8,360,000 distinct states, depth 10.

**Scope boundary:** Apple Container VM internals, VirtioFS UID/GID ownership
mapping, guest processes, image contents, `container machine` persistent
mode, and network allowlist/proxy profiles are NOT modeled. The VirtioFS
ownership question is an explicit host-probe obligation (`sandboxing-ajmn`)
before the experimental runtime ships.

**Change rules:**
- Any change to Apple Container mount planning, admission ordering, network
  gating, or credential artifact lifecycle must update
  `MC_AppleContainerLaunch.tla` first and re-run TLC before Go changes.
- Adding a supported network mode requires extending `SupportedNetworkModes`
  plus new policy-before-launch and network-artifact-cleanup invariants.
- Adding SSH forwarding, socket publishing, or integration env support
  requires updating the forbidden-feature gate and rejection invariants first.
- `container machine` support requires a separate persistent-state model.
- Any cleanup broader than exact session-owned artifact names must contend
  with `ForeignContainersUntouched`.

---

### 17 — Service Harness Lifecycle

| Field | Value |
|-------|-------|
| Spec | `tla/17_service_harness_lifecycle.md` |
| TLA+ files | `tla/MC_ServiceHarnessLifecycle.tla`, `tla/MC_ServiceHarnessLifecycle.cfg` |
| Governed code | `hazmat/internal/serviceharness/lifecycle.go` — service lifecycle runner, request validation, residue recovery ordering, readiness/attach/cleanup orchestration, and redacted lifecycle events |
| Governed code | `hazmat/proxyruntime/service.go` — service-shaped proxy lifecycle runner, local attach validation, typed credential gating, readiness/attach ordering, cleanup, and redacted lifecycle events |
| Governed code | Future `hazmat <service-harness>` command surface and adapter-specific service metadata persistence |
| Key invariants | `PriorResidueHasMetadata`, `SideEffectsHaveMetadata`, `StartOnlyAfterPriorResidueHandled`, `UnsupportedRequestsFailClosed`, `CredentialMaterializationGated`, `ReadyRequiresHealth`, `AttachOnlyAfterReady`, `AttachDetailsAfterReady`, `AttachAuthorityHasMetadata`, `AttachPolicyLocalOnly`, `ProxyServiceAttachPolicy`, `LocalhostPortRequiresToken`, `NoHostDockerSocketExposure`, `NoNativeContainerStart`, `NoProfileDaemonBrowserOrEnvStart`, `TerminalResidueHandled`, `RejectedRequestsHaveNoCurrentSideEffects`, `CredentialRemovedOnlyAfterTypedPlan` |
| Status | **Design Proved; first fake-service suite implemented** — proves the lifecycle boundary future OpenHands-style service adapters must satisfy before implementation and pins it with `make e2e-service-harness-smoke` |

**What this verifies:**

1. **Recovery gates new service start:** prior service, credential, or attach
   residue must be recovered before a new service starts, or the failure is
   recorded and the new service does not launch.

2. **Unsupported features fail closed:** host Docker socket access, host profile
   import, persistent daemon mode, browser automation, integration env
   passthrough, LAN-visible bind, localhost bind without a session token,
   untyped credentials, and container-requiring native service requests cannot
   reach side effects.

3. **Metadata precedes authority:** current service metadata exists before
   credential materialization, service start, attach authority, or printed
   attach details.

4. **Attach waits for readiness:** readiness requires a passed health check, and
   the active ready/attached phases keep the service running; attach and
   user-visible attach details happen only after readiness.

5. **Terminal residue is accounted for:** terminal service, credential, attach,
   or prior residue must be gone or covered by a recorded cleanup failure.

TLC passes across all 2,612,624 distinct states (6,391,472 generated, depth 10,
~11s).

**Scope boundary:** OpenHands internals, HTTP/WebSocket payloads, service logs,
Docker Sandbox or VM internals, browser automation, and concrete service
protocol behavior are not modeled. This is the Hazmat host/session lifecycle
boundary for a future service adapter, not proof that OpenHands itself is safe.

**Change rules:**
- Adding a first-class service harness adapter, service lifecycle phase, service
  metadata field, port/socket attach policy, or crash-cleanup rule must update
  `MC_ServiceHarnessLifecycle.tla` first and re-run TLC before implementation.
- Supporting host Docker socket access, LAN-visible binds, browser automation,
  host profile import, persistent daemon mode, or untyped credentials requires
  a deliberate model change; the current model proves those requests fail closed.
- Native container-requiring services require a separate backend model change.
- Live service smokes are not proof; they become release gates only after this
  model and the fake-service lifecycle suite agree on the adapter behavior.

---

## Quick Reference: Spec → Code Mapping

| Spec | Files governed |
|------|---------------|
| `01_setup_rollback_state_machine` | `hazmat/init.go:runInit()`, remaining root setup callbacks; `hazmat/internal/setup/*.go`; `hazmat/internal/setup/darwin/*.go`; `hazmat/native_account*.go`; `hazmat/native_service*.go`; `hazmat/sudoers.go`; `hazmat/rollback.go:runRollback()`, remaining root rollback callbacks |
| `02_seatbelt_policy_structure` | `hazmat/native_session_policy.go:buildNativeSessionPolicy()`, `hazmat/session_policy_sbpl.go:compileDarwinSBPLChecked()`, `hazmat/session.go:isWithinDir()` |
| `03_backup_restore_safety` | `hazmat/kopia_wrapper.go:runCloudBackup()`, `runCloudRestore()`, `snapshotProject()`; `hazmat/restore.go:runProjectRestore()`; `hazmat/internal/backupruntime/session.go:PreSessionSnapshot()`; `hazmat/session.go:beginPreparedSession()`, `runSessionStartupPhases()`; `hazmat/exec_apple_container.go:runAppleContainerExecSession()` |
| `04_version_migration` | `hazmat/init.go` migration dispatch; `hazmat/internal/setup/rollback.go` rollback resource ordering after migration rollback dispatch; `hazmat/migrate.go` migration functions; `hazmat/internal/state/state.go`; `hazmat/state.go` |
| `05_tier3_launch_containment` | `hazmat/internal/runtime/docker/admission.go:PrepareLaunchAdmission()`; `hazmat/sandbox.go:buildSandboxLaunchSpecWithPlan()`, `prepareSandboxLaunchWithPlan()`, `loadHealthySandboxLaunchBackend()`, `dockerSandboxesBackend.PrepareLaunch()`; `hazmat/path_policy.go:isCredentialDenyPath()`; `hazmat/session.go:isWithinDir()` |
| `06_tier2_tier3_effective_policy_equivalence` | `hazmat/session.go:resolveSessionConfig()`; `hazmat/native_session_policy.go:buildNativeSessionPolicy()`; `hazmat/session_policy_sbpl.go:compileDarwinSBPLChecked()`; `hazmat/native_launch.go:agentEnvPairs()`; `hazmat/sandbox.go:prepareSandboxLaunchWithPlan()`, `buildSandboxLaunchSpecWithPlan()`; `hazmat/path_policy.go:isCredentialDenyPath()` |
| `07_session_permission_repairs` | `hazmat/session_mutation.go`; `hazmat/workspace_acl.go`; `hazmat/git_preflight.go`; `hazmat/integration_resolver.go`; `hazmat/session.go`; `hazmat/explain.go` |
| `08_harness_lifecycle` | `hazmat/harnesses/harnesses.go`; `hazmat/harness.go`; `hazmat/internal/harnessruntime/state.go`; `hazmat/internal/harnessruntime/artifact.go`; `hazmat/internal/harnessruntime/uninstall.go`; `hazmat/internal/harnessruntime/install.go`; `hazmat/internal/state/state.go`; `hazmat/state.go`; `hazmat/bootstrap*.go`; `hazmat/config_import*.go`; `hazmat/migrate.go` |
| `09_launch_fd_isolation` | `hazmat/agent_launch.go`; `hazmat/session.go:runPreparedAgentSeatbeltScriptWithUI()`, `runAgentSeatbeltScriptWithPlan()`; `hazmat/cmd/hazmat-launch/main.go` |
| `10_git_ssh_routing` | `hazmat/configmodel/ssh.go:ValidateProjectSSHConfig()`, `ProjectSSHConfig.NormalizedKeys()`, `ValidateProjectSSHProfileRefs()`, `DetectLegacyFlatSSH()`; `hazmat/config.go:runConfigSSHAdd()`, `runConfigSSHRemove()`; `hazmat/git_ssh.go:resolveProjectSSHKeys()`, `prepareGitSSHRuntime()`, `startGitSSHTransportBroker()`, `runGitSSHTransportHelper()`, `selectSessionGitSSHKey()` |
| `11_git_hook_approval` | Repo-local hook approval command surface, `hazmat/internal/hookruntime/commands.go`, snapshot execution helpers, and rollback cleanup under `hazmat/` |
| `12_secret_store_recovery` | `hazmat/harness_auth_runtime.go`; `hazmat/secret_store.go`; `hazmat/internal/credentialruntime/store.go` |
| `13_credential_capability_lifecycle` | `hazmat/credentials/registry.go`; `hazmat/credential_registry.go`; `hazmat/harness_auth_runtime.go`; future credential backend implementations |
| `14_linux_native_launch` | `hazmat/containment/linux`; future Linux native helper implementation |
| `15_beadpost_broker_boundary` | `hazmat/hostbroker/session.go` (contained-agent submitter + dr-owned host broker membrane; real impl behind `beadpost_hostbroker`, fail-closed stub by default) |
| `16_apple_container_launch_containment` | `hazmat/containment/applecontainer/spec.go` (compiler); `hazmat/internal/runtime/applecontainer/runtime.go` (experimental runtime); `hazmat/exec_apple_container.go` (gated exec path); `hazmat/explain_apple_container.go` (preview) |
| `17_service_harness_lifecycle` | `hazmat/internal/serviceharness/lifecycle.go`; `hazmat/proxyruntime/service.go`; future service harness adapter runtime, `hazmat <service-harness>` command surface, readiness/attach/log/cleanup runtime, and service metadata persistence |

---

## Not Yet Formally Modeled

- Exact curated import file contents, conflict-resolution behavior, and merged JSON/file payload semantics
- Concurrent writes to the same host secret while a harness session is running; the current secret-store proof is crash/restart recovery, not multi-writer synchronization
- Integration activation, project pinning, and integration-specific snapshot ignore rules
- Exact `hooks.yaml` parsing behavior, human-readable diff/summary generation, and foreign raw-`git` entrypoints outside Hazmat-managed wrapper paths
- Exact ACL/chmod filesystem walk semantics for session-time permission repairs
- Reworked setup-completion liveness under the current bounded setup/rollback retry model
- Docker Sandbox or microVM runtime internals after the host-side Tier 3 launch boundary
- Concrete Linux native helper syscall behavior, mount propagation, seccomp
  filter contents, Landlock ruleset shape, and runtime behavior after exec
- Concrete Keychain APIs, git credential-helper protocol bytes, SSH agent socket
  behavior, cloud provider APIs, and integration manifest parsing
- OpenHands internals, HTTP/WebSocket payloads, browser automation, Docker or VM
  internals, and live service protocol behavior

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
