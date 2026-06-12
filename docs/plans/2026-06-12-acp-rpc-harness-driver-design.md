# ACP/RPC Harness Driver Design

Status: Proposed decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.1`
Depends on:
- `sandboxing-lg07.1`
- `sandboxing-lksm`

## Decision

Hazmat should support ACP/RPC protocol agents only through maintainer-owned,
built-in harness adapters. The first supported shape should be a contained
foreground process with a stdio JSON-RPC transport. Hazmat should not become a
generic user-supplied ACP/RPC plugin host.

Near term, ACP/RPC-compatible agents are recipes or foreground harness
candidates: Hazmat contains the local agent process and any child processes it
spawns. A candidate becomes first-class only when a built-in adapter defines its
launch command, protocol transport, prompt/request schema, credential needs,
profile state, lifecycle, smoke tests, and docs.

Hazmat is therefore a containment launcher first. It may later include a
protocol client for specific built-in adapters, but it should not accept
arbitrary protocol descriptors from repos, Open Design registry files, or user
manifests as executable harness definitions.

## Why Not Generic ACP/RPC Plugins

Protocol agents are not just chat frontends. A protocol driver can decide:

- which executable starts
- which workspace and extra paths are visible
- which prompt and attachment bytes are sent
- which model/provider settings are active
- whether external MCP servers are enabled
- how cancellation and errors are interpreted
- whether credentials are materialized, brokered, or denied
- which session logs and sidecars persist

Those choices are part of Hazmat's containment and credential boundary. They
belong in reviewed code and typed registries, not in repo-controlled data or a
third-party plugin package.

## Support Phases

### Phase 0: Recipe-Only Containment

Document local ACP/RPC agents as ordinary contained processes:

```bash
hazmat exec -C ~/workspace/project -- <agent-server> --stdio
```

This is not first-class harness support. Hazmat provides filesystem, process,
network, and credential containment for the launched process, but it does not
parse the protocol, send prompts, manage model selection, or promise resume
semantics.

### Phase 1: Built-In Foreground Protocol Harness

A candidate may become `hazmat <harness>` when a built-in adapter supplies:

- foreground launch argv
- stdio JSON-RPC transport declaration
- typed prompt/request builder
- supported request fields
- stream/event decoder
- cancellation behavior
- model/provider option mapping
- attachment path policy
- lifecycle status and update/probe behavior
- credential/profile/import policy
- fake-server smoke tests

The command remains harness-specific, for example `hazmat devin ...`, not a
generic `hazmat acp --plugin path`. That preserves ordinary harness UX while
keeping the registry closed.

### Phase 2: Shared Protocol Driver Library

After two built-in protocol harnesses need the same machinery, extract shared
internal protocol-driver code. The library should own framing, request DTO
validation, event classification, cancellation, and redaction-safe logs. It
should not load adapter behavior dynamically.

## User-Facing Command Shape

First-class protocol harnesses should look like existing harnesses:

```bash
hazmat <harness> -C ~/workspace/project --prompt "review this change"
hazmat <harness> -C ~/workspace/project --prompt-file prompt.md
hazmat <harness> -C ~/workspace/project --attach src/main.go
hazmat <harness> --model <declared-model-alias> ...
```

Exact flags can vary by harness, but the adapter must map every Hazmat-owned
flag to a typed protocol field or a declared upstream CLI argument. Unknown
adapter-specific flags should be forwarded only when the candidate's adapter
declares the forwarding boundary safe and testable.

Interactive TUI-style protocol clients are out of scope for the first driver.
The first version should prefer single request/streaming-response sessions
because they are easier to test, cancel, and log without making Hazmat a full
IDE client.

## Request Model

The request object should be constructed by Hazmat, then serialized to the
protocol transport. It should include only redaction-safe, typed fields:

- canonical project directory
- requested cwd relative to project, if supported
- prompt bytes or prompt-file bytes
- attachments as canonical paths plus declared role
- model alias after adapter validation
- optional session metadata IDs
- requested network mode and containment mode as metadata, not as authority

Attachments must be canonicalized before serialization. Paths outside the
session's planned project/read/write scopes are rejected before launch. File
contents are sent only when the adapter explicitly supports content attachments;
otherwise the agent receives paths and must read them through the contained
filesystem boundary.

## Cancellation and Errors

Cancellation should be two-stage:

1. If the protocol supports cancellation, send the typed cancellation request.
2. If the process does not exit promptly, terminate the contained process group
   using the same launch cleanup rules as other harnesses.

Streaming events should be classified as:

- assistant output
- progress/status
- tool/request metadata
- recoverable protocol warning
- fatal protocol error
- transport failure

Raw JSON-RPC payloads should not be printed by default. Debug traces may record
method names, event classes, durations, and redaction-safe IDs. Secret-looking
fields are never logged.

## External MCP and Tooling

The ACP/RPC adapter must not import host MCP config or activate external MCP
servers by default. If a protocol agent can spawn MCP servers, those servers run
inside the same contained process tree and inherit only the session credentials
Hazmat intentionally granted to the harness.

Per-MCP credential scoping is a future capability, not something the ACP driver
can claim. Until that exists, docs should recommend separate sessions for
networked MCP work and avoid broad harness-level credential grants.

## Shared vs New Architecture

Shared with existing harnesses:

- root session preparation
- project/read/write access planning
- native and Docker backend selection
- session contract and explain JSON
- credential registry and secret-store runtime
- harness lifecycle state
- asset sync only for declared portable assets
- rollback and delete-user boundaries
- trace bundle conventions

New for protocol harnesses:

- typed protocol request DTOs
- stdio JSON-RPC framing
- stream/event decoding
- cancellation mapping
- fake protocol server fixtures
- protocol-specific status/error classification

The new protocol driver should live behind internal APIs. Existing foreground
harnesses should not be forced through it.

## Governance

No TLA+ update is required for Phase 0 recipes because Hazmat is only launching
an ordinary contained process through existing `exec` semantics.

Implementation of a first-class protocol harness may require model work:

- `MC_HarnessLifecycle` when adding a new `HarnessID`, state version, lifecycle
  status, update, or uninstall scope
- `MC_CredentialCapabilityLifecycle` when adding credential classes or delivery
  modes
- `MC_SecretStoreRecovery` when materialized auth needs harvest/recovery
- backend launch models if the protocol driver adds sockets, persistent
  sidecars, or service readiness gates outside stdio
- setup/rollback models if any persistent host resource is introduced

If the driver stays stdio-only and uses existing session launch, cleanup, and
credential delivery classes, protocol framing itself is governed by tests and
docs rather than a new model.

## Testing

Every first-class protocol harness needs:

- unit tests for request DTO constructors and path rejection
- unit tests for model alias validation
- fake-server tests for success, warning, fatal error, malformed JSON, and
  stream interruption
- cancellation tests that prove protocol cancel is attempted before process
  termination
- explain/session contract tests for native and Docker routing
- lifecycle status/update/uninstall tests
- credential inventory tests for supported and denied auth paths
- hermetic harness smoke coverage with a fake protocol server
- manual test rows only after fake-server coverage exists

The fake server should assert cwd, argv, environment, request shape, attachment
paths, cancellation, and cleanup. It must not call real model providers.

## Candidate Evaluation Rule

For each Open Design ACP/RPC candidate, evaluate in this order:

1. Can the candidate run as a meaningful local foreground process under
   `hazmat exec`?
2. Is its protocol transport local stdio, local socket, remote API, or mixed?
3. Can a fake server reproduce enough behavior for hermetic tests?
4. Does it require host profile import, MCP config import, browser automation,
   persistent daemons, or Docker socket access?
5. Does a built-in adapter add enough user value over a recipe?

Candidates that fail local foreground containment stay recipes or research
items. Candidates that require service lifecycle, browser control, remote
workers, or persistent daemons move to the service-oriented harness boundary
design instead of this foreground protocol driver.

## Follow-Ups

- Evaluate Devin for Terminal against this decision.
- Evaluate Kimi, Kiro, Kilo, Mistral Vibe, Trae CLI, and Pi only after recording
  their actual transport and fake-server feasibility.
- Design the separate service-oriented harness boundary for OpenHands and
  OpenClaw-style platforms before treating them as protocol harnesses:
  [Service-Oriented Harness Boundary](2026-06-12-service-harness-boundary-design.md).
