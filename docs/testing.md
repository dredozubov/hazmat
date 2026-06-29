# Testing Hazmat

Hazmat has several verification surfaces. They answer different questions and
are not interchangeable.

> **Looking for the human-driven checklist?** [docs/manual-testing.md](manual-testing.md) is the release-time / post-harness-change verification list — preconditions, per-harness flows (subscription / API key / host import), cross-cutting features, regression scenarios, and recovery moves. Use it for things this automated matrix can't reach (browser OAuth, terminal UI input, real network).

The auditable pre-release lane model is documented in
[docs/plans/2026-06-14-pre-release-test-procedures-design.md](plans/2026-06-14-pre-release-test-procedures-design.md).
Use that design to decide whether a test belongs with a package-owned contract
or with a product-facing scenario flow.

## Test Matrix

| Surface | What it answers | Runs where | Destructive? |
| --- | --- | --- | --- |
| `scripts/pre-commit` | Are the staged files obviously broken before I create a commit? | Host | No |
| `hazmat check` / `hazmat doctor --dry-run` | Is this local Hazmat install healthy right now, and what should I fix next? | Host | No |
| `scripts/pre-push` | Fast local developer gate before pushing | Host | No |
| `scripts/pre-release-local.sh` | Local release gate, including fast checks, package-boundary guard, hermetic all-harness synthetic e2e smoke, and optional VM lifecycle gate | Host / VM with `--vm` | VM lane only |
| `scripts/check-linux-compile.sh` | Does the current unsupported Linux backend compile without Darwin-only code leaking into common packages? | Host or Linux CI | No |
| `scripts/check-linux-apple-container-smoke.sh` | Do selected Linux Hazmat Go test binaries and container-native `go test` pass inside Apple Container on macOS? | Prepared macOS 26 Apple silicon host, explicit approval only | Creates short-lived Apple Container sessions |
| `scripts/check-privileged-install-ownership.sh` | Do setup-created agent-writeable directories have the expected agent uid/gid after init, accept agent write probes, and avoid root-owned rollback residue? | Disposable prepared host; explicit approval when invoked directly; also runs inside destructive `scripts/e2e.sh` | Runs `sudo -n`, stat probes, and agent-user mkdir/rmdir probes |
| `scripts/check-codex-app-server-smoke.sh` | Does a Hazmat-contained Codex app-server backend initialize and enforce project, credential, and network boundaries? | Prepared macOS host, explicit approval only | Creates a temporary contained session |
| `scripts/check-codex-desktop-attach-smoke.sh` | Does the stock Codex desktop app route through the Hazmat-backed `CODEX_CLI_PATH` proxy? | Prepared macOS host, explicit human approval only | May launch Codex App |
| `scripts/check-session-home-activation-smoke.sh` | Does experimental session-local HOME activation preserve HOME/XDG layout and core toolchain behavior? | Prepared macOS host, explicit human approval only | Creates a temporary contained session |
| `scripts/check-claude-onboarding-smoke.sh` | Does a real `hazmat claude` startup authenticate in print mode and avoid auth/onboarding prompts in interactive TUI startup? | Prepared host, live mode requires explicit approval | Creates temporary contained sessions |
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
`sudo -n` probes, no sudo-adjacent launch-helper validation, and no
helper-backed agent probes in the default quick mode. Quick diagnostics also
skip local snapshot, cloud backup, and cloud restore live validation so the
default path does not run backup smoke tests or send external traffic. Quick
diagnostics should report a repair plan and skip live validation instead of
trying to switch users or touch external services. Use `hazmat check --full`
when you want sudo-adjacent launch-helper validation plus helper-backed, backup,
and cloud live validation directly, or `hazmat status --full` when you want the
setup progress checklist first and the same full validation afterward. Both full
paths are sudo-adjacent in agent workflows and require explicit exact-command
approval.

Prepared-host smoke wrappers are different. Their `--check-prereqs`,
`--check-fixtures`, and `--run` paths may intentionally call `sudo -n`,
`hazmat exec`, `hazmat claude`, or native helper-backed launch paths, so agents
must ask for exact-command approval before running them.

