# Modular Hazmat Architecture Direction

**Date:** 2026-06-02
**Authoring bead:** `sandboxing-kieu`
**Audit bead:** `sandboxing-zm9m`
**Status:** Phase-0 architecture direction, not implementation approval
**Related plans:**
[reusable library decomposition](2026-05-28-reusable-library-decomposition.md),
[Linux-ready backend architecture](2026-05-28-linux-ready-backend-architecture.md),
[Docker Tier 3 audit design](2026-04-01-docker-tier3-audit-design.md)

This document refines the package naming and sequencing in the related plans.
When names conflict, use this document's dependency direction until an
implementation bead settles the exact package path. When governance language
conflicts, `tla/VERIFIED.md` is authoritative.

## Purpose

Hazmat should split into modular packages that can be reused by local CLIs,
future platform-specific binaries, and remote/orchestrated agent runtimes
without importing command wiring or weakening containment guarantees. The split
should also make invalid states hard or impossible to construct. Callers should
be guided toward legal requests, legal policy plans, legal backend choices, and
legal lifecycle transitions by the package APIs themselves.

This document is for audit before implementation. It proposes package
boundaries, type-state direction, data flow, trust boundaries, and migration
gates. It does not approve broad code movement, behavior changes, remote
execution, or new trust-model assumptions.

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
The launch path and the explain/preview path are both in scope: today they do
not flow through one shared `sessioncontract.Plan`, so the planner facade must
preserve both surfaces rather than only wrapping preview JSON.

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
| Backend compilers | `containment/{darwin,linux,docker}` | Compile containment contract into backend launch artifacts. |
| Backend runners | `backends/{darwin,linux,docker,remote}` | Prepare artifacts, launch, monitor, cleanup. |
| Host setup | `setup/{darwin,linux}` | Persistent host resources after model approval. |
| Host execution | `hostexec` | Narrow command runner interfaces and platform implementations. |
| CLI | `package main` | Parse flags, call packages, render output, preserve compatibility. |

The exact names can change during implementation, but the dependency direction
should not. Higher layers may depend on lower data packages. Lower packages
must not import CLI commands, UI rendering, or concrete launch runners.
Planner, contract, path, integration, schema, and remote-envelope packages must
stay pure Go and cross-platform; importing darwin/cgo-only code into those
layers would make remote control-plane reuse inherit local native-runner
constraints.

## Non-Omittable Security Floor

Reusable contracts need a structural security floor before callers outside
`package main` can assemble them. The most important invariant is credential
denial: every executable containment contract must include the credential deny
subpaths from `pathpolicy.CredentialDenySubpaths()` for the relevant base homes,
and every backend compiler must fail closed when that floor is absent.

That rule cannot be optional builder convenience. A future
`containment.NewContract` or equivalent constructor should inject and validate
the deny floor, keep the authority-bearing deny field unexported or otherwise
non-omittable, and expose read-only rendered deny metadata for JSON and audit.
The darwin compiler, Linux compiler, Docker compiler, and remote worker
admission logic should independently assert the floor before producing a
`PreparedLaunch`.

The existing `resolveSessionConfig` deny-zone input rejection is also part of
the floor. A validated request constructor must reject project/read/write roots
that are credential deny zones or parents of deny zones before planner output is
treated as authority. Moving this logic out of `package main` is governed by
the same model discipline as the current Tier 2/Tier 3 policy equivalence: it
must be non-bypassable, not a UI warning.

This invariant belongs under the Seatbelt policy model and the backend
equivalence model. Any change to the floor, the deny subpath list, or compiler
assertion ordering must update or re-run the relevant specs named in
`tla/VERIFIED.md`.

## Multi-OS Direction

Multi-OS support should not become scattered `runtime.GOOS` branching in the
planner. The planner should produce one backend-neutral plan. Backend selection
then records either a concrete backend or explicit capability gaps.

macOS native, Linux native, Docker Sandbox, and future remote workers should
all compile the same core containment contract over the canonical comparable
subset. Their implementation mechanisms will differ, and exact backend identity
is not a design goal:

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

The worker must authenticate, verify, and re-validate the envelope before
launch. It should prove that the requested backend is available, that path
grants map to the intended worker workspace, that credential handles are scoped
to the session, that the network policy can be enforced, that the nonce/session
ID has not been replayed for that worker identity, and that the envelope has not
expired under the accepted clock-skew policy. A remote worker must not accept
raw CLI flags or unauthenticated JSON as authority.

Remote orchestration also needs a clear state boundary. A durable control plane
may keep session records and redacted plan summaries. Workers should keep
per-session runtime state and cleanup artifacts. Agent-created durable state
should be opt-in and classified by harness; it should not accidentally inherit
the local `/Users/agent` persistence model.

