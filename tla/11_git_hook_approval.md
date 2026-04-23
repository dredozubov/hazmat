# Problem 11 — Git Hook Approval

**Status:** proved and implemented boundary for `sandboxing-acjx`.
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
4. managed `core.hooksPath`
5. wrapper-mediated host invocation
6. managed dispatcher execution from approved snapshot
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
- fallback `.git/hooks/*` dispatcher refuses if Git reaches the default path
- `hazmat hooks uninstall` and `hazmat rollback` remove approval + snapshot +
  installed dispatchers atomically

This boundary now governs the current implementation under:

- `hazmat/hook_manifest.go`
- `hazmat/hook_approval.go`
- `hazmat/hook_runtime.go`
- `hazmat/hook_cli.go`
- rollback cleanup in `hazmat/rollback.go`

Future changes to that boundary must preserve the properties below.

## What the TLA+ Model Checks

| Invariant | Meaning |
|-----------|---------|
| `ApprovedContentOnly` | Any approved execution runs bytes from the immutable approved snapshot, not live repo bytes. |
| `HooksPathPinned` | Approved execution requires the managed `core.hooksPath`. |
| `WrapperRefusesReroute` | If the wrapper sees `core.hooksPath` drift away from the managed value, it refuses execution. |
| `ManagedDispatcherRefusesDrift` | Managed dispatcher refuses if repo hash, approved hash, approved hook set, or manifest validity drifts. |
| `FallbackDispatcherOnlyRefuses` | Reaching `.git/hooks` is treated as drift detection, not as an alternate approved execution path. |
| `RollbackClearsHookInstall` | Removing approval also removes snapshot and installed wrapper / dispatcher state. |
| `NoImplicitWidening` | Hook approval does not widen future session network or filesystem policy. |

## Scope Boundary

This model is intentionally narrow. It models Hazmat-managed host-side
entrypoints only:

- the Git wrapper Hazmat installs
- the managed dispatcher path
- the fallback `.git/hooks` drift detector

It does **not** claim to prove behavior for arbitrary direct invocations of a
foreign `git` binary outside the Hazmat-managed wrapper path. That boundary is
documented rather than hand-waved away.

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

Observed TLC result for the promoted model:

- `Model checking completed. No error has been found.`
- `127,229,656 states generated`
- `2,179,200 distinct states found`
- `depth 9`
- runtime around `1-4m` depending on worker count and host
