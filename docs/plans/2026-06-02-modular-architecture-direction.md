# Modular Hazmat Architecture Direction

**Date:** 2026-06-02
**Bead:** `sandboxing-kieu`
**Status:** Architecture direction, not implementation approval
**Related plans:**
[reusable library decomposition](2026-05-28-reusable-library-decomposition.md),
[Linux-ready backend architecture](2026-05-28-linux-ready-backend-architecture.md),
[Docker Tier 3 audit design](2026-04-01-docker-tier3-audit-design.md)

## Purpose

Hazmat should split into modular packages that can be reused by local CLIs,
future platform-specific binaries, and remote/orchestrated agent runtimes
without importing command wiring or weakening containment guarantees. The split
should also make invalid states hard or impossible to construct. Callers should
be guided toward legal requests, legal policy plans, legal backend choices, and
legal lifecycle transitions by the package APIs themselves.

This document is for audit before implementation. It proposes package
boundaries, type-state direction, data flow, trust boundaries, and migration
gates. It does not approve broad code movement or behavior changes.

## Current Starting Point

The repository already has the first pieces of the reusable shape:

- `sessionmeta` owns network parsing, runtime labels, and launch metadata.
- `sessioncontract` owns side-effect-free request and plan JSON shapes.
- `sessionbackend` owns backend selection and capability gap reporting.
- `containment` owns a backend-neutral authority contract.
- `containment/linux` and `platform/linux` are plan/compile-oriented Linux
  slices.
- `pathpolicy` owns shared path and deny-zone helpers.
- `integrations` owns declarative integration manifests.

Most of the operational behavior still lives in `package main`, especially
session preparation, harness-specific branches, credential delivery, setup,
rollback, native launch, Docker routing, and UI rendering. That is acceptable
for the current product, but it makes reuse hard because callers must either
shell out to the CLI or import code with globals, prompts, host assumptions,
and side effects.

## Direction

Hazmat should become a layered library plus product. The CLI remains the
primary user experience, but it should assemble typed requests, call planner
libraries, select a backend, and render results. The reusable packages should
own the product-level containment model and backend compilers. The CLI should
not own policy semantics.

Target shape:

```text
CLI / local API / remote control plane
  -> typed request builder
  -> planner
       -> path policy
       -> integrations
       -> harness requirements
       -> credential grant descriptors
       -> containment contract
       -> host mutation plan
  -> backend compiler
       -> darwin native launch spec / SBPL
       -> linux native launch spec
       -> Docker Sandbox spec
       -> remote worker launch envelope
  -> backend runner
       -> local native runner
       -> local Docker runner
       -> remote/orchestrated worker runner
  -> lifecycle result / telemetry / cleanup
```

The planner is the stable center. It should be side-effect-free and
backend-neutral. Backends compile and enforce the planner's contract. Runners
perform side effects only after receiving a prepared, validated launch artifact.

## Boundary Principles

Reusable packages must not depend on Cobra, terminal UI, prompts, process-wide
flags, hidden `$HOME` reads, global `agentUID`/`sharedGID`, or command-specific
branching. They should accept explicit request structs and return explicit
result structs.

Policy packages should construct authority models, not launch agents. Backend
packages may perform platform-specific work, but only behind narrow interfaces
that receive already-validated plans. Credential packages should return
redaction-safe descriptors, broker handles, or delivery plans; they must not
export raw secrets through reusable APIs.

Platform-specific mechanisms belong at the backend edge. Shared packages should
describe authority in product terms: path grants, credential denies, network
policy, process policy, service grants, agent home policy, temp policy, and
cleanup requirements. SBPL, Linux namespaces, Docker Sandbox configuration, and
remote worker envelopes are compilers of that model.

## Package Direction