## Native Launch Performance Profiling

Use `HAZMAT_SESSION_PREP_PROFILE=yes` before proposing native launch rewrites.
It prints opt-in timings for session preparation, host-repair planning,
startup phases, native launch execution, and the `hazmat-launch` helper. The
minimal latency target should be measured with a helper-backed command such as:

```bash
HAZMAT_SESSION_PREP_PROFILE=yes /usr/bin/time -p ./hazmat --yes exec --no-backup --metadata-json -C <repo> -- /usr/bin/true
```

That command is sudo-adjacent in agent workflows because it uses the native
launch helper and may apply approved host repairs; ask for exact-command
approval before running it. Treat helper or lower-level rewrites as justified
only when the profile shows sudo/helper, policy application, or exec handoff is
the remaining bottleneck after Go-side preparation costs are accounted for.
For broker timing, make sure the helper selected by `HAZMAT_LAUNCH_HELPER` or
the installed default supports `--hazmat-session-temp` and
`--hazmat-direct-exec`; Hazmat caches that capability scan under
`~/.hazmat/launch-helper-capabilities.json` using the helper file identity, so
helper upgrades force a fresh scan.
When running from a checkout, the experimental broker may still use the
installed helper for sudo-authorized broker startup while using the checkout
`hazmat-launch` next to the checkout `hazmat` binary for child launches.

Linux native run-agent support is still plan-only/experimental. Its provider
readiness gates live in
[docs/plans/2026-06-13-linux-run-agent-readiness-gates.md](plans/2026-06-13-linux-run-agent-readiness-gates.md)
and must be satisfied before user-facing Linux native launch support is claimed.
The provider vocabulary is defined in
[Runtime provider status](runtime-provider-status.md), which keeps
`linux-current-user` separate from `linux-agent-user` and prevents unsupported
parity claims.
The current-user lane also requires the
[Linux current-user VM smoke matrix](linux-current-user-vm-smoke-matrix.md);
until completed transcripts satisfy that matrix, Linux current-user launch stays
experimental. Use `scripts/check-linux-current-user-live-smoke.sh` only inside
a disposable Linux VM after exact-command approval.
The agent-user lane has its own
[Linux agent-user VM lifecycle matrix](linux-agent-user-vm-lifecycle-matrix.md);
until completed transcripts satisfy that matrix, Linux agent-user launch stays
setup-required. Use `scripts/check-linux-agent-user-live-smoke.sh` only inside
a disposable prepared Linux VM after exact-command approval.

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

### Linux tests in Apple Container

Use this when you want Linux runtime test execution from a prepared macOS 26
Apple silicon host without switching to a separate Linux VM. The default mode is
a disclosure and does not invoke Apple Container:

```bash
scripts/check-linux-apple-container-smoke.sh
make linux-apple-container-smoke
```

Before a live run, check whether the current host is prepared:

```bash
scripts/check-linux-apple-container-smoke.sh --check-packages
scripts/check-linux-apple-container-smoke.sh --compile-tests
scripts/check-linux-apple-container-smoke.sh --check-prereqs
```

After explicit approval, run the focused Linux package set:

```bash
scripts/check-linux-apple-container-smoke.sh --run --i-understand-this-runs-apple-container-linux-tests
```

The wrapper cross-compiles selected test binaries for `linux/arm64` by default,
mounts the repo and generated binaries read-only, and runs each package in an
exact-named short-lived Apple Container. It defaults to `--network none` and the
`golang:1.25` image. Pre-pull that image if you want the live smoke itself to
avoid registry access.

For the local Linux suite, run actual `go test` inside Apple Container:

```bash
scripts/check-linux-apple-container-smoke.sh --go-test --i-understand-this-runs-apple-container-linux-tests
make linux-apple-container-test APPLE_CONTAINER_ACK=1
```

