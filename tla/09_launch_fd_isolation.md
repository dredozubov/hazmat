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

- Go's `exec` path may or may not collapse `hazmat -> sudo` to stdio only
- `sudo` may or may not apply `closefrom`-style cleanup before execing the helper
- a persistent agent-owned launch broker may hold its listener, accepted request
  socket, and other broker-owned non-stdio fds when it forks a launch child

The useful design claim is narrower and stronger:

- the launch executor closes every inherited fd `>= 3` before sandboxing
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
| `hazmat/internal/runtime/launchbroker/*.go` | authenticated agent-side steady-state request, verified launch request, child-plan fd cleanup contract |
| future launch broker executor wiring | launch-child fork, fd sanitation before `sandbox_init()` |

## TLA+ Model

### Abstract FD Model

The model uses eight abstract fds:

| FD | Meaning |
|----|---------|
| `0,1,2` | stdio |
| `3` | inherited credential-bearing fd |
| `4` | inherited extra fd that may be benign or host-authority-bearing |
| `5` | helper-opened policy file |
| `6` | broker listener socket |
| `7` | accepted broker request socket |

Each fd also tracks:

- target class: `stdio`, `credential`, `benign`, `policy`, `authority`,
  `broker_socket`, `broker_request`, `unused`
- origin: `shell`, `helper`, `broker`, `none`
- `CLOEXEC` flag

### Launch Stages

The state machine follows the actual native launch chain at the point where fd
inheritance matters:

1. `hazmat`
2. either `sudo -> hazmat-launch`, or an authenticated request to a persistent
   agent-owned launch broker
3. launch child/executor
4. executor fd sanitization
5. executor policy-file open
6. optional executor-side session temp preparation, leaving no extra live fds
7. `sandbox_init()`
8. confirmed-containment metadata emission
9. optional host broker activation and token minting
10. final agent `exec`

Two environment knobs are chosen nondeterministically at `Init`:

- `goExecClosesParentFDs ∈ BOOLEAN`
- `sudoClosesInheritedFDs ∈ BOOLEAN`

The checked config fixes the helper-side design knobs to the intended values:

- `HelperClosesInheritedFDs = TRUE`
- `PolicyFileUsesCloexec = TRUE`
- `BrokerAuthenticatesPeer = TRUE`

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `BrokerLaunchRequiresAuthenticatedPeer` | A brokered launch cannot fork/reach the executor path until the host peer is authenticated |
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
- the persistent broker path is viable only if request authentication precedes
  forking the launch child
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
- `608 states generated`
- `416 distinct states found`
- `depth 10`
- `Finished in <1s`

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
- modeled the broker child inheriting broker-owned listener/request fds plus an
  adversarial authority fd before it performs fd sanitation
- added checked obligation `BrokerLaunchRequiresAuthenticatedPeer`
- current metrics: `1,248 generated`, `864 distinct`, `depth 11`

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
- service+helper-executor control-plane benchmark with fake runner:
  `31.588-35.304 us/op` across five local Darwin arm64 runs

## Interpretation

This proof does not claim anything about macOS kernel internals or `sudo`
implementation details. It proves a stronger Hazmat-specific boundary:

- even if upstream exec behavior is less hygienic than expected,
- even if `sudo` contributes no cleanup,
- and even if a persistent broker child inherits broker-owned/request/authority
  fds,
- the launch executor still reaches `sandbox_init()` with an allowlisted fd table
- and any host-side broker authority is minted only after confirmed containment

That turns file-descriptor hygiene from an implicit runtime assumption into an
explicit checked design rule for the native launch path, and keeps the
attestation authority boundary tied to the same confirmed launch event.
