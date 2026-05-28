# Codex App-Server Non-Interference Workflow

This workflow is for developing Hazmat's contained Codex app-server path on a
machine where the stock Codex desktop app may also be in active use.

## Hard Boundary

Backend harness work must not launch, quit, kill, attach to, automate, or
reconfigure the stock Codex desktop app. Do not modify the host user's
`~/.codex` directory, `~/Library/Application Support/Codex`,
`~/Library/Caches/com.openai.codex`, `~/Library/HTTPStorages/com.openai.codex`,
or Codex preferences as part of autonomous backend work.

The supported autonomous backend path is a Hazmat-owned Codex CLI subprocess
started through:

```bash
hazmat codex-app-server -C /path/to/scratch/project --listen stdio://
```

The Codex App CLI-path shim is also safe for autonomous backend testing because
it only handles the subprocess invocation shape the desktop app would use:

```bash
HAZMAT_CODEX_APP_SHIM_PROJECT=/path/to/scratch/project \
HAZMAT_CODEX_APP_SHIM_NETWORK=none \
HAZMAT_CODEX_APP_SHIM_NO_BACKUP=true \
HAZMAT_CODEX_APP_SHIM_SKIP_ASSETS_SYNC=true \
hazmat app-server --analytics-default-enabled
```

Hazmat owns the process lifecycle for that subprocess. The stock desktop app is
not a participant in this path.

## Safe Autonomous Testing

Use `scripts/check-codex-app-server-smoke.sh` for regression coverage. The
smoke creates a scratch project, starts a short-lived contained
`hazmat codex-app-server --listen stdio://` subprocess, talks JSON-RPC over
stdio, and removes its scratch state when it exits.

The smoke may create a fake agent-owned credential probe so it can prove the
outer Hazmat sandbox denies credential reads. It must not use real credentials,
the host user's `.codex` state, or any live desktop app process.

Useful modes:

```bash
scripts/check-codex-app-server-smoke.sh --check-prereqs
scripts/check-codex-app-server-smoke.sh
scripts/check-codex-app-server-smoke.sh --via-cli-path-shim
scripts/check-codex-app-server-smoke.sh --skip-if-missing-prereqs
```

`HAZMAT_CODEX_APP_SERVER_SMOKE=1 scripts/pre-push` opts the smoke into the
local pre-push gate on prepared macOS hosts.

## Future Desktop Attach Probes

Any probe that involves the stock desktop app is separate from backend harness
work and must be explicitly opt-in. Before running such a probe, document:

- Whether it will launch, quit, focus, automate, or attach to the desktop app.
- Whether it will read or write host `.codex`, Codex Application Support,
  caches, HTTP storage, preferences, runtime sockets, or browser-use state.
- Which files, sockets, processes, or preferences may be observed or mutated.
- How the probe restores or isolates any host state it touches.
- How it proves filesystem, process, browser, and shell side effects stay on the
  Hazmat-contained backend rather than falling back to a host-user app-server.

Until those details are explicit and approved for the probe, treat desktop app
attach work as blocked and continue with Hazmat-owned app-server subprocesses
only.

## Handoff Pointers

- Epic: `sandboxing-zz6k` tracks the contained Codex app-server program.
- `sandboxing-zz6k.3` added the managed `hazmat codex-app-server` stdio command.
- `sandboxing-zz6k.4` expanded the autonomous app-server API smoke.
- `sandboxing-zz6k.5` adds the autonomous Codex App CLI-path shim.
- `sandboxing-lsn2` is the separate desktop attach feasibility spike.
- `sandboxing-wsd1` classifies Codex host-state paths before any broader grants.
- `sandboxing-8tj4` assesses residual `/private/tmp` exposure for this backend.
