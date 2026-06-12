# Service-Oriented Harness Boundary

Status: Proposed decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.1`
Depends on:
- `sandboxing-lg07.3`
- `sandboxing-lksm`

## Decision

Hazmat should not treat service-oriented coding-agent platforms as quick
foreground CLI harnesses. OpenHands-style platforms can become first-class only
after Hazmat has a modeled session-scoped service lifecycle. OpenClaw-style
gateway/platform systems should remain monitored research or recipes until
their execution, plugin, credential, and persistence boundaries are understood.

The first supported service shape should be:

- started by an explicit Hazmat command
- scoped to one Hazmat session
- launched under an existing contained backend
- health-checked before attach
- stopped on normal exit
- cleaned up on crash/restart by recorded session metadata
- never installed as a persistent host daemon by default

This keeps service support compatible with Hazmat's core product: a session
contract for a bounded local execution, not a general platform supervisor.

## What Counts As A Service Harness

A service-oriented harness is any agent platform that needs one or more
long-running processes around the coding agent. Typical examples:

- REST API server
- local web UI
- agent runtime server
- worker/sandbox manager
- browser/computer-use sidecar
- MCP hub
- Docker or VM runtime controller
- background task queue

If a tool can run as a normal foreground CLI with one prompt and one stream, it
belongs in the regular harness or ACP/RPC foreground design. If it needs
readiness, ports, logs, attach, stop, crash handling, or persistent state, it
belongs in this service boundary.

## Product Tiers

### Tier 0: Recipe Or Compatibility Note

Use existing `hazmat exec`, `hazmat shell`, Docker Sandbox mode, or external VM
guides. Hazmat contains the process the user launches, but it does not manage
the platform lifecycle.

This is the near-term fit for most OpenHands/OpenClaw exploration.

### Tier 1: Session-Scoped Built-In Service Harness

Hazmat owns a built-in adapter that starts a known service tree for one session,
waits for health, exposes a declared local attach point, tails redaction-safe
logs, stops the service, and records cleanup metadata.

This is the first possible first-class OpenHands shape.

### Tier 2: Backend Or Remote Provider

If the platform is really a sandbox provider, remote worker, or private-cloud
control plane, it should integrate through backend/remote-runner designs, not
as a local harness. That requires worker admission, credential handles, replay
defense, path mapping, cleanup proof, and separate TLA+ work.

This is the likely long-term shape for enterprise/private-cloud agent
platforms.

### Tier 3: Unsupported Platform

If the platform requires host-wide plugins, host browser automation, shared
Docker daemon authority, unbounded MCP/profile import, persistent background
agents, or credentials outside Hazmat's registry, it remains unsupported except
as research.

## User-Facing Shape

Do not add a generic service plugin command. First-class service support should
still be adapter-backed and specific:

```bash
hazmat openhands -C ~/workspace/project
hazmat openhands --docker=sandbox -C ~/workspace/project
hazmat explain --for openhands -C ~/workspace/project
```

If a generic command is needed for development, keep it hidden or debug-only
until the lifecycle model is proved:

```bash
hazmat debug service-harness smoke <adapter>
```

The normal user command should print the session contract, service lifecycle
plan, local attach details, and cleanup behavior before starting the service.

## Lifecycle Model

A first-class service harness needs an explicit state machine:

```text
idle
  -> planned
  -> starting
  -> ready
  -> attached
  -> stopping
  -> stopped

failure states:
  starting -> failed
  ready/attached -> crashed
  stopping -> cleanup_failed
