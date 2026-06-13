# Linux-Ready Backend Architecture for Reusable Hazmat

**Date:** 2026-05-28
**Beads:** `sandboxing-lh7j`, `sandboxing-3ryz`
**Status:** Architecture plan, not implementation approval

## Goal

Make Hazmat reusable and Linux-capable by splitting policy planning from
platform enforcement. Linux should become a native backend behind the same
backend-neutral session contract as macOS and Docker Sandbox sessions. It
should not become a forked CLI path or a pile of `if runtime.GOOS == "linux"`
branches.

For the run-agent provider release gates that turn this architecture into
testable user-facing readiness criteria, see
[Linux Run-Agent Provider Readiness Gates](2026-06-13-linux-run-agent-readiness-gates.md).

The reusable decomposition has two jobs:

1. Let other local tools and automation services plan Hazmat sessions without
   importing Cobra commands or terminal UI.
2. Let Linux implement the same core containment contract with Linux-native
   primitives rather than macOS Seatbelt.

## Core Design

Hazmat should converge on this layered shape:

```text
CLI / local API / future services
  -> sessioncontract.Request
  -> planner.Plan()
       -> pathpolicy
       -> integrations
       -> credentials grant descriptors
       -> containment.Contract
  -> backend compiler
       -> darwin/native: SBPL + hazmat-launch sandbox_init
       -> linux/native: namespaces + mounts + Landlock/seccomp/cgroups
       -> docker/sandbox: Docker Sandbox launch spec + network profile
  -> backend runner
       -> prepare artifacts
       -> launch command
       -> verify cleanup
```

Only the bottom two backend layers should know how a platform enforces the
contract. The planner should produce a backend-neutral result that can be
rendered, tested, serialized, and consumed without launching an agent.

## Backend-Neutral Containment Contract

The core contract should be represented explicitly before any backend compiles
it:

```go
type Contract struct {
    ProjectDir       PathGrant   // read-write
    ReadOnlyDirs     []PathGrant // read-only
    ReadWriteDirs    []PathGrant // read-write
    AgentHome        AgentHomePolicy
    TempDir          TempPolicy
    CredentialDenies []CredentialDeny
    Network          NetworkPolicy
    Process          ProcessPolicy
    Services         []ServiceGrant
    Metadata         sessionmeta.LaunchMetadata
}
```

Important semantics:

- `ProjectDir` is read-write.
- Explicit read-only dirs are read-only unless covered by a stronger writable
  root.
- Explicit write dirs are read-write.
- Credential deny zones are not optional decorations; they are part of
  planning and backend validation.
- Network policy is part of the contract, not a CLI afterthought.
- Backend differences are recorded as capability gaps, not hidden behavior.

The contract is not "SBPL in Go structs." It is a product-level authority
model. SBPL, Linux mounts/Landlock, and Docker Sandbox specs are compilers of
that model.

## Linux Native Backend Strategy

Linux should use a different enforcement model from macOS. Seatbelt is
last-match-wins path policy. Linux should prefer absence and mount topology
over trying to emulate deny-over-allow with ad hoc filters.

Recommended Linux native stack:

| Layer | Linux primitive | Purpose |
| --- | --- | --- |
| User boundary | dedicated `agent` user and `dev` group | separate host identity from invoker |
| File view | mount namespace with explicit bind mounts | expose only planned roots |
| Read-only grants | read-only bind mounts | enforce `ReadOnlyDirs` |
| Writable grants | read-write bind mounts | enforce project/write dirs |
| Temp state | per-session tmpfs or agent-owned temp root | avoid cross-session state |
| Path hardening | Landlock when available | defense-in-depth allowlist after mounts |
| Syscall hardening | seccomp-bpf after runtime smoke data | reduce high-risk kernel surface |
| Resource limits | cgroups v2 | bound CPU, memory, pids, disk if possible |
| Network none | network namespace with no external route | enforce native `--network none` |
| Network default | host network as agent user, later nftables profile | preserve current agent workflow |
| Privilege drop | `no_new_privs`, clear caps, exec as agent | ensure helper setup power does not reach agent |
| FD isolation | close inherited fds before namespace/policy setup | preserve launch FD invariant |

