# Testing Hazmat

Hazmat has several verification surfaces. They answer different questions and
are not interchangeable.

> **Looking for the human-driven checklist?** [docs/manual-testing.md](manual-testing.md) is the release-time / post-harness-change verification list — preconditions, per-harness flows (subscription / API key / host import), cross-cutting features, regression scenarios, and recovery moves. Use it for things this automated matrix can't reach (browser OAuth, terminal UI input, real network).

## Test Matrix

| Surface | What it answers | Runs where | Destructive? |
| --- | --- | --- | --- |
| `scripts/pre-commit` | Are the staged files obviously broken before I create a commit? | Host | No |
| `hazmat check` / `hazmat doctor --dry-run` | Is this local Hazmat install healthy right now, and what should I fix next? | Host | No |
| `scripts/pre-push` | Fast local developer gate before pushing | Host | No |
| `scripts/pre-release-local.sh` | Local release gate, including fast checks and hermetic all-harness synthetic e2e smoke | Host | No |
| `scripts/check-linux-compile.sh` | Does the current unsupported Linux backend compile without Darwin-only code leaking into common packages? | Host or Linux CI | No |
| `scripts/check-codex-app-server-smoke.sh` | Does a Hazmat-contained Codex app-server backend initialize and enforce project, credential, and network boundaries? | Prepared macOS host, explicit approval only | Creates a temporary contained session |
| `scripts/check-codex-desktop-attach-smoke.sh` | Does the stock Codex desktop app route through the Hazmat-backed `CODEX_CLI_PATH` proxy? | Prepared macOS host, explicit human approval only | May launch Codex App |
| `scripts/check-session-home-activation-smoke.sh` | Does experimental session-local HOME activation preserve HOME/XDG layout and core toolchain behavior? | Prepared macOS host, explicit human approval only | Creates a temporary contained session |
| `scripts/check-cache-integration-smoke.sh` | Do Hugging Face, Ollama, and PyTorch torch-hub cache-only integrations work against selected local fixtures? | Prepared host, live mode requires explicit approval | Creates temporary contained sessions |
| `scripts/check-openhands-recipe-smoke.sh` | Does the recipe-only OpenHands path launch OpenHands through `hazmat exec` without host profile or Docker-socket shortcuts? | Prepared host, live mode requires explicit approval | Creates a temporary contained session |
| `scripts/test-entrypoint-guards.sh` | Do the test harness safety rails fail loudly and correctly? | Host | No |
| `scripts/e2e-bootstrap.sh` | Can Hazmat develop Hazmat inside containment? | Host | No |
| `scripts/e2e-harness-smoke.sh` | Do harness command parsing, auth materialization/harvest, env delivery, and foreground launch scripts compose for every managed harness? | Host or CI | No |
| `scripts/e2e-harness-smoke-native.sh` | Does the prepared-host launch-helper and seatbelt path still compose with every managed harness? | Prepared macOS host, explicit approval only | Temporarily swaps agent harness binaries, then restores |
| `scripts/e2e-stack-matrix.sh` | Do supported stacks detect and behave correctly on real repos? | Host | No |
| `scripts/e2e.sh` | Does the full install / contain / backup / restore / rollback lifecycle work? | Host | Yes |
| `scripts/e2e-vm.sh` | Run the destructive lifecycle test in an isolated macOS VM | VM | Destroys the VM, not your host setup |

`hazmat check` and `hazmat doctor --dry-run` are diagnostics, not live smoke
wrappers. They must stay read-only and non-prompting: no direct sudo probes, no
`sudo -n` probes, and no helper-backed agent probes in the default quick mode.
Quick diagnostics should report a repair plan and skip agent-backed probes
instead of trying to switch users. Use `hazmat check --full` only when you want
helper-backed live validation; that path is sudo-adjacent in agent workflows and
requires explicit exact-command approval.

Prepared-host smoke wrappers are different. Their `--check-prereqs` and `--run`
paths may intentionally call `sudo -n`, `hazmat exec`, or native helper-backed
launch paths, so agents must ask for exact-command approval before running them.

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

If you install the repo-local hooks with `hazmat hooks install -C .` (or
`make hooks`, which now delegates to that command), Hazmat adds:

