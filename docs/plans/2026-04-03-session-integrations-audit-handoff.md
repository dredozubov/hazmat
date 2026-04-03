# Auditor Handoff: Session Integrations Exploration

Status: Proposed
Date: 2026-04-03
Owner: Hazmat
Primary issue: `sandboxing-4o0`
Handoff task: `sandboxing-ome`
Related design note: `docs/plans/2026-04-03-session-integrations-exploration-design.md`
Planning commit: `f72c74d`

## Purpose

This document is a review and audit handoff for Hazmat's current exploration of
replacing user-facing "stack packs" with an explicit session-integrations
model.

The intended audience is a security auditor, red-team reviewer, or technical
reviewer who needs a concise description of:

- the current security and UX model
- what problem the new work is trying to solve
- which design decisions are already converging
- which security invariants must remain true
- which open questions still require scrutiny before implementation

This is not an implementation spec and not a user tutorial. No runtime
behavioral change is proposed as accepted in this document by itself.

## Current State

Hazmat today already behaves as a path-based sandbox with a contract-first user
interface:

- the session contract prints the effective project write scope, extra read-only
  paths, active packs, and pack-derived notes in `hazmat/session.go`
- the base seatbelt policy already allows process execution from `/usr/local`
  and `/opt/homebrew`, and broad read access under those prefixes
- the session model already includes at least one implicit read-only toolchain
  share: Go module cache via `go env GOMODCACHE`
- "packs" are pure-data manifests that may add read-only dirs, a bounded env
  passthrough set, snapshot excludes, warnings, and commands
- repo-controlled pack recommendations are subject to host approval and are not
  treated as an authority boundary

Relevant code points an auditor should inspect:

- `hazmat/session.go`
  - `renderSessionContract` for current contract rendering
  - `implicitReadDirs` and `invokerGoModCache` for existing implicit path
    sharing
  - `generateSBPL` for the current `/usr/local` and `/opt/homebrew` base rules
  - `agentEnvPairs` for current launch-time env injection
- `hazmat/pack.go`
  - safe env allowlist
  - pack schema and path validation
  - repo pack approval flow
  - active-pack resolution order

## Problem Statement

The current "packs" concept mixes several concerns into one user-facing
abstraction:

- stack detection
- path discovery
- env passthrough policy
- snapshot exclude policy
- user ergonomics and naming
- repo recommendation and approval flow

That makes the feature harder to explain than the actual Hazmat model warrants.
The effective security boundary is not the pack name. The effective boundary is
still the concrete list of paths and env keys that reach the session.

The exploration therefore asks whether Hazmat should move to a clearer model:

- automatic session integrations for well-understood stacks
- explicit host-owned read/write extensions for local exceptions
- Homebrew used only as one path resolver, not as the security abstraction

## Proposed Direction

The current design direction is:

1. Keep the effective Hazmat contract path-based.
2. Treat Homebrew metadata as an implementation detail for discovering
   machine-local directories.
3. Replace user-facing "packs" with explicit session integrations and explicit
   user extensions.
4. Show the resulting paths directly in the session contract, with provenance as
   secondary context if useful.

In practical terms, that means Hazmat would compute session shape from:

- project markers
- runtime introspection from language tooling where available
- `brew --prefix <formula>` as a narrow fallback resolver
- explicit host-owned configuration for extra read-only or read-write paths

The proposal does **not** treat Homebrew formulas, package recipes, or repo
metadata as the real policy surface. The real policy remains:

- exact read-only directories
- exact read-write directories
- explicit env keys, validated by Hazmat

## Important Security Framing

An auditor should pay special attention to one subtle point:

Hazmat already grants broad base read access to `/usr/local` and
`/opt/homebrew` in the generated seatbelt policy. That means any future
"consent to use Homebrew metadata" would be a UX-consent boundary, not a new
filesystem security boundary, unless Hazmat also narrows the base seatbelt
policy.

