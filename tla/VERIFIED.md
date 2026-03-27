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
| Key invariants | `CredentialReadDenied`, `ReadDirsNoWrite`, `ProjectDirWritable`, `ReadDirSubsumption` |
| Known violation | `CredentialWriteDenied` — static `.config` allow covers `.config/gcloud` writes |
| Status | **Proved** — credential reads always denied; write exposure documented as design tradeoff |

**What was found:** Credential `file-write*` access is not fully denied.
Two vectors: (a) `ProjectDir = /Users/agent` grants write to all of agent home
including `.ssh`; (b) static `.config` allow covers `.config/gcloud`.

**Assessment:** Design tradeoff — agent needs `.config` write for toolchain
state. Credential deny covers `file-read*` only (exfiltration prevention).
Writing to credential dirs is corruption, not exfiltration.

**Change rules:**
- Do not reorder the sections in `generateSBPL()` — credential denies MUST be
  last. Moving any allow after the denies would break `CredentialReadDenied`.
- Adding new credential paths to the deny list requires adding them to
  `CredPaths` in the TLA+ model and re-running TLC.
- Adding new static allow paths (new `AgentHomeSubs`) requires checking whether
  they cover any credential paths — add to the model and re-verify.

---

## Quick Reference: Spec → Code Mapping

| Spec | Files governed |
|------|---------------|
| `01_setup_rollback_state_machine` | `hazmat/setup.go:runSetup()`, all `setupX()`; `hazmat/rollback.go:runRollback()`, all `rollbackX()` |
| `02_seatbelt_policy_structure` | `hazmat/session.go:generateSBPL()`, `isWithinDir()` |

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
