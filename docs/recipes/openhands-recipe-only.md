# OpenHands Under Hazmat

Use this recipe when you want to experiment with OpenHands from inside an
existing Hazmat containment boundary.

This is recipe-only support. Hazmat does not provide `hazmat openhands`, does
not install OpenHands, does not import host `~/.openhands`, does not expose the
host Docker socket from native containment, and does not treat OpenHands process
mode as the isolation layer.

## Fit

Good fit:

- an already-installed `openhands` CLI available to the agent account
- contained interactive CLI sessions
- contained headless runs for one task
- local-only `openhands web` experiments bound to `127.0.0.1`
- fake-provider or low-risk evaluation tasks where no provider credential is
  needed

Poor fit:

- `openhands serve` with direct host Docker socket access from native Hazmat
- importing host `~/.openhands` settings, MCP config, conversations, or secrets
- OpenHands Cloud or remote sandbox credentials until Hazmat has typed entries
- browser-exposed OpenHands web sessions bound to `0.0.0.0`
- claiming that OpenHands process mode provides sandbox isolation
- treating a live OpenHands smoke as proof of Hazmat service-harness support

## Boundary

OpenHands has multiple execution shapes. Hazmat's current safe recipe boundary
is simple: Hazmat contains the process tree it launches, and OpenHands runs
inside that boundary as an ordinary command.

That means:

- Native `hazmat exec` gives OpenHands the project, agent user, SBPL policy,
  and network policy that Hazmat planned for the session.
- OpenHands process mode is not a separate sandbox. It is acceptable only
  because Hazmat is the outer containment boundary.
- OpenHands Docker mode is not supported by native Hazmat recipes because
  passing `/var/run/docker.sock` would grant host Docker authority.
- OpenHands profile and persistence state should live in the contained agent
  account. Do not copy host `~/.openhands` into the session.

## Process-Mode CLI

Use an interactive session when you want the normal terminal UI:

```bash
hazmat shell -C ~/workspace/project --network none
openhands
```

Use a one-shot headless run when you want a bounded task:

```bash
hazmat exec -C ~/workspace/project --network none -- \
  openhands --headless --json -t "Inspect the project and report what tests exist."
```

Do not add `--always-approve` by default. If you choose it for a controlled
task, treat it as an OpenHands approval decision inside Hazmat containment:

```bash
hazmat exec -C ~/workspace/project -- \
  openhands --headless --json --always-approve -t "Run the unit tests and summarize failures."
```

For provider-backed runs, prefer configuring the provider inside the contained
agent account. A future first-class adapter should materialize typed
credentials; this recipe intentionally avoids env passthrough examples such as
`LLM_API_KEY=... hazmat exec ...`.

## Local Web Interface

OpenHands `web` exposes the CLI in a browser. Bind it to localhost only:

```bash
hazmat exec -C ~/workspace/project -- \
  openhands web --host 127.0.0.1 --port 12000
```

This is still a long-running service, not a Hazmat service harness. Stop it
with `Ctrl+C`, and do not expose it to the network unless a separate access
control story exists.

## Docker-Backed GUI

OpenHands `serve` is Docker-backed and mounts the project for the GUI server.
Do not run this shape from native Hazmat by passing the host Docker socket into
the session.

Use one of these instead:

- a separate VM where Docker socket authority is scoped to that VM
- Docker Sandbox/private-daemon work after confirming OpenHands can use that
  daemon without reaching the host Docker socket
- wait for a first-class Hazmat service harness that models readiness, local
  attach, credentials, cleanup, ports, logs, and residual service state

## State and Credentials

OpenHands V1 uses `~/.openhands` for local settings, MCP configuration, and
conversation history. It may also use environment variables such as
`LLM_API_KEY`, `LLM_MODEL`, `OH_PERSISTENCE_DIR`, and `SANDBOX_VOLUMES`.

For this recipe:

- keep OpenHands state inside the contained agent account
- do not mount or copy host `~/.openhands`
- do not pass host MCP secrets through generic env
- do not use `SANDBOX_VOLUMES` to widen project access without reviewing the
  exact host paths
- use `--network none` for fake-provider or local-only evaluation tasks

Hazmat denies and hardens `~/.openhands` as a host credential/state root while
OpenHands remains recipe-only.

## Validation Checklist

- `openhands` starts only through `hazmat shell` or `hazmat exec`.
- `~/.openhands` is agent-owned contained state, not copied from the host.
- No host Docker socket is mounted into native Hazmat containment.
- Web mode binds to `127.0.0.1`, not `0.0.0.0`.
- Provider credentials are absent, configured inside the contained account, or
  postponed until typed credential support exists.
- `--network none` works for tasks that do not need a live model/provider.
- Any live service validation is recorded as manual evidence, not as proof that
  `hazmat openhands` is implemented.

## First-Class Exit Criteria

A future `hazmat openhands` should be a service harness, not a thin foreground
wrapper. It needs:

- session-local OpenHands persistence
- typed provider/cloud credentials
- Docker Sandbox, VM, or other non-host-socket container authority
- local-only attach with tokenized access
- readiness and health checks before attach
- log redaction
- crash/hang cleanup and residue accounting
- fake-service tests before live OpenHands smokes become release gates

## Sources

- OpenHands CLI installation:
  <https://docs.openhands.dev/openhands/usage/cli/installation>
- OpenHands CLI command reference:
  <https://docs.openhands.dev/openhands/usage/cli/command-reference>
- OpenHands sandbox overview:
  <https://docs.openhands.dev/openhands/usage/sandboxes/overview>
- OpenHands process sandbox:
  <https://docs.openhands.dev/openhands/usage/sandboxes/process>
- OpenHands Docker sandbox:
  <https://docs.openhands.dev/openhands/usage/sandboxes/docker>
- OpenHands configuration options:
  <https://docs.openhands.dev/openhands/usage/advanced/configuration-options>
- Hazmat service harness boundary:
  [../plans/2026-06-12-service-harness-boundary-design.md](../plans/2026-06-12-service-harness-boundary-design.md)
- OpenHands harness decision:
  [../plans/2026-06-12-openhands-harness-evaluation.md](../plans/2026-06-12-openhands-harness-evaluation.md)