The container-native path mounts the repo read-only, copies it without `.git`,
`.beads`, Apple Container spike output, or `tla/states` to `/work/src` inside a
writable temp mount, uses isolated `HOME`, `GOCACHE`, and `GOTMPDIR`, disables
local `go.work` with `GOWORK=off`, runs as the invoking UID/GID instead of root,
mounts a writable `/private/tmp` compatibility directory, and mounts the host Go
module cache read-only when present. It defaults to `go test ./...` with
`GOFLAGS=-mod=readonly` and `--network none`; use
`HAZMAT_LINUX_APPLE_CONTAINER_GO_TEST_ARGS` to narrow the package set or test
filter while debugging.

To override either package set:

```bash
HAZMAT_LINUX_APPLE_CONTAINER_PACKAGES='./...' \
  scripts/check-linux-apple-container-smoke.sh --run --i-understand-this-runs-apple-container-linux-tests
HAZMAT_LINUX_APPLE_CONTAINER_GO_TEST_ARGS='./platform/linux ./containment/linux' \
  scripts/check-linux-apple-container-smoke.sh --go-test --i-understand-this-runs-apple-container-linux-tests
```

`--check-packages` and `--compile-tests` are non-live sanity checks.
`--check-prereqs`, `--skip-if-missing-prereqs`, `--run`, and `--go-test` are
approval-gated because they inspect or invoke local Apple Container host state.

### Linux development in Apple Container

Use this when you want a quick Linux shell or command runner while filling
Linux-native gaps. The default mode is disclosure-only:

```bash
bash scripts/linux-apple-container-dev.sh
```

Before a live session, check whether the current host is prepared:

```bash
bash scripts/linux-apple-container-dev.sh --check-prereqs
```

After explicit approval, open a shell in a writable Linux copy of the repo:

```bash
bash scripts/linux-apple-container-dev.sh --shell --i-understand-this-runs-apple-container-linux-dev
make linux-apple-container-dev APPLE_CONTAINER_ACK=1
```

Or run one Linux command:

```bash
bash scripts/linux-apple-container-dev.sh --run --i-understand-this-runs-apple-container-linux-dev -- go test ./platform/linux ./containment/linux
```

The dev wrapper mounts the host checkout read-only at `/hazmat-src`, copies it
to `/work/src/hazmat`, and runs commands from that writable copy. It uses
isolated `HOME`, `GOCACHE`, `GOTMPDIR`, and a writable `/private/tmp`
compatibility directory, with the host Go module cache mounted read-only when
present. Changes made inside the container are disposable; edit and commit from
the host, then use this lane for Linux execution feedback.

`--check-prereqs`, `--skip-if-missing-prereqs`, `--shell`, and `--run` are
approval-gated because they inspect or invoke local Apple Container host state.

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
For setup prerequisites, the message distinguishes a fresh host (`hazmat init`)
from drift in an already-initialized host (`hazmat doctor --fix`, with
`hazmat doctor --dry-run` as preview).
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

`--check-prereqs` fails closed if Codex App is already running, and performs
that process check before any `sudo -n` capability probe. The script never
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

The proxy privacy boundary has a non-live self-test. This does not launch
Codex, does not run Hazmat, and does not inspect host Codex state. It generates
the temporary `CODEX_CLI_PATH` proxy, drives it against a fake local backend,
and verifies that request logs contain method names and sorted `paramKeys`
without raw request params:

```bash
scripts/check-codex-desktop-attach-smoke.sh --self-test-proxy
```

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

By default the wrapper pairs the repo-built `hazmat/hazmat` binary with the
repo-built `hazmat/hazmat-launch` helper when both exist. Override the binary
with `HAZMAT_SESSION_HOME_SMOKE_HAZMAT` and the helper with
`HAZMAT_SESSION_HOME_SMOKE_LAUNCH_HELPER` when validating an installed pair.

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

If activation stops before the toolchain matrix, the wrapper runs a plan-only
`hazmat explain --json` with the same binary and scratch project and prints the
`session_home` block. Use the `activation_blockers` there as the authoritative
next engineering input; do not rerun `hazmat init` for adapter-required state.

