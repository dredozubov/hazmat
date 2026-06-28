# Problem 1 — Setup/Rollback State Machine

## Problem Statement

Hazmat setup creates core system resources in a fixed order: agent user, dev
group, home-directory traverse ACL, local snapshot repository, hardening
repairs, seatbelt wrapper, command wrappers, pf firewall, DNS blocklist,
LaunchDaemon, launch helper verification, sudoers, default Claude bootstrap,
and agent credentials. Rollback removes the host-managed resources in
reverse-safe order.

The interesting correctness questions are NOT about concurrency (setup is
sequential) but about the **state machine** formed by all possible
setup/interrupt/rollback/re-setup sequences:

1. **Security windows** — are there intermediate states where the agent user is
   launchable (sudoers exists) but uncontained (firewall not yet active)?

2. **Reversibility** — can the system always return to a clean state from any
   intermediate setup point, even after interruption?

3. **Idempotency** — does re-running setup after a partial failure converge to
   fully-configured without creating inconsistencies?

4. **Rollback completeness** — after full rollback, are there any orphaned
   artifacts that reference deleted resources?

5. **Dependency ordering** — are resources that depend on other resources
   always created after their dependencies?

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/init.go` | `runInit()`, `setupAgentUser()`, `setupDevGroup()`, `setupHomeDirTraverse()`, `setupLocalRepo()`, `setupHardeningGaps()`, `setupSeatbelt()`, `setupUserExperience()`, `setupPfFirewall()`, `setupDNSBlocklist()`, `setupLaunchDaemon()`, `setupLaunchHelper()`, `setupSudoers()` |
| `hazmat/sudoers.go` | `maybeSetupOptionalAgentMaintenanceSudoers()`, `installLaunchSudoers()`, `installAgentMaintenanceSudoers()` |
| `hazmat/bootstrap.go` | `runBootstrap()` |
| `hazmat/config_agent.go` | `runConfigAgent()` |
| `hazmat/rollback.go` | `runRollback()`, `rollbackSudoers()`, `rollbackLaunchDaemon()`, `rollbackPfFirewall()`, `rollbackDNSBlocklist()`, `rollbackSeatbelt()`, `rollbackUserExperience()`, `rollbackHomeDirTraverse()`, `rollbackUmask()`, `rollbackLocalRepo()`, `rollbackAgentUser()`, `rollbackDevGroup()` |

## Setup Step Ordering (as implemented)

```
Step  0: setupAgentUser        → agentUser
Step  1: setupDevGroup         → devGroup
Step  2: setupHomeDirTraverse  → homeDirTraverse
Step  3: setupLocalRepo        → localRepo
Step  4: setupHardeningGaps    → umask + hostCredentialModes
Step  5: setupSeatbelt         → seatbelt
Step  6: setupUserExperience   → wrappers (host wrappers, agent env, agent-owned XDG/toolchain parents)
Step  7: setupPfFirewall       → pfAnchor      ← firewall activates
Step  8: setupDNSBlocklist     → dnsBlocklist
Step  9: setupLaunchDaemon     → launchDaemon
Step 10: setupLaunchHelper     → (verify only — fails if absent)
Step 11: setupSudoers          → sudoers       ← narrow launch-helper privilege (AFTER firewall)
Step 12: maybeSetupOptionalAgentMaintenanceSudoers → maintenanceSudoers? (optional, still AFTER firewall)
Step 13: runBootstrap          → claudeCode
Step 14: runConfigAgent        → credentials
```

## TLA+ Model

### Variables

Each resource is a `BOOLEAN` (present/absent). Control variables track the
current phase (`idle`/`setting_up`/`rolling_back`), which setup step is next,
and how many setup/rollback attempts have occurred.

### Actions

- `BeginSetup` — start a setup attempt from idle
- `SetupStepSucceed` — current step succeeds, resource becomes present
- `SetupStepFail` — current step fails, setup aborts (resources from earlier steps remain)
- `SetupComplete` — all 15 steps succeeded, return to idle
- `BeginRollback` — start rollback from idle
- `RollbackCore` — remove all non-destructive resources (preserves user, group, workspace)
- `RollbackDestructive` — also removes user and group (models `--delete-user --delete-group`)
- `InstallLaunchHelper` — external action: user builds and installs the helper binary

### Key Design Choices

1. **Step 10 (launchHelper) is verify-only.** It does not create the helper — it
   checks that it exists. If absent, setup MUST fail. This prevents sudoers
   from referencing a nonexistent binary.

2. **The broader maintenance sudoers rule is modeled as optional.** Step 12 can
   either install `maintenanceSudoers` or skip it. Both branches are valid:
   interactive init can skip it, while `hazmat init --yes` now takes the
   install branch by default. In every case, the rule still appears only after
   firewall containment is already active.

3. **Rollback is atomic in the model.** The real code runs ~9 rollbackX()
   functions sequentially, but none abort on failure (they warn and continue).
   Modeling them as one atomic step is safe because the real code always
   completes all steps.

4. **Host credential mode repairs are persistent hardening.** Step 4 models
   owner-only mode repairs for existing host credential paths separately from
   the managed umask block. That set includes common cloud/tool credentials and
   known AI-agent local state roots such as Amp config/plugins, Devin config
   and auth-adjacent state, external agent roots for Kilo, Kimi, Kiro, Vibe,
   Trae, Pi, Crush, OpenClaw, Qoder, GitHub Copilot CLI,
   CodeWhale/DeepSeek TUI, Grok Build, OpenHands, and Goose
   config/session/log state. Core rollback removes the managed umask block but
   intentionally preserves owner-only credential modes because they are host
   security posture, not a Hazmat artifact.

5. **The wrappers resource owns agent toolchain/XDG parent directories.**
   `setupUserExperience` creates and repairs the parent directories that make
   the agent shell environment usable, including `~/.config`, `~/.local`, and
   child cache/data/bin/npm paths. Those directories are modeled as part of the
   existing `wrappers` resource rather than as a separate setup phase, so
   partial setup and rollback reasoning keeps the same ordering boundary.
   `AgentWritableSetupParentsOwned` pins the ownership contract for that
   abstraction: setup-created parents that an agent writes through must not
   exist after the agent identity they are owned by is gone.

6. **Linux setup reuses the same resource boundary.** The model is currently
   checked with `Platform = "linux"` before Linux privileged lifecycle or
   release artifacts are enabled. Under that interpretation, `pfAnchor` means
   the Linux firewall policy is active, `dnsBlocklist` means the Linux resolver
   / hosts policy is active, and `launchDaemon` means the Linux service-manager
   persistence unit is active. `LinuxPrivilegeRequiresContainment` proves that
   either sudoers rule can exist only after all three Linux containment
   resources are present.

7. **Linux agent-user setup has named resource projections.** The model now
   names the Linux multi-user resources from the two-lane design:
   `LinuxAgentUser`, `LinuxSharedGroup`, `LinuxAgentHome`,
   `LinuxWorkspaceAccess`, `LinuxLaunchHelper`, `LinuxSudoers`,
   `LinuxCgroupRoot`, `LinuxDistroProfile`, and `LinuxToolHome`. These are
   projections over the current setup resource order until `setup/linux` owns
   concrete callbacks. `LinuxAgentUserSetupGraph`,
   `LinuxAgentUserRollbackRevokesPrivilegeFirst`, and
   `LinuxAgentUserDestructiveRollbackBoundary` prove helper/sudoers/cgroup
   ordering, privilege-last setup, privilege-first rollback, and destructive
   rollback boundaries for the future agent-user lane. A shared group may
   outlive the dedicated user only as non-launchable residue after a rollback
   attempt; workspace access, tool-home state, and sudoers privilege must be
   absent in that state.

8. **Harness/session ergonomics are outside this setup model.** Optional
   harness-specific commands such as `hazmat bootstrap opencode`, curated
   import flows, and session-only integration activation are not part of
   `runInit()`. They are modeled separately where applicable and are still
   intentionally outside this setup/rollback state machine.

## What TLC Finds

### Finding 1: Security Window (AgentContained — FIXED)

**Invariant:** `(agentUser ∧ sudoers) ⇒ pfAnchor`

**Original violation:** Setup installed sudoers (step 8) before pf firewall
(step 9). If setup was interrupted between those steps, the agent was
launchable with no network containment.

**Fix applied:** Reordered setup so pf/dns/daemon (steps 7-9) run before
launchHelper verification and sudoers (steps 10-11), and kept the optional
broader maintenance sudoers rule at step 12 behind the same containment gate. The firewall's
`user agent` rules only require the agent user to exist (step 0).

**TLC confirmation:** After fix, `AgentContained` and `CanAlwaysReachClean`
pass across all 35,005 distinct states (65,662 generated). The agent is never
launchable without firewall containment in any reachable state, even when the
optional broader maintenance sudoers rule is present, and the bounded model
still guarantees a path back to a clean state.

### Checked Properties That Pass

| Invariant | Meaning |
|-----------|---------|
| `NoOrphanedArtifacts` | Destructive rollback leaves no hazmat artifacts, including optional maintenance sudoers |
| `LinuxPrivilegeRequiresContainment` | On Linux, sudoers requires firewall, resolver policy, and service persistence |
| `SudoersRequiresHelper` | The narrow launch-helper sudoers rule only exists when launch helper is present |
| `PrivilegeRequiresAgentUser` | Any passwordless sudoers rule requires the agent user |
| `AgentDepsRequireUser` | Agent-owned resources require agent user |
| `AgentWritableSetupParentsOwned` | Agent-writable setup-created parent directories remain tied to the agent identity |
| `LinuxAgentUserSetupGraph` | Linux multi-user setup resources obey identity, helper, cgroup, and sudoers ordering, except non-launchable shared-group residue after rollback |
| `LinuxAgentUserRollbackRevokesPrivilegeFirst` | Linux rollback revokes helper privilege before removing cgroup, service, firewall, resolver, or identity resources |
| `LinuxAgentUserDestructiveRollbackBoundary` | Destructive rollback removes agent-writable setup state before deleting the dedicated agent identity |
| `CanAlwaysReachClean` | System can always return to clean state (liveness) |

### Important Scope Boundary

The bounded-retry model does **not** currently prove `SetupEventuallyCompletes`.
With `MaxSetupAttempts = 2` and `MaxRollbackAttempts = 2`, TLC can exhaust both
attempt counters after repeated failures and stutter in a partially configured
idle state. The verified liveness claim for this model is recoverable clean
exit, not eventual successful completion after arbitrary bounded failures.

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxSetupAttempts` | 2 | Covers: first setup fails, re-run succeeds |
| `MaxRollbackAttempts` | 2 | Covers: rollback after first setup, then after re-setup |

