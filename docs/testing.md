# Testing Hazmat

Hazmat has several verification surfaces. They answer different questions and
are not interchangeable.

## Test Matrix

| Surface | What it answers | Runs where | Destructive? |
| --- | --- | --- | --- |
| `hazmat check` | Is this local Hazmat install healthy right now? | Host | No |
| `scripts/pre-push` | Fast local developer gate before pushing | Host | No |
| `scripts/test-entrypoint-guards.sh` | Do the test harness safety rails fail loudly and correctly? | Host | No |
| `scripts/e2e-bootstrap.sh` | Can Hazmat develop Hazmat inside containment? | Host | No |
| `scripts/e2e-stack-matrix.sh` | Do supported stacks detect and behave correctly on real repos? | Host | No |
| `scripts/e2e.sh` | Does the full install / contain / backup / restore / rollback lifecycle work? | Host | Yes |
| `scripts/e2e-vm.sh` | Run the destructive lifecycle test in an isolated macOS VM | VM | Destroys the VM, not your host setup |

## Recommended Local Workflows

### Fast local loop

Use this during normal development:

```bash
cd hazmat
go test ./...
golangci-lint run ./...
bash ../scripts/pre-push
```

This intentionally skips the expensive or environment-heavy checks.

### Harness guardrails

Use this when changing the test harness itself, especially destructive gating
or the shared host-side lock:

```bash
bash scripts/test-entrypoint-guards.sh
cd hazmat && make test-entrypoint-guards
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
cd hazmat && make e2e E2E_ACK=1
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

If you want the strongest local release signal, prefer the VM path plus CI.

## CI Mapping

Current GitHub Actions coverage:

- `.github/workflows/ci.yml`
  - lint
  - Go vet and unit tests
  - CLI help/smoke checks including `hazmat init --help`
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
- Do not treat `hazmat check` as a substitute for the script-based test suite.
  It validates the installed system, not the full repo release workflow.
- Do not use `scripts/e2e.sh` casually on a machine where you want to preserve
  the current Hazmat init state.
