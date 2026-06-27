# Linux Support Two-Lane Design

**Date:** 2026-06-27
**Bead:** `sandboxing-347h`
**Status:** design documentation, not implementation approval

## Purpose

Hazmat's Linux support needs two explicit operating modes:

1. **Single-user mode:** no additional system user, no persistent setup, and
   sandboxing enforced from the validated session contract.
2. **Multi-user mode:** macOS-style setup with a dedicated `agent` account,
   host repair/rollback, and stronger identity isolation.

Both modes must consume the same backend-neutral authority contract. They
should differ only in the Linux runtime and setup resources used to enforce
that contract. Neither mode may silently fall back to the other.

## Common Foundation

Both Linux modes start from the existing architecture:

```text
sessioncontract.Plan
  -> containment.Contract
  -> containment/linux.LaunchSpec
  -> Linux runner/helper
  -> metadata sidecar
  -> harness exec
```

The shared contract includes:

- project read-write authority;
- explicit read-only grants;
- explicit read-write grants;
- a structural credential-deny floor;
- agent-home and temp policy;
- network mode;
- process policy;
- backend capability gaps.

Linux runtime code must rebuild authority from typed objects, not from
`hazmat explain --json` bytes. JSON previews stay descriptive.

The launch ordering is governed by
[tla/14_linux_native_launch.md](../../tla/14_linux_native_launch.md):
validate spec, close inherited file descriptors, create namespaces, apply
mounts, enforce network policy, drop privileges or freeze privilege gain,
apply Landlock/seccomp decisions, emit metadata, then exec.

## Lane 1: Single-User Contract Sandboxing

Single-user mode runs the harness as the invoking Linux user. It creates no
`agent` account, group, sudoers file, cgroup delegation, or persistent helper
installation.

This lane is for low-friction Linux development, CI, containers, and users who
cannot or will not mutate the host. It is not equivalent to macOS Hazmat's
identity boundary unless the kernel policy fully enforces the contract.

### Runtime Shape

```text
hazmat
  -> writes validated Linux launch spec in session temp
  -> starts current-user Linux runner
  -> runner unshares user and mount namespaces
  -> runner builds the planned filesystem view
  -> runner applies Landlock allow rules
  -> runner applies seccomp and no_new_privs
  -> runner emits metadata
  -> runner execs harness as current uid
```

The runner should be an ordinary executable, not a setuid binary and not a root
helper. If a host cannot create the required namespaces as the current user,
this mode reports a capability gap and fails closed.

### Required Linux Primitives

| Primitive | Requirement |
| --- | --- |
| User namespace | Required to gain namespace-local mount capability without host root. |
| Mount namespace | Required to build the contract filesystem view. |
| Landlock | Required for production single-user file policy. Same-uid DAC is not enough. |
| Seccomp + `no_new_privs` | Required before exec for the initial supported profile. |
| Network namespace | Required only for `--network none`; missing support blocks that mode. |
| cgroup v2 | Optional in this lane; missing resource controls are reported as gaps. |

Single-user mode should not accept a Landlock gap for production launch. If
Landlock is unavailable, the mode can remain plan-only or experimental with an
explicit accepted gap, but docs and CLI output must not call it supported.

### Filesystem Policy

Linux should prefer a narrow filesystem view over broad deny overlays:

- project path is read-write;
- declared read-only roots are bind-mounted read-only;
- declared write roots are bind-mounted read-write;
- session temp and HOME are session-local;
- credential-deny paths are rejected during planning if any grant overlaps;
- broad host home mounts are not allowed;
- host credential stores are absent unless represented by typed grants.

Because the process still has the user's uid, policy must not rely on UNIX DAC
to protect other files owned by that uid. Landlock must restrict filesystem
access to the planned grants after the mount view is assembled. Mount topology
reduces what is visible; Landlock enforces the contract if a path remains
reachable through the host root or a toolchain mount.

### Agent Home And Tool State

Single-user mode should default to a session-local HOME:

```text
$TMPDIR/hazmat-linux/<session>/home
$TMPDIR/hazmat-linux/<session>/xdg-cache
$TMPDIR/hazmat-linux/<session>/xdg-config
$TMPDIR/hazmat-linux/<session>/xdg-data
```

Persistent host tool state is out of scope unless it is explicitly granted.
This keeps the first mode honest: it can run checks and agents from a prepared
toolchain, but it does not pretend to provide a durable agent identity.

### Credentials

Credentials enter through the existing typed credential runtime:

- raw host secret stores are never mounted;
- env grants are redacted in previews;
- materialized files live under session temp or session-local HOME;
- cleanup removes materialized credential files before returning.

