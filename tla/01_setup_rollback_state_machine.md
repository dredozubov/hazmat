# Problem 1 — Setup/Rollback State Machine

## Problem Statement

Hazmat setup creates core system resources in a fixed order: agent user, dev
group, home-directory traverse ACL, local snapshot repository, umask,
seatbelt wrapper, command wrappers, pf firewall, DNS blocklist, LaunchDaemon,
launch helper verification, sudoers, default Claude bootstrap, and agent
credentials. Rollback removes the host-managed resources in reverse-safe order.

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
| `hazmat/bootstrap.go` | `runBootstrap()` |
| `hazmat/config_agent.go` | `runConfigAgent()` |
| `hazmat/rollback.go` | `runRollback()`, `rollbackSudoers()`, `rollbackLaunchDaemon()`, `rollbackPfFirewall()`, `rollbackDNSBlocklist()`, `rollbackSeatbelt()`, `rollbackUserExperience()`, `rollbackHomeDirTraverse()`, `rollbackUmask()`, `rollbackLocalRepo()`, `rollbackAgentUser()`, `rollbackDevGroup()` |

## Setup Step Ordering (as implemented)

```
Step  0: setupAgentUser        → agentUser
Step  1: setupDevGroup         → devGroup
Step  2: setupHomeDirTraverse  → homeDirTraverse
Step  3: setupLocalRepo        → localRepo
Step  4: setupHardeningGaps    → umask
Step  5: setupSeatbelt         → seatbelt
Step  6: setupUserExperience   → wrappers
Step  7: setupPfFirewall       → pfAnchor      ← firewall activates
Step  8: setupDNSBlocklist     → dnsBlocklist
Step  9: setupLaunchDaemon     → launchDaemon
Step 10: setupLaunchHelper     → (verify only — fails if absent)
Step 11: setupSudoers          → sudoers       ← agent becomes launchable (AFTER firewall)
Step 12: runBootstrap          → claudeCode
Step 13: runConfigAgent        → credentials
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
- `SetupComplete` — all 14 steps succeeded, return to idle
- `BeginRollback` — start rollback from idle
- `RollbackCore` — remove all non-destructive resources (preserves user, group, workspace)
- `RollbackDestructive` — also removes user and group (models `--delete-user --delete-group`)
- `InstallLaunchHelper` — external action: user builds and installs the helper binary

### Key Design Choices

1. **Step 7 (launchHelper) is verify-only.** It does not create the helper — it
   checks that it exists. If absent, setup MUST fail. This prevents sudoers
   (step 8) from referencing a nonexistent binary.

2. **Rollback is atomic in the model.** The real code runs ~9 rollbackX()
   functions sequentially, but none abort on failure (they warn and continue).
   Modeling them as one atomic step is safe because the real code always
   completes all steps.

3. **Harness/session ergonomics are out of model.** Optional harness-specific
   commands such as `hazmat bootstrap opencode`, curated import flows, and
   session-only stack pack activation are not part of `runInit()` and are
   therefore outside this setup/rollback model.

## What TLC Finds

### Finding 1: Security Window (AgentContained — FIXED)

**Invariant:** `(agentUser ∧ sudoers) ⇒ pfAnchor`

**Original violation:** Setup installed sudoers (step 8) before pf firewall
(step 9). If setup was interrupted between those steps, the agent was
launchable with no network containment.

**Fix applied:** Reordered setup so pf/dns/daemon (steps 7-9) run before
launchHelper verification and sudoers (steps 10-11). The firewall's
`user agent` rules only require the agent user to exist (step 0).

**TLC confirmation:** After fix, AgentContained passes across all 1887 distinct
states. The agent is never launchable without firewall containment in any
reachable state.

### Invariants That Pass

| Invariant | Meaning |
|-----------|---------|
| `NoOrphanedArtifacts` | Destructive rollback leaves no hazmat artifacts |
| `SudoersRequiresHelper` | Sudoers only exists when launch helper is present |
| `AgentDepsRequireUser` | Agent-owned resources require agent user |
| `CanAlwaysReachClean` | System can always return to clean state (liveness) |

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxSetupAttempts` | 2 | Covers: first setup fails, re-run succeeds |
| `MaxRollbackAttempts` | 2 | Covers: rollback after first setup, then after re-setup |

**Confirmed state space:** 3372 states generated, 1887 distinct. Runtime: <1 second.
All 5 safety invariants + liveness pass after the fix.
