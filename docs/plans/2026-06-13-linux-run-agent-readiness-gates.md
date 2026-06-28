# Linux Run-Agent Provider Readiness Gates

**Date:** 2026-06-13
**Bead:** `sandboxing-vhqs`
**Status:** readiness gates, not implementation approval

## Purpose

The future Linux run-agent provider must satisfy the same machine-facing
contract as Darwin native execution before it becomes user-facing. The goal is
not exact syscall parity with macOS. The goal is that a caller can rely on the
same result semantics: planned authority, enforced launch, raw harness streams,
metadata emitted only after containment, and honest capability gaps when Linux
cannot enforce a requested contract.

These gates translate the existing Linux backend architecture and
`MC_LinuxNativeLaunch` proof into concrete release criteria.

For the identity/setup split, see
[Linux Support Two-Lane Design](2026-06-27-linux-support-two-lane-design.md).
For the reusable core versus provider-layer boundary, see
[Reusable Runtime Core And Experimental User Isolation Design](2026-06-27-reusable-runtime-core-user-isolation-design.md).

## Gate Summary

| Gate | Owner surface | Required evidence | Blocks support? |
|------|---------------|-------------------|-----------------|
| Backend-neutral request and plan | `sessionrequest`, `sessionplanner`, `sessioncontract`, `containment` | package tests proving Linux consumes the same contract fields as Darwin/Docker planning | Yes |
| Linux launch spec compiler | `hazmat/containment/linux` | unit tests for project grants, read-only grants, write grants, temp, agent home, network, and capability gaps | Yes |
| Metadata sidecar parity | future Linux runner/helper and run-agent result writer | fake-helper tests for planned, launched, contained, exited, failed, and cancelled phases; malformed/missing metadata tests | Yes |
| Raw stdout/stderr contract | run-agent facade and fake harness | e2e fake harness test proving stdout/stderr contain only harness bytes in raw mode | Yes |
| Launch helper strategy | Linux native helper design and setup model | explicit `linux.helper_strategy` value (`root-helper` or `rootless-userns`); no silent fallback | Yes |
| Network default/none behavior | Linux launch spec and helper | tests for default as declared capability, `none` enforced before metadata, unsupported network modes as gaps | Yes |
| Capability gap vocabulary | `sessionbackend`, explain JSON/text, docs | golden tests for missing user namespace, mount namespace, network namespace, Landlock, seccomp, cgroup, and setup support | Yes for launch; gaps may be surfaced in plan-only |
| Distro probe API | `platform/linux` | hermetic parser tests plus distro fixture table for kernel, namespace, Landlock, seccomp, cgroup v2, unprivileged userns | Blocks production; plan-only may report unknown |
| Setup/rollback model boundary | future `setup/linux` | extend `MC_SetupRollback` from its platform privilege requires containment starting point before user creation, sudoers, helper install, cgroup delegation, or persistent state mutation | Yes for persistent setup |
| Linux smoke target | scripts/CI/manual VM docs | containerized plan-only smoke, Linux compile-only check, and manual VM smoke before production claim | Yes |

## Contract Gates

### Backend-Neutral Plan

Linux must consume the same authority contract that Darwin and Docker planning
consume. A Linux provider is not ready if it accepts ad hoc path/env/network
inputs that bypass `containment.Contract`, `pathpolicy`, credential-deny
validation, or session metadata.

Required tests:

- Linux plan includes project read-write authority.
- Read-only grants stay read-only in the launch spec.
- Write grants are explicit and do not weaken credential deny zones.
- Agent-home and temp policy are represented as structured fields, not string
  interpolation in a shell script.
- Unsupported capabilities appear as structured gaps in explain output.

### Launch Spec Compiler

The Linux compiler may initially be plan-only. It still needs to reject invalid
contracts before any helper exists.

Required tests:

- reject relative paths and empty roots;
- reject project/read/write roots that overlap credential deny zones;
- preserve network mode and process policy in the compiled spec;
- classify unsupported Landlock/seccomp/cgroup requirements as capability gaps;
- keep spec rendering deterministic for golden tests.

## Execution Gates

### Metadata Sidecar Parity

Run-agent stdout and stderr must remain harness-owned. Linux launch facts must
arrive through a side channel, not by printing control JSON into raw streams.

Required result phases:

