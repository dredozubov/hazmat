# OpenHands Harness Candidate Evaluation

Status: Compatibility decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.2`
Depends on:
- `sandboxing-lg07.7.1`
- `sandboxing-lg07.3`
- `sandboxing-lksm`

Sources:
- OpenHands README: <https://github.com/OpenHands/openhands>
- OpenHands CLI command reference:
  <https://docs.openhands.dev/openhands/usage/cli/command-reference>
- OpenHands sandbox provider overview:
  <https://docs.openhands.dev/openhands/usage/sandboxes/overview>
- OpenHands agent server architecture:
  <https://docs.openhands.dev/sdk/arch/agent-server>
- OpenHands Docker sandbox guide:
  <https://docs.openhands.dev/sdk/guides/agent-server/docker-sandbox>
- OpenHands remote sandbox guide:
  <https://docs.openhands.dev/openhands/usage/sandboxes/remote>

## Decision

Do not add `hazmat openhands` as a first-class foreground harness in the next
release.

OpenHands should stay Tier 0: recipe or compatibility-note only. It is a strong
future Tier 1 service-harness candidate, but the useful OpenHands surface is not
just a one-process CLI. It includes a local GUI, REST/WebSocket server, ACP
server, Docker-backed workspaces, remote sandbox providers, and enterprise/VPC
deployment shapes. Those are service and backend boundaries, not ordinary
foreground harness boundaries.

The next safe user-facing output is a recipe that explains what Hazmat can
contain today and what it deliberately does not manage. Adapter code should wait
until Hazmat has the service lifecycle model and fake-service proof described in
[Service-Oriented Harness Boundary](2026-06-12-service-harness-boundary-design.md).

## Upstream Surface

OpenHands currently exposes several modes that matter for Hazmat:

- terminal/headless CLI with task seeding, resume, JSONL output, and automatic
  approval options
- `serve`, which launches the GUI server through Docker
- `web`, which starts a browser-accessible app and defaults to a LAN-visible
  bind address unless overridden
- `acp`, which starts an Agent Client Protocol server for IDE integrations
- MCP configuration commands
- Docker, process, and remote sandbox providers
- an agent-server package with HTTP endpoints, WebSocket streaming, health
  checks, per-user workspaces, and Docker container management
- SDK workspace types that can pull/start/health-check/clean up Docker-backed
  agent-server containers

That makes OpenHands strategically important, but it also means Hazmat must not
claim support by wrapping the first executable named `openhands`.

## Compatibility Matrix

| OpenHands shape | Hazmat fit now | Decision |
|---|---|---|
| Headless foreground CLI | Possible recipe through `hazmat exec` if install, auth, profile, and approval behavior are documented | Do not promote to `hazmat openhands` until a reviewed adapter defines lifecycle, credential, profile, and smoke-test coverage |
| Local web UI / GUI server | Service harness | Wait for session-scoped service lifecycle, local-only bind policy, token/log redaction, readiness, attach, stop, and crash cleanup |
| Docker sandbox provider | Backend-sensitive service harness | Use only with Docker Sandbox/private-daemon or future VM/container backends; native containment must not pass the host Docker socket |
| Process sandbox provider | Directly conflicts with Hazmat's containment claim if treated as the safety boundary | Recipe may contain the process, but docs must say OpenHands process mode itself is not the isolation layer |
| Remote sandbox / Runtime API / Cloud | Remote provider/backend | Out of scope for a local harness until Hazmat has remote worker admission, credential handles, replay defense, and cleanup proof |
| Enterprise/VPC deployment | Platform integration | Monitor; do not implement as a local harness |
| ACP server | Protocol/service hybrid | Can inform ACP/RPC work, but not as a generic plugin or repo-defined protocol descriptor |

## Required First-Class Shape

A first-class OpenHands adapter should be a session-scoped service harness, not a
simple CLI wrapper. The command can eventually look like:

```bash
hazmat openhands -C ~/workspace/project
hazmat openhands --docker=sandbox -C ~/workspace/project
hazmat explain --for openhands -C ~/workspace/project
```

That command is supportable only after Hazmat can model and test:

- service lifecycle phases: plan, start, ready, attach, stop, crash, cleanup
- recorded residue recovery after Hazmat or the service crashes
- local attach authority: stdio, Unix socket, or `127.0.0.1` random port with a
  per-session token
- denial of `0.0.0.0`/LAN-visible binds by default
- no host Docker socket in native containment
- Docker Sandbox or VM-backed container authority when OpenHands needs Docker
- typed credential registry grants for LLM, GitHub, MCP, and remote-sandbox
  credentials
- no wholesale host OpenHands profile, MCP, plugin, browser, or task-queue
  import
- redaction-safe logs that exclude prompt text, response bodies, env dumps,
  bearer tokens, cookies, and credential paths
- fake-service tests before any live OpenHands smoke is treated as proof

## Recipe Boundary

The recipe-only phase can document how to run an already-installed OpenHands
entrypoint inside an existing Hazmat session boundary, with narrow caveats:

- prefer headless/JSON foreground use over GUI/server modes
- require users to pass local-only host/port flags for web modes
- avoid `--always-approve` as a Hazmat default; if a user chooses it, Hazmat is
  only the outer containment boundary
- do not copy host OpenHands state or MCP configuration
- do not expose host Docker socket from native containment
- treat OpenHands remote/cloud credentials as ordinary secrets until typed
  registry entries exist

This recipe should be tested manually before it appears in the main harness
docs. Until then, `docs/harnesses.md` should mention OpenHands only as an
unsupported service-oriented candidate.

## Follow-Up Work

The compatibility decision creates three concrete work items:

1. Write and manually validate an OpenHands recipe that uses existing Hazmat
   containment without claiming first-class support.
2. Add the service lifecycle TLA+ model and design note before any
   session-scoped service harness implementation.
3. Build a fake-service harness smoke suite that exercises readiness, attach,
   logs, crash, stop, cleanup, port policy, and credential redaction before any
   live OpenHands smoke becomes a release gate.

No implementation code should be added for OpenHands before those gates exist.
