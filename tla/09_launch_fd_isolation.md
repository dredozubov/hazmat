# Problem 9 — Launch FD Isolation

## Problem Statement

Hazmat's native launch path already uses a different Unix user and a Seatbelt
policy, but neither of those layers revokes access granted by an already-open
file descriptor. If the launch executor starts with an inherited fd to a
credential file, daemon socket, or broker authority handle, the path-based
Seatbelt deny rules are moot for that handle.

The specific question is therefore:

> At the moment the native launch executor calls `sandbox_init()`, can any
> non-stdio fd inherited from the invoking user's process tree, `sudo`, or a
> persistent agent-side broker still be alive?

This spec treats the upstream launch chain as partially adversarial:

- Go's `exec` path may or may not collapse `hazmat -> sudo` or
  `hazmat -> current-user helper` to stdio only
- `sudo` may or may not apply `closefrom`-style cleanup before execing the helper
- a persistent agent-owned launch broker may hold its listener, accepted request
  socket, and other broker-owned non-stdio fds when it forks a launch child
- broker startup may inherit host-origin non-stdio fds unless it is routed
  through Hazmat's fd-cleaning helper exec boundary
- a future persistent forkserver may hold a private broker/executor control fd
  when it forks a launch child

The useful design claim is narrower and stronger:

- the launch executor closes every inherited fd `>= 3` before sandboxing
- the long-lived launch broker closes startup-inherited fds before listening
- a persistent forkserver closes startup-inherited fds before it accepts broker
  launch work, retaining only stdio plus its private control fd
- any fd the helper opens for policy validation is explicitly `CLOEXEC`
- helper-side session temp preparation leaves no additional live descriptors at
  `sandbox_init()`
- a brokered launch path must authenticate the host peer before it may fork the
  launch child, and the child must apply the same fd sanitation before
  `sandbox_init()`
