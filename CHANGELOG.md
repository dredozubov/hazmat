# Changelog

All notable changes to Hazmat are documented in this file.
Safety-facing entries should follow the proof/caveat convention in
[docs/release-notes.md](docs/release-notes.md).

## [Unreleased]

## [0.10.2] - 2026-08-25

### Added
- Typed OpenAI-compatible HTTP and stdio MCP proxy foundations, including bounded process lifecycle, policy/evidence records, external provider attachment modeling, and a strict Planescape provider client.
- Runtime-provider admission and evidence lanes for macOS current-user Seatbelt, Apple Container development, and Linux current-user/agent-user designs.
- A live supported-harness matrix that supplies CI provider credentials as process-scoped environment grants and records redacted per-harness evidence.

### Changed
- Runtime launch preparation now separates reusable provider authority from harness-specific session plumbing, and external proxy mode no longer depends on an internal provider declaration.
- The pinned `https://claude.ai/install.sh` snapshot now trusts the reviewed upstream installer with optional checksum-verified Zstandard downloads and full-binary fallback.

### Fixed
- `hazmat claude` skips host login-Keychain synchronization by default so startup does not block on a macOS keychain password prompt. Set `HAZMAT_CLAUDE_HOST_KEYCHAIN_SYNC=1` to opt into host Keychain import/publish.
- Claude credential materialization is restricted to the expected Keychain item and happens only after the installed CLI version probe; Antigravity state is isolated from unrelated agent Keychain data.
- Live provider tokens stay out of the persistent host secret store, broker child launches require the trusted helper path, and repo setup approval cannot be inferred from a nominally safe command.

### Proof
- `tla/MC_ServiceHarnessLifecycle.tla`, `tla/VERIFIED.md`, runtime-provider tests, package-boundary guards, and the live-harness contract cover the modeled authority and proxy boundaries.
- Release preflight runs Go vet/tests, import-boundary checks, entrypoint guards, and CLI smoke tests before producing artifacts.

### Caveats
- Native Linux execution remains plan-only: setup, rollback, and release artifacts are not yet promoted. Apple Container Linux is a development lane.
- Live harness results remain fixture- and approval-dependent; proxy foundations do not imply blanket support for arbitrary providers or MCP servers.

### Next
- Complete and verify the native Linux setup/rollback/runtime path before publishing Linux artifacts.

## [0.10.1] - 2026-06-26

### Fixed
- `hazmat claude` no longer re-runs Claude's onboarding/sign-in flow on each launch. Partial in-session Claude state updates are now merged into the host-owned store and materialized before launch, so the agent stays onboarded across sessions.

## [0.10.0] - 2026-06-26

### Added
- Antigravity is now a contained harness, launched with `hazmat antigravity` (alias `agy`), replacing the Gemini harness after Google retired the standalone Gemini CLI. The installer is pinned and checksum-verified, sessions receive the read-only macOS Security-framework TLS surface needed for the Google OAuth token exchange, and the agent login-keychain OAuth item remains an adapter-required external boundary.
- Local model-cache detection for Ollama, PyTorch Hub, and Hugging Face. When a project uses one of these caches, Hazmat suggests the matching integration and grants read-only cache access for offline or cached inference.

### Changed
- Setup creates the agent toolchain directories in a single `sudo` prompt instead of prompting once per directory.
- `hazmat check` and `hazmat doctor` summarize the repair plan and concrete next steps directly in the terminal.