The Linux backend should first implement a narrow native mode that can prove
the core path contract. It should not start by owning every distro bootstrap
or firewall shape.

## Linux Launch Helper Shape

The current `hazmat-launch` helper already owns an important invariant:

1. close inherited file descriptors
2. validate a host-produced policy artifact
3. apply platform sandboxing
4. emit metadata only after enforcement succeeds
5. exec the target directly

Keep that shape. The Linux helper should receive a signed or validated launch
spec instead of an SBPL file:

```text
hazmat
  -> writes /run or /tmp hazmat-linux-spec-<pid>.json
  -> sudo/root helper validates spec ownership, mode, schema, nonce
  -> helper creates namespaces/mounts/network/cgroups
  -> helper drops privileges to agent
  -> helper applies Landlock/seccomp/no_new_privs
  -> helper emits metadata
  -> helper execs target
```

There are two viable privilege strategies:

1. **Root helper strategy:** `sudo` runs a narrow root `hazmat-launch` only for
   namespace/mount setup, then the helper drops to `agent` before policy
   finalization and exec. This works on locked-down hosts but expands setup and
   TLA surface.
2. **Unprivileged user namespace strategy:** helper runs as `agent` and uses
   user namespaces when the host allows them. This is smaller operationally but
   unavailable on many hardened systems.

The architecture should support both as backend capabilities, but the first
production-grade Linux backend should choose one explicitly after the TLA model
and distro probes are written. Do not silently fall back from one to the other.

## File Policy Differences from macOS

macOS SBPL can re-assert writes after broad reads and deny credential paths
last. Linux should not rely on that shape.

Linux should enforce credential protection primarily through planning:

- Reject project/read/write roots that are credential deny zones or parents of
  credential deny zones.
- Prefer materialized workspaces or explicit bind mounts over broad home or
  workspace-parent mounts.
- For sensitive review packets, use absent secret bytes, not live path-deny
  overlays.
- If a future Linux backend supports overlayfs masking, model it separately
  before treating it as equivalent to SBPL deny rules.

This means Linux native sessions may initially support a smaller accepted input
set than macOS native sessions. That is acceptable if the planner reports the
capability gap clearly.

## Network Policy

Keep two levels distinct:

- **Core contract:** `default`, `none`, and later named egress profiles.
- **Backend enforcement:** the mechanism a backend uses to implement that
  contract.

Linux MVP should support:

- `default`: outbound allowed as the `agent` user.
- `none`: launch in a network namespace without external network access, while
  preserving loopback if needed for local child processes.

Named allowlist profiles should come later. They likely require nftables,
transparent proxying, or a per-session network namespace with a managed egress
proxy. That is real product work and should not block the first Linux native
file-containment proof.

## Setup and Rollback Model

Linux setup must not reuse macOS resource names blindly. The current
unsupported constants keep compile-only working, but real Linux setup needs a
Linux resource graph.

Proposed Linux setup resources:

| Resource | Purpose |
| --- | --- |
| `linuxAgentUser` | dedicated agent account |
| `linuxSharedGroup` | controlled collaboration group |
| `linuxAgentHome` | agent-owned home and XDG roots |
| `linuxLaunchHelper` | root or agent helper binary with fixed digest/path |
| `linuxSudoers` | narrow sudo rule for helper strategy, if used |
| `linuxCgroupRoot` | cgroup v2 subtree or delegation, if used |
| `linuxDistroProfile` | inspected distro facts and capability support |
| `linuxToolHome` | agent-owned toolchain root |

This needs a new TLA model or an extension of setup/rollback before persistent
Linux mutations land. Until then, Linux should remain compile-only or
plan-only.