- the final agent process starts with stdio only
- broker activation and attestation-token minting happen only after confirmed
  containment metadata, not merely after a prepared host launch

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/agent_launch.go` | native sudo + helper launch construction |
| `hazmat/session.go` | `runAgentSeatbeltScriptWithUI()`, policy-file generation |
| `hazmat/cmd/hazmat-launch/main.go` | helper-side fd cleanup, policy read, session temp preparation, `sandbox_init()`, final `exec` |
| `hazmat/internal/runtime/darwin/*.go` | agent-user and current-user Darwin launch argument/admission helpers |
| `hazmat/cmd/hazmat-launch-fast/main.c` | experimental lower-level broker child helper for profiling the same fd cleanup, policy read, session temp preparation, `sandbox_init()`, final `exec` boundary |
| `hazmat/internal/runtime/launchbroker/*.go` | authenticated agent-side steady-state request, verified launch request, child-plan fd cleanup contract |
| `hazmat/native_launch_broker.go` | host-side broker request/client path for buffered non-interactive launches |
| `hazmat/launch_broker_supervisor.go` | host-side broker startup command construction through `hazmat-launch exec` |
| future launch broker executor wiring | forkserver or equivalent lower-level executor, interactive stdio/session transport, and default persistent broker lifecycle |

## TLA+ Model

### Abstract FD Model

The model uses nine abstract fds:

| FD | Meaning |
|----|---------|
| `0,1,2` | stdio |
| `3` | inherited credential-bearing fd |
| `4` | inherited extra fd that may be benign or host-authority-bearing |
| `5` | helper-opened policy file |
| `6` | broker listener socket |
| `7` | accepted broker request socket |
| `8` | private broker/forkserver control fd |

Each fd also tracks:

- target class: `stdio`, `credential`, `benign`, `policy`, `authority`,
  `broker_socket`, `broker_request`, `executor_control`, `unused`
- origin: `shell`, `helper`, `broker`, `forkserver`, `none`
- `CLOEXEC` flag

### Launch Stages

The state machine follows the actual native launch chain at the point where fd
inheritance matters:

1. `hazmat`
2. either `sudo -> hazmat-launch`, direct current-user `hazmat-launch`, or
   startup of a persistent agent-owned launch broker through a fd-cleaning
   helper exec boundary
3. broker startup fd sanitization before listening
4. optional forkserver startup fd sanitization before it accepts launch work
5. authenticated request to the persistent broker
6. launch child/executor, either through per-launch helper exec or forkserver
7. executor fd sanitization
8. executor policy-file open
9. optional executor-side session temp preparation, leaving no extra live fds
10. `sandbox_init()`
11. confirmed-containment metadata emission
12. optional host broker activation and token minting
13. final agent `exec`

Cold-start optimization may choose the already-modeled direct
`sudo -> hazmat-launch -> sandbox_init()` path instead of starting the
experimental broker when no default broker is listening. That choice is a
selection between existing proved launch modes, not a new fd boundary: the
direct helper still performs inherited-fd cleanup before policy validation and
`sandbox_init()`, while an already-listening broker still follows the
authenticated broker child path.

Two environment knobs are chosen nondeterministically at `Init`:

- `goExecClosesParentFDs ∈ BOOLEAN`
- `sudoClosesInheritedFDs ∈ BOOLEAN`

The checked config fixes the helper-side design knobs to the intended values:

- `HelperClosesInheritedFDs = TRUE`
- `PolicyFileUsesCloexec = TRUE`
- `BrokerAuthenticatesPeer = TRUE`
- `BrokerStartupClosesInheritedFDs = TRUE`
- `ForkserverStartupClosesInheritedFDs = TRUE`
- `ChildLaunchHelperSource = "installed"`

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `BrokerLaunchRequiresAuthenticatedPeer` | A brokered launch cannot fork/reach the executor path until the host peer is authenticated |
| `BrokerLaunchUsesTrustedHelper` | A brokered launch cannot reach its child helper boundary unless the selected helper is trusted |
| `BrokerFDTableDropsHostInheritedFDs` | Once the persistent broker is listening or serving, it no longer holds host-origin non-stdio fds inherited during startup |
| `ForkserverFDTableAllowlistedWhenReady` | A persistent forkserver retains only stdio plus its private control fd before serving launch work |
| `HelperFDTableAllowlistedAtSandbox` | Once sandboxing starts, the helper holds only stdio plus its helper-opened policy fd |
| `NoInheritedShellFDsAtSandbox` | No shell-origin fd `>= 3` survives into or past `sandbox_init()` |
| `CredentialFDsGoneBeforeSandbox` | No credential-bearing fd is live when `sandbox_init()` runs |
| `AgentFDTableAllowlisted` | The final exec'd agent keeps only stdio |
| `StdioSurvivesToAgent` | The agent still has all three stdio descriptors |
| `BrokerStartsOnlyAfterSandboxConfirmed` | The host broker cannot activate before `sandbox_init()` succeeds and confirmed-containment metadata is emitted |
| `TokenMintedOnlyAfterSandboxConfirmed` | Attestation token minting follows broker activation, which follows confirmed containment |
| `AgentFDTableDoesNotCarryAuthority` | No inherited fd carrying host authority material survives into the final agent process |

## What This Found

This model makes one design fact explicit:

- relying on Go's current exec behavior, `sudo`'s current fd cleanup, or a
  broker's steady-state fd hygiene is not a proof, because these upstream
  behaviors are adversarial or explicitly modeled with extra live fds
- the first launch-child action must therefore be inherited-fd cleanup,
  regardless of whether the child came from `sudo` or the persistent broker
- the persistent broker itself also needs a startup fd cleanup boundary before
  it listens, otherwise it can retain leaked host credential/authority fds even
  though each child still sanitizes before `sandbox_init()`
- the persistent broker path is viable only if request authentication precedes
  forking the launch child
- a forkserver optimization is viable only if the forkserver parent keeps a
  private control fd and every forked child closes that control fd before
  policy validation and `sandbox_init()`
- a pre-sudo prepared launch is not a confirmed containment boundary; broker
  authority starts only after `sandbox_init()` and metadata confirmation

With the checked config, TLC passes. With a temporary negative config that sets
`HelperClosesInheritedFDs = FALSE`, TLC immediately finds a counterexample where
an inherited non-stdio fd reaches `sandbox_init()`.

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers auto \
  -config MC_LaunchFDIsolation.cfg \
  MC_LaunchFDIsolation.tla
```

Observed result:

- `Model checking completed. No error has been found.`
- `2,688 states generated`
- `1,920 distinct states found`
- `depth 15`
- `Finished in <1s`

2026-06-29 macOS current-user helper mode:

- added an explicit `current_user_helper` launch mode for the planned
  `macos-current-user` provider
- modeled `hazmat` directly execing the helper without `sudo`, with Go's fd
  cleanup still adversarial
- preserved the existing helper cleanup, policy-open, session-temp,
  `sandbox_init`, metadata, and final exec obligations
- this proves the same fd-safety property for same-uid Seatbelt launch: direct
  current-user helper invocation is safe only because the helper still closes
  inherited descriptors before policy validation and `sandbox_init()`
- current metrics: `3,840 generated`, `2,688 distinct`, `depth 15`

2026-06-04 proof-hygiene refactor:

- factored the repeated stdio descriptor set into `StdioFDs`
- preserved checked obligations: `TypeOK`, `HelperFDTableAllowlistedAtSandbox`,
  `NoInheritedShellFDsAtSandbox`, `CredentialFDsGoneBeforeSandbox`,
  `AgentFDTableAllowlisted`, `StdioSurvivesToAgent`
- before metrics: `128 generated`, `112 distinct`, `depth 7`
- after metrics: `128 generated`, `112 distinct`, `depth 7`

2026-06-09 Beadpost broker ordering addition:

- added confirmed-containment metadata, broker activation, and token minting
  order to the same launch model
- added an adversarial inherited `authority` fd class to represent host signing
  material that must never reach the contained agent
- added checked obligations `BrokerStartsOnlyAfterSandboxConfirmed`,
  `TokenMintedOnlyAfterSandboxConfirmed`, and
  `AgentFDTableDoesNotCarryAuthority`
- current metrics: `608 generated`, `416 distinct`, `depth 10`

2026-06-15 helper-managed session temp preparation:

- added an explicit helper-side session temp preparation stage between policy
  read and `sandbox_init()`
- the modeled phase leaves `helperFds` unchanged, preserving the requirement
  that `sandbox_init()` sees only stdio plus helper-opened policy state
- current metrics: `640 generated`, `448 distinct`, `depth 11`

2026-06-15 persistent launch broker proof addition:

- added a brokered launch mode alongside the existing `sudo -> hazmat-launch`
  mode
- modeled broker startup inheriting host-origin credential/authority fds before
  the fd-cleaning helper exec boundary drops them
- modeled the broker child inheriting broker-owned listener/request fds before
  it performs fd sanitation
- added checked obligations `BrokerLaunchRequiresAuthenticatedPeer` and
  `BrokerFDTableDropsHostInheritedFDs`
- current metrics: `1,312 generated`, `928 distinct`, `depth 13`

2026-06-15 forkserver executor alternative:

- added an explicit brokered child executor choice: current per-launch
  `hazmat-launch` helper exec or future persistent forkserver
- modeled forkserver startup from the already-clean broker process with a
  private socketpair-style control fd
- added `ForkserverFDTableAllowlistedWhenReady`, proving the forkserver parent
  can retain only stdio plus that private control fd before serving launch work
- modeled forkserver launch children inheriting the private control fd and
  proved the existing child cleanup obligation removes it before policy open,
  `sandbox_init()`, and final agent exec
- current metrics: `2,688 generated`, `1,920 distinct`, `depth 15`

2026-06-15 broker transport boundary:

- added `hazmat/internal/runtime/launchbroker` as the concrete broker request
  boundary governed by this model
- Unix peer authentication is required before request verification
- only a `VerifiedLaunchRequest` can construct a `ChildPlan`
- `ChildPlan` construction requires `ChildFDPolicyCloseInherited`, preserving
  the model's launch-child fd cleanup obligation before future executor wiring
- broker child command planning now invokes the existing `hazmat-launch`
  helper directly with a minimal `SUDO_UID=<authenticated-peer>` environment,
  so helper-side policy validation and inherited-fd cleanup are preserved
  without the future hot path needing per-launch `sudo`
- broker validation rejects policy paths that the helper would reject before
  constructing a child command
- the broker helper executor can run the planned helper command through an
  injectable runner, propagate exit code/stdout/stderr in the launch response,
  and fails closed if requested confirmed-containment metadata is not observed
  on helper stderr
- added a broker service wrapper that owns Unix socket readiness, cancellation,
  and socket cleanup while wiring the helper executor as the default launch
  handler
- added the hidden agent-entry `_launch_broker` command and production runner
  that starts the service under a cancellable signal-aware context
- added a host-side broker start plan/supervisor that starts `_launch_broker`
  through `hazmat-launch exec`, reusing the proved startup fd cleanup boundary
  before the long-lived broker opens its socket
- added an opt-in host-side broker client path for buffered non-interactive
  native launches (`HAZMAT_LAUNCH_BROKER_SOCKET` or
  `HAZMAT_EXPERIMENTAL_LAUNCH_BROKER=1`), preserving metadata confirmation,
  stdout/stderr replay, nonzero exit status, and post-session repair/denial
  recording while bypassing per-launch `sudo` when a broker is already running.
  The experimental default path can start the per-uid broker once through the
  proved `hazmat-launch exec` startup boundary and retry the broker request; an
  explicitly configured socket remains fail-fast if it cannot be used.
- the broker client now sends direct-exec launches as argv plus working
  directory, without also carrying the shell-script field. That preserves the
  broker's request validation rule that direct exec and shell launches are
  mutually exclusive while allowing capable helpers to skip the shell wrapper.
- broker startup and broker child launches may use different explicitly
  configured helper paths: the startup command still goes through the
  sudo-authorized helper for the proved fd-cleaning `hazmat-launch exec`
  boundary, while the agent-owned broker can use a separately configured child
  helper for per-launch execution. The broker never automatically discovers a
  sibling helper beside the current executable because that executable may be
  in an agent-writable checkout. If the
  broker path is unavailable, Hazmat repairs the agent temp dir before falling
  back to an older sudo helper that cannot create helper-managed session temp.
- the host-side broker supervisor removes only stale Unix socket path residue
  before startup: live sockets are left intact and reported, and non-socket or
  symlink paths are refused. This does not change the fd invariants, but keeps
  crash residue from forcing the experimental broker path back to per-launch
  `sudo`.
- the default per-uid broker runtime directory is revalidated through the same
  agent-side shared-directory preparation even when it already exists. This
  repairs mode/group drift before broker startup and keeps the experimental
  path on the proved broker startup boundary instead of silently falling back to
  per-launch `sudo`.
- service+helper-executor control-plane benchmark with fake runner:
  `31.588-35.304 us/op` across five local Darwin arm64 runs
- live profiling with a default experimental broker and checkout-built child
  helper that supports `--hazmat-session-temp` and `--hazmat-direct-exec`:
  after a broker restart, cold startup measured `0.88s`; warm launches measured
  mostly `0.09-0.10s` but still spiked to `0.11-0.14s`, with the built-in
  profile attributing the remaining variance to `run native broker command`
  (`0.03-0.16s` observed). This is implementation evidence, not a new model
  obligation; strict sub-`0.1s` launch is not yet proved.

## Interpretation

This proof does not claim anything about macOS kernel internals or `sudo`
implementation details. It proves a stronger Hazmat-specific boundary:

- even if upstream exec behavior is less hygienic than expected,
- even if `sudo` contributes no cleanup,
- even if broker startup would otherwise inherit host credential/authority fds,
- and even if a persistent broker child inherits broker-owned listener/request
  fds,
- and even if a persistent lower-level forkserver child inherits its private
  broker/executor control fd,
- the launch executor still reaches `sandbox_init()` with an allowlisted fd table
- and any host-side broker authority is minted only after confirmed containment

That turns file-descriptor hygiene from an implicit runtime assumption into an
explicit checked design rule for the native launch path, and keeps the
attestation authority boundary tied to the same confirmed launch event.
