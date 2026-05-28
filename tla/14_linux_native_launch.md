# Problem 14 — Linux Native Launch Ordering

## Problem Statement

Hazmat's Linux native backend is still experimental and plan-only. Before any
helper is implemented, the launch ordering needs a checked contract for the
parts that are easy to get wrong:

- validate the launch spec before side effects
- close inherited file descriptors before creating namespaces
- create user/mount/network namespaces before mounts and network policy
- apply mounts before reporting the session as ready
- enforce `--network none` before metadata or exec
- drop privileges and set `no_new_privs` before Landlock/seccomp decisions
- emit metadata only after enforcement is complete
- exec only after metadata has described the enforced state

The model treats host feature availability as nondeterministic. User namespace,
mount namespace, network namespace, Landlock, and seccomp support may each be
present or absent. The launch spec may also explicitly allow a Landlock/seccomp
capability gap; otherwise the helper must fail closed.

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/containment/linux` | Existing plan-only launch spec compiler |
| future Linux native helper | Spec validation, namespace/mount/network setup, privilege drop, LSM/seccomp, metadata, exec |

No runtime Linux helper exists yet. This spec is a design contract for the
implementation bead that follows it.

## TLA+ Model

### Abstract State

`MC_LinuxNativeLaunch` uses one record variable with three classes of fields:

- launch-spec inputs: spec validity, mount plan validity, network mode, and
  whether the spec accepts Landlock/seccomp gaps
- host capability inputs: user namespace, mount namespace, network namespace,
  Landlock, and seccomp availability
- enforcement facts: fd cleanup, namespace creation, mounts, network policy,
  privilege drop, `no_new_privs`, Landlock/seccomp applied-or-skipped state,
  metadata emission, exec, and failure

The model has 1024 initial states, covering every combination of those inputs.

### Launch Stages

The state machine is deliberately linear:

1. `start`
2. `validated`
3. `fds_closed`
4. `namespaces`
5. `mounts`
6. `network`
7. `privileges`
8. `landlock`
9. `seccomp`
10. `metadata`
11. `exec`

Any invalid spec, missing namespace, or unaccepted Landlock/seccomp gap moves
to `failed`, where metadata and exec remain false.

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `SpecValidatedBeforeSideEffects` | No fd, namespace, mount, network, privilege, metadata, or exec fact can appear before spec validation |
| `FDsClosedBeforeNamespaces` | Namespace/mount/metadata/exec cannot happen while inherited fds are still live |
| `MountsAfterNamespaces` | Mount application requires user and mount namespaces |
| `NetworkNoneDeniedBeforeMetadata` | `--network none` sessions have a created network namespace and deny flag before metadata/exec |
| `PrivilegeDropBeforeLSMAndMetadata` | Landlock/seccomp decisions, metadata, and exec require dropped privileges and `no_new_privs` |
| `MetadataAfterEnforcement` | Metadata is emitted only after validation, fd cleanup, namespaces, mounts, network, privilege drop, and LSM/seccomp decisions |
| `ExecAfterMetadata` | Exec cannot precede metadata |
| `NoExecOnFailure` | Failed launches never emit metadata or exec |
| `NoExecWithMissingRequiredFeature` | Successful exec requires all required capabilities or an explicit accepted LSM/seccomp gap |

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers auto \
  -config MC_LinuxNativeLaunch.cfg \
  MC_LinuxNativeLaunch.tla
```

Observed result on 2026-05-28:

- `Model checking completed. No error has been found.`
- `3866 states generated`
- `2842 distinct states found`
- `depth 11`
- `Finished in 00s`

## Implementation Guidance

The future Linux helper should mirror this order in code. In particular:

- parse and validate the launch spec before touching namespaces or mounts
- close inherited descriptors before namespace creation
- treat missing user/mount/network namespace support as a hard launch failure
- for `--network none`, do not emit metadata until the network denial state is
  enforced
- set `no_new_privs` before Landlock/seccomp decisions
- if Landlock or seccomp is unsupported, continue only when the launch spec
  explicitly accepts that capability gap and metadata records it
- emit metadata after enforcement, not as an intent preview
- never exec from a failed state

This proof does not model concrete kernel APIs, syscall return values, mount
propagation details, seccomp filter contents, or Landlock ruleset shape. Those
must be covered by implementation tests and Linux VM smoke tests once the helper
exists.
