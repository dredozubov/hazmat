# Hazmat Reusable Library Decomposition Plan

**Date:** 2026-05-28
**Bead:** `sandboxing-lh7j`
**Linux backend companion:** [2026-05-28-linux-ready-backend-architecture.md](2026-05-28-linux-ready-backend-architecture.md)
**Goal:** split Hazmat into composable Go packages that can be reused by other
tools without importing CLI wiring or weakening verified containment behavior.

## Direction

Hazmat should stay a product and CLI, but its core decisions should become
library-shaped. The CLI should eventually assemble request structs, call
planner/backend libraries, then render human output. Reusable packages should
own pure data contracts, policy planning, integration resolution, credential
grant descriptions, and backend launch adapters.

Do not start by moving everything. Start with small pure boundaries that have
stable JSON or policy contracts and low side-effect risk. Setup/init, rollback,
seatbelt policy semantics, credential delivery, launch fd isolation, and
session permission repair are verified areas; changing behavior there still
starts with the relevant TLA+ model and design note.

## Package Boundary Rules

Reusable packages should follow these rules:

- No Cobra commands, global CLI flags, terminal color, status-bar rendering, or
  prompts.
- No hidden dependency on process-wide globals such as `flagDryRun`,
  `flagVerbose`, `agentUID`, `sharedGID`, or host `$HOME`.
- Prefer request/result structs over reading environment variables directly.
- Keep pure policy construction separate from backend execution.
- Put filesystem/process/network side effects behind narrow interfaces.
- Keep platform-specific code in backend packages, not in shared contract
  packages.
- Preserve current machine-readable JSON shapes unless a versioned schema
  change is intentional.
- Do not export raw secrets from library APIs; export credential grant metadata
  and broker handles only.

## Proposed Package Map

| Package | Status | Responsibility | Depends on | Must not depend on |
| --- | --- | --- | --- | --- |
| `sessionmeta` | First proof slice | Network mode parsing, runtime mode labels, launch metadata JSON | Standard library | CLI, setup, credentials, filesystem |
| `sessioncontract` | Planned | Backend-neutral session request/plan: project, read/write dirs, network, integrations, credential grants, service access, metadata flags | `sessionmeta` | Cobra, SBPL rendering, Docker CLI |
| `pathpolicy` | Planned | Path normalization, path-set operations, read/write overlap checks, credential deny-zone matching | Standard library | CLI, seatbelt syntax, host mutation |
| `integrations` | Planned | Manifest parsing, marker detection, env passthrough planning, snapshot excludes, warnings | `pathpolicy` | Session launch, credential brokers |
| `credentials` | Planned/internal first | Credential registry types, grant descriptors, redaction-safe metadata, broker request contracts | `pathpolicy` | CLI output, agent process execution |
| `seatbelt` | Planned with TLA guard | macOS SBPL rendering from a backend-neutral policy plan | `sessioncontract`, `pathpolicy` | Cobra, Docker, bootstrap |
| `backends/native` | Planned/internal | Native macOS launch preparation, SBPL artifact lifecycle, env materialization | `sessioncontract`, `seatbelt`, `credentials` | CLI parsing |
| `backends/docker` | Planned/internal | Docker Sandbox routing, lifecycle, network profile, metadata, teardown | `sessioncontract` | Native seatbelt internals |
| `platform/linux` | Planned | Linux distro/kernel feature inspection for plan-only and future native launch support | Standard library | setup mutation, launch execution |
| `setup/linux` | Planned with TLA guard | Linux agent user, helper, sudoers, cgroup, and tool-home resources after model approval | `platform/linux`, hostexec | session planning |
| `harnesses` | Planned | Agent harness descriptors and launch argument shaping for Claude, Codex, Gemini, OpenCode | `sessioncontract` | Backend execution |
| `hostexec` | Planned/internal | Host command execution interface and concrete sudo/helper runners | Standard library | Session policy construction |

The table is a target map, not an instruction to move all code immediately.
Each package should land only when there is a small bead, focused tests, and a
compatibility shim in `package main`.

## Public API Shape

The reusable API should converge toward this shape:

