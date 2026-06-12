# Core Session Extraction Design

**Date:** 2026-06-12
**Status:** design for model-first implementation
**Design bead:** `sandboxing-rluw`
**Parent bead:** `sandboxing-vh9q`
**Related docs:** [package split roadmap](2026-06-03-package-split-implementation-roadmap.md),
[package split architecture](2026-06-02-package-split-architecture.md),
[TLA+ verified areas](../../tla/VERIFIED.md)

## Purpose

The package split has already moved many reusable pieces out of `package main`:
`sessionrequest`, `sessionplanner`, `sessioncontract`, `sessionbackend`,
`containment/darwin`, `containment/docker`, `internal/runtime/*`, and
`internal/backupruntime` all exist. The remaining problem is not lack of
packages. It is that `package main` still owns the core session call order and
the compatibility shims that connect those packages:

- `resolveSessionConfig()`
- `generateSBPL()` / `generateSBPLChecked()`
- `beginPreparedSession()` and `preSessionSnapshot()` ordering
- Tier-2/Tier-3 backend planning
- `buildSandboxLaunchSpec()` / `buildSandboxLaunchSpecWithPlan()`
- `prepareSandboxLaunchWithPlan()`

This design scopes the next extraction path without changing behavior.

## Non-Goals

- No session behavior changes.
- No setup, rollback, credential-delivery, or harness-lifecycle movement.
- No remote runtime execution.
- No deletion of package-main shims until all equivalence gates pass.
- No privileged or live harness validation as part of the default design gate.

## Current State

The residual shims are smaller than the original audit described:

- `resolveSessionConfig()` already routes through `sessionrequest` and typed
  `pathpolicy` constructors.
- `generateSBPL()` already compiles a backend-neutral native policy through the
  Darwin compiler package.
- `buildSandboxLaunchSpecWithPlan()` already compiles the Docker launch spec
  through `containment/docker`.
- `preSessionSnapshot()` already delegates to
  `internal/backupruntime.PreSessionSnapshot()`.
- `buildSessionPlanForHostFacts()` already feeds `sessionplanner`.
- `internal/runtime.Select()` already records the selected runtime package.

The remaining extraction should therefore move ownership of orchestration and
artifact preparation, not re-create already-completed package splits.

## Governing Specs

| Surface | Spec gate |
| --- | --- |
| Native SBPL section order, credential deny floor, session-home grants, network-none grants | `MC_SeatbeltPolicy` |
| Snapshot-before-launch ordering and repo-before-snapshot behavior | `MC_BackupSafety` |
| Docker Sandbox mount filtering, backend validation, approval, policy before launch | `MC_Tier3LaunchContainment` |
| Shared Tier-2/Tier-3 rejected inputs and canonical containment equivalence | `MC_TierPolicyEquivalence` |
| Native helper fd inheritance and launch helper precondition | `MC_LaunchFDIsolation` |

Any slice that changes a governed behavior must update the model first. Pure
movement may re-run the existing model, but the close reason must say why the
model did not need a semantic update.

## Extraction Sequence

### Slice 1: Session Start Order Plan

Add a side-effect-free session start order planner, most likely under
`internal/runtime` or a narrow `internal/sessionflow` package. It should accept
only primitive mode flags and return an ordered list of phases:

- render contract
- apply host mutation plan before snapshot when required
- take pre-session snapshot
- apply host mutation plan after snapshot when required
- continue to runtime launch

The package must not call `snapshotProject`, execute mutation plans, print
output, run commands, or inspect the host. `beginPreparedSession()` remains the
effectful compatibility wrapper and executes the returned phases.

Acceptance gates:

- Table tests for native, native with preflight-before-snapshot, Docker
  Sandbox, and skip-snapshot paths.
- Existing `beginPreparedSession()` behavior tests remain green or are added
  with injected fake actions.
- `MC_BackupSafety` re-run. If the order plan changes the modeled ordering,
  update the model first.

### Slice 2: Native Policy Artifact Preparation

Move native policy artifact preparation behind a runtime-facing API while
keeping `generateSBPL()` and `generateSBPLChecked()` as package-main shims for
tests and compatibility. The new API should consume a validated
`containment.Contract` or the existing native policy value, not raw JSON.

