# Problem 07 — Session Permission Repairs

## Problem Statement

Hazmat now surfaces persistent host-side changes in the session contract, but
the permission-repair subset still needs a precise modeled contract:

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
| `hazmat/repair.go` | explicit `hazmat repair project-acl-backfill` operator path |
| `hazmat/git_preflight.go` | `collectGitPermissionProblems()`, `ensureGitMetadataHealthy()` |
| `hazmat/integration_resolver.go` | `planHomebrewToolAccessRepair()`, `repairHomebrewToolAccessImpl()` |
| `hazmat/session.go` | `resolvePreparedSession()`, `beginPreparedSession()` |
| `hazmat/explain.go` | preview path for planned host mutations |

## TLA+ Model

The model abstracts the four user-visible launch repair classes:

- `projectACL`
- `traverseACL`
- `gitACL`
- `homebrewMode`

`projectACL` specifically means the bounded startup repair for the project
root and a finite set of likely-mutable existing paths. It does not mean
"recursively chmod every pre-existing file before launch." Historical full-tree
backfill is represented as `projectBackfill` in the model so the boundary is
explicit, but that backfill is not part of the automatic startup mutation set.

It treats the host as a finite permission state:

- each repair class may or may not currently be needed
- full-tree project backfill may independently be needed, but it does not
  drive launch-time mutation planning
- an operator may explicitly run full-tree project backfill while the model
  keeps that action outside the automatic session mutation set
- Homebrew repair has an extra eligibility bit that represents the
  invoker-owned Cellar-root requirement
- the session mode is `native` or `docker`
- the command path is either preview (`hazmat explain`) or a real launch

The model snapshots the repair needs at planning time, then checks:

- what gets planned
- that single-path probes, batched probes, and metadata-validated cache hits all
  represent the same host repair snapshot. This includes both startup ACL
  observations and cached `.git` metadata health: a cached healthy Git result is
  usable only while every required Git metadata path still has the same
  validation metadata. Any missing or stale fingerprint falls back to the fresh
  host probe before planning or post-session repair decisions.
- what preview is allowed to mutate
- what must be fixed before launch can succeed
- what rollback is allowed to remove

This is intentionally a contract model, not a filesystem-syscall model. It
does not attempt to encode the exact `chmod +a` walk semantics or Homebrew
mode-bit edits on concrete paths. The `validatedCache` probe mode abstracts an
implementation cache that may be used only when path identity and metadata prove
the cached ACL-bearing object has not changed; stale or missing validation must
fall back to a fresh host probe before planning. Cache retention is not part of
the safety contract: the implementation may evict unrelated entries to keep
startup bounded, because an evicted entry is simply a cache miss.

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `PlannedRepairsMatchSnapshot` | The planned repair set exactly matches the repair needs visible at planning time |
| `ValidatedCacheRequiresFreshMetadata` | Cached permission observations are used only when their validation metadata is fresh |
| `PreviewIsReadOnly` | `hazmat explain` never applies a host mutation |
| `DockerSkipsNativeACLRepairs` | Docker Sandbox sessions never plan the native-only project/traverse/git ACL repairs |
| `HomebrewRepairRequiresEligibleCellar` | Homebrew repair is planned only when the path is eligible and still blocked |
| `LaunchClearsFatalRepairNeeds` | Launch cannot succeed while fatal repair classes (`gitACL`, eligible Homebrew repair) are still unresolved |
| `RollbackPreservesSessionRepairs` | Core rollback does not revert any already-applied session repair |
| `BackfillIsOutsideStartupPlan` | Historical full-tree project ACL backfill is never planned as an automatic launch repair |

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
- `194,023 states generated`
- `79,098 distinct states found`
- `depth 7`
- `Finished in <1s`

## Interpretation

This spec does not claim that Hazmat proves individual ACL commands correct at
the macOS syscall level. It proves the higher-level state-machine contract the
CLI now presents to users:

- preview is non-mutating
- native and Docker modes plan different repair classes intentionally
- project ACL startup repair is bounded; expensive historical backfill stays
  outside automatic launch planning and is available only through an explicit
  operator command
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