```

The implementation must record enough metadata to recover after Hazmat itself
crashes:

- session ID
- adapter ID and version
- process IDs or container IDs
- socket/port paths
- temporary directories
- log locations
- credential artifact IDs
- cleanup obligations
- readiness evidence
- last observed phase

Startup is not complete until health checks pass. Attach must not happen before
readiness. Cleanup must run on normal exit and on the next Hazmat command if a
previous service session left residue.

## Ports, Sockets, And UI

Services must not bind `0.0.0.0` or LAN-visible addresses by default. The first
service harness should prefer:

- stdio attach, or
- Unix-domain socket inside session temp, or
- `127.0.0.1` with an explicit random port and a per-session token when a local
  browser UI is the only practical interface

Hazmat should print attach details only after readiness and should classify them
as session authority. Tokens, bearer secrets, and full URLs containing secrets
must not appear in logs or JSON unless redacted.

Browser automation is out of scope for the first service harness. Opening a UI
in the user's browser can be a manual step, but Hazmat should not grant browser
TCC authority, import browser profiles, or drive a host browser as part of
service launch.

## Docker, VM, And Sandbox Authority

No service harness may receive the host Docker socket in native containment.
If a platform needs containers, the supported options are:

- Docker Sandbox mode with a private daemon
- a future Apple Container or VM backend that declares equivalent isolation
- an external VM recipe outside first-class service support

OpenHands local Docker-provider workflows should therefore start as Docker
Sandbox or VM recipes, not native sessions that punch through to the host
daemon.

Service adapters must reject socket publishing, SSH forwarding, broad home
mounts, and integration env passthrough unless the selected backend already
models and supports those features.

## Credentials And Profiles

Service platforms usually have more credential surfaces than foreground CLIs:
provider keys, GitHub tokens, browser tokens, MCP tokens, sandbox-provider
tokens, database URLs, and platform admin secrets.

Hazmat should support only typed registry-backed credentials. A service adapter
may consume existing credential classes only when the registry declares the
adapter as an allowed consumer. New classes require the credential lifecycle
model first.

Do not import host platform profiles wholesale. Do not sync:

- MCP server configs
- plugins
- skills with executable code
- browser profiles
- platform admin settings
- token caches
- memory databases
- task queues

Portable prompts or repo-local config can be considered only after the adapter
declares them non-secret and non-executable.

## Logs And Telemetry

Service logs are useful but high risk. The first implementation should record
only:

- phase transitions
- health-check status
- process/container IDs
- method names or endpoint classes
- duration and exit status
- redaction-safe error classes

Raw request/response bodies, prompt text, model output, env dumps, bearer
tokens, credential paths, and browser/session cookies must not be logged by
default.

An explicit debug mode may collect more, but it must document redaction limits
and stay out of normal session output.

## OpenHands Decision

OpenHands is a plausible future Tier 1 service harness candidate, not a quick
foreground CLI addition.

Near term:

- document recipes for running OpenHands components under `hazmat exec`,
  Docker Sandbox, or a VM when practical
- evaluate which OpenHands mode can run without host Docker socket access
- require fake-service lifecycle tests before adapter code

First-class support requires:

- service lifecycle TLA+ model
- private container/VM story for sandboxed execution
- explicit port/UI policy
- registry-backed credentials only
- no host profile import
- crash cleanup and residue recovery

## OpenClaw Decision

OpenClaw-style platforms should be monitored, not implemented as a quick
harness. The platform shape is closer to a gateway/agent operating environment
than a single local coding CLI. Its plugin, skill, MCP, browser, credential,
and persistence boundaries need separate research before Hazmat can safely
claim first-class support.

Near-term output should be a compatibility or risk note, not adapter code.

## TLA+ Requirements

Implementation must start with model work if any of these become true:

- adding a managed service harness ID or lifecycle state:
  `MC_HarnessLifecycle`
- adding service start/ready/attach/stop/crash cleanup phases: new service
  lifecycle spec, promoted before implementation
- changing credential classes or delivery modes:
  `MC_CredentialCapabilityLifecycle`
- materializing service auth files with crash recovery:
  `MC_SecretStoreRecovery`
- exposing sockets, ports, or backend launch artifacts with authority:
  relevant backend launch model or a new service launch boundary model
- adding persistent host daemons, launch agents, or setup resources:
  `MC_SetupRollback`

If the service stays recipe-only through `hazmat exec`, no new model is needed
because Hazmat is using existing foreground process semantics.

## Testing Gates

Before first-class service support:

- fake service binary that can start, report health, stream logs, accept attach,
  crash, hang, and stop
- unit tests for lifecycle transitions
- crash-recovery tests for stale service metadata
- port/socket policy tests
- credential grant denial and redaction tests
- explain/session contract tests
- Docker/native backend compatibility tests
- hermetic e2e smoke with fake service
- manual tests only after fake-service coverage exists

Live OpenHands or OpenClaw testing should be treated as a guarded manual smoke,
not the primary proof that the boundary works.

## Follow-Ups

- OpenHands has been evaluated against this boundary in
  [OpenHands Harness Candidate Evaluation](2026-06-12-openhands-harness-evaluation.md).
  The current decision is recipe-only for the next release; adapter code waits
  for service lifecycle modeling and fake-service proof.
- Record OpenClaw as monitor/service-platform research unless a narrow local
  mode emerges.
- If OpenHands remains strategically important, create the service lifecycle
  TLA+ spec before writing adapter code.