For autonomous gates that should avoid false failures on unprepared machines,
use:

```bash
scripts/check-session-home-activation-smoke.sh --skip-if-missing-prereqs
```

This is a live native Hazmat smoke. Its prerequisite mode and live mode may
exercise sudo-adjacent host capability checks or helper-backed launch behavior,
so agents must ask for explicit approval before running `--check-prereqs`,
`--skip-if-missing-prereqs`, or `--run`.

### Claude onboarding smoke

Use this when validating the repeated Claude auth/onboarding regression against
the real `hazmat claude` startup path. The default mode is a disclosure; it
prints the exact live command shape and exits without running Hazmat or Claude:

```bash
scripts/check-claude-onboarding-smoke.sh
make e2e-claude-onboarding-smoke
```

Fixture checks are non-mutating host checks. They verify that the selected
Hazmat binary exists and that local shell utilities needed for the bounded
print-mode and PTY startup probes are available. Agents still need explicit
approval before running fixture checks because they inspect local Hazmat setup:

```bash
scripts/check-claude-onboarding-smoke.sh --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat claude`, which may
materialize, observe, or repair host Claude credential/onboarding state. Agents
must ask for explicit approval before running:

```bash
scripts/check-claude-onboarding-smoke.sh --run --i-understand-this-runs-hazmat-claude
```

Set `HAZMAT_CLAUDE_ONBOARDING_SMOKE_HAZMAT` to an installed binary path when
validating an installed build instead of the checkout binary. The live smoke
creates a scratch project, runs Claude print mode with a sentinel prompt,
requires the sentinel response, then launches real interactive Claude in a
pseudo-terminal for a short observation window. It fails on timeout, early
child crash, or output that looks like an auth/onboarding/visual-style prompt.

### Claude Workflow export smoke

Use this when validating `hazmat export claude session` against live
Workflow/subagent sidecar artifacts. The default mode is a disclosure; it prints
the exact live command shape and exits without running Hazmat or Claude:

```bash
scripts/check-claude-workflow-export-smoke.sh
make e2e-claude-workflow-export-smoke
```

Fixture checks are non-mutating host checks. They verify that the selected
Hazmat binary exists, the host Claude CLI is present and answers `--version`,
and the Workflow prompt file is readable, but do not run `hazmat claude` or
host `claude --resume`. By default the script uses
`scripts/fixtures/claude-workflow-export-prompt.txt`; set
`HAZMAT_CLAUDE_WORKFLOW_SMOKE_PROMPT_FILE` only when validating a stronger
local reproduction prompt. Prompt fixture paths must be readable regular files;
if the default prompt fixture is absent, the script uses its embedded fallback.
When `HAZMAT_CLAUDE_WORKFLOW_SMOKE_CLAUDE` is an explicit path, it must be
absolute; command-name overrides are resolved from `PATH`.
Agents still need explicit approval before running fixture checks because they
inspect local Hazmat/Claude tool setup:

```bash
scripts/check-claude-workflow-export-smoke.sh --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat claude`, and it also runs
host Claude with `--resume`. Agents must ask for explicit approval before
running:

```bash
scripts/check-claude-workflow-export-smoke.sh --run --i-understand-this-runs-hazmat-claude-and-host-claude
```

The prompt file should be a task known to create Claude Workflow/subagent
sidecar artifacts. If the default fixture file is missing and no override is
set, the script falls back to an embedded prompt with the same Task-tool shape
so a live pass is not blocked on fixture setup. The live smoke uses a scratch
project, exports the contained session, checks that the host transcript,
project `sessions-index.json`, and sidecar files no longer contain stale
`/Users/agent/.claude/projects` paths, then resumes the exported session with
host Claude. If stale agent paths remain, the smoke prints a bounded list of
matching exported files without dumping file contents. It does not broaden the
export policy for opaque Workflow caches; the docs still treat those caches as
best-effort.

### Cache integration smoke

