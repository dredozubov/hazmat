# Changelog

All notable changes to Hazmat are documented in this file.

## [Unreleased]

### Added
- Multi-key per-project Git SSH routing. `hazmat config ssh add --name <n> --host <h>... <path>` appends a named, host-scoped key; `hazmat config ssh remove --name <n>` removes one. Each destination host resolves to exactly one configured key; overlap and mixed legacy/new configs are rejected at config-save time.
- Reusable SSH profiles. `ssh_profiles:` in `~/.hazmat/config.yaml` defines a named identity (private key + optional known_hosts + optional default_hosts) usable from any project via `hazmat config ssh add --profile <name>`. Project keys inherit `default_hosts` when they declare no hosts of their own; declared `--host` always overrides. Full CLI: `hazmat config ssh profile add | list | show | remove | rename`. Removal refuses while any project references the profile; `--force` detaches and removes atomically. Rename updates every referrer in one save.
- TLA+ formal verification of the multi-key routing and profile resolution contract (`MC_GitSSHRouting`). 884,736 distinct states checked for determinism, overlap rejection, legacy single-key fallback, per-key socket distinctness, dangling-reference rejection, profile+inline identity conflict rejection, and orphan-key rejection.

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

[Unreleased]: https://github.com/dredozubov/hazmat/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/dredozubov/hazmat/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/dredozubov/hazmat/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dredozubov/hazmat/compare/v0.4.3...v0.5.0
[0.4.3]: https://github.com/dredozubov/hazmat/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/dredozubov/hazmat/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/dredozubov/hazmat/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/dredozubov/hazmat/releases/tag/v0.4.0
