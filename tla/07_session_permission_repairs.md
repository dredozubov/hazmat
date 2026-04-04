# Problem 07 — Session Permission Repairs

## Problem Statement

Hazmat now surfaces persistent host-side permission mutations in the session
contract under `Host permission changes`, but that only helps if the contract
itself is precise:

1. `hazmat explain` must preview the same repair classes a real session may
   execute without mutating the host.
2. Native sessions may plan project ACL repair, exposed-directory traverse ACL
   repair, and `.git` metadata ACL repair; Docker Sandbox sessions must not
   silently inherit those native-only repairs.
3. Homebrew-backed integration resolution may plan a bounded toolchain repair
   in either mode, but only for an eligible Homebrew Cellar path.
4. Core rollback does **not** revert these session-time repairs. That
   persistence is part of the current product contract and should be proved,
   not left as prose.

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/session_mutation.go` | `buildNativeSessionMutationPlan()`, `mergeSessionMutationPlans()`, `executeSessionMutationPlan()` |
| `hazmat/workspace_acl.go` | `projectNeedsACLRepair()`, `pendingAgentTraverseTargets()`, `ensureProjectWritable()`, `ensureAgentCanTraverseExposedDirs()` |
| `hazmat/git_preflight.go` | `collectGitPermissionProblems()`, `ensureGitMetadataHealthy()` |
| `hazmat/integration_resolver.go` | `planHomebrewToolAccessRepair()`, `repairHomebrewToolAccessImpl()` |
| `hazmat/session.go` | `resolvePreparedSession()`, `beginPreparedSession()` |
| `hazmat/explain.go` | preview path for planned host mutations |

## TLA+ Model

The model abstracts the four user-visible repair classes:

- `projectACL`
- `traverseACL`
- `gitACL`
- `homebrewMode`

It treats the host as a finite permission state:

- each repair class may or may not currently be needed
- Homebrew repair has an extra eligibility bit that represents the
  invoker-owned Cellar-root requirement
- the session mode is `native` or `docker`
- the command path is either preview (`hazmat explain`) or a real launch

The model snapshots the repair needs at planning time, then checks:

- what gets planned
- what preview is allowed to mutate
- what must be fixed before launch can succeed
- what rollback is allowed to remove

This is intentionally a contract model, not a filesystem-syscall model. It
does not attempt to encode the exact `chmod +a` walk semantics or Homebrew
mode-bit edits on concrete paths.

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `PlannedRepairsMatchSnapshot` | The planned repair set exactly matches the repair needs visible at planning time |
| `PreviewIsReadOnly` | `hazmat explain` never applies a host mutation |
| `DockerSkipsNativeACLRepairs` | Docker Sandbox sessions never plan the native-only project/traverse/git ACL repairs |
| `HomebrewRepairRequiresEligibleCellar` | Homebrew repair is planned only when the path is eligible and still blocked |
| `LaunchClearsFatalRepairNeeds` | Launch cannot succeed while fatal repair classes (`gitACL`, eligible Homebrew repair) are still unresolved |
| `RollbackPreservesSessionRepairs` | Core rollback does not revert any already-applied session repair |

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers auto \
  -config MC_SessionPermissionRepairs.cfg \
  MC_SessionPermissionRepairs.tla
```

Observed result:

- `Model checking completed. No error has been found.`
- `15,663 states generated`
- `6,634 distinct states found`
- `depth 7`
- `Finished in 2s`

## Interpretation

This spec does not claim that Hazmat proves individual ACL commands correct at
the macOS syscall level. It proves the higher-level state-machine contract the
CLI now presents to users:

- preview is non-mutating
- native and Docker modes plan different repair classes intentionally
- Homebrew repair is not a generic escape hatch
- rollback leaves these session-time permission changes in place

That closes the previous verification gap where this behavior was visible in
the product but only documented, not modeled.

## Change Rules

1. **Adding a new host permission repair class**: add a new mutation kind to
   `MC_SessionPermissionRepairs.tla`, define when it is planned, and decide
   whether launch is allowed to proceed if it remains unresolved.
2. **Changing native vs Docker planning rules**: update this spec before code.
   The current proof intentionally keeps project/traverse/git ACL repair
   native-only while allowing Homebrew repair in either mode.
3. **Changing rollback scope for these repairs**: update this spec first. The
   current proof bar is explicit non-reversion.
4. **Changing preview semantics**: if `hazmat explain` ever starts applying or
   probing mutations differently from real launch planning, update this model
   and its invariants first.