Remote work is a new threat model for Hazmat, not just a new backend. Before
any remote runner implementation, `sandboxing-nmqn` must define at least:

- envelope signing or MAC strategy and worker-side verification order
- control-plane authentication and worker identity binding
- replay defense bound to worker identity, session ID, nonce, and expiry
- clock-skew handling
- worker readiness or attestation evidence sufficient for admission
- whether v1 remote sessions are credential-free, external-reference only, or
  backed by a new credential-broker protocol
- how host-owned secret-store crash recovery and credential capability
  lifecycle guarantees map to worker state, or why they are declared capability
  gaps
- required updates to `docs/design-assumptions.md`, `docs/cve-audit.md`, and
  any affected threat-model docs before remote execution is claimed

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
- Credential-deny floors should not be caller-supplied slices. They should be
  derived by validated constructors and checked by backend compilers.
- `PreparedLaunch` should carry exactly one backend artifact variant:
  `DarwinSeatbelt`, `LinuxLaunchSpec`, `DockerSandboxSpec`, or
  `RemoteEnvelope`.
- `PreparedLaunch` should be constructible only when capability gaps are empty
  or are represented by a typed `AcceptedGap` that the caller deliberately
  accepted for a non-executing or experimental path.
- `CleanupHandle` should exist only after a backend prepares an artifact that
  needs cleanup.

This does not mean every package needs heavy generics. Plain Go types with
unexported fields and small constructors are enough for most of the invariant.
The important rule is that exported structs should not let external callers
assemble known-bad authority states by filling public fields directly.

## Wire Types Are Not Authority

Serialization needs a separate category from validated authority types. Go
`json.Unmarshal` can fill exported DTO fields without running constructors, and
unexported fields do not round-trip through JSON. Therefore any reusable
wire-facing package should use this pattern:

```text
ExportedDTO
  -> ParseAndValidate(...)
  -> ValidatedType with constructors and unexported authority fields
```

DTOs are never authority. A remote envelope, saved plan, or schema fixture
becomes authority only after parse/validation re-runs the same invariants as
local constructors: path normalization, credential-deny floor, deny-zone input
rejection, backend support checks, schema version checks, capability-gap
acceptance, credential-handle scope checks, and cleanup requirements.

`sandboxing-nmqn` owns the remote-envelope DTO and validation plan. Until that
plan is settled, the `RemoteEnvelope` variant in `sandboxing-jx71` should be
treated as experimental and non-executable.

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

## Schema Versioning

Every artifact that crosses a package, process, machine, or durable-record
boundary needs an explicit schema version and a producer/consumer skew rule.
`sessioncontract.Plan`, `sessionmeta.LaunchMetadata`, and Linux launch specs
already carry versions; the future stable `containment.Contract`,
`sessionbackend.Plan`, prepared backend artifacts, remote envelopes, telemetry
records, and cleanup proofs should do the same.

The default skew rule should be fail closed: consumers accept only the versions
they know how to validate unless a migration adapter is explicitly implemented
and tested. Version fields are not cosmetic. They determine whether a DTO can be
parsed into a validated authority type.

## Telemetry And Record Classification

"Redaction-safe" must mean more than "no raw secret bytes." Session records,
telemetry, and cleanup proofs can reveal host paths, project topology,
credential grant names, env var names, harness state paths, and service access.
Reusable APIs should classify output fields before exposing them:

- public diagnostic data safe for normal CLI output
- operator-private data safe for local logs and support bundles
- control-plane-private data safe for remote scheduling records
- secret or secret-adjacent data that must be redacted, hashed, or omitted

Remote/orchestrated records should default to the stricter classification. Raw
secrets, credential file contents, token-like env values, broker socket paths,
and unredacted credential store paths must not appear in telemetry or cleanup
proofs.

## Error And Gap Taxonomy

Reusable packages should return typed errors and typed capability gaps rather
than free-form strings for decisions that affect launch. At minimum, separate:

- validation rejection: the request is invalid and must not be planned
- policy rejection: the requested authority violates Hazmat's security floor
- capability gap: the request is valid but this backend cannot enforce it
- internal failure: Hazmat could not inspect, compile, prepare, or clean up
- user approval pending: a host mutation or integration approval is not yet
  authorized
- experimental gap accepted: a non-executing or explicitly experimental path
  records a gap without pretending it was enforced

Only validation-success plus zero executable capability gaps should reach a
normal `PreparedLaunch`. This mirrors the Linux native launch model's
"no exec with missing required feature" rule and should become package API
shape, not just CLI prose.

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
| Integration manifest author | Repo-specific hints and declared tool needs | Adding write scope, credential env, or unsafe paths |
| Integration approval record | Host consent for a path+hash manifest snapshot | Security validation or policy bypass |
| Harness registry | Built-in harness metadata and lifecycle boundaries | Arbitrary third-party harness trust |