Use this when validating cache-only integrations for Hugging Face, Ollama, or
PyTorch torch-hub. The default mode is only a disclosure; it prints the selected
targets, fixture environment, and live command shape without running Hazmat:

```bash
scripts/check-cache-integration-smoke.sh
scripts/check-cache-integration-smoke.sh --target huggingface
make e2e-cache-integration-smoke
```

Fixture checks are non-mutating host checks. They verify the local binary or
Python package and required fixture environment, but do not run `hazmat exec`.
Agents still need explicit approval before running them because they inspect
local tool/cache setup. When `--target all` is used, missing fixture messages
are prefixed with the target name, such as `[huggingface]` or `[torch-hub]`, so
the missing local cache setup is attributable:

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
path; fixture checks verify that local paths have a readable `config.json`, or
that matching Hub cache snapshots under `HF_HUB_CACHE`, `HF_HOME`,
`XDG_CACHE_HOME`, or the default `$HOME/.cache/huggingface/hub` contain
`config.json`, before the network-disabled live smoke. PyTorch
torch-hub requires `HAZMAT_TORCH_HUB_REPO` and
`HAZMAT_TORCH_HUB_MODEL` to name a pre-cached hub entry; fixture checks verify
that the matching repo cache exists under `TORCH_HOME`, `XDG_CACHE_HOME`, or the
default `$HOME/.cache/torch/hub`, and that `hubconf.py` appears to define the
requested callable before the network-disabled live smoke. Ollama
requires the `ollama` CLI and a running host daemon; the fixture check runs
`ollama list` through the selected binary to catch daemon or `OLLAMA_HOST`
problems before the live Hazmat session. Override the executable with
`HAZMAT_OLLAMA_SMOKE_BIN` when validating a non-default Ollama path. Explicit
path overrides must be absolute because the live command executes from a
scratch project inside containment.

### OpenHands recipe smoke

Use this only for the recipe-only OpenHands path. The default mode is a
disclosure: it prints the exact live command and exits without running Hazmat.

```bash
scripts/check-openhands-recipe-smoke.sh
```

Fixture checks are non-mutating host checks. They verify that the selected
Hazmat binary and OpenHands CLI are present, and run the selected OpenHands
binary with `--help` to catch a broken local installation before the live
contained run. They do not run `hazmat exec`, do not prove that a command-name
override is reachable from the contained agent PATH, and remain optional when
the OpenHands CLI is absent. Agents still need explicit approval before running
them because they inspect local OpenHands/Hazmat tool setup:

```bash
scripts/check-openhands-recipe-smoke.sh --check-fixtures
```

When `HAZMAT_OPENHANDS_RECIPE_SMOKE_BIN` is an explicit path, missing or
non-executable overrides are reported as path fixture failures instead of PATH
lookup failures. Explicit path overrides must be absolute because the live
command executes from a scratch project inside containment.

Live mode is sudo-adjacent because it invokes `hazmat exec`. Agents must ask
for explicit approval before running:

```bash
scripts/check-openhands-recipe-smoke.sh --run --i-understand-this-runs-hazmat-exec
```

The live smoke uses a scratch project and runs `openhands --help` under
`hazmat exec --network none --no-backup`. It does not install OpenHands, import
host `~/.openhands`, pass a host Docker socket, configure provider credentials,
or prove first-class `hazmat openhands` support.

### README proof-stack smoke

Use this only when capturing the public README proof snippets. The default mode
is a disclosure; it prints the exact live command and exits without running
Hazmat:

```bash
scripts/check-readme-proof-stack-smoke.sh
```

Fixture checks inspect the selected host secret fixture, local Hazmat binary,
and any requested `--output-dir` destination, but do not run `hazmat exec` or
create the output directory. Agents still need explicit approval before running
them because they inspect local secret-path setup. The
`HAZMAT_README_PROOF_STACK_SMOKE_HAZMAT` override must be an absolute path
because the live smoke changes into the scratch project before capturing
recovery evidence with `hazmat diff`:

```bash
scripts/check-readme-proof-stack-smoke.sh --check-fixtures
```

