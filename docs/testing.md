# Testing Hazmat

Hazmat has several verification surfaces. They answer different questions and
are not interchangeable.

> **Looking for the human-driven checklist?** [docs/manual-testing.md](manual-testing.md) is the release-time / post-harness-change verification list — preconditions, per-harness flows (subscription / API key / host import), cross-cutting features, regression scenarios, and recovery moves. Use it for things this automated matrix can't reach (browser OAuth, terminal UI input, real network).

## Test Matrix

| Surface | What it answers | Runs where | Destructive? |
| --- | --- | --- | --- |
| `scripts/pre-commit` | Are the staged files obviously broken before I create a commit? | Host | No |
| `hazmat check` | Is this local Hazmat install healthy right now? | Host | No |
| `scripts/pre-push` | Fast local developer gate before pushing | Host | No |
| `scripts/check-linux-compile.sh` | Does the current unsupported Linux backend compile without Darwin-only code leaking into common packages? | Host or Linux CI | No |
| `scripts/test-entrypoint-guards.sh` | Do the test harness safety rails fail loudly and correctly? | Host | No |
| `scripts/e2e-bootstrap.sh` | Can Hazmat develop Hazmat inside containment? | Host | No |
| `scripts/e2e-stack-matrix.sh` | Do supported stacks detect and behave correctly on real repos? | Host | No |
| `scripts/e2e.sh` | Does the full install / contain / backup / restore / rollback lifecycle work? | Host | Yes |
| `scripts/e2e-vm.sh` | Run the destructive lifecycle test in an isolated macOS VM | VM | Destroys the VM, not your host setup |

## Recommended Local Workflows

### Fast local loop

Use this during normal development:

```bash
git diff --cached --check
make test
make lint
bash scripts/pre-push
```

This intentionally skips the expensive or environment-heavy checks. It does run
the Linux compile-only probe, which cross-compiles test binaries into a temporary
directory and removes them before exiting.

If you install the git hooks with `make hooks`, Hazmat also adds:

- `pre-commit`: staged diff sanity, `gofmt` on staged Go files, and shell syntax
  checks for staged scripts, plus two staged secret scans:
  - `scripts/check-secret-patterns.sh --staged` — fast Google `AIza` regex (no
    dependencies)
  - `scripts/check-gitleaks.sh --staged` — broader scanner via
    [`gitleaks`](https://github.com/gitleaks/gitleaks) covering ~100 provider
    patterns and high-entropy detection (config: `.gitleaks.toml`). Requires
    `gitleaks` on `PATH`; install with `brew install gitleaks` or
    `go install github.com/zricethezav/gitleaks/v8@latest`
- `pre-push`: the shared fast gate via `scripts/check-fast.sh` (tracked-file
  Google API key scan, full-tree gitleaks scan, `go vet`, `go test`,
  `golangci-lint`, and CLI smoke tests)

### Harness guardrails

Use this when changing the test harness itself, especially destructive gating
or the shared host-side lock:

```bash
bash scripts/test-entrypoint-guards.sh
make test-entrypoint-guards
```

This is non-destructive. It only checks refusal paths.

### Self-hosting

Use this when changing containment, bootstrap, toolchain resolution, or the
developer workflow inside the Hazmat repo:

```bash
bash scripts/e2e-bootstrap.sh
```

This script assumes `hazmat init` has already been run on the host and that the
required host toolchains are available. It does not require any specific AI
coding agent harness to be installed.

### Repo-matrix validation

Use this when changing integration detection, runtime resolution, or
repo-specific usability:

```bash
bash scripts/e2e-stack-matrix.sh --contract
bash scripts/e2e-stack-matrix.sh --smoke --id next-js --id pydantic-ai
```

By default the script rebuilds the local Hazmat binary before running. Pass
`--skip-build` only when you intentionally want to trust the existing local
binary.

### Full lifecycle

Use this only on a disposable host setup, or prefer the VM wrapper below:

```bash
HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh --quick
make e2e E2E_ACK=1
```

This script runs `hazmat init`, exercises containment and restore behavior,
then runs `hazmat rollback --delete-user --delete-group --yes` before re-initializing.
It is intentionally destructive to the local Hazmat setup.

### VM-backed lifecycle

This is the safer local release-grade path:

```bash
bash scripts/e2e-vm.sh --quick
```

The VM wrapper provisions a Lume macOS guest, copies the repo into the guest,
and runs `scripts/e2e.sh` there.

## Host vs VM Model

- `hazmat check`, `pre-push`, `e2e-bootstrap`, and `e2e-stack-matrix` are
  host-side verification surfaces.
- `scripts/e2e.sh` is also host-side, but destructive.
- `scripts/e2e-vm.sh` is the isolated wrapper for the destructive lifecycle
  test.
- `scripts/check-linux-compile.sh` is compile-only. It proves the current
  unsupported Linux backend still builds; it does not claim Linux setup,
  rollback, launch, firewall, ACL, account, or service behavior is implemented.

If you want the strongest local release signal, prefer the VM path plus CI.

## Linux Support Test Plan

Until `sandboxing-pk5x` implements Linux setup and rollback resources, Linux
testing stays compile-only plus unit coverage for platform dispatch. Do not
enable Linux install or release artifacts from a compile-only result.

The first Linux implementation should land behind four gates:

1. **Model first:** extend `MC_SetupRollback` or add a scoped Linux setup /
   rollback model for Linux-owned resources such as users, groups, systemd
   units, firewall/DNS policy, sudoers, helper installation, and rollback
   cleanup.
2. **Linux unit lane:** run normal Go unit tests on `ubuntu-latest`, including
   mocked platform backend tests for Linux account, service, ACL, launch, and
   integration resolver behavior.
3. **Privileged disposable lifecycle:** run a destructive Linux e2e lane only
   in a disposable VM or disposable CI runner with the required service-manager
   and firewall capabilities. Container-only smoke tests are not enough for the
   setup/rollback contract.
4. **Artifact/install smoke:** enable Linux release artifacts and installer
   paths only after the model, Linux unit lane, and privileged lifecycle lane
   pass. The installer smoke must verify platform-specific artifact names,
   checksum validation, install layout, and rollback cleanup.

## CI Mapping

Current GitHub Actions coverage:

- `.github/workflows/ci.yml`
  - lint
  - Go vet and unit tests
  - Linux compile-only gate for the unsupported backend
  - CLI help/smoke checks via `scripts/check-cli-smoke.sh`
  - test-entrypoint guard regression checks
  - self-hosting bootstrap on macOS (`--skip-tla`)
  - repo-matrix required-track contract checks
  - TLA+ model checking
  - host-side lifecycle e2e on macOS
  - wave-1 repo-matrix smoke on push
- `.github/workflows/stack-matrix-drift.yml`
  - non-blocking scheduled drift checks against upstream heads

## Important Warnings

- Host-side test entrypoints take a shared local lock and are intended to run
  one at a time. If another host-side test is already running, they should
  fail fast instead of racing on local build outputs or Hazmat state.
- CI initializes Hazmat with `--bootstrap-agent skip` for containment-only
  jobs, so those lanes do not depend on vendor-specific agent downloads.
- Linux CI is intentionally compile-only until Linux setup/rollback resources
  are implemented and mapped to the verified TLA+ setup/rollback model.
- Do not treat `hazmat check` as a substitute for the script-based test suite.
  It validates the installed system, not the full repo release workflow.
- Do not use `scripts/e2e.sh` casually on a machine where you want to preserve
  the current Hazmat init state.
