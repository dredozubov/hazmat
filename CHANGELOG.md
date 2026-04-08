# Changelog

All notable changes to Hazmat are documented in this file.

## [Unreleased]

### Added
- Zsh completion support during `hazmat init` with managed fpath block
- Blog post link in README

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

[Unreleased]: https://github.com/dredozubov/hazmat/compare/v0.4.3...HEAD
[0.4.3]: https://github.com/dredozubov/hazmat/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/dredozubov/hazmat/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/dredozubov/hazmat/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/dredozubov/hazmat/releases/tag/v0.4.0