Live mode is sudo-adjacent because it invokes `hazmat exec`. Agents must ask
for explicit approval before running:

```bash
scripts/check-readme-proof-stack-smoke.sh --run --i-understand-this-runs-hazmat-exec
```

By default the host secret fixture is `$HOME/.ssh/id_ed25519`; override it with
`HAZMAT_PROOF_STACK_SECRET_PATH` if the machine uses a different private
fixture. The secret fixture path must be absolute, so the proof cannot pass
only because a relative path was resolved inside the scratch project. The live
smoke creates a scratch demo project, writes `proof.txt` inside the contained
session, attempts to read the host secret fixture without printing its bytes,
and then runs `hazmat diff` from the scratch project for recovery evidence. Use
`--output-dir <dir>` during an approved live run to save the sanitized session
and diff snippets for README work.

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
scripts/pre-release-audit.sh
```

This runs the fast repository gate (`scripts/pre-push`), the package-boundary
guard, the all-harness synthetic e2e smoke, and the fake service-harness
lifecycle smoke. `scripts/release.sh` runs the same local gate before it asks
Hazmat-contained Claude to draft `CHANGELOG.md`, so a release cannot proceed
locally if the hermetic harness smoke fails. The release script requires
`--i-understand-this-runs-hazmat-claude`; non-dry mode also requires
`--i-understand-this-may-push-release`. `scripts/pre-release-audit.sh` writes a
markdown evidence record for the lane results and any skipped live/disposable
host checks; it does not run tests itself.
Linux release evidence is split by identity lane in the
[Linux release checklist](linux-release-checklist.md). The audit output must
record `linux-current-user` and `linux-agent-user` separately, with completed
transcripts or explicit status-preserving skip reasons.

For the release-grade local lifecycle gate, include the isolated VM lane:

```bash
bash scripts/pre-release-local.sh --vm
make pre-release-local PRERELEASE_ARGS=--vm
```

The VM lane runs `scripts/e2e-vm.sh --quick`. The large image import is
intentionally separated from the lifecycle run. Pull the maintained prebuilt
base image once before the first VM run:

```bash
bash scripts/e2e-vm.sh --step download --quick
```

`download` is a compatibility alias for the image pull step; it does not
download an IPSW. The default provider is Tart because Cirrus base images are
SSH-ready without driving macOS Setup Assistant. The default image is
`ghcr.io/cirruslabs/macos-tahoe-base:latest`, imported as `hazmat-e2e-base`,
with documented `admin`/`admin` SSH credentials and `sshpass` required for
non-interactive SSH. Override it with `HAZMAT_E2E_BASE_IMAGE` when a runner uses
a curated internal Tart image.

Base provisioning copies the host Darwin/arm64 Go toolchain into the VM and
installs passwordless sudo rules for the guest test user. Test VM preparation
stages the repo and a host-generated `vendor/` tree through Tart virtiofs
shares, then builds with `GOFLAGS="-mod=vendor -trimpath"`. The guest lifecycle
therefore does not require guest internet access, Homebrew, or Go module
downloads. The host-side caches live under `~/.cache/hazmat/` by default and
are safe to delete when you want them regenerated.

Lume remains available only for an explicitly supplied SSH-enabled base image
or existing base VM. Do not use Lume `vanilla` images for this lane: upstream
Lume documents SSH auto-enablement for unattended-created VMs, and that path
drives Setup Assistant through VNC/OCR. If you intentionally use Lume, set
`HAZMAT_E2E_VM_PROVIDER=lume` and `HAZMAT_E2E_BASE_IMAGE` or provide an existing
`HAZMAT_E2E_BASE_VM` with Remote Login already enabled.

The base image must expose SSH before Hazmat can provision it. The VM lane does
not use screen automation to enable Remote Login. For Tart, validate a suspect
base with:

```bash
tart run hazmat-e2e-base
ssh admin@$(tart ip hazmat-e2e-base)
```

On hosts with Docker Desktop, Apple Container, VPNs, or custom routes, the
plain `ssh admin@$(tart ip ...)` path can route to the wrong interface even
when the Tart bridge has the VM. Hazmat auto-binds Tart SSH to `bridge100` when
the VM IP is on that bridge. Override with
`HAZMAT_E2E_TART_SSH_BIND_INTERFACE=<interface>` or set it to `none` to disable
binding. To diagnose manually:

```bash
ip=$(tart ip hazmat-e2e-base)
route -n get "$ip"
ssh -B bridge100 admin@"$ip"
```

If SSH still fails with the correct interface binding, use a curated internal
image with Remote Login enabled via `HAZMAT_E2E_BASE_IMAGE`.

CI exposes the same lane through the manual `CI` workflow input
`run_e2e_vm=true`. The default provider is `tart` with runner labels
`["self-hosted","macOS","ARM64","tart"]`; override `e2e_vm_provider` and
`e2e_vm_runner` only for an explicitly prepared alternate provider.

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
bash scripts/e2e.sh --vm --quick
make e2e E2E_ACK=1
```

