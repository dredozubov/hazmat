# Explicit Session Integrations Exploration

Status: Proposed
Date: 2026-04-03
Related issue: `sandboxing-4o0`

## Position

Hazmat should move away from user-facing "packs" as the main abstraction for
"batteries included" developer ergonomics. The durable security and UX model is
already path-based:

- the sandbox contract is defined by exact read-only directories, exact
  read-write directories, and an explicit env surface
- the session contract is legible only when it shows the resulting directories,
  not an opaque profile name
- Homebrew is not part of the trust model; it is one possible resolver for
  discovering machine-local paths

The replacement model should therefore be **explicit session integrations**:

- Hazmat computes a small set of auto-integrations for well-understood stacks
- each integration resolves concrete directories at launch time
- user-controlled extensions remain explicit and path-based
- the session contract shows the final effective paths and, optionally, their
  provenance

This keeps Hazmat consistent with its existing contract-first design. The
security boundary remains the resolved path list, not formula names, recipes,
or repo-declared stack hints.

## Goals

- Keep the user-facing model path-based and explicit
- Preserve a "batteries included" experience for common macOS development
  setups
- Let Hazmat use Homebrew metadata as a narrow path resolver for known stacks
- Make automatic behavior legible in the session contract
- Provide explicit host-owned read and write extensions similar to Docker mode
  overrides

## Non-Goals

- Full recipe introspection or generic package-manager exploration
- A second package database vendored into the Hazmat repo
- Repo-controlled requests for credential capabilities or arbitrary path
  expansion
- Silent widening of session authority beyond the resolved path contract

## Recommended Model

### 1. Auto-integrations for common stacks

Hazmat ships a bounded set of integration rules for common macOS ecosystems. A
rule may inspect:

- project markers such as `package.json`, `Cargo.toml`, `pyproject.toml`,
  `go.mod`, `pom.xml`, `Package.swift`
- stable local filesystem conventions
- `brew --prefix <formula>` for a narrow allowlist of known formulas

Each rule resolves to a concrete set of candidate paths. Only paths that exist
and pass Hazmat's normal credential-deny and canonicalization checks are added.

### 2. Consent for Homebrew-backed resolution

Hazmat should ask once during `hazmat init` or at first relevant launch whether
it may use Homebrew metadata to improve session integrations. That consent is
not the actual policy boundary. It is a UX boundary that keeps the behavior
explicit and predictable.

Actual path resolution should still happen at launch time so Homebrew upgrades,
formula relinks, and newly installed toolchains are reflected automatically.

### 3. Explicit user extensions

User-managed extensions should match the existing Hazmat style: host-owned,
path-based, and project-scoped when needed. The important abstraction is not
"enable profile X" but:

- add these read-only directories to this project's sessions
- add these extra write directories to this project's sessions
- show the result in the session contract before launch

This mirrors Docker routing well: safe defaults first, then explicit user
overrides.

### 4. Contract output

The session contract should continue to show the exact effective directory
lists. Provenance can be additive, but secondary. For example:

- `Extra read-only: /opt/homebrew/opt/openjdk, /Users/dr/.cargo/registry`
- `Auto-integrations: openjdk (Homebrew), rust (filesystem)`
- `User extensions: /Users/dr/workspace/shared-lib (read-only)`

The real contract remains directory-based.

## Architecture Sketch

The implementation should separate four concerns that are currently mixed by
"packs":

1. detection
   project markers and explicit user/project config
2. resolution
   Homebrew prefixes and filesystem probing for known integration rules
3. policy validation
   canonicalization, deny-zone enforcement, duplication removal, runtime
   compatibility checks
4. presentation
   session contract sections and status-bar labels

This keeps the runtime honest. Resolution may use Homebrew, but validation and
contract rendering should only see resolved directories and env keys.

## Research Scope

Hazmat needs a focused exploration pass across major macOS/Homebrew ecosystems:

- Python
- Node and TypeScript
- Go
- Rust
- C
- C++
- Java
- Kotlin
- Swift
- Scala
- Haskell
- OCaml

For each ecosystem, the exploration should answer:

1. Which project markers are reliable enough for passive detection?
2. Which Homebrew formulas or built-in macOS toolchains matter?
3. Which directories are stable read-only candidates?
4. Which mutable caches, registries, virtualenvs, or package stores should stay
   outside default scope unless explicitly granted?
5. Which env keys are passive selectors/path pointers, and which are dangerous
   flag or preload vectors?
6. Should Hazmat resolve paths from Homebrew metadata, filesystem probing, or
   both?

## Initial Exploration Takeaways

The first research pass already points to a clearer architecture than the
current pack model:

- prefer language-native runtime introspection over Homebrew recipe parsing
- use `brew --prefix <formula>` as a fallback resolver, not the primary model
- auto-add stable toolchain roots and shared dependency caches only when they
  are clearly read-only and non-secret
- keep mutable package stores, compiler caches, switches, and per-project build
  outputs out of default scope unless the user grants them explicitly

The ecosystem clustering also looks uneven in a useful way:

- **Python, Node/TypeScript, Go, Rust** benefit from runtime probing (`python`,
  `npm`, `go env`, `cargo`/`rustup`) more than from Homebrew metadata alone
- **Java, Kotlin, Scala** look like one shared JVM integration family where
  Hazmat should resolve concrete JDK homes and keep Gradle/Maven/Coursier state
  explicit
- **Swift** is primarily an Apple toolchain/Xcode path problem on macOS, with
  Homebrew as an optional fallback rather than the default story
- **C/C++** are strong candidates for Apple-toolchain-first auto-integrations
  plus optional Homebrew `llvm`/`gcc` overlays
- **Haskell and OCaml** appear to need a smaller automatic story and a much
  stronger explicit-extension story because the useful state usually lives in
  user-private roots such as `~/.cabal`, `~/.stack`, `~/.ghcup`, and `~/.opam`

This supports the main design direction: use explicit integrations where the
platform layout is stable, and use explicit user extensions where the real
value lives in mutable home-directory state.

## Error Handling

- Missing formulas or absent local toolchains should degrade silently to "no
  integration applied"
- Invalid or credential-overlapping resolved paths should fail closed and
  surface a clear warning
- Runtime backends that cannot honor a resolved feature should fail explicitly
  rather than silently dropping it
- User extensions should always be visible in the contract and config output

## Testing Direction

- Unit tests for resolver logic per ecosystem/integration
- Unit tests for canonical path validation and deny-zone rejection
- Session contract tests showing auto-integrations separately from user
  extensions
- Tests proving launch-time resolution updates when Homebrew prefixes change
- Backend parity tests for native and Docker-backed session preparation

## Deliverables From Exploration

- An ecosystem matrix for major macOS language/toolchain layouts
- A proposed config surface for explicit read/write extensions
- A proposed session-contract rewrite that replaces user-facing "packs"
- A migration plan from existing built-in packs to explicit integrations or
  explicit capabilities where appropriate

## Tracked Exploration Tasks

- `sandboxing-06s` — Python, Node/TypeScript, Go, Rust
- `sandboxing-nwz` — Java, Kotlin, Scala, Swift
- `sandboxing-8ji` — C, C++, Haskell, OCaml
- `sandboxing-6e8` — Homebrew consent and launch-time resolution
- `sandboxing-v0k` — explicit read/write extension config
- `sandboxing-28u` — migration plan from packs
