# Problem 11 — Git Hook Approval

**Status:** proved and implemented boundary for `sandboxing-acjx`, extended
for explicit composed hooksPath ownership in `sandboxing-9ks1`.
This spec is listed in `VERIFIED.md`, wired into `check_suite.sh`, and now
governs the shipped repo-local Git hook approval command surface.

## Problem Statement

Hazmat needs a first-class UX for repo-local Git hooks that preserves the same
trust shape already used for session integrations:

- the repo declares intent in tracked files
- the host records approval in host-owned state
- future activation is gated on that approval

The main attack is not only post-approval mutation of hook files. A contained
agent can rewrite `.git/config` and reroute future host-side Git invocations by
changing `core.hooksPath`. A useful model therefore has to include:

1. repo-declared hook set plus bundle hash
2. host-owned approval record
3. host-owned immutable approved snapshot
4. managed `core.hooksPath`, or an explicitly composed existing hooksPath owner
5. wrapper-mediated host invocation
6. managed or composed dispatcher execution from approved snapshot
7. fallback dispatcher refusal when Git reaches `.git/hooks`
8. uninstall / rollback cleanup

## Governed Boundary

This spec governs the future Hazmat-managed host-side hook activation boundary:

- tracked manifest at `.hazmat/hooks/hooks.yaml`
- tracked repo-local hook bundle under `.hazmat/hooks/`
- approval stored outside the repo, keyed by repo path + bundle hash
- approved snapshot copied into host-owned immutable storage
- Hazmat-installed wrapper validates `core.hooksPath`, snapshot hash, and hook
  layout before invoking real Git
- managed dispatcher executes only approved snapshot bytes
- explicit composed mode may preserve an existing repo-relative hooksPath owner
  such as `.beads/hooks` by installing a Hazmat-managed chain block there
- fallback `.git/hooks/*` dispatcher refuses if Git reaches the default path
- `hazmat hooks uninstall` and `hazmat rollback` remove approval + snapshot +
  installed dispatchers atomically

This boundary now governs the current implementation under:

- `hazmat/hook_manifest.go`
- `hazmat/hook_approval.go`
- `hazmat/hook_runtime.go`
- `hazmat/internal/hookruntime/commands.go`
- `hazmat/hook_cli.go`
- rollback cleanup in `hazmat/rollback.go`

Future changes to that boundary must preserve the properties below.

## What the TLA+ Model Checks

| Invariant | Meaning |
|-----------|---------|
| `ApprovalStateWellFormed` | Approval, snapshot, wrapper, dispatcher, composed, and fallback state stay internally consistent. |
| `ApprovedContentOnly` | Any approved execution runs bytes from the immutable approved snapshot, not live repo bytes. |
| `HooksPathPinned` | Approved execution requires a Hazmat-approved hooksPath mode: managed or explicit composed. |
| `WrapperRefusesReroute` | If the wrapper sees `core.hooksPath` drift away from the approved managed/composed modes, it refuses execution. |
| `ManagedDispatcherRefusesDrift` | Managed dispatcher refuses if repo hash, approved hash, approved hook set, or manifest validity drifts. |
| `ComposedDispatcherRefusesDrift` | Composed dispatcher refuses if repo hash, approval state, or the composed Hazmat block drifts. |
| `FallbackDispatcherOnlyRefuses` | Reaching `.git/hooks` is treated as drift detection, not as an alternate approved execution path. |
| `RollbackClearsHookInstall` | Removing approval also removes snapshot and installed wrapper / dispatcher state. |
| `NoImplicitWidening` | Hook approval does not widen future session network or filesystem policy. |

## Scope Boundary

This model is intentionally narrow. It models Hazmat-managed host-side
entrypoints only:

- the Git wrapper Hazmat installs
- the managed dispatcher path
- the Hazmat-managed block inside an explicitly composed hooksPath owner
- the fallback `.git/hooks` drift detector

It does **not** claim to prove behavior for arbitrary direct invocations of a
foreign `git` binary outside the Hazmat-managed wrapper path. That boundary is
documented rather than hand-waved away.

It also does not prove safety of the external hooksPath owner's own code. In
composed mode, existing owner content such as beads hooks remains external
behavior. The model only proves Hazmat's approved hook execution still comes
from the immutable Hazmat snapshot and that Hazmat refuses when its composed
entrypoint drifts.

## Model Bounds

Default config:

- `HookTypes = {hk_pre_commit, hk_pre_push, hk_commit_msg}`
- `Hashes = {hash_a, hash_b}`
- `NoHash = no_hash`
- `NoHook = no_hook`

Two bundle hashes are enough to witness:

- initial approval
- repo drift after approval
- re-approval
- wrapper refusal when the approved and live hashes differ

Three hook types are enough for the first cut because v1 scope is limited to
`pre-commit`, `pre-push`, and `commit-msg`.

## How to Run

```bash
cd tla
bash run_tlc.sh -workers auto -config MC_GitHookApproval.cfg MC_GitHookApproval.tla
```

This spec is also part of the maintained local suite:

```bash
cd tla
bash check_suite.sh
```

Observed TLC result for the composed hooksPath model, re-run on 2026-06-12:

- `Model checking completed. No error has been found.`
- `520,417,388 states generated`
- `8,601,760 distinct states found`
- `depth 10`
- runtime `5m25s` with 10 workers on the local host
