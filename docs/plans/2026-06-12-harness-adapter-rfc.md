# Harness Adapter RFC

Status: Proposed
Date: 2026-06-12
Related issue: `sandboxing-lksm`
Blocks:
- `sandboxing-lg07.5.1`
- `sandboxing-lg07.7.1`

## Purpose

Hazmat has enough built-in harnesses that adding the next one should not mean
copying policy decisions across bootstrap, import, asset sync, lifecycle
status, session launch, Docker routing, credentials, uninstall, docs, and tests.

This RFC defines a future harness adapter contract that keeps Hazmat's trust
boundary maintainer-owned. The goal is composability inside the codebase, not an
arbitrary third-party plugin system.

## Position

Harness adapters should be static, built-in, and reviewed as Hazmat code. A
harness adapter may describe how one known harness is installed, probed,
launched, authenticated, summarized, and cleaned up. It may not define new
containment policy, new credential authority, new host mutations, new rollback
semantics, or executable policy loaded from a project or third-party manifest.

The contract is a closed registry of maintainer-owned adapters:

- supported harnesses ship in the Hazmat binary
- experimental harnesses can ship in the same registry with narrower declared
  capabilities
- community contributions are proposed as code and docs, then reviewed against
  the same security and verification gates
- project files may request or configure an already-known harness, but they do
  not install or define harness behavior

This is stricter than a plugin model by design. A harness controls agent process
startup and often touches credentials, local profiles, installers, OAuth flows,
MCP settings, and long-lived agent-home state. Those are trust-boundary
decisions, not ordinary extension points.

## Approaches Considered

### Recommended: Closed Adapter Registry

Each harness has a registry entry with declarative facts plus maintainer-written
functions for the parts that cannot be safely described as data. The registry is
the single source for lifecycle status, update, import support, asset sync,
launch requirements, uninstall scope, and test coverage.

This matches the current direction in `hazmat/harness.go`, the credential
registry, and the shared session preparation pipeline. It keeps cross-cutting
security behavior centralized while still reducing per-harness copy/paste.

### Alternative: Per-Harness Modules Own Everything

Each harness could own its full lifecycle end to end. That is easy to reason
about locally, but it fragments the guarantees users care about: dry-run
behavior, secret-store delivery, rollback preservation, Docker/native routing,
and uninstall boundaries would drift by harness.

### Rejected: Arbitrary Plugin or Manifest Harnesses

A manifest or plugin system would let users add harnesses without rebuilding
Hazmat. That is attractive for coverage, but it is unsafe without a separate
trust model. A harness plugin would need authority over installers, executable
paths, forwarded arguments, environment, credentials, profile state, and cleanup
scope. Treating that as repo-controlled data would let untrusted projects shape
the containment boundary.

## Adapter Surface

An adapter may provide these categories of behavior.

### Identity and Lifecycle

- stable `HarnessID`
- display name and command aliases
- state version
- supported lifecycle commands
- install/update availability
- read-only version probe
- managed code artifact list
- preserved data summary
- uninstall plan metadata

Lifecycle operations must use shared runner, dry-run, state, and uninstall
helpers. Adapters describe ownership; shared code enforces path scope, type
checks, dry-run behavior, prompt behavior, and metadata persistence.

### Launch Shape

- foreground command path inside the agent account
- default forwarded arguments, if any
- explicit app-level permission-bypass flags Hazmat owns
- supported session modes: native, Docker Sandbox, or plan-only backend preview
- resume/fork/session-history sync requirements
- status-bar and metadata support
- session-home or profile-root requirements

The adapter must not write Seatbelt rules, Docker mount policy, network policy,
or backend admission rules. It can declare requirements; the shared session
planner decides whether the selected backend can satisfy them.

### Credentials and Profiles

- credential registry references the harness may consume
- delivery modes it supports: env, materialized file, broker, external backend,
  or adapter-required
- curated host import support and exact imported file classes
- agent-side auth/profile paths that are preserved by default
- volatile session artifacts that should be harvested, deleted, or ignored
- non-secret portable asset sync inputs

Adapters do not read secret bytes directly. They ask typed credential stores and
runtime helpers for redacted summaries, materialization, harvest, cleanup, and
crash recovery. New credential classes remain registry work first, not adapter
private code.

### Diagnostics and UX

- status summaries
- unsupported-capability reasons
- bootstrap/update next actions
- import availability
- manual verification commands
- docs anchors

Adapters may contribute copy and redaction-safe facts. They do not generate
repair shell recipes or approve host mutations. Diagnostics remain typed and
repair actions remain Hazmat-owned.