### Fixed
- `hazmat claude` no longer aborts at launch with `security add-generic-password failed: exit status 2`; the agent Keychain write now passes the required `-a <account>` argument.
- Agent toolchain and project directory ownership repair no longer follows symlinks, and agents can again create and write the shared subdirectories owned by the `dev` group. (#17)
- Host Keychain synchronization fails closed when the host Keychain item's freshness cannot be determined, instead of risking a stale or empty credential write.

### Removed
- The Gemini harness and the `hazmat gemini` command, superseded by Antigravity.

## [0.9.0] - 2026-06-10

### Added
- `hazmat harness status|update|uninstall` now exposes per-harness lifecycle management for the agent user. Status reports binary probes, recorded state, import status, credential hints, managed code artifacts, and preserved auth/profile/session boundaries; update shares the existing bootstrap paths; uninstall removes only declared Hazmat-owned code artifacts and selected harness metadata by default.
- Qwen Code is now a contained harness. `hazmat bootstrap qwen` installs `@qwen-code/qwen-code@latest` into the agent user's local prefix, prepares `/Users/agent/.qwen`, launches Qwen through `hazmat qwen`, applies the configured permission-bypass mode without duplicating `--yolo`, and keeps host `~/.qwen` auth/settings out of the v1 import surface.
- Hermes is now an experimental foreground-only contained harness. `hazmat bootstrap hermes` verifies a manually installed agent-owned executable, sessions use managed `HERMES_HOME=/Users/agent/.hazmat/hermes`, provider API keys can be delivered through the shared credential registry, and gateway/dashboard/API/server/cron entrypoints are rejected in v1.
- Shared provider API keys are registry-driven and can be granted to multiple allowed harness consumers without duplicating durable secret files.
- `hazmat codex-app-server` starts a contained Codex `app-server --listen stdio://` backend, and `hazmat codex-app-shim` provides the `CODEX_CLI_PATH` compatibility entrypoint for the stock Codex desktop app.
- Developer debug tracing now supports cross-harness trace bundles for Claude, Codex, OpenCode, Gemini, Hermes, and Qwen, with macOS and Linux trace backends behind strict debug-build gates.

### Changed
- Docker Sandbox routing now covers Claude, Codex, OpenCode, Gemini, Hermes, Qwen, `hazmat shell`, and `hazmat exec` through the shared session backend.
- Harness launch, asset sync, credential delivery, and integration resolution share more of the same backend/session contract plumbing across native and Docker paths.

### Fixed
- `hazmat init` and `hazmat bootstrap claude` no longer abort with "Claude installer checksum mismatch": the pinned SHA-256 of `https://claude.ai/install.sh` matches the current upstream installer again, and a daily CI drift check now opens a bump PR with a reviewable script diff whenever Anthropic updates the installer. (#11, #12)
- Native Claude API-key sessions launch in bare mode so newer Claude Code builds do not prompt for the dedicated agent account Keychain.
- Native Claude OAuth/imported-subscription sessions prepare and unlock the dedicated agent login keychain before launch, with `hazmat claude-keychain doctor|reset` for inspection and recovery.
- Harness lifecycle and synthetic e2e smoke coverage now protect Hermes/Qwen state setup and Claude host-owned auth harvest behavior during release gates.

## [0.8.1] - 2026-05-23

### Fixed
- Upgraded the embedded Kopia dependency to v0.23.0, picking up the upstream fix for CVE-2026-45695 / GHSA-2q4c-3mrw-63c3, an unauthenticated RCE path through SSH ProxyCommand handling in Kopia server storage probing.

## [0.8.0] - 2026-05-22

### Added
- Interactive setup and session commands now notify at command start and exit when the Homebrew tap metadata says a newer Hazmat release is available, without invoking `brew` during startup.
- Gemini is now a first-class contained harness, with setup import, explain-mode coverage, resume sync, and Docker Sandbox routing across every harness entrypoint.
- Repo onboarding can approve, persist, or reject suggested integrations, with expanded built-in stack coverage across Python, JS/TS package managers, mobile, infra, and build systems.
- Repo-local Git hooks now have a Hazmat-managed approval path with manifest hashing, install/review/uninstall UX, runtime enforcement, and rollback support.
- Credential handling now routes imported harness auth, Git HTTPS/GitHub credentials, Git SSH identities, and cloud backup credentials through typed host-owned capability stores instead of broad sandbox grants.
- Multi-key per-project Git SSH routing. `hazmat config ssh add --name <n> --host <h>... <path>` appends a named, host-scoped key; `hazmat config ssh remove --name <n>` removes one. Each destination host resolves to exactly one configured key; overlap and mixed legacy/new configs are rejected at config-save time.
- Reusable SSH profiles. `ssh_profiles:` in `~/.hazmat/config.yaml` defines a named identity (private key + optional known_hosts + optional default_hosts) usable from any project via `hazmat config ssh add --profile <name>`. Project keys inherit `default_hosts` when they declare no hosts of their own; declared `--host` always overrides. Full CLI: `hazmat config ssh profile add | list | show | remove | rename`. Removal refuses while any project references the profile; `--force` detaches and removes atomically. Rename updates every referrer in one save.
- TLA+ formal verification of the routing + profile resolution contract (`MC_GitSSHRouting`). Nine invariants checked across 884,736 distinct states: determinism, overlap rejection, host-outside-allowlist rejection, inline-key-has-declared-hosts, per-key socket distinctness, dangling-reference rejection, profile+inline identity conflict rejection, orphan-key rejection, and binding integrity.

### Changed (breaking)
- Docker routing now defaults to native code-only containment (`--docker=none`). Use `--docker=sandbox`, `--docker=auto`, or `hazmat config docker auto` to opt into Docker Sandbox routing for private-daemon Docker workflows.
- Retired the legacy any-host SSH fallback. Every inline project SSH key must now declare at least one `--host`. The `hazmat config ssh set <path>` subcommand has been removed — use `hazmat config ssh add --name <n> --host <h> <path>` instead. Configs that still use the pre-migration flat shape (`ssh: {private_key, known_hosts}` with no `keys:` list) are rejected at load with a copy-paste YAML snippet showing the replacement.

### Fixed
- First launch in large worktrees no longer shells out once per file during project and `.git` ACL repair.
- Contained sessions are easier to stop and resume: SIGTERM/SIGINT now propagate through supervised harnesses, and transcript syncing works for Claude, Codex, Gemini, and OpenCode.
- Harness asset-sync warnings now explain skipped host prompt files, including symlink escapes outside the managed source root.

## [0.7.0] - 2026-04-18

### Added
- Managed harness prompt-asset sync for built-in harness commands, toggled via `session.harness_assets` (default on) with a per-launch `--skip-harness-assets-sync` escape hatch

### Changed
- Route Hazmat-owned agent maintenance (bootstrap, config import, git safe-directory, SSH setup) through `hazmat-launch` under the narrow NOPASSWD rule; the broader opt-in sudoers rule is now only needed for manual `sudo -u agent` commands
- Resolve macOS system utilities (`chmod`, `sudo`, `ls`, `dscl`, `pfctl`, `launchctl`, `git`, etc.) by absolute path so Homebrew coreutils on `PATH` can no longer shadow `/bin/chmod` and break ACL repair (#7)

### Fixed
- ACL detection on directories: `pathHasDevACL` now inspects the directory itself, and the agent traverse-ACL check accepts macOS's normalized `search` token, so rollback and down-migration reliably remove the traverse ACL

## [0.6.0] - 2026-04-10

### Added
- Managed per-project git SSH key selection (`hazmat config ssh set`, `unset`, `test`)
- SSH key shell completions
- SSH test support for host aliases
- Show selected SSH key in session contract
- Opt-in agent maintenance sudoers rule (`hazmat config sudoers --enable-agent-maintenance`)
- Default maintenance sudoers on `init --yes`
- User-level Hazmat install targets in Makefile
- TLA+ formal verification of native helper fd isolation (`MC_LaunchFDIsolation`)

### Changed
- Reject public keys in `hazmat config ssh set` (must be private key)
- Harden native launch fd isolation before `sandbox_init()`
- Move Makefile to repo root
- Simplify SSH key selection UX with positional key-path argument

### Fixed
- Managed git SSH agent auth
- SSH test probe host parsing
- User-local launch helper startup
- Keep optional sudoers within verified containment
- Sudo cwd for agent bootstrap

### Tests
- Run e2e unit tests from hazmat module
- Document and test SSH test alias behavior and security boundary

## [0.5.0] - 2026-04-08

### Added
- Zsh completion support during `hazmat init` with system fpath installation
- AI-assisted release script with CHANGELOG management
- Blog post link in README

### Fixed
- Shell permission denied on fresh installs (#3)
- Zsh completion file permissions set to 644 after sudo write
- Release script quoting by writing prompt to temp file

### Tests
- Strengthen shell permission denied regressions

## [0.4.3] - 2026-04-05

### Added
- `curl | bash` install script for GitHub releases

### Fixed
- Session sync permissions: `agentSessionDir()` now uses `sudo mkdir + chmod 2770` so the host user can create temp files for `--resume`

### Tests
- 17 new unit tests covering resume/export pure functions

## [0.4.2] - 2026-04-05

### Fixed
- Session startup no longer requires sudo password
- Export/resume works after relaxing agent umask and bootstrap permissions
- ACL walk skips `.git`, `.venv`, `vendor` contents for performance
- `safe.directory` write reverted to `sudo -u agent`
- `requireInit` guard made mockable for CI

### Changed
- `hazmat config agent` now runs for all harnesses, not just Claude

### Tests
- Verify `requireInit` guard and bootstrap permissions

## [0.4.1] - 2026-04-04

### Added
- VHS tape recordings for quickstart demo
- Auto-install `hazmat-launch` from Homebrew libexec during init

### Fixed
- `TERMINFO_DIRS` leak in `TestAgentEnvPairsExposeSessionConfig`

## [0.4.0] - 2026-04-03

First tagged release with the full containment stack.

### Added
- Dedicated `agent` macOS user with kernel-enforced seatbelt sandbox
- Per-session SBPL policy generation via `sandbox_init()` (cgo)
- `pf` firewall rules scoped to agent user (SMTP, IRC, FTP, Tor, VPN blocked)
- DNS blocklist for known tunnel/paste/C2 services
- Supply chain hardening: npm `ignore-scripts=true`, pip trusted-host lockdown
- Automatic Kopia snapshots before every session with local + S3 cloud backup
- `hazmat claude`, `hazmat shell`, `hazmat exec` session commands
- `hazmat init` one-time setup with interactive bootstrap for Claude/Codex/OpenCode
- `hazmat rollback` to undo all system changes
- `hazmat config` unified configuration system
- `hazmat check` integration test suite
- `hazmat explain` session preview
- `hazmat export claude session` for handing sessions back to host Claude
- `hazmat snapshots`, `hazmat diff`, `hazmat restore` for snapshot management
- Docker Sandbox mode for private-daemon projects
- Session integrations for Go, Node, Python, TLA+, and more
- Repo-recommended integrations via `.hazmat/integrations.yaml`
- TLA+ formal verification of 8 subsystems (setup/rollback ordering, seatbelt policy, backup safety, version migration, Tier 3 launch containment, tier policy equivalence, session permission repairs, harness lifecycle)
- GitHub Actions CI: lint, test, TLA+ model checking, cross-compile, E2E lifecycle
- Homebrew tap distribution (`brew install dredozubov/tap/hazmat`)

[Unreleased]: https://github.com/dredozubov/hazmat/compare/v0.10.1...HEAD
[0.10.1]: https://github.com/dredozubov/hazmat/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/dredozubov/hazmat/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/dredozubov/hazmat/compare/v0.8.1...v0.9.0
[0.8.1]: https://github.com/dredozubov/hazmat/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/dredozubov/hazmat/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/dredozubov/hazmat/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/dredozubov/hazmat/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dredozubov/hazmat/compare/v0.4.3...v0.5.0
[0.4.3]: https://github.com/dredozubov/hazmat/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/dredozubov/hazmat/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/dredozubov/hazmat/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/dredozubov/hazmat/releases/tag/v0.4.0
