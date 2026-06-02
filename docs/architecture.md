# Hazmat Architecture

This document describes Hazmat's current module shape, authority boundaries,
and safety invariants. It is the stable architecture map for readers who want
to understand how a request becomes a contained agent session.

For formal proof scope and governance rules, read
[../tla/VERIFIED.md](../tla/VERIFIED.md). For non-obvious product assumptions,
read [design-assumptions.md](design-assumptions.md). For the historical Phase 1
direction, read
[plans/2026-06-02-modular-architecture-direction.md](plans/2026-06-02-modular-architecture-direction.md).
For the proposed next package split, read
[plans/2026-06-02-package-split-architecture.md](plans/2026-06-02-package-split-architecture.md).

## Current State

Hazmat is a Go CLI plus a growing set of reusable planning packages. The CLI is
still the product entrypoint and still owns many host-side effects: setup,
rollback, harness lifecycle, credential materialization, backup, native launch,
Docker routing, and user-facing rendering.

The modular architecture work moved the central containment decisions into
smaller packages:

- `pathpolicy` validates and canonicalizes path authority.
- `sessioncontract` builds redaction-safe session contract plans.
- `sessionbackend` selects backends, reports capability gaps, and validates
  prepared artifact variants.
- `sessionplanner` composes pure contract and backend plans.
- `containment` owns the backend-neutral authority contract and structural
  credential-deny floor.
- `containment/linux` compiles the same contract into a plan-only Linux launch
  spec.
- `sessionmeta` owns stable mode, network, and launch metadata labels.
- `integrations` owns declarative integration manifests and merge rules.

macOS native containment is executable today. Docker Sandbox mode is executable
for supported private-daemon workflows. Linux native launch specs and remote
launch envelopes are plan-only until their runner/admission models and
implementations land.

## System Context

```mermaid
flowchart LR
    user["Host user"]
    cli["hazmat CLI"]
    agent["agent macOS user"]
    project["Project directory"]
    hostSecrets["Host credentials"]
    backup["Kopia snapshots"]
    pf["pf and DNS hardening"]
    seatbelt["macOS Seatbelt"]
    harness["Agent harness"]
    internet["Network"]

    user --> cli
    cli --> backup
    cli --> pf
    cli --> seatbelt
    cli --> agent
    agent --> harness
    harness --> project
    harness -. denied .-> hostSecrets
    harness --> internet

    style hostSecrets fill:#fee,stroke:#c33,color:#000
    style project fill:#dfd,stroke:#3a3,color:#000
    style agent fill:#ffd,stroke:#a80,color:#000
```

The important product boundary is structural. The agent is not your normal
account with a polite denylist. It runs as the dedicated `agent` user, inside
per-session policy, with explicit project and read-only grants.

## Module Topology

```mermaid
flowchart TB
    subgraph entry["Entry and rendering"]
        cli["package main CLI"]
        explain["explain JSON and text renderers"]
    end

    subgraph pure["Pure planning packages"]
        path["pathpolicy"]
        meta["sessionmeta"]
        contractPlan["sessioncontract"]
        backendPlan["sessionbackend"]
        planner["sessionplanner"]
        integrationPkg["integrations"]
        containment["containment"]
    end

    subgraph compilers["Backend compilers"]
        darwinCompiler["Darwin SBPL compiler"]
        linuxCompiler["containment/linux"]
        dockerCompiler["Docker Sandbox spec builder"]
        remoteEnvelope["Remote envelope plan"]
    end

    subgraph effects["Side-effect owners"]
        setup["setup and rollback"]
        credentials["credential registry and delivery"]
        harnesses["harness registry and lifecycle"]
        backup["backup and restore"]
        nativeRunner["native launch runner"]
        dockerRunner["Docker Sandbox runner"]
    end

    cli --> path
    cli --> integrationPkg
    cli --> planner
    cli --> credentials
    cli --> harnesses
    cli --> setup
    cli --> backup
    planner --> contractPlan
    planner --> backendPlan
    contractPlan --> meta
    backendPlan --> meta
    cli --> containment
    containment --> meta
    containment --> darwinCompiler
    containment --> linuxCompiler
    containment --> dockerCompiler
    backendPlan --> remoteEnvelope
    darwinCompiler --> nativeRunner
    dockerCompiler --> dockerRunner

    style pure fill:#eef,stroke:#55c,color:#000
    style effects fill:#ffe,stroke:#a80,color:#000
```

The dependency rule is one-way. Pure planning packages may describe authority,
but they do not prompt, mutate files, run commands, create sandboxes, or launch
agents. Side-effect owners consume validated plans and are responsible for the
host operations they perform.