## What Stays Built-In

These areas are not adapter extension points:

- setup and rollback resources
- sudoers, launch helper, pf, DNS, and account management
- Seatbelt policy and path-deny rules
- Docker Sandbox backend admission, mounts, env, and network handling
- Apple Container or future backend launch boundaries
- credential secret-store schema, broker protocols, and crash recovery
- Git SSH and Git HTTPS broker authority
- session-time host permission repair planning
- repo hook approval and dispatcher policy
- root CLI command registration for new top-level capabilities
- TLA+ model membership and promoted proof obligations

If a new harness needs one of these areas to change, that work is a Hazmat core
change with its own design, tests, and model updates. The adapter may depend on
the capability only after the core capability exists.

## Data Flow

1. The static registry maps `HarnessID` to an adapter.
2. Lifecycle commands read adapter identity, probe, artifact, and preserved-data
   metadata.
3. Import commands ask whether curated import is supported and then call the
   reviewed import path for that harness.
4. Session preparation resolves the target harness ID, shared access plan,
   integrations, Docker routing, credential grants, auth materialization,
   profile/session-home plan, and host mutations.
5. Backend admission checks the resolved plan. Unsupported adapter requirements
   fail closed with a typed reason.
6. Launch uses shared native or Docker entrypoints. Adapter-specific code
   contributes only command argv/env/profile details already accepted by the
   planner.
7. Cleanup and harvest use shared credential/session runtime helpers.
8. Status and explain output render redaction-safe facts from the same resolved
   plan.

## Error Handling

Unknown harness IDs fail before planning. Unsupported adapter requirements fail
closed with the exact missing capability, such as `credential adapter required`,
`Docker Sandbox unsupported`, or `service harness boundary not implemented`.

Adapter probes are read-only. A probe failure is status data, not an install or
repair attempt. Update and import paths mutate only through explicit commands,
respect `--dry-run`, and record state only after verification succeeds.

Uninstall removes only declared Hazmat-owned code artifacts and selected
metadata. Profile roots, auth, sessions, imported basics, provider secrets, Git
credentials, SSH identities, and project files remain preserved unless a future
mode explicitly models and documents a wider delete.

## Testing and Verification

Adding a built-in adapter requires:

- registry completeness tests
- lifecycle status/update/uninstall tests
- credential summary and delivery tests for every consumed credential class
- import tests if curated import is supported
- asset-sync tests if portable assets are supported
- explain/session contract tests for native and Docker routing behavior
- hermetic harness smoke coverage in `scripts/e2e-harness-smoke.sh`
- docs in `docs/harnesses.md` and manual-testing rows

TLA+ work is required when the adapter changes a modeled set or boundary:

- adding a managed harness updates `MC_HarnessLifecycle`
- adding or changing file-backed auth recovery updates
  `MC_SecretStoreRecovery`
- adding credential classes or delivery modes updates
  `MC_CredentialCapabilityLifecycle`
- changing setup, rollback, seatbelt, backend launch, Git hook, or permission
  repair behavior starts from the corresponding model

No adapter may merge with only manual testing if it changes credential delivery,
host mutation, rollback, or containment behavior.

## Protocol and Service Harnesses

ACP/RPC harnesses, OpenHands-style services, OpenClaw-style platforms, and
browser-control agents need a service-oriented adapter shape, but not a looser
trust model. Their adapter can declare service process roles, sockets, ports,
workspace roots, credential requirements, and health probes. The broker,
network policy, launch ordering, and host authority still stay in Hazmat core.

For v1, service harnesses should start as explicit built-in experimental
adapters with foreground or session-scoped lifetimes. Persistent daemons,
background servers, browser automation, or privileged sidecars require separate
model-first designs before they become supported adapter capabilities.

## Non-Goals

- loading harness definitions from project files
- running third-party adapter plugins
- allowing manifests to add credential delivery modes
- allowing adapters to write SBPL, Docker mounts, pf rules, or sudoers entries
- generic import of host profile directories
- broad uninstall or purge semantics without model updates

## Follow-Ups

- Design the ACP/RPC foreground harness driver against this adapter boundary.
  See [ACP/RPC Harness Driver Design](2026-06-12-acp-rpc-harness-driver-design.md).
- Design the service-oriented boundary for OpenHands and OpenClaw-style
  platforms without persistent host daemons by default.
- Refactor current built-in harness metadata toward the adapter categories only
  after status JSON and credential summaries are stable.