Every boundary crossing should use a typed, versioned artifact. For local CLI
use, that artifact can stay in memory. For remote/orchestrated use, it should
be serializable, auditable, and revalidated at the worker.

## TLA+ And Audit Gates

`tla/VERIFIED.md` is the authoritative governance table. Do not maintain a
parallel hand-written list in this document. If a modularization bead touches a
governed function, modeled ordering, modeled field, or modeled invariant, the
bead must follow the change rules in `tla/VERIFIED.md`: update the model first
when semantics change, run TLC, and record "No error has been found" before
implementation.

Pure code movement does not require a model update when behavior is unchanged,
but it does require compatibility tests. For verified areas, a refactor should
prove byte-for-byte or exact structural equivalence where the model depends on
ordering or presence. "Structurally equivalent" is not enough for SBPL section
ordering if the relevant spec requires exact order.

Phase-to-spec map for the current modularization direction:

| Phase or change | TLA+ governance |
| --- | --- |
| Phase 1 validated requests and contract floor | Spec 2 `MC_SeatbeltPolicy` for credential denies; Spec 6 `MC_TierPolicyEquivalence` for deny-zone input rejection and backend equivalence. |
| Phase 2 planner extraction | Spec 6 for `resolveSessionConfig` behavior and both Tier 2/Tier 3 planning; Spec 7 for host mutation preview-vs-launch if mutation planning moves; Specs 12/13 if credential descriptors or delivery semantics move. |
| Phase 3 backend artifact compilers | Spec 2 for darwin SBPL; Spec 5 for Docker Tier 3 launch; Spec 6 for comparable core contract; Spec 14 for Linux launch specs. |
| Phase 4 backend runners | Spec 9 `MC_LaunchFDIsolation`; Spec 5 for Docker policy-before-launch; Spec 14 for Linux helper ordering if execution is introduced. |
| Phase 5 remote envelope | New model required before execution; also map to Specs 12/13 if credential handles cross machine boundaries and to cleanup lifecycle rules if worker artifacts persist. |
| Phase 6 CLI thinning | Re-run the specs governing any moved behavior, especially Specs 2, 6, 8, 12, and 13 when session, harness, or credential surfaces are touched. |
| Setup, rollback, migration, backup, Git SSH, hooks | Specs 1, 3, 4, 10, and 11 apply whenever those subsystems become part of package moves, even if this document does not name a dedicated phase for them. |

The Linux native ordering model already exists as Spec 14. Remote launch
envelope lifecycle, worker admission, and cross-host cleanup do not yet have an
approved model and must be treated as new verified surfaces before remote
execution.

## Golden Equivalence Baseline

Before implementation beads move behavior out of `package main`, capture and
commit golden or exact structural baselines for the outputs whose equivalence
will be claimed. At minimum:

- darwin SBPL output for representative sessions, including section order,
  credential denies, network-none, session temp, read-parent/project-write
  reassertion, and Claude keychain exception
- `hazmat explain --json` and session preview shape for representative native
  and Docker-routed sessions
- backend capability-gap output for supported, unsupported, Docker, and Linux
  plan-only backends
- integration env/read-dir rejection and warning output
- launch metadata JSON

Substring tests are not enough for SBPL refactors because ordering is part of
the modeled behavior. The golden baseline should be captured before the move,
then kept green through each extraction.

## Documentation Obligations

Implementation beads must update docs when the modularization changes the
reader's trust model or user-visible behavior:

- `tla/VERIFIED.md` and the relevant spec design note when modeled behavior or
  governed code ownership changes
- `docs/design-assumptions.md` when remote execution, worker state, or
  multi-party trust changes Hazmat's current local-machine assumptions
- `docs/cve-audit.md` and threat docs when a new backend or remote plane
  changes attack surfaces
- `README.md`, `docs/overview.md`, and user-facing docs when command behavior,
  JSON output, backend support, or setup requirements change
- the older architecture plans when a new package name supersedes the names
  used there, or this document when it remains the controlling direction

## Invariant Ownership And CI

Invariants introduced here need owners and tests, not just constructors. Each
implementation bead should name the package that owns the invariant, the tests
that enforce it, and any CI/static check that prevents bypass. Examples:

- credential-deny floor owner: `containment` plus backend compilers
- deny-zone input rejection owner: validated request/path policy package
- safe integration env owner: `integrations`, not caller-injected callbacks
- zero-gap prepared launch owner: backend artifact package
- remote DTO validation owner: remote envelope package

The integration package should be the model for self-enforcing validation:
unsafe env keys and unsafe read dirs should be rejected by reusable integration
logic itself, not only by callbacks supplied from `package main`.

## Migration Phases

### Phase 0: Freeze The Direction

