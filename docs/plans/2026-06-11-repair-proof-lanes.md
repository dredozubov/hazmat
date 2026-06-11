# Diagnostic Repair Proof Lanes

`hazmat check` and plain `hazmat doctor` may plan repairs without mutation. Any
future mutation path (`doctor --fix`, init reconciliation, rollback cleanup, or
session repair reuse) must declare a proof lane before implementation.

TLA+ covers safety and ordering. It does not prove that the UX converges on a
real machine, so executable dirty-state tests remain mandatory for smoothness.

## Lanes

| Lane | Use |
| --- | --- |
| `tla.MC_SetupRollback` | setup-owned resources: agent account, shell block, workspace baseline, pf, DNS, LaunchDaemon, launch helper, sudoers, rollback ordering |
| `tla.MC_SessionPermissionRepairs` | session-time host permission repairs: project/traverse/git ACLs and Homebrew permission repair; extend before adding a new permission-repair class |
| `tla.MC_CredentialCapabilityLifecycle` | credential registry behavior: support status, backend class, delivery mode, adapter-required denial, session grant cleanup |
| `tla.MC_SecretStoreRecovery` | host-owned file-backed secret recovery, conflict preservation, temporary agent-side materialization/removal |
| `tests.dirty-state-convergence` | dirty host fixtures that prove check/doctor/init stop recommending the same opaque action after repair attempts |
| `tests.verify-after-action` | executor tests that rerun the typed verification after each applied action and classify remaining drift |
| `tests.guarded-real-host-smoke` | opt-in host smoke tests for behavior fake fixtures cannot model cheaply, especially pf, hosts, launchd, and ACL behavior |
| `tests.classification` | optional, manual-external, unsupported, and informational findings stay non-executable |
| `tests.secret-scan` | credential repairs must prove no raw secret values leak into output, docs, fixtures, or tracked files |

## Current Action Mapping

| Repair action | Proof lane before mutation | Convergence lane |
| --- | --- | --- |
| `repair.workspace.setgid` | `MC_SetupRollback` | unit, dirty-state, verify-after-action |
| `repair.workspace.access` | `MC_SetupRollback` and `MC_SessionPermissionRepairs` | unit, dirty-state, verify-after-action |
| `repair.agent-home.permissions` | `MC_SetupRollback` | unit, dirty-state, verify-after-action |
| `repair.agent-shell.umask` | `MC_SetupRollback` | unit, dirty-state, verify-after-action |
| `repair.network.pf` | `MC_SetupRollback` | unit, dirty-state, guarded real-host smoke, verify-after-action |
| `repair.network.dns-blocklist` | `MC_SetupRollback` | unit, dirty-state, guarded real-host smoke, verify-after-action |
| `repair.network.persistence` | `MC_SetupRollback` | unit, dirty-state, guarded real-host smoke, verify-after-action |
| `repair.credential.claude-state` | `MC_CredentialCapabilityLifecycle` and `MC_SecretStoreRecovery` | unit, dirty-state, secret-scan, verify-after-action |
| `repair.credential.cloud-secret-key` | `MC_CredentialCapabilityLifecycle` and `MC_SecretStoreRecovery` | unit, dirty-state, secret-scan, verify-after-action |
| `repair.credential.residue` | `MC_CredentialCapabilityLifecycle` and `MC_SecretStoreRecovery` | unit, dirty-state, secret-scan, verify-after-action |
| `repair.claude.project-permissions` | `MC_SessionPermissionRepairs`; extend the model if this is treated as a new repair class rather than existing project/traverse ACL repair | unit, dirty-state, verify-after-action |

## Rules

New executable repair actions must update `diagnosticRepairActionDefinitions`
with proof lanes and proof notes. Validation fails when either is missing.

Adding a persistent setup or rollback mutation starts with the relevant TLA+
model and design note, then TLC, then implementation. Adding a session-time host
permission repair class starts with `MC_SessionPermissionRepairs`. Credential
repairs that change delivery, backend, residue recovery, or host-store ownership
start with the credential lifecycle and secret-store recovery models.

Dirty-state convergence tests are not optional even when TLA passes. They are
the guard against the observed bug class: init or doctor applies something, then
check recommends the same generic setup action again.