| Layer | Package direction | Responsibility |
| --- | --- | --- |
| Core metadata | `sessionmeta` | Stable labels, network modes, launch metadata. |
| Request contract | `sessionrequest` or expanded `sessioncontract` | Builder and validated request types for callers. |
| Session plan | `sessioncontract` | JSON-safe backend-neutral plan and preview output. |
| Path policy | `pathpolicy` | Canonical paths, overlap rules, deny-zone validation, path grants. |
| Containment model | `containment` | Product-level authority contract independent of OS. |
| Integrations | `integrations` | Manifest parsing, repo detection, env passthrough planning, warnings. |
| Harness registry | `harnesses` | Built-in harness descriptors, launch requirements, lifecycle metadata. |
| Credentials | `credentials` | Registry descriptors, grant requests, delivery handles, redaction. |
| Planner | `sessionplanner` | Convert validated request into contract, backend plan, host mutation plan, warnings. |
| Backend selection | `sessionbackend` | Backend kind, capability gaps, artifact classes. |
| Backend compilers | `backends/*/compile` or `containment/{darwin,linux,docker}` | Compile containment contract into backend launch artifacts. |
| Backend runners | `backends/{darwin,linux,docker,remote}` | Prepare artifacts, launch, monitor, cleanup. |
| Host setup | `setup/{darwin,linux}` | Persistent host resources after model approval. |
| Host execution | `hostexec` | Narrow command runner interfaces and platform implementations. |
| CLI | `package main` | Parse flags, call packages, render output, preserve compatibility. |

The exact names can change during implementation, but the dependency direction
should not. Higher layers may depend on lower data packages. Lower packages
must not import CLI commands, UI rendering, or concrete launch runners.

## Multi-OS Direction

Multi-OS support should not become scattered `runtime.GOOS` branching in the
planner. The planner should produce one backend-neutral plan. Backend selection
then records either a concrete backend or explicit capability gaps.

macOS native, Linux native, Docker Sandbox, and future remote workers should
all compile the same containment contract. Their implementation mechanisms will
differ:

- macOS native compiles to SBPL plus `hazmat-launch` sandbox initialization.
- Linux native compiles to a launch spec for namespaces, mounts, network
  namespace policy, privilege drop, and optional Landlock/seccomp/cgroups.
- Docker Sandbox compiles to a private-daemon runtime spec and network profile.
- Remote/orchestrated launch compiles to a worker envelope plus constraints the
  worker must verify before starting the agent.

Capability gaps are first-class output. If Linux native cannot yet enforce a
deny shape that macOS can, the planner should not silently approximate it. It
should either reject the request or return a structured gap that the caller can
render or act on.

## Remote And Orchestrated Reuse

Remote/orchestrated execution should reuse Hazmat's planner and containment
contract, not fork a separate security model. The remote control plane can run
the same planning packages as the local CLI, then send a validated launch
envelope to a worker that has a backend runner.

Recommended control-plane shape:

```text
operator / service API
  -> request builder
  -> sessionplanner.Plan
  -> remote launch envelope
  -> worker admission check
  -> backend compiler on worker
  -> backend runner
  -> result, telemetry, cleanup proof
```

The remote envelope should carry only redaction-safe material:

- request and plan format versions
- selected backend kind and required capabilities
- normalized project/workspace identity
- path grant intent in worker-local terms
- credential grant handles, not secret bytes
- network policy
- expected lifecycle artifacts
- cleanup requirements
- nonce/session ID and expiry

The worker must re-validate the envelope before launch. It should prove that
the requested backend is available, that path grants map to the intended worker
workspace, that credential handles are scoped to the session, and that the
network policy can be enforced. A remote worker must not accept raw CLI flags as
authority.

Remote orchestration also needs a clear state boundary. A durable control plane
may keep session records and redacted plan summaries. Workers should keep
per-session runtime state and cleanup artifacts. Agent-created durable state
should be opt-in and classified by harness; it should not accidentally inherit
the local `/Users/agent` persistence model.

## Illegal States Should Be Irrepresentable

The package split should move Hazmat away from large structs of strings and
booleans that can represent contradictory states. The desired model is a typed
pipeline:

```text
Raw CLI/service input
  -> ParsedInput
  -> ValidatedRequest
  -> AuthorizedRequest
  -> PlannedSession
  -> PreparedLaunch
  -> RunningSession
  -> CompletedSession
  -> CleanedSession
```

Each transition should be a constructor or method that validates and returns
the next type. Callers should not be able to prepare a launch from raw flags,
run an unprepared backend artifact, clean up an artifact that was never
created, or attach credentials that were not granted by the credential
registry.

Specific type directions:

- `AbsolutePath`, `ExistingDir`, `CanonicalDir`, `ProjectRoot`,
  `ReadOnlyGrant`, and `ReadWriteGrant` should replace ambiguous path strings
  at package boundaries.