The first movement target is policy artifact preparation and cleanup, not the
whole command launch. `defaultRunAgentSeatbeltScriptWithPlan()` can keep the
sudo command wiring until launch fd tests and status bar behavior are covered
from the new package boundary.

Acceptance gates:

- SBPL golden fixtures are byte-identical unless the TLA model is updated first.
- `generateSBPL()` and `generateSBPLChecked()` tests stay green.
- `MC_SeatbeltPolicy` and `MC_TierPolicyEquivalence` re-run.
- `MC_LaunchFDIsolation` re-run if command construction, policy file lifetime,
  launch helper argv, or fd handling moves.

### Slice 3: Docker Launch Admission Facade

Move Docker launch admission from `sandbox.go` toward `internal/runtime/docker`
in a facade that preserves the exact order:

1. reject integration env passthrough
2. probe and load a healthy backend
3. select backend adapter
4. compile launch spec
5. validate launch compatibility
6. verify approval
7. prepare the sandbox and apply policy
8. record the managed sandbox

`buildSandboxLaunchSpec()` and `buildSandboxLaunchSpecWithPlan()` can remain
package-main shims until the compiler input and legacy tests are fully routed
through the new facade.

Acceptance gates:

- Existing `sandbox_test.go` coverage for credential-zone rejection, mount
  filtering, approval, and compatibility stays green.
- Add fake-probe runtime tests that assert the admission order above.
- `MC_Tier3LaunchContainment` and `MC_TierPolicyEquivalence` re-run.

### Slice 4: Snapshot Trigger Relocation

After Slice 1 owns the session start order, move the `preSessionSnapshot()`
compatibility wrapper or replace it with direct calls to
`internal/backupruntime.PreSessionSnapshot()` from the order executor.

This is intentionally after the order planner exists. Moving the wrapper before
the order is explicit would make it too easy to hide a launch-order change in a
package move.

Acceptance gates:

- Tests prove snapshot is attempted before runtime launch for native and Docker
  sessions.
- Tests prove skip-snapshot still launches and still reports the skipped phase
  consistently.
- `MC_BackupSafety` re-run.

### Slice 5: Shim Retirement

Only after the above slices are merged should `package main` shims shrink.
Remove or demote shims one at a time:

- `generateSBPL()` / `generateSBPLChecked()`
- `buildSandboxLaunchSpec()` / `buildSandboxLaunchSpecWithPlan()`
- `preSessionSnapshot()`
- `buildSessionBackendPlan()` helpers, if call sites can use
  `sessionplanner`/`sessionbackend` directly

Each deletion must include import-boundary guard updates, tests for all public
callers that used the shim, and a docs update to `tla/VERIFIED.md` if the
governed-code map changes.

## Default Verification

Every implementation slice should run:

- `git diff --check`
- relevant package tests for the moved package and its old package-main tests
- golden tests for SBPL, planner, backend, and explain output when touched
- `bash proof_ownership_check.sh` from `tla`
- `bash trace_artifact_check.sh` from `tla`
- the owning TLC specs listed in the slice

The full `go test ./...` suite is the expected local gate for code movement.
Live helper-backed harness smokes, `hazmat check`, and push hooks that invoke
live Hazmat paths are sudo-adjacent and require explicit user approval.

## Bead Plan

The parent `sandboxing-vh9q` should stay open until code movement lands. This
design bead closes after this note is committed. Follow-up implementation beads
should be children of `sandboxing-vh9q` and depend on this design bead:

1. `sandboxing-mknb` - add the side-effect-free session start order plan.
2. `sandboxing-whm8` - move native policy artifact preparation behind the
   runtime facade.
3. `sandboxing-vpze` - move Docker launch admission behind the Docker runtime
   facade.
4. `sandboxing-77t4` - relocate the snapshot trigger after the order planner is
   in place.
5. `sandboxing-hw4m` - retire package-main compatibility shims one at a time.

This order keeps the first code slice small and makes the backup ordering proof
visible before any effectful runtime movement.