## Module Responsibilities

| Module | Owns | Must not own |
| --- | --- | --- |
| `pathpolicy` | Absolute, existing, canonical path types; deny-zone predicates; overlap checks; typed `ProjectRoot`, `ReadOnlyGrant`, and `ReadWriteGrant`. | Session rendering, backend compilation, credential delivery. |
| `sessionmeta` | Mode labels, network mode normalization, launch metadata, network policy metadata. | Security decisions beyond stable metadata rendering. |
| `sessioncontract` | Redaction-safe session contract plan and preview JSON shape. | Host mutation execution, SBPL, Docker calls, process launch. |
| `sessionbackend` | Backend kind selection, capability gaps, lifecycle artifacts, prepared artifact variant validation. | Launch execution and worker admission. |
| `sessionplanner` | Pure composition of contract and backend plan artifacts. | Filesystem inspection, process execution, network calls, prompts. |
| `containment` | Backend-neutral authority contract, path grants, agent home/temp policy, network/process policy, service grants, structural credential floor. | OS-specific syscall behavior or CLI UI. |
| `containment/linux` | Plan-only Linux launch spec compilation from a validated containment contract. | Creating namespaces, mounts, helpers, or seccomp/Landlock rules at runtime. |
| `integrations` | Manifest schema, merge rules, safe env passthrough rejection, read-dir validation, warnings. | Adding write scope, credential env, unsafe paths, or caller-specific callbacks. |
| `package main` | CLI parsing, compatibility behavior, host setup, rollback, harness lifecycle, credentials, backup, launch, rendering. | Long-term ownership of policy semantics that reusable packages can enforce. |

## User Flow

```mermaid
flowchart TD
    start["User runs hazmat harness command"]
    initCheck{"Init complete?"}
    init["Run hazmat init or fail with repair guidance"]
    inspect["Resolve project, config, harness, integrations"]
    explain{"Preview only?"}
    preview["Render contract and backend plan"]
    snapshot["Take pre-session snapshot"]
    mutations["Apply approved host repairs"]
    backend{"Backend mode"}
    native["Native macOS launch"]
    docker["Docker Sandbox launch"]
    unsupported["Fail closed with capability gap"]
    result["Session result, cleanup, telemetry"]

    start --> initCheck
    initCheck -- no --> init
    initCheck -- yes --> inspect
    inspect --> explain
    explain -- yes --> preview
    explain -- no --> snapshot
    snapshot --> mutations
    mutations --> backend
    backend -- darwin-native --> native
    backend -- docker-sandbox --> docker
    backend -- linux or remote plan-only --> unsupported
    native --> result
    docker --> result
    unsupported --> result
```

The preview path and launch path share the same planning core where possible.
The preview path is read-only for planned host mutations. The launch path is the
only path that may apply repairs, materialize credentials, create launch
artifacts, or start a harness.

## Session Data Flow

```mermaid
flowchart LR
    raw["CLI flags and project config"]
    resolved["Resolved host facts"]
    paths["Validated path grants"]
    integrations["Merged integrations"]
    credentialDesc["Credential descriptors"]
    floor["Credential deny floor"]
    contractPlan["sessioncontract.Plan"]
    backendPlan["sessionbackend.Plan"]
    containment["containment.Contract"]
    artifact["Prepared backend artifact"]
    runner["Runner side effects"]
    harness["Harness process"]

    raw --> resolved
    resolved --> paths
    resolved --> integrations
    resolved --> credentialDesc
    resolved --> floor
    paths --> contractPlan
    integrations --> contractPlan
    credentialDesc --> contractPlan
    contractPlan --> backendPlan
    paths --> containment
    floor --> containment
    containment --> artifact
    backendPlan --> artifact
    artifact --> runner
    runner --> harness

    style paths fill:#dfd,stroke:#3a3,color:#000
    style containment fill:#dfd,stroke:#3a3,color:#000
    style credentialDesc fill:#fee,stroke:#c33,color:#000
    style floor fill:#fee,stroke:#c33,color:#000
```

Redaction-safe descriptors can appear in plans and JSON output. Raw secret
bytes stay behind the credential registry and delivery code. Backend compilers
receive a validated containment contract with a structural credential-deny
floor, not raw CLI strings.

## Authority Pipeline

```mermaid
flowchart TD
    input["Raw path input"]
    absolute["AbsolutePath"]
    existing["ExistingDir"]
    canonical["CanonicalDir"]
    deny{"Deny zone overlap?"}
    grant["Typed path grant"]
    contract["containment.Contract"]
    compiler["Backend compiler"]
    launch["Launch artifact"]

    input --> absolute
    absolute --> existing
    existing --> canonical
    canonical --> deny
    deny -- yes --> reject["Reject before planning"]
    deny -- no --> grant
    grant --> contract
    contract --> compiler
    compiler --> launch

    style reject fill:#fee,stroke:#c33,color:#000
```

