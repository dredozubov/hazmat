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

## Homebrew-Backed Resolver Design

Homebrew resolution needs tighter rules than the generic integration model. The
goal is not to teach Hazmat about Homebrew as a policy layer. The goal is to
let Hazmat ask a known local package manager where a known toolchain lives,
then print the resulting directories in the session contract.

### Consent storage

Recommend one host-owned consent setting in `~/.hazmat/config.yaml`:

- absent: ask during `hazmat init` or the first launch that would otherwise use
  Homebrew-backed resolution
- enabled: allow Homebrew-backed resolution for known integrations
- disabled: skip Homebrew-backed resolution until the host user changes the
  setting

This should be global in the first slice. Per-project overrides are extra UX
surface without adding a real security boundary, because the current seatbelt
base rules already expose `/opt/homebrew` and `/usr/local` for reading.

The prompt language must stay honest. Recommended wording:

> Hazmat can inspect local Homebrew metadata to resolve known integration paths
> at launch time. This does not by itself grant new filesystem access; it only
> helps Hazmat compute the directory list it will show before launch. Allow
> Homebrew-backed path resolution?

### Resolver inputs and boundaries

Homebrew should remain a narrow, built-in resolver:

- integration rules own the formula allowlist; repo files and user extensions
  do not get to name arbitrary formulas
- Hazmat should probe only canonical Homebrew executables at
  `/opt/homebrew/bin/brew` and `/usr/local/bin/brew`, not an arbitrary `brew`
  found via `PATH`
- probe commands should run with a minimal environment plus
  `HOMEBREW_NO_AUTO_UPDATE=1` so path resolution does not trigger package
  manager mutation
- machine-readable queries should prefer `brew --prefix --installed <formula>`
  for fast prefix lookup and `brew info --json=v1 --installed <formula>` when
  structured metadata is needed
- Hazmat should not parse formula Ruby, walk the Cellar generically, or treat
  `brew list` output as the primary contract source

This keeps the interface small and explainable. Hazmat is not exploring
Homebrew; it is resolving a short list of known formulas for a short list of
known integrations.

### Launch-time resolution pipeline

Recommended order at session setup:

1. Detect candidate integrations from project markers, explicit `--integration`
   flags, repo recommendations, and project pinning.
2. Resolve integration-owned runtime signals first. Examples: `go env`,
   `rustup`, `xcode-select`, interpreter introspection.
3. If an integration still has unresolved machine-local paths and Homebrew
   consent is enabled, probe the integration's known formula allowlist.
4. Derive concrete candidate directories from the resolved runtime/homebrew
   metadata using integration-owned mapping rules.
5. Canonicalize, de-duplicate, and reject anything that overlaps Hazmat's
   credential deny zones or other runtime restrictions.
6. Render only the surviving concrete dirs into the session contract and backend
   launch config.

This work must happen at launch, not at `init`, because Homebrew installs,
relinks, and upgrades are machine-local runtime state. The contract should
reflect the machine and repo as they exist now, not as they existed when the
project was initialized.

### Contract and explain output

The session contract should continue to foreground the concrete path list:

- `Auto read-only` shows the resolved directories
- `Integrations` shows the activated integration names
- optional provenance can annotate how a path was resolved, for example
  `node (Homebrew prefix)` or `swift (xcode-select)`

`hazmat explain` can carry more detail than the launch contract. In particular,
it is a good place to show:

- which runtime probes succeeded
- which Homebrew formulas were consulted
- whether Homebrew resolution was skipped because consent is disabled or Homebrew
  is absent

The contract itself should not show formula names unless it also shows the
actual directories they produced.

### Failure handling and operational guardrails

- Missing `brew`, missing formulas, or probe timeouts should degrade to "no
  additional dirs from this resolver" rather than blocking the session.
- Resolver commands should use short timeouts and fail closed.
- Homebrew-derived directories should never bypass Hazmat's normal path
  validation, credential deny checks, or backend compatibility checks.
- Repo-controlled files must remain unable to widen the Homebrew formula
  allowlist or force Homebrew-backed resolution on.

## Migration Plan

The migration should separate current pack behavior into three buckets rather
than replacing the word "pack" everywhere and calling the job done.

### 1. Keep as integrations