## Package Plan

Extend the decomposition package map like this:

| Package | Role in Linux port |
| --- | --- |
| `sessionmeta` | Already reusable metadata/labels/network parsing |
| `sessioncontract` | Backend-neutral request, plan, capability gaps |
| `pathpolicy` | Shared path containment and credential deny validation |
| `containment` | Backend-neutral authority contract |
| `containment/darwin` | Compile contract to SBPL |
| `containment/linux` | Compile contract to Linux launch spec |
| `backends/native/darwin` | Darwin policy artifact + sudo helper command |
| `backends/native/linux` | Linux spec artifact + helper command |
| `backends/docker` | Docker Sandbox launch spec |
| `platform/linux` | Distro inspection, OS facts, kernel feature probes |
| `setup/linux` | Linux setup/rollback resources after model approval |

This keeps Linux support in the architecture from the beginning, while still
allowing the implementation to land in small slices.

## Migration Phases

### Phase A: Library contracts

Finish `sessioncontract` and `pathpolicy` first. Linux needs these more than it
needs a launch helper, because they define what every backend is trying to
enforce.

Deliverables:

- `sessioncontract.Request` and `sessioncontract.Plan`
- `containment.Contract`
- shared path deny-zone validation
- backend capability gap reporting
- compatibility shims in `package main`

### Phase B: Linux plan-only mode

Add Linux platform inspection and planning without launching.

Deliverables:

- `hazmat explain --json` works on Linux for safe, side-effect-free planning
- Linux reports native backend as unsupported with structured reasons
- distro/kernel feature probes are available behind a package API
- CI keeps Linux compile-only plus package tests green

### Phase C: TLA models for Linux launch/setup

Before persistent setup or real launch enforcement:

- model Linux launch ordering: validate spec, close fds, namespace/mount setup,
  network setup, privilege drop, Landlock/seccomp/no_new_privs, metadata emit,
  exec
- model Linux setup/rollback resources if root helper, sudoers, cgroups, or
  user creation are implemented
- extend native-vs-Docker equivalence language to include Linux native as a
  backend with capability gaps

### Phase D: Experimental Linux native backend

Implement a guarded experimental backend.

Initial scope:

- dedicated agent user
- project read-write bind
- explicit read-only and read-write binds
- per-session temp
- no persistent agent-writable cache reuse beyond declared agent home policy
- `--network none`
- metadata emitted only after enforcement succeeds

Out of scope for first launch:

- egress allowlist profiles
- distro bootstrap automation
- overlayfs deny masking
- broad host workspace mounts
- exact parity with macOS path edge cases

### Phase E: Linux capability bootstrap

Only after native launch/setup is coherent, revisit distro capability support
from the existing Linux capability proposal. Keep tool installation mostly
agent-local, with system packages as an explicit fallback.

## Model and Test Gates

Before Linux native launch can move beyond experimental:

- `go test ./...`
- Linux compile-only check
- Linux package tests for path policy and launch spec compilation
- helper tests for spec validation and fd closing
- containerized smoke tests for plan-only mode
- TLC for Linux launch ordering
- TLC for Linux setup/rollback if persistent resources are changed
- manual VM smoke before claiming production support

The first working Linux backend should advertise itself as experimental until
the model, smoke tests, and manual VM checklist are in the release path.

## Immediate Bead Shape

Recommended next beads under `sandboxing-lh7j`:

1. Extract `sessioncontract` with backend capability gaps.
2. Extract `pathpolicy` and credential deny-zone validation.
3. Add `containment.Contract` and Darwin SBPL compiler adapter.
4. Draft Linux launch TLA model and design note.
5. Add Linux plan-only platform inspection package.
6. Prototype Linux launch spec compiler without executing it.
7. Implement guarded Linux native launch helper after model approval.

This keeps architecture, model, and implementation sequenced. The Linux port
becomes a backend of reusable Hazmat, not a second Hazmat.