- `planned`: request accepted and launch spec/result paths created;
- `launched`: helper accepted the spec and started enforcement;
- `contained`: helper emitted metadata after enforcement completed;
- `exited`: harness process exited with status or signal;
- `failed`: helper or setup failed before exec;
- `cancelled`: caller cancellation reached cleanup.

Required tests:

- missing metadata sidecar fails closed;
- malformed metadata fails closed;
- metadata emitted before enforcement is rejected by the result merger;
- helper failure preserves raw stderr separation;
- cancellation writes an atomic result and removes disposable sidecar state.

### Raw Stream Contract

In raw mode, stdout and stderr are for the selected harness only. Status UI,
metadata JSON, launch diagnostics, and helper traces must use the result file,
logs, or structured errors.

Required tests:

- fake harness stdout is byte-for-byte preserved;
- fake harness stderr is byte-for-byte preserved;
- helper diagnostics do not appear in raw stdout;
- non-raw mode may render user-facing status, but raw mode must not.

## Linux Helper Gates

### Strategy Choice

The first Linux provider must choose one `linux.helper_strategy` explicitly:

- `root-helper`: root performs namespace/mount/cgroup setup, validates spec,
  then drops to `agent`; or
- `rootless-userns`: helper runs without a root setup path and uses user
  namespaces only when the host permits them.

The provider must not silently fall back from one to the other. If the selected
strategy cannot run on a host, report a capability gap.

### Modeled Ordering

The implementation must follow `MC_LinuxNativeLaunch`:

1. validate launch spec and mount plan;
2. close inherited file descriptors;
3. create required namespaces;
4. apply mounts;
5. enforce network policy;
6. drop privileges and set `no_new_privs`;
7. apply or explicitly skip Landlock/seccomp according to accepted gaps;
8. emit metadata;
9. exec the harness.

Any change to metadata-before-enforcement, exec-before-metadata, missing
namespace handling, or gap acceptance requires TLA model work before code.

## Network Gates

Linux readiness needs two supported policy states before user-facing launch:

- `default`: represented honestly as host-network or agent-network authority,
  with no claim of egress filtering;
- `none`: enforced before metadata by a network namespace or an equivalent
  proved mechanism.

Named allowlists, transparent proxies, nftables profiles, and service-specific
egress policy are future work. They must be reported as unsupported rather than
silently approximated.

## Capability Gap Vocabulary

A Linux plan or failed launch should name the missing surface precisely:

| Gap ID | Meaning |
|--------|---------|
| `linux.native-launch-helper-missing` | Linux native launch helper is not implemented yet |
| `linux.runtime-not-linux` | the inspected runtime is not Linux |
| `linux.user-namespace-unavailable` | selected strategy needs user namespaces and the host disables them |
| `linux.mount-namespace-unavailable` | helper cannot create the required mount namespace |
| `linux.network-namespace-unavailable` | `--network none` cannot be enforced |
| `linux.landlock-unavailable` | Landlock is unavailable and the spec did not accept the gap |
| `linux.seccomp-unavailable` | seccomp is unavailable and the spec did not accept the gap |
| `linux.cgroup-v2-unavailable` | resource controls cannot be attached |
| `linux.setup-required` | persistent Linux setup resources are missing |
| `linux.helper-strategy-unsupported` | the host cannot run the chosen helper strategy |

These IDs should appear in explain JSON and in human text without suggesting
unsafe workarounds such as broad home mounts, host Docker socket exposure, or
running as the primary user.

## Distro And Smoke Gates

Minimum non-live gates:

- `scripts/check-linux-compile.sh` remains green.
- Linux plan-only package tests run on macOS and Linux CI.
- Distro probe parser tests cover Ubuntu, Debian, Fedora, Arch, and an unknown
  distro fixture.
- Explain JSON reports plan-only Linux gaps without requiring root.

Minimum live/manual gates before production support:

- VM smoke for the chosen helper strategy.
- `--network none` smoke proving no external route before metadata.
- fake harness smoke for stdout/stderr/result separation.
- cancellation smoke proving cleanup and atomic result state.
- negative smoke for missing Landlock/seccomp when the spec does not accept
  the gap.

## Release Rule

Until every blocking gate above has evidence, Linux run-agent support must stay
plan-only or experimental. User-facing docs may advertise capability gaps and
design direction, but they must not imply that Linux native launch provides the
same production support level as Darwin native containment.