- `NetworkPolicy` should be a closed type. Unsupported combinations such as a
  remote backend with unimplemented `network none` enforcement should become a
  capability gap or validation error.
- `BackendKind` should be closed over known backends, with an explicit
  unsupported/gap state rather than free-form strings.
- `CredentialGrant` should be constructible only from registry/broker APIs.
  Raw env var strings can appear in rendered compatibility output, not in the
  authority model.
- `HostMutationPlan` should separate `None`, `DryRun`, `PendingApproval`, and
  `Approved` states so setup or repair steps cannot run by accident.
- `PreparedLaunch` should carry exactly one backend artifact variant:
  `DarwinSeatbelt`, `LinuxLaunchSpec`, `DockerSandboxSpec`, or
  `RemoteEnvelope`.
- `CleanupHandle` should exist only after a backend prepares an artifact that
  needs cleanup.

This does not mean every package needs heavy generics. Plain Go types with
unexported fields and small constructors are enough for most of the invariant.
The important rule is that exported structs should not let external callers
assemble known-bad authority states by filling public fields directly.

## Legal States Should Be Easy To Produce

Making illegal states unrepresentable should not make callers hand-build every
detail. The public API should provide simple constructors and builders for the
common legal cases:

```go
req, err := sessionrequest.New("codex", projectRoot).
    WithNetwork(sessionmeta.NetworkDefault).
    WithReadOnlyDirs(extraDocs).
    WithIntegrationHints(hints).
    Build()

plan, err := sessionplanner.Plan(ctx, req, env)
```

The builder should default safe values, normalize paths, de-duplicate grants,
collect integration hints, and report clear validation errors. It should not
require callers to know SBPL order, Linux mount semantics, Docker routing, or
credential deny subpaths.

For remote callers, provide an equally direct shape:

```go
envelope, err := remote.PrepareEnvelope(ctx, plan, workerTarget)
```

That function should fail if the plan contains local-only assumptions, raw
secrets, unsupported host mutations, or backend gaps the selected worker cannot
close.

## Trust Boundaries

The architecture should keep these actors separate:

| Actor | Trusted for | Not trusted for |
| --- | --- | --- |
| CLI/service caller | User intent and display preferences | Policy enforcement, credential materialization |
| Planner | Contract construction and validation | Backend-specific enforcement |
| Backend compiler | Translating contract to artifact | Broad host mutation outside its backend |
| Backend runner | Applying one prepared artifact | Changing planner authority |
| Remote control plane | Scheduling and durable records | Secret bytes, worker-local enforcement shortcuts |
| Worker | Local enforcement and cleanup proof | Expanding requested authority |
| Agent process | Performing requested work | Boundary decisions, credential routing, cleanup |
| Credential broker | Secret release by scoped grant | Session planning or path expansion |

Every boundary crossing should use a typed, versioned artifact. For local CLI
use, that artifact can stay in memory. For remote/orchestrated use, it should
be serializable, auditable, and revalidated at the worker.

## TLA+ And Audit Gates

Existing model-first areas stay model-first:

- setup/init step order and persistent mutations
- rollback behavior
- seatbelt policy ordering and deny/allow semantics
- credential delivery and broker lifecycle
- session permission repair
- launch fd isolation
- native-vs-Docker effective policy equivalence

The modularization work adds likely new model surfaces:

- Linux native launch ordering before real Linux enforcement ships.
- Remote launch envelope lifecycle before orchestrated workers accept sessions.
- Host mutation approval state if repair/setup plans move into reusable APIs.
- Cleanup lifecycle if backend artifacts become reusable across local and
  remote runners.

Pure code movement does not require a model update when behavior is unchanged,
but it does require compatibility tests. For verified areas, a refactor should
prove byte-for-byte or structurally equivalent output before and after the move.

## Migration Phases

### Phase 0: Freeze The Direction

Audit this document and decide whether the package layering and type-state
direction are acceptable. Do not move high-risk code until the review has
settled the dependency direction and state vocabulary.

### Phase 1: Strengthen Pure Contracts

Expand the existing `sessioncontract`, `containment`, `pathpolicy`, and
`sessionbackend` packages without changing launch behavior. Add validated path
and request constructors where they can wrap existing behavior. Keep
compatibility shims in `package main`.