In host mode, this script runs `hazmat init`, verifies privileged install
ownership on the real host, exercises containment and restore behavior, then
runs `hazmat rollback --delete-user --delete-group --yes` and verifies there is
no root-owned ownership-lane residue before re-initializing. It is
intentionally destructive to the local Hazmat setup.

Use `--vm` for the same lifecycle inside an isolated macOS VM. The compatibility
wrapper `scripts/e2e-vm.sh` delegates to `scripts/e2e.sh --vm` so the host and
VM lifecycle cannot drift.

### VM-backed lifecycle

This is the safer local release-grade path:

```bash
bash scripts/e2e.sh --vm --quick
```

The VM mode provisions a macOS guest, copies the repo into the guest, and
runs the same `scripts/e2e.sh` lifecycle there. For Tart, the long-running
guest lifecycle is launched detached inside the VM and the host polls a status
file so transient SSH drops do not kill the test. The default base path uses a
maintained Tart/Cirrus prebuilt image instead of driving macOS Setup Assistant.
If the base VM does not exist, `base`, `prepare`, and `all` fail fast and tell
you to run the explicit pull step first. If first-time base provisioning fails,
Hazmat preserves the base VM for inspection or repair. A later run will try to
resume base provisioning in-place. Use
`bash scripts/e2e.sh --vm --vm-step pull --reset-vm-base --quick` followed by
`bash scripts/e2e.sh --vm --vm-step base --quick` only when you intentionally
want to delete and rebuild the cached base VM. For Lume-only runs, if Lume
reports that the base VM is still being provisioned, wait for that operation to
finish and then rerun the VM lifecycle instead of resetting the base.

For faster iteration, run only the failed VM lifecycle step:

```bash
bash scripts/e2e.sh --vm --vm-step pull --quick
bash scripts/e2e.sh --vm --vm-step base --quick
bash scripts/e2e.sh --vm --vm-step prepare --quick
HAZMAT_E2E_TEST_VM=hazmat-e2e-12345 bash scripts/e2e.sh --vm --vm-step guest --keep --quick
```

`pull` imports the prebuilt base image and stops there. `base` provisions or
resumes the cached base VM and stops there. `prepare` clones and boots a test
VM, copies the repo plus the staged vendor tree, and keeps that VM for reruns.
`guest` refreshes the repo and vendor tree, then reruns only the destructive
Hazmat lifecycle inside an existing test VM; set
`HAZMAT_E2E_TEST_VM` to the kept VM name printed by `prepare`.

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
- `scripts/check-linux-compile.sh` is compile-only. It proves the Linux package
  set still builds; it does not claim live Linux setup, rollback, launch,
  firewall, ACL, account, or service behavior passed on a host.

If you want the strongest local release signal, prefer the VM path plus CI.

## Linux Support Test Plan