This matters because the product language must stay honest. A "yes" to
Homebrew-backed resolution should be understood as:

- permission for Hazmat to inspect local Homebrew installation metadata and use
  it to compute session paths

not as:

- the moment at which the session first gains read access to Homebrew content

Unless the base seatbelt rules change, those are not the same thing.

## Resolver Guardrails That Have Converged

The exploration has now converged on a narrower Homebrew model than the
earliest brainstorming implied:

- Homebrew consent should be one host-owned global setting, not repo-owned
  state and not a new permission boundary
- Homebrew resolution should run at launch time, not only at `hazmat init`, so
  upgrades, relinks, and newly installed toolchains are reflected in the
  contract that the user sees before launch
- Hazmat should prefer runtime/toolchain introspection first and use Homebrew as
  a fallback resolver when a known integration still needs machine-local path
  discovery
- Hazmat should probe only canonical Homebrew executables at
  `/opt/homebrew/bin/brew` and `/usr/local/bin/brew`, not an arbitrary `brew`
  discovered through `PATH`
- machine-readable Homebrew access should prefer stable interfaces such as
  `brew --prefix --installed <formula>` and `brew info --json=v1 --installed
  <formula>`
- Hazmat should not parse formula Ruby, enumerate Homebrew recipes generically,
  or rely on `brew list` output as the primary policy source

These are useful audit points because they constrain the attack surface of the
resolver itself. Homebrew is treated as a bounded metadata source, not as a new
authority layer inside Hazmat.

## Evidence Gathered So Far

The first exploration pass covered the following ecosystem groups:

- Python, Node/TypeScript, Go, Rust
- Java, Kotlin, Scala, Swift
- C, C++, Haskell, OCaml

The main conclusions are:

### 1. Runtime introspection is usually better than recipe parsing

For several ecosystems, the most trustworthy source of the active local layout
is the toolchain itself:

- Python / virtualenv / interpreter inspection
- `npm prefix -g` and `npm root -g`
- `go env`
- `cargo` and `rustup`

This suggests Hazmat should prefer runtime introspection first and use
Homebrew prefix resolution as a fallback, not as the primary model.

### 2. Auto-integration is strongest where toolchain roots are stable

The clearest candidates for automatic read-only integration are stable,
non-secret toolchain roots and shared dependency caches. Examples include:

- Go `GOROOT` and module cache
- rustup toolchains and selected Cargo shared cache dirs
- JDK homes
- Xcode or Command Line Tools roots
- Homebrew `llvm` or `gcc` toolchain prefixes

### 3. User-private package stores should remain explicit

Several ecosystems derive much of their practical value from mutable state in
the user's home directory. Those are poor automatic candidates and better
modeled as explicit extensions. Examples include:

- `~/.gradle`, `~/.m2`, `~/.ivy2`, `~/.coursier`
- `~/.opam`
- `~/.cabal`, `~/.stack`, `~/.ghcup`
- `~/.conan2`, `vcpkg` trees, compiler caches
- Python and Node caches or tool-version-manager trees

### 4. Ecosystem clustering is uneven

The research suggests these review buckets:

- Python, Node/TypeScript, Go, Rust:
  strong runtime-introspection candidates
- Java, Kotlin, Scala:
  mostly one shared JVM integration family
- Swift:
  primarily an Apple toolchain problem, with Homebrew as fallback
- C/C++:
  strong Apple-toolchain-first auto-integration candidates
- Haskell and OCaml:
  limited auto story, stronger explicit-extension story

## Security Invariants That Must Be Preserved

Any implementation that follows this exploration should preserve all of the
following:

1. The effective session policy remains a concrete path list and env list.
2. Repo-controlled files must not be able to widen host path access or request
   credential capabilities.
3. Any Homebrew-derived or runtime-derived path must still pass the same
   canonicalization and credential-deny validation as any other path.
4. Automatic integrations may add only read-only, non-secret, stable toolchain
   roots or shared dependency caches.