These behaviors remain appropriate as bounded, non-secret, mostly read-only
ergonomics:

| Current built-in | Destination | Notes |
|------------------|-------------|-------|
| `go` | integration | project marker + `go env` / shared module cache story |
| `node` | integration | project marker + runtime/Homebrew path resolution |
| `python-poetry` | integration | likely split into a broader Python story over time, but still an integration |
| `rust` | integration | stable toolchain roots, explicit mutable caches where needed |
| `terraform-plan` | integration | mostly snapshot excludes plus small read-only story |
| `tla-java` | integration | transitional JVM/tooling integration, likely folded into broader JVM rules later |

Integration-owned behavior should stay limited to:

- auto-resolved read-only directories
- session-scoped snapshot excludes
- safe passive env selectors
- warnings and command hints

### 2. Move to explicit extensions

These behaviors should not hide behind named profiles:

- machine-specific reference trees
- writable virtualenv directories
- mutable package stores and caches
- shared sibling repos or worktrees
- any path that is valuable mainly because of this developer's local layout

The explicit UX should stay path-based:

- per-session `-R` / `-W`
- project-scoped `hazmat config access add/remove`
- session contract showing these paths separately from auto-integrations

### 3. Move to first-class capabilities

Anything that widens authority beyond passive filesystem ergonomics should not
be modeled as an integration at all. Examples:

- GitHub or other credential-backed service access
- future package-publish or cloud-deploy authority
- any token- or credential-bearing env passthrough beyond the safe selector set

These should use separate CLI/config surfaces and stronger session-contract
language than integrations. The GitHub follow-up (`sandboxing-9lz`) is the
model for this direction.

## CLI and Config Migration

Recommended staged migration:

### Phase 1: dual-surface, integration-first

This phase is now implemented:

- `hazmat integration` is the primary inspect command
- `--integration` is the primary session flag
- `hazmat pack` and `--pack` remain legacy aliases
- project-scoped path extensions use `hazmat config access add/remove`
- the session contract separates integrations from explicit read/write
  extensions

### Phase 2: keep compatibility, narrow semantics

- keep `.hazmat/packs.yaml` as the repo recommendation file for now
- document it as recommending **integrations only**
- keep `packs.pin` / `packs.unpin` as compatibility aliases for
  `integrations.pin` / `integrations.unpin`
- ensure integrations never request write dirs or credential capabilities

### Phase 3: optional cleanup after ecosystem/resolver work stabilizes

- decide whether the repo recommendation filename should stay `.hazmat/packs.yaml`
  for compatibility or move to a clearer name
- remove `pack` wording from default docs/help once the alias has had a full
  transition window
- collapse legacy internal names where doing so no longer adds migration risk

## Repo Recommendation Implications

Repo recommendations should survive the migration, but with tighter semantics:

- they may recommend only known integrations
- they may not request explicit write dirs
- they may not request credential capabilities
- approval remains host-owned and hash-bound

This preserves the useful current workflow while keeping the security boundary
where it belongs: the resolved path/env contract, not the repo-controlled hint.

## Documentation Implications

The docs should teach the new model in this order:

1. exact read/write directories are the real session contract
2. integrations are a bounded convenience layer on top
3. explicit extensions handle machine-specific or writable state
4. capabilities are separate when authority, credentials, or external service
   access are involved

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

## Ecosystem Matrix

This matrix is based on current vendor/tooling documentation for each
ecosystem plus Homebrew's official formula and query surfaces.

### Python

- Detect: `pyproject.toml` first, then `setup.py`, `setup.cfg`, `.venv`,
  `venv`, and `pyvenv.cfg`.
- Auto read-only: resolved interpreter/toolchain prefix, preferably from the
  active interpreter, with `brew --prefix python@X.Y` as fallback.
- Keep explicit: virtualenvs outside the repo, pip caches, user site-packages,
  and tool-managed homes such as Poetry, PDM, uv, or pipx state.
- Safe env: `VIRTUAL_ENV` only when it points at an already approved path.
- Avoid env: `PYTHONPATH`, `PYTHONHOME`, `PYTHONSTARTUP`, `PYTHONUSERBASE`,
  `PIP_CONFIG_FILE`, `PIP_INDEX_URL`, `PIP_EXTRA_INDEX_URL`.