Linux testing currently has compile, unit, model, transcript-scaffold,
prepared-host runtime, and an explicit on-demand Ubuntu lifecycle lane.
`.github/workflows/linux-evidence.yml` adds an on-demand and release-tag Ubuntu
evidence lane for current-user live smoke plus agent-user transcript
scaffolding; it also has a manual `agent-user-live` lane that runs setup,
prepared-host root-helper launch, default rollback, and destructive rollback on
a disposable Ubuntu runner. The same workflow has a manual `distro-container`
lane for Debian, Fedora, and Arch container transcripts. That lane is useful for
package-manager, distro-fact, and supplemental live-run drift, but it is not a
substitute for release-grade disposable VM lifecycle transcripts.
`.github/workflows/linux-vm-evidence.yml` is the on-demand QEMU path for
Debian, Fedora, and Arch disposable VM transcripts. Do not enable Linux install
or release artifacts until disposable VM lifecycle transcripts pass setup,
doctor, root-helper launch, default rollback, and destructive rollback across
the required distro matrix. The model-first gate has a checked resource-ordering
contract in
`tla/MC_SetupRollback` with `Platform = "linux"`: Linux sudoers privilege
requires firewall policy, resolver policy, and service-manager persistence to
all be active.

The first Linux implementation should land behind four gates:

1. **Model first:** keep `MC_SetupRollback` green for the Linux interpretation
   and extend it before adding any new Linux-owned setup resource such as users,
   groups, systemd units, firewall/DNS policy, sudoers, helper installation, or
   rollback cleanup.
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
  - Go vet and unit tests on macOS
  - Linux `go test ./...` plus compile-only gate for the unsupported backend
  - package import-boundary and package-split guardrails
  - CLI help/smoke checks via `scripts/check-cli-smoke.sh`
  - test-entrypoint guard regression checks
  - self-hosting bootstrap on macOS (`--skip-tla`)
  - repo-matrix required-track contract checks
  - TLA+ model checking
  - host-side lifecycle e2e on macOS
  - wave-1 repo-matrix smoke on push
- `.github/workflows/linux-evidence.yml`
  - on-demand Linux evidence collection via `workflow_dispatch`
  - release-tag Ubuntu current-user live smoke transcript artifact
  - release-tag Ubuntu agent-user transcript scaffold artifact
  - manual Ubuntu agent-user live lifecycle transcript artifact
  - manual Debian/Fedora/Arch distro-container transcript artifacts
  - not a substitute for Debian/Fedora/Arch VM lifecycle transcripts
- `.github/workflows/linux-vm-evidence.yml`
  - manual Debian/Fedora/Arch disposable QEMU VM transcript artifacts
  - current-user, agent-user, or combined VM evidence mode
- `.github/workflows/stack-matrix-drift.yml`
  - non-blocking scheduled drift checks against upstream heads
- `.github/workflows/release.yml`
  - release preflight on the tagged commit before artifact builds
  - Darwin artifact build, checksums, GitHub release creation, and Homebrew tap update

The target CI shape is lane-based: source safety, package-boundary checks, Linux
and macOS package tests, CLI/product-flow checks, release artifact checks, TLA
proof hygiene plus deep TLC model checking, and optional self-hosted Apple
Container or disposable-VM lanes. See the pre-release test procedure design for
the lane contracts and audit checklist.

## Important Warnings

- Host-side test entrypoints take a shared local lock and are intended to run
  one at a time. If another host-side test is already running, they should
  fail fast instead of racing on local build outputs or Hazmat state.
- CI initializes Hazmat with `--bootstrap-agent skip` for containment-only
  jobs, so those lanes do not depend on vendor-specific agent downloads.
- Linux CI runs package tests and compile checks, but Linux setup/rollback
  remains blocked for release until disposable lifecycle transcripts satisfy
  the verified TLA+ setup/rollback model.
- Do not treat `hazmat check` as a substitute for the script-based test suite.
  It validates the installed system, not the full repo release workflow.
- Do not treat release preflight as a replacement for pull-request/main CI. It
  protects tag artifacts on the final commit, but it does not run every matrix
  and live/disposable-host lane.
- Do not use `scripts/e2e.sh` casually on a machine where you want to preserve
  the current Hazmat init state.
