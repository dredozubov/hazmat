# Problem 9 — Launch FD Isolation

## Problem Statement

Hazmat's native launch path already uses a different Unix user and a Seatbelt
policy, but neither of those layers revokes access granted by an already-open
file descriptor. If the helper starts with an inherited fd to a credential file
or daemon socket, the path-based Seatbelt deny rules are moot for that handle.

The specific question is therefore:

> At the moment `hazmat-launch` calls `sandbox_init()`, can any non-stdio fd
> inherited from the invoking user's process tree still be alive?

This spec treats the upstream launch chain as partially adversarial:

- Go's `exec` path may or may not collapse `hazmat -> sudo` to stdio only
- `sudo` may or may not apply `closefrom`-style cleanup before execing the helper

The useful design claim is narrower and stronger:

- the helper itself closes every inherited fd `>= 3` before sandboxing
- any fd the helper opens for policy validation is explicitly `CLOEXEC`
- helper-side session temp preparation leaves no additional live descriptors at
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

## TLA+ Model

### Abstract FD Model

The model uses six abstract fds:

| FD | Meaning |
|----|---------|
| `0,1,2` | stdio |
| `3` | inherited credential-bearing fd |
| `4` | inherited extra fd that may be benign or host-authority-bearing |
| `5` | helper-opened policy file |

Each fd also tracks:

- target class: `stdio`, `credential`, `benign`, `policy`, `authority`, `unused`
- origin: `shell`, `helper`, `none`
- `CLOEXEC` flag

### Launch Stages

The state machine follows the actual native launch chain at the point where fd
inheritance matters:

1. `hazmat`
2. `sudo`
3. `hazmat-launch`
4. helper fd sanitization
5. helper policy-file open
6. optional helper-side session temp preparation, leaving no extra live fds
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

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
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

- relying on Go's current exec behavior or `sudo`'s current fd cleanup is not a
  proof, because either upstream behavior can be toggled adversarially in the model
- the first helper-side action must therefore be inherited-fd cleanup
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

## Interpretation

This proof does not claim anything about macOS kernel internals or `sudo`
implementation details. It proves a stronger Hazmat-specific boundary:

- even if upstream exec behavior is less hygienic than expected,
- and even if `sudo` contributes no cleanup,
- the helper still reaches `sandbox_init()` with an allowlisted fd table
- and any host-side broker authority is minted only after confirmed containment

That turns file-descriptor hygiene from an implicit runtime assumption into an
explicit checked design rule for the native launch path, and keeps the
attestation authority boundary tied to the same confirmed launch event.