**Confirmed state space:** 65,662 states generated, 35,005 distinct. Runtime: ~3 seconds
with `-lncheck final`. All checked safety invariants plus `CanAlwaysReachClean`
pass after the fix.

**2026-06-14 Linux setup interpretation:** `MC_SetupRollback.cfg` is now checked
with `Platform = "linux"` and includes `LinuxPrivilegeRequiresContainment`.
Under that interpretation, Linux sudoers privilege may exist only when the
firewall policy, resolver / hosts policy, and service-manager persistence
resources are all active. Re-run with TLC using `-lncheck final`: "No error has
been found" across 65,662 generated states, 35,005 distinct states, depth 56.
This satisfies the model-first gate for future Linux setup/rollback design, but
does not enable Linux privileged lifecycle, artifact install, or release
packaging without the corresponding backend implementation and disposable-host
tests.

**2026-06-28 Linux agent-user setup graph:** `MC_SetupRollback.cfg` now also
checks the named Linux multi-user resource projections from the two-lane design:
agent identity, shared group, workspace access, launch helper, sudoers,
cgroup/service-manager root, distro profile, and tool-home state. TLC re-run
with `-lncheck final` reported "No error has been found" across 65,662
generated states, 35,005 distinct states, depth 56. The graph allows a shared
group without the dedicated user only as unprivileged rollback residue; no
workspace access, tool-home state, or sudoers privilege may remain there.
