# Problem 12 — Secret Store Crash Recovery

**Status:** proved and implemented boundary for `sandboxing-49ys`.
This spec models the host-owned harness secret store and the crash/restart
contract around temporary `/Users/agent` auth material.

## Problem Statement

Hazmat now keeps durable harness auth under `~/.hazmat/secrets/` and
materializes harness-specific files into `/Users/agent` only for the matching
session. That creates a crash-recovery state machine:

1. a previous launch may have copied host auth into `/Users/agent`
2. the harness may refresh or create auth while the session runs
3. Hazmat may crash before harvest, after harvest, or before removing the
   runtime copy
4. the next launch must recover without losing the newest known auth value and
   without starting a new session with unrelated leftover agent-side secrets

This is exactly the kind of interrupted-operation problem TLC is good at:
the individual file operations are simple, but their crash interleavings are
easy to reason about incorrectly.

## Governed Boundary

This model governs the current content-level contract for file-backed harness
auth artifacts:

- startup recovery scans the selected harness auth artifacts
- if only an agent-side copy exists, Hazmat promotes it into
  `~/.hazmat/secrets/` and removes the agent copy
- if host and agent copies match, Hazmat removes the agent copy
- if host and agent copies differ, Hazmat archives the previous host copy under
  a host-owned `.conflicts` directory, promotes the agent copy, then removes
  the agent copy
- session materialization copies the host value into `/Users/agent` only after
  startup recovery has completed
- session harvest copies refreshed agent auth back into the host store and
  removes the agent copy
- a crash can happen at any phase, and the next launch restarts recovery

The implementation covered by this model is in:

- `hazmat/harness_auth_runtime.go`
- `hazmat/secret_store.go`

## What the TLA+ Model Checks

| Invariant | Meaning |
|-----------|---------|
| `LatestValueNeverSilentlyLost` | A credential value known to be latest is always still present in the host store, agent residue, or host-owned conflict archive. |
| `CleanRecoveredStateHasNoAgentResidue` | After recovery reaches an idle clean state, no modeled auth artifact remains under `/Users/agent`. |
| `CleanRecoveredStateKeepsLatestHostOwned` | After recovery reaches an idle clean state, the latest known value is host-owned: primary store or conflict archive, not agent-only residue. |
| `NoCrossHarnessAgentExposure` | During an active session, only the selected harness may have materialized agent-side auth. |
| `LaunchOnlyAfterRecovery` | Materialization, running, harvest, and removal only occur after startup recovery has completed. |
| `IdleClearsSessionBaseline` | The materialization baseline used for harvest conflict checks cannot leak across sessions. |

## Scope Boundary

This proof is intentionally about crash/restart safety, not arbitrary
concurrent host mutation.

It does **not** prove:

- exact JSON merge semantics for Claude state
- concrete filesystem permissions beyond the abstract host/agent ownership
  split
- Keychain-backed auth behavior
- concurrent writes to the same host secret while a session is running
- freshness ordering between two different credential values without metadata

The important consequence of the last point: if we later need to prove
concurrent host-store edits while a harness session is active, content equality
is not enough. The model would need a revision/epoch field or equivalent
metadata so harvest can distinguish "unchanged baseline" from "host rewrote the
same value during the session."

## Model Bounds

Default config:

- `Harnesses = {codex, claude}`
- `Versions = {v1, v2}`
- `NoSecret = no_secret`
- `NoHarness = no_harness`

Two harnesses are enough to check cross-harness exposure. Two credential values
are enough to witness host/agent divergence, token refresh, archive-before-
overwrite recovery, and repeated crash/restart paths.

## How to Run

```bash
cd tla
bash run_tlc.sh -workers auto -config MC_SecretStoreRecovery.cfg MC_SecretStoreRecovery.tla
```

This spec is also part of the maintained local suite:

```bash
cd tla
bash check_suite.sh
```

Observed TLC result for the promoted model:

- `Model checking completed. No error has been found.`
- `34,723 states generated`
- `9,238 distinct states found`
- `depth 28`
- runtime under 1s on the local 10-worker run

## Change Rules

1. Changes to startup migration/recovery of harness auth artifacts must update
   this model first and re-run TLC.
2. Changes to when `/Users/agent` auth files are materialized or removed must
   preserve `LaunchOnlyAfterRecovery`, `CleanRecoveredStateHasNoAgentResidue`,
   and `NoCrossHarnessAgentExposure`.
3. Any path that can overwrite a host-owned auth file with agent-side auth must
   either prove the host value is the expected session baseline or archive the
   divergent host value first.
4. Adding concurrent host-store mutation guarantees requires adding explicit
   revision metadata to the model before claiming the stronger property.