- Resolver: runtime interpreter inspection first; Homebrew fallback second. If
  an in-repo virtualenv exists, Hazmat should just use it inside project scope
  rather than broadening host read access.

### Node / TypeScript

- Detect: `package.json` first, then `workspaces`, `tsconfig.json`,
  `jsconfig.json`, and `packageManager`.
- Auto read-only: resolved Node runtime prefix, optionally resolved
  Homebrew `typescript` when the repo clearly needs a global compiler fallback.
- Keep explicit: global npm installs, npm cache, Corepack home, pnpm store,
  Yarn cache, and version-manager trees such as `~/.nvm`, `~/.volta`, and
  `~/.fnm`.
- Safe env: default to none; path-redirecting npm/corepack vars are better
  modeled as explicit host-owned extensions.
- Avoid env: `NODE_OPTIONS`, `NODE_PATH`, `npm_config_userconfig`,
  `npm_config_registry`, and arbitrary `npm_config_*` overrides.
- Resolver: repo-local tools first, then the active local Node install, then
  `brew --prefix node` / `brew --prefix typescript` as bounded fallback.

### Go

- Detect: `go.mod` for modules and `go.work` for workspaces.
- Auto read-only: `GOROOT` and a conservative read-only view of `GOMODCACHE`.
- Keep explicit: `GOCACHE`, `GOPATH/bin`, legacy `GOPATH/src` trees outside the
  repo, and any shared checkout area that the user edits directly.
- Safe env: prefer direct resolution over passthrough; if any selectors are
  preserved later, keep them to path pointers Hazmat already resolved.
- Avoid env: `GOFLAGS`, `GOTOOLCHAIN`, `GOPROXY`, `GOSUMDB`, `GOPRIVATE`,
  `GONOPROXY`, `GONOSUMDB`, `CGO_*`, `CC`, `CXX`, `PKG_CONFIG`.
- Resolver: `go env` first, Homebrew `go` prefix second. Hazmat should be
  careful not to let toolchain auto-download behavior silently widen session
  behavior.

### Rust

- Detect: `Cargo.toml`, `Cargo.lock`, `rust-toolchain`,
  `rust-toolchain.toml`, and `.cargo/config.toml`.
- Auto read-only: active Rust sysroot/toolchain root and optional `rust-src`
  component when present.
- Keep explicit: all of `CARGO_HOME`, cargo install roots, custom target dirs
  outside the repo, registry caches, and git caches unless Hazmat later grows a
  narrower selective exposure model.
- Safe env: `RUSTUP_TOOLCHAIN` only if Hazmat already resolved and validated
  the toolchain it points to.
- Avoid env: `RUSTFLAGS`, `RUSTDOCFLAGS`, `CARGO_ENCODED_RUSTFLAGS`,
  `CARGO_ENCODED_RUSTDOCFLAGS`, `RUSTC_WRAPPER`,
  `RUSTC_WORKSPACE_WRAPPER`, `CARGO_TARGET_*_RUNNER`, registry token envs.
- Resolver: `rustup` / `rustc` introspection first; `brew --prefix rust` or
  `brew --prefix rustup` as fallback.

### JVM family: Java, Kotlin, Scala

- Detect: `pom.xml`, `mvnw`, `.mvn/`, `build.gradle`,
  `build.gradle.kts`, `settings.gradle`, `settings.gradle.kts`, `build.sbt`,
  `project/build.properties`, `project.scala`, `src/main/kotlin`, and
  similar JVM build markers.
- Auto read-only: resolved JDK home first; optionally Kotlin or Scala compiler
  install roots when the project clearly depends on a system install.
- Keep explicit: `~/.m2/repository`, `~/.gradle`, `~/.ivy2`, Coursier caches,
  Scala CLI/Bloop state, Gradle-downloaded JDKs, and any project-external local
  repository override.
- Safe env: `JAVA_HOME`, and optionally `JDK_HOME` or `KOTLIN_HOME` when
  Hazmat already resolved the corresponding roots.
