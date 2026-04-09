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
- the final agent process starts with stdio only

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/agent_launch.go` | native sudo + helper launch construction |
| `hazmat/session.go` | `runAgentSeatbeltScriptWithUI()`, policy-file generation |
| `hazmat/cmd/hazmat-launch/main.go` | helper-side fd cleanup, policy read, `sandbox_init()`, final `exec` |

## TLA+ Model

### Abstract FD Model

The model uses six abstract fds:

| FD | Meaning |
|----|---------|
| `0,1,2` | stdio |
| `3` | inherited credential-bearing fd |
| `4` | inherited benign extra fd |
| `5` | helper-opened policy file |

Each fd also tracks:

- target class: `stdio`, `credential`, `benign`, `policy`, `unused`
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
6. `sandbox_init()`
7. final agent `exec`

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

## What This Found

This model makes one design fact explicit:

- relying on Go's current exec behavior or `sudo`'s current fd cleanup is not a
  proof, because either upstream behavior can be toggled adversarially in the model
- the first helper-side action must therefore be inherited-fd cleanup

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
- `128 states generated`
- `112 distinct states found`
- `depth 7`
- `Finished in <1s`

## Interpretation

This proof does not claim anything about macOS kernel internals or `sudo`
implementation details. It proves a stronger Hazmat-specific boundary:

- even if upstream exec behavior is less hygienic than expected,
- and even if `sudo` contributes no cleanup,
- the helper still reaches `sandbox_init()` with an allowlisted fd table

That turns file-descriptor hygiene from an implicit runtime assumption into an
explicit checked design rule for the native launch path.
