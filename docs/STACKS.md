# Stack Coverage

The working list of stacks Hazmat ships built-in integrations for. Each entry shows the detection marker, the smoke-test status, and a short note about anything unusual.

The integration schema and the hard limits on what any manifest is allowed to do live in [integrations.md](integrations.md) under "What Integrations Cannot Do".

## Status legend

- ✓ — end-to-end run verified inside `hazmat exec`
- ◐ — unit-test coverage only; end-to-end deferred (real toolchain, emulator, or cluster required)
- ⊘ — works with a documented limitation (warning shipped in the manifest)

## Python

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `python-uv` | `uv.lock` | ✓ | Probes the active Python interpreter; `~/.local/share/uv` and `~/.cache/uv` opened for the agent. |
| `python-pip` | `requirements.txt` (when no `uv.lock` or `poetry.lock`) | ✓ | Plain pip + venv. Same interpreter probe as `python-uv`; arbitrates against the other two so only the right one fires. |
| `python-poetry` | `poetry.lock` | ✓ | `~/.local/share/pypoetry` opened. |

## JavaScript / TypeScript

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `node` | `package.json` | ✓ | Active runtime probe; on macOS also opens `/opt/homebrew/lib/node_modules`. |
| `pnpm` | `pnpm-lock.yaml` | ✓ | Coexists with `node`; opens the pnpm content-addressable store at `~/.local/share/pnpm` or `~/Library/pnpm`. |
| `yarn` | `yarn.lock` | ✓ | Classic and Berry. Cache locations at `~/.cache/yarn`, `~/Library/Caches/Yarn`, `~/.yarn`. Berry's zero-install `.yarn/cache` is intentionally not snapshot-excluded. |
| `bun` | `bun.lockb`, `bun.lock`, `bunfig.toml` | ✓ | LookPath probe; `~/.bun` opened. Same runtime Claude Code itself uses. |
| `deno` | `deno.json`, `deno.jsonc`, `deno.lock` | ⊘ | Local cache opened. Remote URL imports need network authority the integration does not grant; pre-cache with `deno cache` before the session. |

## JVM

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `java-gradle` | `build.gradle`, `settings.gradle`, `.kts` variants | ✓ | Resolved JDK home plus Gradle prefix. |
| `java-maven` | `pom.xml` | ✓ | Resolved JDK home plus Maven prefix. |
| `tla-java` | `*.cfg` with sibling `*.tla` | ✓ | TLA+ model-checking. Java + `tla2tools.jar`. |

## Mobile

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `swift` | `Package.swift` | ⊘ | Probes Xcode developer dir via `xcode-select -p`. SwiftPM's own nested sandbox is incompatible with running inside Hazmat; pass `--disable-sandbox` to `swift build/test/run`. |
| `android-gradle` | `AndroidManifest.xml`, `local.properties` | ◐ | SDK probe (`$ANDROID_HOME` / `$ANDROID_SDK_ROOT` / `~/Library/Android/sdk`). Layers on top of `java-gradle`. End-to-end `./gradlew assembleDebug` deferred (needs a real device + SDK). |
| `flutter` | `pubspec.yaml` | ⊘ | Probes the Flutter SDK; opens `~/.pub-cache`. Two warnings shipped: (1) the Flutter SDK at `/opt/homebrew/share/flutter` is a git checkout and trips `git config --add safe.directory` for the agent user; (2) online `pub.dev` fetches hit a TLS verification gap. |

## Systems and other languages

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `go` | `go.mod` | ✓ | Resolved GOROOT. |
| `rust` | `Cargo.toml` | ✓ | Resolved sysroot from `rustc --print sysroot`. |
| `cmake` | `CMakeLists.txt` | ✓ | C / C++. Probes cmake plus the Xcode CLT toolchain for clang. |
| `haskell-cabal` | `cabal.project`, `*.cabal`, `stack.yaml` | ✓ | Resolved GHC and Cabal prefixes. |
| `elixir-mix` | `mix.exs`, `mix.lock` | ✓ | Resolved Elixir and Erlang prefixes. |
| `ruby-bundler` | `Gemfile`, `Gemfile.lock` | ✓ | Resolved Ruby prefix. |
| `php-composer` | `composer.json` | ✓ | `~/.composer`, `~/.config/composer`, `~/.cache/composer` opened. |
| `dotnet` | `global.json`, `*.csproj`, `*.fsproj`, `*.vbproj`, `*.sln`, `Directory.Build.props` | ✓ | LookPath probe; `~/.nuget`, `~/.dotnet`, `~/.local/share/NuGet` opened. |

## Infrastructure and build

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `terraform-plan` | `*.tf` | ✓ | Plan and validate only; no apply, no cloud credentials. |
| `opentofu-plan` | manual activation | ✓ | Same shape as `terraform-plan`, OpenTofu binary. |
| `kubernetes-render` | `Chart.yaml`, `kustomization.yaml`, `helmfile.yaml` | ✓ | Render, lint, and template only. `KUBECONFIG` and `~/.kube` are explicitly NOT in scope; cluster operations are out of native containment by design. |
| `docker` | `Dockerfile`, `compose.*.{yml,yaml}`, `.devcontainer/` | ⊘ | The integration manifest is a thin UX surface over the existing Tier 3 Docker Sandbox mode. Docker commands do not work in native containment; the manifest's warning points at `--docker=sandbox`. See [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md). |

## Project tooling

| Integration | Detects | Status | Notes |
|---|---|---|---|
| `beads` | `.beads/` root dir | ✓ | The `bd` and `dolt` binaries get a Homebrew Cellar permission repair if they were installed 0700 and the agent user could not exec them. |

## How activation works

The fastest path is `hazmat <harness> --integration <name>` for a single session (the flag accepts multiple). To make that sticky on a project you revisit often, pin it with `hazmat config set integrations.pin "~/workspace/my-app:python-uv,node"`. The third option is a checked-in `.hazmat/integrations.yaml` that other contributors pick up after a one-time host approval.

When a project has a marker that matches a built-in (`uv.lock`, `package.json`, `Cargo.toml`, etc.) and you have not pinned anything, Hazmat surfaces the suggestion at session start and you can accept inline.

## Recipes that combine these

The [recipes](recipes/) directory shows worked end-to-end setups, including the three database recipes (SQLite, ephemeral Tier 3 PostgreSQL/Redis, and cloud DBs without credential grants).

## Adding a new integration

Manifests are pure YAML. The full schema is in [integrations.md](integrations.md); a step-by-step author guide is in [integration-author-kit.md](integration-author-kit.md). If your stack is missing from this list, that is a contribution gap, not a fundamental limitation.