- Avoid env: `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `JDK_JAVA_OPTIONS`,
  `MAVEN_OPTS`, `GRADLE_OPTS`, `CLASSPATH`, `SBT_OPTS`.
- Resolver: macOS-native JDK resolution first (`JAVA_HOME` or
  `/usr/libexec/java_home`), then bounded Homebrew fallback. Kotlin and Scala
  should usually sit on top of one shared JVM resolver rather than separate
  large integration stories.

### Swift

- Detect: `Package.swift`, `.xcodeproj`, `.xcworkspace`, `Package.resolved`,
  `Sources/`, `Tests/`, and `*.swift`.
- Auto read-only: active Apple developer dir plus the selected Swift toolchain
  root when one is clearly active.
- Keep explicit: Xcode `DerivedData`, SwiftPM clone/cache state, Swiftly's
  mutable toolchain store, and any custom clone or cache directories.
- Safe env: `DEVELOPER_DIR` and `TOOLCHAINS`.
- Avoid env: `SDKROOT`, `DYLD_*`, `LD_*`, and compiler or linker flag vars.
- Resolver: Apple-toolchain-first via `xcode-select --print-path`, with
  Homebrew `swift` or Swiftly-derived toolchains as explicit fallbacks.

### C / C++

- Detect: `CMakeLists.txt`, `meson.build`, `Makefile`, `configure.ac`,
  `configure`, `compile_commands.json`, `vcpkg.json`, and `conanfile.*`.
- Auto read-only: active Apple developer dir, active SDK path, and optionally
  bounded Homebrew `llvm` or `gcc` prefixes when they are clearly relevant.
- Keep explicit: build directories, `DerivedData`, `ccache`, Conan state, and
  `vcpkg` trees.
- Safe env: `DEVELOPER_DIR`, and a validated `SDKROOT` only if Hazmat resolves
  it to the active SDK path itself.
- Avoid env: `CC`, `CXX`, `CFLAGS`, `CXXFLAGS`, `CPPFLAGS`, `LDFLAGS`,
  `CPATH`, `C_INCLUDE_PATH`, `LIBRARY_PATH`, `DYLD_*`.
- Resolver: Apple-toolchain introspection first; Homebrew toolchain prefixes as
  a narrow overlay when the project clearly opts into them.

### Haskell

- Detect: `*.cabal`, `cabal.project`, `stack.yaml`, and optionally
  `package.yaml` as a weaker secondary hint.
- Auto read-only: active compiler root only.
- Keep explicit: `~/.cabal`, `~/.stack`, most of `~/.ghcup`, package
  databases, and downloaded tarball/cache state.
- Safe env: `GHCUP_INSTALL_BASE_PREFIX`, `GHCUP_USE_XDG_DIRS`, `CABAL_DIR`,
  `STACK_ROOT`, `STACK_XDG`.
- Avoid env: `GHC_PACKAGE_PATH`, `GHC_ENVIRONMENT`, `GHCRTS`, `STACK_YAML`,
  `STACK_CONFIG`, `STACK_GLOBAL_CONFIG`, `STACK_WORK`, and any token-bearing
  env used by Stack or package registries.
- Resolver: runtime compiler inspection first, then bounded Homebrew fallback.
  Haskell should keep a small automatic story because most useful state is
  mutable and user-private.

### OCaml

- Detect: `dune-project`, `*.opam`, `dune-workspace`, and supporting
  `dune` files.
- Auto read-only: compiler stdlib/toolchain root only.
- Keep explicit: `~/.opam`, non-project-local switches, `_opam` outside the
  normal project write model, Dune build directories, and Dune cache roots.
- Safe env: `OPAMROOT`, `OPAMSWITCH`, `OPAM_SWITCH_PREFIX`, and `OCAMLLIB`
  only when Hazmat already resolved the corresponding path.
- Avoid env: `CAML_LD_LIBRARY_PATH`, `OCAMLPATH`, `OCAMLFIND_CONF`,
  `OCAMLRUNPARAM`, and generic loader overrides.
- Resolver: active compiler or switch inspection first; Homebrew `ocaml` /
  `opam` prefixes as fallback. OCaml is closer to explicit-extension-first than
  most other ecosystems in this pass.

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
- Unit tests for Homebrew consent states: unset/prompt, enabled, disabled
- Unit tests proving Hazmat probes only canonical Homebrew binaries
- Unit tests proving repo-controlled data cannot widen the formula allowlist
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