Same-user mode must assume that the host user's durable secrets are sensitive
even though the process uid matches the owner. They stay protected by absence
and Landlock, not by advisory instructions.

### Network

| Mode | Enforcement |
| --- | --- |
| `default` | Host network as current user. Metadata must say no egress filtering is claimed. |
| `none` | Network namespace with loopback only and no external route before metadata. |
| named profiles | Unsupported; report a capability gap. |

If user/network namespaces cannot enforce `none`, the runner fails with
`linux.network-namespace-unavailable`.

### Admission And Gaps

Single-user mode is admissible only when the selected contract can be enforced
without persistent setup. Expected blocking gaps:

- `linux.user-namespace-unavailable`
- `linux.mount-namespace-unavailable`
- `linux.landlock-unavailable`
- `linux.seccomp-unavailable`
- `linux.network-namespace-unavailable`
- `linux.helper-strategy-unsupported`

It must never repair the host by creating users, groups, sudoers files,
helpers, or cgroup delegation. Those belong to multi-user mode.

### Strengths And Limits

| Strength | Limit |
| --- | --- |
| No host setup. | Same uid as the user; policy bugs have a larger blast radius. |
| Works naturally in many containers and CI jobs. | Depends on unprivileged user namespaces and Landlock. |
| Good first Linux execution target. | No durable isolated agent identity. |
| Easy rollback: delete session temp. | cgroup controls are optional or unavailable without setup. |

Single-user mode should ship first as **experimental**. It can become
supported only after VM smoke tests prove file denial, credential denial,
network-none denial, metadata ordering, cancellation cleanup, and negative
capability gaps across representative distros.

## Lane 2: Multi-User Setup Like macOS Hazmat

Multi-user mode mirrors macOS Hazmat's security posture: the host user remains
the driver, while agent work runs as a dedicated OS account with a controlled
filesystem and network boundary.

This is the production-parity lane.

### Setup Resources

Linux setup needs its own modeled resource graph. It must not reuse macOS
resource names casually.

| Resource | Purpose |
| --- | --- |
| `linuxAgentUser` | Dedicated `agent` account for harness execution. |
| `linuxSharedGroup` | Controlled collaboration group for project access. |
| `linuxAgentHome` | Agent-owned HOME and XDG roots. |
| `linuxWorkspaceAccess` | Project traversal and group/ACL access where needed. |
| `linuxLaunchHelper` | Root-owned helper binary with fixed path, owner, mode, and digest. |
| `linuxSudoers` | Narrow host-user-to-helper rule, if the root-helper strategy is selected. |
| `linuxCgroupRoot` | cgroup v2 subtree or delegation for session resource controls. |
| `linuxDistroProfile` | Persisted capability facts used by diagnostics, not by launch authority. |
| `linuxToolHome` | Optional agent-owned tool cache root. |

Persistent mutations require a TLA setup/rollback model before code lands.
The model must define setup ordering, rollback ordering, what is preserved by
default, destructive rollback behavior, and failed-step recovery.

### Runtime Shape

```text
hazmat
  -> writes validated Linux launch spec in host-owned session temp
  -> invokes narrow helper through approved setup path
  -> helper validates spec ownership, mode, schema, nonce, and digest
  -> helper closes inherited file descriptors
  -> helper creates namespaces, mounts, cgroup, and network policy
  -> helper drops to agent uid/gid and clears capabilities
  -> helper sets no_new_privs
  -> helper applies Landlock/seccomp decisions
  -> helper emits metadata
  -> helper execs harness as agent
```

The helper must perform only setup that cannot be done safely as an
unprivileged process. It must not become a general command runner.

### Identity Boundary

The dedicated `agent` account gives Linux the same product property Hazmat has
on macOS: the agent is not the user's real account.

Expected defaults:

- host user's HOME is absent;
- agent HOME is durable but owned by `agent`;
- project path is exposed according to the contract;
- host credential stores are not readable by `agent`;
- git and harness credentials are typed grants, brokered credentials, or
  agent-owned durable credentials;
- rollback preserves the agent account by default unless destructive rollback
  is explicitly requested, matching the macOS preservation model.

This lane can protect host-user files with ordinary DAC even before Landlock.
Landlock, mount namespaces, and seccomp still remain required defense in depth
for a supported runtime.

### Filesystem Policy

Multi-user mode should use both identity and namespace policy:

- DAC separates host-user secrets from `agent`;
- mount namespace exposes only planned roots;
- read-only grants are read-only bind mounts;
- write grants are read-write bind mounts;
- credential-deny overlaps are rejected before launch;
- Landlock narrows the final process to the planned allowlist;
- session temp is private to the launched session;
- persistent agent HOME is excluded from broad project mounts unless explicitly
  modeled as session-home bridge state.