```go
type Request struct {
    Target       string
    ProjectDir   string
    ReadDirs     []string
    WriteDirs    []string
    NetworkMode  sessionmeta.NetworkMode
    Integrations []string
    Credentials  []CredentialGrantRequest
    Harness      HarnessID
}

type Plan struct {
    Contract       Contract
    Backend        BackendKind
    Metadata       sessionmeta.LaunchMetadata
    HostMutations  []HostMutation
    Warnings       []string
}

type Backend interface {
    Prepare(context.Context, Plan) (Prepared, error)
    Run(context.Context, Prepared, Command) error
}
```

This is not intended as the first commit. It is the shape that should emerge
after pure metadata, path policy, integration resolution, and backend-neutral
contract extraction are in place.

## Migration Order

### Phase 1: Pure contracts

Extract data contracts and JSON metadata that do not touch the host. The first
safe slice is `sessionmeta`: it moves network mode parsing and launch metadata
construction behind a reusable package while `package main` keeps compatibility
wrappers.

Next, extract `sessioncontract` for request/plan structs. That package should
not render SBPL, launch processes, mutate host state, or know about Cobra.

### Phase 2: Path and integration planning

Extract path normalization and containment checks into `pathpolicy`, then move
integration manifest parsing/resolution into `integrations`. The existing tests
around integration manifests, snapshot excludes, read-only dirs, and deny zones
should move with the code or gain package-level equivalents.

This phase is high value because integrations are useful outside the CLI:
local tools, automation services, and future frontends need the same "what
does this repo need?" answer without launching an agent.

### Phase 3: Backend-neutral session planner

Move `prepareLaunchSession` decision logic toward a planner that returns a
side-effect-free plan plus an explicit host mutation plan. The current CLI can
then render the session contract and ask the selected backend to execute.

This phase must keep the current native-vs-Docker routing behavior unchanged.
Any change to path-policy semantics, network-none behavior, credential grants,
or host mutation ordering requires the same model-first discipline as direct
implementation work.

### Phase 4: Backend adapters

Split native macOS execution and Docker Sandbox execution into separate
backend packages. Backend packages can have side effects, but they should
receive explicit prepared plans and return explicit artifacts/cleanup handles.

The native backend owns SBPL artifact preparation and launch helper invocation.
The Docker backend owns sandbox lifecycle, network profile application, and
teardown verification. Neither backend should parse CLI flags.

### Phase 5: CLI thinning

Once the planner and backend interfaces are stable, command files should become
thin adapters:

1. parse flags
2. build a request
3. call planner
4. render human output or JSON
5. run selected backend

This is where duplicate flag handling across `shell`, `exec`, `claude`,
`codex`, `gemini`, and `opencode` should collapse into shared request-building
helpers.

## TLA+ Governance Boundaries

These areas remain model-first:

- setup/init step order and persistent mutations
- rollback behavior and preserved mutations
- seatbelt policy ordering and deny/allow semantics
- credential delivery and broker semantics
- session permission repair
- launch fd isolation
- native-vs-Docker core containment equivalence

Refactoring that only moves code without changing modeled behavior does not
need a model change, but it should add regression tests that prove the emitted
policy, JSON, or mutation order is byte-for-byte or structurally equivalent.

## Testing Strategy

Each package extraction should include:

- package-level unit tests for the extracted logic
- compatibility tests through existing CLI-facing functions
- golden or structural tests for machine-readable JSON
- `go test ./...`
- `scripts/check-linux-compile.sh` when platform wrappers are touched
- `scripts/check-cli-smoke.sh` when command wiring changes
- full `scripts/pre-push` before landing

For model-governed areas, run the affected TLC spec before implementation
tests. If a refactor claims behavior is unchanged, add tests that compare old
and new output shape rather than relying only on compile success.

## What Not To Split Yet

Avoid broad moves of these areas until the package contract around them is
clear:

- `init.go`, `init_steps.go`, and rollback code
- credential store migration and live secret broker internals
- Codex desktop attach/app-server experiment code
- hook approval runtime and `core.hooksPath` enforcement
- native account setup and sudoers mutation

Those areas are either verified, security-sensitive, or still under active
product discovery. They can be decomposed, but only after the pure planning
packages stop forcing everything through `package main`.

## Current First Slice

The current branch includes the first proof slice: `sessionmeta`. It is
intentionally small:

- exports network mode parsing
- exports runtime mode labels
- exports launch metadata structs and JSON marshaling
- leaves existing `package main` wrappers intact
- preserves current JSON shape and session contract labels

This slice should be treated as the pattern for future decomposition: extract a
small contract, keep compatibility shims, move tests with it, and verify the CLI
still behaves the same.