Audit this document and decide whether the package layering and type-state
direction are acceptable. Do not move high-risk code until the review has
settled the dependency direction and state vocabulary.

Phase 0 exits only when this document names:

- the non-omittable credential-deny floor and deny-zone request rejection
- the DTO-to-validated-type rule for every serialized authority artifact
- remote envelope integrity, replay, worker identity, and threat-model
  obligations
- schema versioning and producer/consumer skew rules
- telemetry and record data classification
- typed error and capability-gap taxonomy
- TLA+ phase mapping through `tla/VERIFIED.md`
- golden baseline requirements for claimed equivalence
- documentation obligations and invariant ownership/CI

### Phase 1: Strengthen Pure Contracts

Expand the existing `sessioncontract`, `containment`, `pathpolicy`, and
`sessionbackend` packages without changing launch behavior. Add validated path
and request constructors where they can wrap existing behavior. Keep
compatibility shims in `package main`.

Success criteria:

- current CLI behavior unchanged
- package tests cover constructors and validation errors
- credential-deny floors and deny-zone input rejection are non-bypassable
- JSON shape changes are either absent or versioned
- `go test ./...` passes from the module root

### Phase 2: Extract Planner Inputs

Move integration resolution summaries, harness requirements, credential grant
descriptors, and host mutation descriptions into planner input/output packages.
The planner should still be side-effect-free. Existing session launch code can
consume the plan through adapters.

The facade must reproduce both current surfaces: launch-time policy/session
preparation and explain/preview JSON. Wrapping only
`sessioncontract.BuildPlan` is insufficient because current launch preparation
compiles native policy from `sessionConfig` directly.

Success criteria:

- session preview/explain output is structurally equivalent
- credential grants remain redacted in reusable API output
- host mutations are described, not executed, by planner packages
- Docker/native routing behavior is unchanged
- self-enforcing integration validation rejects unsafe env and unsafe read dirs
  without relying on caller-supplied callbacks

### Phase 3: Compile Backend Artifacts

Extract backend compilers that turn `containment.Contract` into concrete
artifacts. macOS SBPL extraction needs the relevant TLA+ guard if semantics or
ordering change. Linux and Docker compilers can start plan-only.

Success criteria:

- darwin native policy output remains equivalent
- golden baselines cover SBPL section order and representative plan output
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
- envelope authenticity, integrity, replay defense, and worker identity binding
  are specified and tested
- worker admission rejects unsupported gaps
- worker path mapping cannot expand authority
- cleanup and telemetry records are versioned
- remote threat-model docs are updated before execution support is claimed

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
- Is the credential-deny floor structural, compiler-checked, and model-mapped?
- Can callers construct invalid path grants, credential grants, backend
  choices, or lifecycle states through exported fields?
- Are wire DTOs separated from validated authority types?
- Are all remote/orchestrated artifacts authenticated, redaction-safe,
  versioned, and replay-resistant?
- Does every backend compile from the same core containment contract over the
  canonical comparable subset?
- Are capability gaps explicit instead of hidden fallback behavior?
- Are model-first areas unchanged or covered by updated TLA+ specs?
- Are golden baselines in place before equivalence is claimed?
- Are telemetry and records classified beyond "no raw secrets"?
- Are errors and capability gaps typed enough for fail-closed decisions?
- Are documentation obligations identified for trust-model changes?
- Is every invariant assigned to a package-level owner and a CI/test gate?
- Do existing CLI users keep the same command surface and JSON output unless a
  versioned change is approved?
- Is there a small first implementation bead that can prove the direction
  without touching setup, rollback, or live credential delivery?

## Recommended First Implementation Beads

The first implementation beads were intentionally created behind
`sandboxing-zm9m`; do not claim them until that audit bead is closed:

1. `sandboxing-zr7t` - add validated path/request constructors around existing
   `sessioncontract` and `pathpolicy` behavior, including non-bypassable
   deny-zone input rejection.
2. `sandboxing-ip8g` - add typed or constructor-backed containment path grant
   variants, with adapters that preserve current JSON and a structural
   credential-deny floor.
3. `sandboxing-slu6` - extract a side-effect-free `sessionplanner` facade that
   reproduces both launch-time planning and explain/preview planning while
   keeping `package main` launch behavior unchanged.
4. `sandboxing-jx71` - add backend artifact variant types for prepared launch
   specs without moving launch execution; keep `RemoteEnvelope` experimental
   until `sandboxing-nmqn` settles DTO validation and integrity.
5. `sandboxing-nmqn` - draft the remote launch envelope schema and worker
   admission checklist as a plan-only document before any remote runner
   implementation, including integrity, replay, worker identity, threat-model,
   credential lifecycle, cleanup, and telemetry obligations.

These beads keep the work auditable. They improve type safety and caller
ergonomics before touching the highest-risk containment code.