5. Dangerous injection env vars must remain out of the passthrough surface.
6. Explicit extra read-only and read-write dirs must remain host-owned config,
   not repo-owned config.
7. The session contract must remain legible enough for the operator to see what
   was auto-added, what was explicitly configured, and why.

## Likely Review Position by Ecosystem

The current exploration suggests the following implementation posture, pending
audit review:

- **Good auto-integration candidates**
  - Go
  - Rust toolchains and selected shared caches
  - JVM toolchain roots
  - Apple/Xcode and Homebrew native toolchains for C/C++
  - Swift toolchain roots

- **Auto-integration with caution**
  - Python
  - Node / TypeScript

  The caution here is that the useful machine-local state is often mixed with
  mutable, user-private installers and caches.

- **Prefer explicit-extension-first**
  - Haskell
  - OCaml

  The main value in those ecosystems often lives in mutable home-directory
  state, which is a poor fit for silent or broad automatic inclusion.

## Review Questions for an Auditor

An auditor or red team should explicitly try to answer:

1. Does using runtime introspection or Homebrew prefix resolution create any new
   attack surface beyond the current path-based model?
2. Is the proposed product language honest about the difference between
   "Hazmat may inspect Homebrew metadata" and "the sandbox can already read
   `/opt/homebrew`"?
3. Can a project or toolchain influence runtime resolution so Hazmat mounts
   broader or different paths than intended?
4. Are user-private caches, package stores, credentials, or token-bearing files
   excluded from auto-integration by construction?
5. Are any proposed env passthrough keys actually loader, compiler, or runtime
   injection knobs in disguise?
6. Does replacing packs with integrations reduce ambiguity, or does it merely
   rename the same hidden complexity?
7. Can the migration away from repo-recommended packs preserve the current rule
   that repo content does not control authority?
8. Is there any ecosystem where automatic read-only path addition is more
   dangerous than the UX improvement is worth?
9. Does resolving `brew` from a fixed canonical location materially reduce the
   chance of resolver spoofing compared to `PATH`-based invocation?
10. Are the chosen Homebrew query surfaces stable and narrow enough to avoid
    formula-code execution or brittle output parsing?

## Tracked Work

Active beads created from this exploration:

- `sandboxing-4o0` — epic: replace stack packs with explicit session
  integrations
- `sandboxing-06s` — Python, Node/TypeScript, Go, Rust research
- `sandboxing-nwz` — Java, Kotlin, Scala, Swift research
- `sandboxing-8ji` — C, C++, Haskell, OCaml research
- `sandboxing-6e8` — Homebrew consent and launch-time resolution design
- `sandboxing-v0k` — explicit read/write extension config design
- `sandboxing-28u` — migration plan from packs
- `sandboxing-ome` — this auditor handoff document

## What Has Not Been Decided Yet

This handoff should not be read as final approval for any of the following:

- exact CLI replacement for `--pack`
- final config schema for explicit extra read-only or read-write paths
- whether repo-recommended packs disappear entirely or are replaced by a
  narrower repo advisory mechanism
- whether the base seatbelt rules for `/opt/homebrew` and `/usr/local` should
  be narrowed
- which ecosystems deserve automatic integration in v1
- which env keys, if any, survive from current pack passthrough into the new
  model unchanged

## Acceptance Criteria Before Implementation

This exploration is ready to move into implementation planning only if all of
the following are true:

- the path-based contract remains the real policy surface
- Homebrew stays a resolver, not a trust abstraction
- explicit user extensions are host-owned and legible
- the session contract can explain automatic behavior without resorting to
  opaque profile names
- dangerous env passthrough remains out of scope
- the auditor review questions above have concrete answers and test plans

## Summary

The key audit point is simple:

Hazmat is not trying to become package-manager-aware policy. It is trying to
stay an explicit path-based sandbox while becoming better at deriving safe,
machine-local defaults for common development ecosystems.

If that line becomes blurry, the design should be rejected or narrowed.