- `pre-commit`: the tracked source lives under `.hazmat/hooks/pre-commit.sh`
  and runs staged diff sanity, `gofmt` on staged Go files, and shell syntax
  checks for staged scripts, plus two staged secret scans:
  - `scripts/check-secret-patterns.sh --staged` — fast regex gate for the
    highest-signal provider-issued patterns Hazmat docs/tests are likely to
    touch (currently Google API keys, Anthropic API keys, GitHub PATs, AWS
    access key IDs, OpenRouter keys, and Context7 keys). No external
    dependencies. Safe placeholder guidance lives in
    [docs/synthetic-credentials.md](synthetic-credentials.md).
  - `scripts/check-credential-regressions.sh --staged` — structural credential
    lifecycle gate. It rejects new ad hoc durable writes to agent credential
    paths, `credential.helper store` additions, host secret-store writes outside
    the registry/store owners, and credential-shaped integration env passthrough
    entries that should be modeled as SecretRef-backed
    credentials instead.
  - `scripts/check-gitleaks.sh --staged` — broader scanner via
    [`gitleaks`](https://github.com/gitleaks/gitleaks) covering ~100 provider
    patterns and high-entropy detection (config:
    `.hazmat/hooks/gitleaks.toml`). Requires `gitleaks` on `PATH`; install with
    `brew install gitleaks` or
    `go install github.com/zricethezav/gitleaks/v8@latest`
- `pre-push`: the tracked source lives under `.hazmat/hooks/pre-push.sh` and
  runs the fast local gate (tracked-file secret-pattern scan, structural
  credential regression scan, full-tree gitleaks scan, `go vet`, `go test`,
  Linux compile-only, `golangci-lint`, and CLI smoke tests)

The legacy `scripts/pre-commit`, `scripts/pre-push`, and `scripts/check-fast.sh`
entrypoints remain as compatibility wrappers for manual runs and older docs, but
they are no longer the source of truth for Git hook installation.

### Codex app-server smoke

Use this when changing the contained Codex app-server backend path. It starts a
short-lived `hazmat codex-app-server --network none --listen stdio://`
subprocess as the Hazmat agent user, talks JSON-RPC over stdio, and verifies
initialize, `command/exec`, project `fs/readFile`, project `fs/writeFile` and
`fs/remove`, `process/spawn` when the installed app-server exposes it,
`thread/shellCommand`, fake credential-path denial through filesystem APIs and
through process APIs when available, and outbound-network denial. It does not
launch, quit, attach to, or mutate the stock Codex desktop app.
The non-interference rules for backend work and any future desktop attach probe
are documented in
[docs/codex-app-server-non-interference.md](codex-app-server-non-interference.md).

The default mode is a disclosure; it prints the exact live command shape and
exits without running Hazmat or sudo-adjacent prerequisite probes:

```bash
scripts/check-codex-app-server-smoke.sh
```

To exercise the Codex App `CODEX_CLI_PATH` compatibility shim without launching
the desktop app, add `--via-cli-path-shim`. This starts the same backend through
the root-level `hazmat app-server --analytics-default-enabled` invocation shape
that the desktop app uses when `CODEX_CLI_PATH` points at Hazmat.

First check whether the current host is prepared:

```bash
scripts/check-codex-app-server-smoke.sh --check-prereqs
```

Run the smoke strictly when prerequisites are present:

```bash
scripts/check-codex-app-server-smoke.sh --run --i-understand-this-runs-hazmat-codex-app-server
scripts/check-codex-app-server-smoke.sh --run --via-cli-path-shim --i-understand-this-runs-hazmat-codex-app-server
```

For autonomous gates that should avoid false failures on unprepared machines,
use the skip mode:

```bash
scripts/check-codex-app-server-smoke.sh --skip-if-missing-prereqs
```

`--check-prereqs` exits 2 and prints precise missing requirements when the host
is not ready. `--skip-if-missing-prereqs` prints the same reasons but exits 0.
The normal run still fails closed on protocol, filesystem, credential, process,
or network regressions. `--check-prereqs`, `--skip-if-missing-prereqs`, and
`--run` are sudo-adjacent because they probe or invoke helper-backed native
containment. To include this smoke in the pre-push gate on a prepared macOS
host, opt in explicitly:

```bash
HAZMAT_CODEX_APP_SERVER_SMOKE=1 bash scripts/pre-push
```

### Codex desktop attach smoke

Use this only for the explicit opt-in live desktop proof. It is not part of
autonomous backend testing because it can launch the stock Codex app and cause
that app to read or update normal host-user app state. The default command is a
safe dry run that prints the required host-state disclosure:

```bash
scripts/check-codex-desktop-attach-smoke.sh
scripts/check-codex-desktop-attach-smoke.sh --print-disclosure
```

Before a live run, check whether the machine is in a safe state. This is also
approval-gated because it performs non-interactive sudo capability probes with
`sudo -n`:

```bash
scripts/check-codex-desktop-attach-smoke.sh --check-prereqs
```

`--check-prereqs` fails closed if Codex App is already running. The script never
quits or kills the app for you. After explicit approval and after quitting any
existing Codex App instance manually, run:

```bash
scripts/check-codex-desktop-attach-smoke.sh --run --i-understand-this-may-launch-codex-app
```

The live run builds a scratch Hazmat binary, creates a scratch project under
`/tmp`, launches Codex through `/usr/bin/open --env CODEX_CLI_PATH=...`, and
records app-server JSON-RPC method names in `proxy.jsonl` without logging
request params by default. The method log is the evidence for whether desktop
side-effect APIs route through the Hazmat-backed backend; unobserved methods
remain unproven and should be recorded as residual risk.

### Session-home activation smoke

Use this only when validating the experimental session-local HOME activation
path. The default mode is a disclosure; it prints the exact live command shape
and exits without running Hazmat or sudo-adjacent prerequisite probes:

```bash
scripts/check-session-home-activation-smoke.sh
make e2e-session-home-activation-smoke
```

The live smoke starts a native `hazmat exec` session with
`HAZMAT_EXPERIMENTAL_SESSION_HOME=activate`, asserts that `HOME` and XDG roots
point under `/private/tmp/hazmat-home`, writes to the disposable home, and runs
go, npm, pip, cargo, and git probes inside the contained session.

Managed harness env delivery is also pinned by
`TestNativeLaunchBaseEnvPairsUsesSessionHomeForEveryManagedHarness`, which
asserts every managed harness receives the session-local `HOME` and XDG roots
when a session-home runtime plan is present. That unit test does not replace a
live harness startup run; it keeps the non-live contract from drifting while
host-backed validation remains approval-gated.

First check whether the current host is prepared. This script is approval-gated:
its prerequisite path probes non-interactive agent-user switching with `sudo -n`
and therefore is sudo-adjacent even though it is non-mutating.

```bash
scripts/check-session-home-activation-smoke.sh --check-prereqs
```

After explicit approval, run:

```bash
scripts/check-session-home-activation-smoke.sh --run --i-understand-this-runs-hazmat-exec
```

For autonomous gates that should avoid false failures on unprepared machines,
use:

```bash
scripts/check-session-home-activation-smoke.sh --skip-if-missing-prereqs
```

This is a live native Hazmat smoke. Its prerequisite mode and live mode may
exercise sudo-adjacent host capability checks or helper-backed launch behavior,
so agents must ask for explicit approval before running `--check-prereqs`,
`--skip-if-missing-prereqs`, or `--run`.

### Claude Workflow export smoke

Use this when validating `hazmat export claude session` against live
Workflow/subagent sidecar artifacts. The default mode is a disclosure; it prints
the exact live command shape and exits without running Hazmat or Claude:

```bash
scripts/check-claude-workflow-export-smoke.sh
make e2e-claude-workflow-export-smoke
```

Fixture checks are non-mutating host checks. They verify that the selected
Hazmat binary, host Claude CLI, and caller-supplied Workflow prompt file are
present, but do not run `hazmat claude` or host `claude --resume`:

```bash
HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE=workflow-prompt.txt \
  scripts/check-claude-workflow-export-smoke.sh --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat claude`, and it also runs
host Claude with `--resume`. Agents must ask for explicit approval before
running:

```bash
HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE=workflow-prompt.txt \
  scripts/check-claude-workflow-export-smoke.sh --run --i-understand-this-runs-hazmat-claude-and-host-claude
```

The prompt file should be a task known to create Claude Workflow/subagent
sidecar artifacts. The live smoke uses a scratch project, exports the contained
session, checks that the host transcript/sidecar no longer contain stale
`/Users/agent/.claude/projects` paths, then resumes the exported session with
host Claude. It does not broaden the export policy for opaque Workflow caches;
the docs still treat those caches as best-effort.

### Cache integration smoke

Use this when validating cache-only integrations for Hugging Face, Ollama, or
PyTorch torch-hub. The default mode is only a disclosure; it prints the selected
targets, fixture environment, and live command shape without running Hazmat:

```bash
scripts/check-cache-integration-smoke.sh
scripts/check-cache-integration-smoke.sh --target huggingface
```

Fixture checks are non-mutating host checks. They verify the local binary or
Python package and required fixture environment, but do not run `hazmat exec`:

```bash
scripts/check-cache-integration-smoke.sh --target huggingface --check-fixtures
scripts/check-cache-integration-smoke.sh --target ollama --check-fixtures
scripts/check-cache-integration-smoke.sh --target torch-hub --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat exec`. Agents must ask
for explicit approval before running commands in this form:

```bash
scripts/check-cache-integration-smoke.sh --target huggingface --run --i-understand-this-runs-hazmat-exec
scripts/check-cache-integration-smoke.sh --target ollama --run --i-understand-this-runs-hazmat-exec
scripts/check-cache-integration-smoke.sh --target torch-hub --run --i-understand-this-runs-hazmat-exec
```

Hugging Face requires `HAZMAT_HF_SMOKE_MODEL` to name a pre-cached model ID or
path. PyTorch torch-hub requires `HAZMAT_TORCH_HUB_REPO` and
`HAZMAT_TORCH_HUB_MODEL` to name a pre-cached hub entry. Ollama requires the
`ollama` CLI and a running host daemon.

### OpenHands recipe smoke

Use this only for the recipe-only OpenHands path. The default mode is a
disclosure: it prints the exact live command and exits without running Hazmat.

```bash
scripts/check-openhands-recipe-smoke.sh
```

Fixture checks are non-mutating host checks. They verify that the selected
Hazmat binary and OpenHands CLI are present, but do not run `hazmat exec`:

```bash
scripts/check-openhands-recipe-smoke.sh --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat exec`. Agents must ask
for explicit approval before running:

```bash
scripts/check-openhands-recipe-smoke.sh --run --i-understand-this-runs-hazmat-exec
```

The live smoke uses a scratch project and runs `openhands --help` under
`hazmat exec --network none --no-backup`. It does not install OpenHands, import
host `~/.openhands`, pass a host Docker socket, configure provider credentials,
or prove first-class `hazmat openhands` support.

### Adding Credential Surfaces

New credential handling must be represented in the typed credential registry, or
explicitly documented as an external backend such as Keychain. Do not add direct
writes to durable `/Users/agent` credential paths, do not add a new
`credential.helper store` path, and do not write to `~/.hazmat/secrets` outside
the registry/store owner files.

For environment delivery, integration `env_passthrough` is only for passive
selectors and path pointers. Credential-shaped names such as `*_TOKEN`,
`*_SECRET`, `*_API_KEY`, `*_PASSWORD`, `*_PRIVATE_KEY`, and `*_ACCESS_KEY`
belong behind a registry-backed SecretRef or brokered delivery path, not in
`safeEnvKeys` or an integration manifest.

If a line is a deliberate temporary exception, place
`credential-regression: allow <issue-id and reason>` on the same line or the
immediately preceding line. Treat that as a maintainer-reviewed escape hatch, not
as the normal way to add credentials. Add or update a fixture in
`scripts/test-credential-regressions.sh` whenever the scanner's intended
boundary changes.

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

### Harness smoke

Use this when changing a harness launch path, harness auth materialization, or
adding any new managed harness:

```bash
bash scripts/e2e-harness-smoke.sh
make e2e-harness-smoke
```

The smoke does not call real harness services. It creates a disposable fixture
root, redirects Hazmat's host home and agent home into that root, installs
synthetic harness binaries there, and runs the real `hazmat <harness>` command
entrypoints for every managed harness. It does not require `hazmat init`, an
`agent` account, non-interactive `sudo -n`, or writes to `/Users/agent`.

Use the optional native variant when you specifically need to validate the
prepared-host launch-helper and seatbelt path:

```bash
bash scripts/e2e-harness-smoke-native.sh --run --i-understand-this-runs-native-hazmat-smoke
```

The native smoke backs up the touched agent-owned harness binaries, contained
Hermes/Qwen/Cursor state, and host secret-store files, installs synthetic
agent-owned binaries, runs the native `hazmat <harness>` launch paths, then
restores everything it touched. The default invocation is disclosure-only.
Live mode requires `hazmat init`, an `agent` account, non-interactive `sudo -n`,
and explicit approval for the exact command above.

The Claude case seeds synthetic host-owned auth, lets the fake contained Claude
process rewrite the runtime credential file to `{}`, and verifies that Hazmat
preserves `~/.hazmat/secrets/claude/credentials.json` instead of persisting the
logged-out runtime shape. That is the automated regression check for
update-induced logout failures.

Harness policy: every entry in `managedHarnessRegistry` must be represented in
`scripts/e2e-harness-smoke.sh --list-harnesses`. The Go test
`TestHarnessSmokeCoversEveryManagedHarness` fails if a future managed harness is
added without synthetic e2e coverage, so the smoke gate is part of the harness
contract rather than a best-effort checklist item.

### Local Pre-Release Gate

Run this before cutting a release:

```bash
bash scripts/pre-release-local.sh
make pre-release-local
```

This runs the fast repository gate (`scripts/pre-push`) and then the all-harness
synthetic e2e smoke. `scripts/release.sh` runs the same local gate before it
asks Hazmat-contained Claude to draft `CHANGELOG.md`, so a release cannot
proceed locally if the hermetic harness smoke fails. The release script requires
`--i-understand-this-runs-hazmat-claude`; non-dry mode also requires
`--i-understand-this-may-push-release`.

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

- `hazmat check`, `pre-push`, `pre-release-local`, `e2e-bootstrap`, the
  hermetic harness smoke, and `e2e-stack-matrix` are host-side verification
  surfaces.
- `scripts/e2e-harness-smoke-native.sh` is host-side and prepared-host-only:
  default mode is disclosure-only; live mode requires `hazmat init`, an
  `agent` account, `sudo -n`, and exact-command approval.
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
