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
| Status | **Fixed** — pf/dns/daemon now run before sudoers; AgentContained proved |

**What was found:** Setup originally installed sudoers (step 8) before pf
firewall (step 9). If setup was interrupted between those steps, the agent was
launchable via `sudo -u agent` with no firewall containment.

**Fix applied:** Reordered setup so `setupPfFirewall`, `setupDNSBlocklist`, and
`setupLaunchDaemon` run before `setupLaunchHelper` and `setupSudoers`. The
firewall's `user agent` rules only require the agent user to exist (step 0),
not sudoers. `AgentContained` now passes TLC (1887 distinct states, <1s).

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

## Quick Reference: Spec → Code Mapping

| Spec | Files governed |
|------|---------------|
| `01_setup_rollback_state_machine` | `hazmat/setup.go:runSetup()`, all `setupX()`; `hazmat/rollback.go:runRollback()`, all `rollbackX()` |

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