The helper must emit metadata only after the filesystem view and Landlock
policy are enforced.

### Network And Resources

| Contract mode | Multi-user enforcement |
| --- | --- |
| `default` | Host network as `agent`, with no claim of egress filtering. |
| `none` | Network namespace before metadata and exec. |
| future named profile | nftables/proxy profile after a separate design and model. |

cgroup v2 should be part of the multi-user lane because setup can create or
delegate a controlled subtree. Missing cgroup support should report
`linux.cgroup-v2-unavailable` unless the selected profile explicitly does not
request resource controls.

### Diagnostics, Doctor, And Rollback

Linux diagnostics should preserve the current Hazmat guidance split:

- fresh host with no Linux setup: point to the Linux setup command once it
  exists;
- partial setup drift: point to doctor repair, with dry-run as preview;
- unsupported distro/feature: report capability gaps, not unsafe workarounds;
- rollback: remove sudoers/helper/cgroup access before removing weaker
  resources;
- destructive rollback: remove `agent` user/group only under explicit flags.

Default `hazmat check` should remain read-only and avoid sudo-adjacent probes.
Full validation, setup repair, helper-backed smoke, and `git push` hooks that
run those paths remain approval-gated in agent workflows.

### Strengths And Limits

| Strength | Limit |
| --- | --- |
| Strong host-user secret isolation through OS identity. | Requires persistent setup and rollback. |
| Can support cgroups and locked-down hosts. | Needs distro-specific setup probes. |
| Best fit for production support. | Requires TLA setup/rollback governance before implementation. |
| Matches macOS Hazmat's mental model. | More install friction than single-user mode. |

## Strategy Selection

Linux mode selection should be explicit:

```text
linux.identity = current-user | agent-user
linux.helper_strategy = rootless-userns | root-helper
```

Initial defaults:

- `current-user` selects the single-user lane and requires rootless userns plus
  Landlock.
- `agent-user` selects the multi-user lane and requires completed Linux setup.

The backend must not silently fall back from `agent-user` to `current-user`,
or from `root-helper` to `rootless-userns`. If the selected strategy cannot
run, return a structured capability gap.

## Release Phases

### Phase 0: Current Plan-Only State

Already present or in progress:

- `platform/linux` side-effect-free host probes;
- `containment/linux` plan-only launch spec compiler;
- Linux capability gaps in launch specs;
- Apple Container Linux test/dev lanes;
- `MC_LinuxNativeLaunch` ordering model.

### Phase 1: Single-User Experimental Runtime

Deliver:

- current-user Linux runner package;
- fake-helper tests for metadata phases;
- contract-to-mount tests;
- Landlock/seccomp rule builder tests;
- rootless namespace admission tests;
- negative tests for missing userns, mount ns, Landlock, seccomp, and net ns.

This phase must not add persistent setup resources.

### Phase 2: Single-User VM Smokes

Deliver:

- VM smoke for project write and read-only denial;
- secret denial smoke against host-user credential paths;
- `--network none` smoke;
- cancellation cleanup smoke;
- distro matrix for Ubuntu, Debian, Fedora, Arch, and unknown.

Single-user mode can be documented as experimental if these pass.

### Phase 3: Multi-User Setup Model

Before code:

- extend or add TLA setup/rollback model for Linux resources;
- define preservation versus deletion semantics;
- define helper/sudoers/cgroup ordering;
- update `tla/VERIFIED.md` mappings.

No user creation, sudoers, helper install, or cgroup delegation should land
before this model exists and TLC passes.

### Phase 4: Multi-User Experimental Runtime

Deliver:

- `internal/setup/linux` resource implementations;
- Linux root-helper strategy;
- setup/doctor/rollback verification;
- VM lifecycle smoke;
- helper-backed fake harness smoke;
- cgroup enforcement smoke where available.

### Phase 5: Supported Linux Native

Linux native support becomes user-facing only when:

- the selected lane's blocking gates pass;
- capability gaps are stable in JSON and text;
- manual VM smoke is in the release checklist;
- docs avoid implying parity for unsupported lanes;
- pre-release checks include the Linux lane being claimed.

## Documentation Rules

Until Phase 5:

- say "plan-only" for compiler/probe work;
- say "experimental" for any executable Linux lane without full gates;
- name the selected identity strategy;
- name missing kernel/setup capabilities as gaps;
- do not suggest broad HOME mounts, host Docker sockets, or running agents as
  the primary user as a workaround.

The user-facing distinction should be simple:

- **Current-user Linux:** easy to try, no setup, requires strong kernel
  sandboxing, experimental first.
- **Agent-user Linux:** more setup, stronger isolation, intended production
  path.