The path constructors are the first authority boundary. A project/read/write
grant cannot be constructed for a credential or host-state deny zone. The
containment contract is the second boundary: it rejects empty credential floors,
mismatched floors, invalid project access, and path grants that overlap
credential deny paths.

## Backend Compilation Flow

```mermaid
flowchart LR
    plan["sessionbackend.Plan"]
    contract["containment.Contract"]
    gaps{"Capability gaps?"}
    accept{"All gaps explicitly accepted?"}
    variant["Exactly one artifact variant"]
    prepared["PreparedLaunch"]
    darwin["DarwinSeatbelt"]
    linux["LinuxLaunchSpec plan-only"]
    docker["DockerSandboxSpec"]
    remote["RemoteEnvelope plan-only"]

    plan --> gaps
    gaps -- none --> variant
    gaps -- present --> accept
    accept -- no --> reject["Fail closed"]
    accept -- yes --> variant
    contract --> variant
    variant --> darwin
    variant --> linux
    variant --> docker
    variant --> remote
    darwin --> prepared
    linux --> prepared
    docker --> prepared
    remote --> prepared

    style reject fill:#fee,stroke:#c33,color:#000
    style linux fill:#ffe,stroke:#a80,color:#000
    style remote fill:#ffe,stroke:#a80,color:#000
```

`sessionbackend.NewPreparedLaunch` enforces that a prepared launch carries
exactly one artifact variant and that every capability gap is deliberately
accepted. Future runners must construct prepared launches through that
constructor instead of hand-assembling the public JSON fields.

## Backend Status

| Backend | Status | Artifact | Execution owner | Current fail-closed condition |
| --- | --- | --- | --- | --- |
| `darwin-native` | Executable | SBPL policy plus `hazmat-launch` | Native runner in `package main` and `cmd/hazmat-launch` | Invalid containment contract or launch helper fd precondition. |
| `docker-sandbox` | Executable for supported private-daemon workflows | Docker Sandbox spec/profile | Docker Sandbox runner in `package main` | Unsupported shared-daemon workflows and integration env passthrough gaps. |
| `linux-native` | Plan-only | Linux launch spec | None yet | `native-launch` capability gap. |
| `remote-envelope` | Plan-only | Remote envelope placeholder | None yet | `remote-launch` capability gap and no worker admission implementation. |
| `unsupported-native` | Not executable | None | None | `native-launch` capability gap. |

## Credential Flow

```mermaid
flowchart TD
    hostStore["Host-owned secret store"]
    registry["Credential registry"]
    request["Session capability request"]
    descriptor["Redaction-safe descriptor"]
    delivery{"Delivery mode"}
    env["Session env grant"]
    broker["Brokered helper"]
    file["Temporary agent materialization"]
    external["External adapter reference"]
    cleanup["Cleanup and crash recovery"]
    plan["sessioncontract.Plan"]

    hostStore --> registry
    request --> registry
    registry --> descriptor
    descriptor --> plan
    registry --> delivery
    delivery --> env
    delivery --> broker
    delivery --> file
    delivery --> external
    env --> cleanup
    broker --> cleanup
    file --> cleanup
    external --> cleanup

    style hostStore fill:#fee,stroke:#c33,color:#000
    style descriptor fill:#dfd,stroke:#3a3,color:#000
```

Plans may contain credential IDs, env var names, source labels, and redaction
flags. They must not contain raw provider keys, file contents, broker socket
paths, or host secret-store paths. Remote envelopes must remain
credential-free until a remote credential-handle model is proved.

## Invariant Ownership