Success criteria:

- current CLI behavior unchanged
- package tests cover constructors and validation errors
- JSON shape changes are either absent or versioned
- `go test ./...` passes from the module root

### Phase 2: Extract Planner Inputs

Move integration resolution summaries, harness requirements, credential grant
descriptors, and host mutation descriptions into planner input/output packages.
The planner should still be side-effect-free. Existing session launch code can
consume the plan through adapters.

Success criteria:

- session preview/explain output is structurally equivalent
- credential grants remain redacted in reusable API output
- host mutations are described, not executed, by planner packages
- Docker/native routing behavior is unchanged

### Phase 3: Compile Backend Artifacts

Extract backend compilers that turn `containment.Contract` into concrete
artifacts. macOS SBPL extraction needs the relevant TLA+ guard if semantics or
ordering change. Linux and Docker compilers can start plan-only.

Success criteria:

- darwin native policy output remains equivalent
- Linux compile/package tests stay green
- Docker Sandbox specs fail closed when private-daemon guarantees are absent
- capability gaps are rendered through existing CLI paths

### Phase 4: Split Runners

Move launch side effects into backend runner packages. Runners should accept
only prepared artifacts and return lifecycle handles/results. CLI commands
should no longer own backend-specific execution details.

Success criteria:

- launch fd isolation remains proved and tested
- cleanup handles are explicit
- local smoke tests still launch supported harnesses
- runner APIs have no Cobra/UI dependency

### Phase 5: Add Remote Envelope Support

Introduce remote/orchestrated launch envelopes as a serialization of the same
plan and backend requirements. Start with plan-only validation and worker
admission tests. Do not run remote agents until the envelope lifecycle and
worker admission rules have been modeled and audited.

Success criteria:

- envelope contains no raw secrets
- worker admission rejects unsupported gaps
- worker path mapping cannot expand authority
- cleanup and telemetry records are versioned

### Phase 6: Thin The CLI

After planner and runners are stable, reduce command files to parsing,
request-building, rendering, and compatibility behavior. Shared session
commands should stop copying policy and backend decisions.

Success criteria:

- command tests assert output and compatibility
- package tests assert policy behavior
- fewer globals are needed by session launch paths
- unsupported states fail at request construction or planning time

## What Not To Move First

Do not start with broad moves of:

- `init.go`, setup step order, or rollback mutations
- live credential store internals or broker secret materialization
- seatbelt ordering semantics
- launch helper fd isolation
- native account and sudoers mutation
- desktop app attach probes
- destructive harness lifecycle behavior

Those areas can be modularized later, but the first moves should be pure
contracts, path validation, planner outputs, and backend-neutral artifact
construction.

## Audit Checklist Before Implementation

Before opening implementation beads, answer these questions:

- Does every proposed package have a single responsibility and a clear side
  effect boundary?
- Can callers construct invalid path grants, credential grants, backend
  choices, or lifecycle states through exported fields?
- Are all remote/orchestrated artifacts redaction-safe and versioned?
- Does every backend compile from the same containment contract?
- Are capability gaps explicit instead of hidden fallback behavior?
- Are model-first areas unchanged or covered by updated TLA+ specs?
- Do existing CLI users keep the same command surface and JSON output unless a
  versioned change is approved?
- Is there a small first implementation bead that can prove the direction
  without touching setup, rollback, or live credential delivery?

## Recommended First Implementation Beads

The first implementation beads are intentionally blocked on
`sandboxing-zm9m`, the audit of this direction:

1. `sandboxing-zr7t` - add validated path/request constructors around existing
   `sessioncontract` and `pathpolicy` behavior.
2. `sandboxing-ip8g` - add typed or constructor-backed containment path grant
   variants, with adapters that preserve current JSON.
3. `sandboxing-slu6` - extract a side-effect-free `sessionplanner` facade that
   wraps current plan construction while keeping `package main` launch behavior
   unchanged.
4. `sandboxing-jx71` - add backend artifact variant types for prepared launch
   specs without moving launch execution.
5. `sandboxing-nmqn` - draft the remote launch envelope schema and worker
   admission checklist as a plan-only document before any remote runner
   implementation.

These beads keep the work auditable. They improve type safety and caller
ergonomics before touching the highest-risk containment code.