| Invariant | Owner | Gate |
| --- | --- | --- |
| Project/read/write paths cannot overlap credential or host-state deny zones. | `pathpolicy` constructors and `resolveSessionConfig` compatibility shims. | `pathpolicy` tests, session config tests, `MC_TierPolicyEquivalence`, `MC_Tier3LaunchContainment`. |
| Credential-deny floor is non-omittable. | `containment.NewCredentialFloor`, `containment.NewContract`, backend compiler validation. | `containment` tests, golden SBPL tests, `MC_SeatbeltPolicy`. |
| SBPL credential denies remain the final broad credential boundary. | Darwin SBPL compiler and native session policy construction. | Golden SBPL fixtures, `MC_SeatbeltPolicy`. |
| Backend exact identity is not claimed beyond the comparable core. | `sessionbackend`, Docker planning, native planning. | `MC_TierPolicyEquivalence`. |
| Capability gaps are explicit and accepted before prepared launch. | `sessionbackend.NewPreparedLaunch`. | `sessionbackend` tests and backend plan goldens. |
| Integration env passthrough cannot carry credential-shaped keys. | `integrations` package and registry-backed credential descriptors. | Integration tests and credential-regression hook. |
| Preview is read-only for host mutations. | Session mutation planning and explain path. | `MC_SessionPermissionRepairs`, explain goldens. |
| Launch helper starts sandboxed code without inherited credential-bearing fds. | `cmd/hazmat-launch`. | `MC_LaunchFDIsolation`, launch helper tests. |
| Harness lifecycle rollback removes Hazmat-owned metadata and preserves user state where documented. | Harness registry and lifecycle code. | `MC_HarnessLifecycle`, harness tests. |
| Remote envelopes are not executable authority. | `sessionbackend` gap reporting and remote-envelope plan docs. | `GapRemoteLaunch`, remote plan doc, future remote model before code. |

## Verification Flow

```mermaid
flowchart TD
    change["Code or docs change"]
    governed{"Touches verified area?"}
    model{"Semantic change?"}
    updateModel["Update TLA+ model and design note first"]
    runTLC["Run affected TLC specs"]
    tests["Run Go tests and goldens"]
    record["Update VERIFIED.md when proof scope changes"]
    commit["Commit and push"]

    change --> governed
    governed -- no --> tests
    governed -- yes --> model
    model -- yes --> updateModel
    model -- no --> runTLC
    updateModel --> runTLC
    runTLC --> tests
    tests --> record
    record --> commit
```

Pure refactors in governed areas still need TLC confirmation. Semantic changes
must update the model first. Goldens prove byte-level output equivalence; TLC
proves the abstract invariants still hold inside the modeled scope. They answer
different questions.

## Remote And Orchestrated Flow

```mermaid
flowchart LR
    control["Control plane"]
    request["Validated request"]
    plan["sessionplanner.Plan"]
    envelope["RemoteEnvelope DTO"]
    validate["ParseAndValidate"]
    admission["Worker admission"]
    compiler["Worker backend compiler"]
    runner["Worker runner"]
    result["Result, telemetry, cleanup proof"]

    control --> request
    request --> plan
    plan --> envelope
    envelope --> validate
    validate --> admission
    admission --> compiler
    compiler --> runner
    runner --> result

    envelope -. current state .-> inert["Plan-only and gap-gated"]
    style inert fill:#ffe,stroke:#a80,color:#000
```

This flow is intentionally not executable today. Any remote runner must first
add a model for schema versioning, canonical serialization, integrity, replay
defense, worker identity, path mapping, capability gaps, cleanup, telemetry,
and any credential handles. Until then, `RemoteEnvelope` is a typed placeholder
and `KindRemoteEnvelope` reports a `remote-launch` capability gap.

## Lifecycle Sequence

```mermaid
sequenceDiagram
    actor User
    participant CLI as hazmat CLI
    participant Planner as sessionplanner
    participant Backup as Backup
    participant Credentials as Credential delivery
    participant Backend as Backend compiler
    participant Launch as Runner
    participant Agent as Agent harness

    User->>CLI: hazmat claude
    CLI->>Planner: build pure contract/backend plans
    Planner-->>CLI: redaction-safe plans and gaps
    CLI->>Backup: pre-session snapshot
    CLI->>Credentials: materialize scoped grants
    CLI->>Backend: compile validated contract
    Backend-->>CLI: launch artifact
    CLI->>Launch: start contained session
    Launch->>Agent: exec harness as agent user
    Agent-->>Launch: exit status
    Launch-->>CLI: result
    CLI->>Credentials: cleanup or harvest
    CLI-->>User: status, diff, restore options
```

If preview mode is selected, the sequence stops after the planner and renderer.
No snapshot, credential materialization, host mutation, backend artifact
creation, or launch occurs.

## Reading Map

| Need | Read |
| --- | --- |
| User-facing tier choice | [overview.md](overview.md) |
| Daily commands | [usage.md](usage.md) |
| Harness support | [harnesses.md](harnesses.md) |
| Integration rules | [integrations.md](integrations.md) |
| Docker private-daemon boundary | [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) |
| Threat and CVE mapping | [threat-matrix.md](threat-matrix.md), [cve-audit.md](cve-audit.md) |
| Design assumptions and remote-worker scope | [design-assumptions.md](design-assumptions.md) |
| Formal verification scope | [../tla/VERIFIED.md](../tla/VERIFIED.md) |
