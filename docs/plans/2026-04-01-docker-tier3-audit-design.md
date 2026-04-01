# Auditor Design: Secure Docker Support for Hazmat

Status: Proposed
Date: 2026-04-01
Owner: Hazmat
Related issue: `sandboxing-wgs`

## Purpose

This document proposes a first-class Docker-capable runtime for Hazmat that
improves user adoption on Docker-oriented repositories without weakening
Hazmat's existing Tier 2 containment model.

The intended audience is a security auditor or red-team reviewer. This is not a
marketing document and not an end-user tutorial. It focuses on trust
boundaries, security invariants, attack surface, failure modes, and rollout
constraints.

## Background

Hazmat's current default runtime is a dedicated macOS user plus host-side
containment. That design is the right default for non-Docker work because it
meaningfully reduces host credential exposure without requiring a VM. It is
also intentionally incompatible with host Docker daemon access.

Today, Docker-oriented repositories are detected early and blocked unless the
operator opts into `--ignore-docker`. That behavior avoids a bad security
compromise, but it also creates a product gap: users with `Dockerfile`,
`compose.yaml`, or `.devcontainer/` workflows hit a dead end instead of a
native Hazmat path.

The proposal below keeps the current Tier 2 model intact and adds a separate
Tier 3 runtime for Docker-capable sessions.

## Relevant Existing Materials

An auditor reviewing this proposal should also inspect:

- `docs/overview.md` for the current tier-selection model
- `docs/threat-matrix.md` for the attack-by-attack comparison
- `docs/tier3-docker-sandboxes.md` for the current Docker guidance
- `hazmat/session.go` for the current Docker artifact gate and `--ignore-docker`
  behavior

## Problem Statement

Hazmat needs a way to let users work productively on Docker-heavy repositories
without encouraging unsafe workarounds such as:

- exposing the host Docker socket to the existing agent user
- bypassing Hazmat entirely for Docker-based projects
- using `--ignore-docker` on sessions that actually need Docker
- treating devcontainer workflows as a documentation-only escape hatch

The product goal is to make the secure path the low-friction path.

## Security Objectives

The design must satisfy all of the following:

1. Preserve the current Tier 2 security properties for non-Docker repositories.
2. Allow Docker-capable sessions only when the agent is using a dedicated
   Docker daemon or microVM boundary.
3. Prevent any session from reaching a host Docker daemon or shared Docker
   socket.
4. Apply deny-by-default or explicit-allow network controls to the Docker
   runtime automatically, not as an optional manual step.
5. Keep the operator-facing UX simple enough that users prefer the secure
   runtime over manual bypasses.

## Non-Goals

This proposal does not attempt to:

- make host Docker socket sharing safe
- retrofit Docker support into the existing Tier 2 runtime
- solve all exfiltration risks inside a Docker-capable runtime
- support every possible Docker backend in v1
- provide general plugin support for arbitrary container orchestrators

## Recommended Product Shape

Hazmat should ship a first-class Docker mode that behaves as a Tier 3 runtime.
The operator continues to use `hazmat claude`, `hazmat exec`, and
`hazmat shell` as the primary entrypoints. Hazmat detects whether the current
project requires Docker and routes the session to the correct runtime.

Recommended behavior:

- Non-Docker repository: launch the current Tier 2 runtime.
- Docker-oriented repository with a healthy Docker backend configured: launch
  Tier 3 automatically.
- Docker-oriented repository with no Docker backend configured: show a setup
  path that keeps the user inside Hazmat instead of pushing them to external
  docs.
- Docker-oriented repository where the user only wants code editing:
  `--ignore-docker` remains available as an explicit override.

## Recommendation for v1 Backend

Support exactly one Docker backend in v1:

- Docker Sandboxes or an equivalent dedicated microVM-based Docker runtime

Reasons:

- narrow implementation scope
- clearer audit surface
- better UX than generic devcontainer orchestration
- strongest alignment with the current Tier 3 guidance

Do not support Colima, Podman, or generic devcontainer execution as first-class
Hazmat backends in v1. Those may be considered later behind a backend adapter,
but they should not expand the initial threat surface.

## Proposed Command Surface

Primary commands:

- `hazmat claude`
- `hazmat exec`
- `hazmat shell`

New Docker-specific commands:

- `hazmat docker setup`
- `hazmat docker doctor`
- `hazmat docker claude`
- `hazmat docker exec`
- `hazmat docker shell`

The generic commands remain the default UX. The Docker-specific commands exist
for explicit control, diagnostics, support, and testing.

## Architecture Overview

### Components

1. Tier detector
2. Docker backend manager
3. Docker network policy manager
4. Mount planner
5. Runtime launcher
6. Session recorder and export layer

### Tier detector

The tier detector is responsible for deciding whether a project should run in:

- Tier 2 host-side containment
- Tier 3 Docker-capable containment
- blocked state with setup guidance

Inputs:

- repository Docker markers such as `Dockerfile`, `compose.yaml`,
  `docker-compose.yml`, `Containerfile`, and `.devcontainer/`
- explicit user override such as `--ignore-docker`
- backend readiness state from `hazmat docker doctor`

### Docker backend manager

The backend manager is responsible for:

- verifying backend availability and version floor
- creating or reusing a per-project sandbox
- associating stable project identity with stable sandbox identity
- ensuring the runtime uses its own Docker daemon rather than the host daemon

The backend manager is a trust boundary. It must fail closed if it cannot prove
that the runtime is using a private daemon or microVM boundary.

### Docker network policy manager

The network manager is responsible for turning the Docker-capable runtime from
"allow-all by default" into "explicitly allowed only." It must apply policy
before the agent session starts.

At minimum, v1 should allow:

- model API endpoint
- source control endpoint
- package registries that the operator explicitly approves or that are part of
  a Hazmat-managed allowlist

It must not rely on the user manually applying `docker sandbox network proxy`
commands out of band.

### Mount planner

The mount planner translates Hazmat's existing project and read-only directory
model into the Docker backend.

Required mapping:

- project directory: read-write
- `-R` directories: read-only
- credentials and unrelated host paths: not mounted

The mount planner must not silently broaden access compared to the current
Hazmat session model.

### Runtime launcher

The launcher starts the agent tool inside the Tier 3 runtime with:

- stable working directory
- restricted environment
- backend-specific network policy already in place
- no host Docker socket
- no host SSH agent socket
- no host credential mounts

### Session recorder and export layer

Hazmat should preserve its current session model as much as possible:

- user sees a Hazmat session, not an unrelated Docker workflow
- session export and resume behavior stays consistent where possible
- audit logs identify whether the session ran in Tier 2 or Tier 3

## Session Lifecycle

### 1. Setup

`hazmat docker setup` verifies the presence and version of the supported Docker
backend and records approved configuration. It should not grant broad trust to
arbitrary Docker installations; it should validate the exact backend type and
minimum version.

### 2. Preflight

Before launching a Docker-oriented session, Hazmat runs:

- Docker marker detection
- backend health check
- version floor check
- network policy materialization
- mount plan generation

If any step fails, launch must fail closed.

### 3. Policy application

Hazmat applies the required network policy to the specific sandbox or runtime
instance before starting the agent process.

### 4. Launch

Hazmat launches the requested tool within the Tier 3 runtime using the prepared
mounts and environment. The operator should not need to invoke raw Docker
commands.

### 5. Active session

During the session, the agent can use Docker workflows inside its private
runtime, including `docker build`, `docker run`, and `docker compose`, without
access to the host daemon.

### 6. Cleanup

Hazmat may either reuse a stable per-project sandbox or destroy ephemeral
instances after use. Either choice must be explicit in the product model and
auditable. v1 should prefer stable per-project sandboxes for UX, with explicit
reset and removal commands.

## Trust Boundaries

### Boundary A: Host user and host secrets

The host operator's secrets, shell state, Docker socket, SSH agent, Keychain,
and unrelated project directories remain outside the runtime.

### Boundary B: Hazmat control plane

Hazmat on the host is trusted to:

- detect project type
- apply policy
- choose the runtime
- mount only intended paths

Hazmat must not delegate these choices to the agent session itself.

### Boundary C: Tier 3 runtime

The Tier 3 runtime is trusted as the primary Docker isolation boundary. It must
provide:

- a private Docker daemon or microVM
- isolation from the host daemon
- an enforceable network control plane

### Boundary D: Agent process

The agent process is untrusted. It may attempt:

- host discovery
- daemon escape
- mount abuse
- network exfiltration
- downgrade attempts back into host execution

The design assumes the agent will actively test the boundary.

## Mandatory Security Invariants

These invariants are non-negotiable:

1. No host Docker socket exposure.
2. No shared host Docker daemon access.
3. Network policy is applied automatically before launch.
4. Project mounts cannot exceed Hazmat's declared read-write and read-only
   scope.
5. The runtime cannot inherit arbitrary host environment variables.
6. The runtime cannot use the host SSH agent socket.
7. Hazmat must fail closed when backend identity, backend health, or network
   policy state is uncertain.
8. Tier 2 remains unchanged for non-Docker projects.

If any one of these invariants cannot be enforced, Docker mode should not ship.

## Threat Model

### Assets to protect

- host user credentials and shell state
- host Docker daemon and any other privileged host helper
- unrelated repositories and local data
- network policy integrity
- Hazmat's routing and approval state

### Adversaries

- malicious repository content
- prompt injection in repository files or generated output
- compromised package install or build scripts
- a malicious or compromised agent tool
- a user confused by ambiguous UX into selecting an unsafe path

### Expected attack classes

- mount expansion or path confusion
- environment inheritance leaks
- daemon confusion or daemon substitution
- network policy bypass or policy-not-applied launch
- downgrade from Tier 3 to Tier 2 or host execution
- abuse of stable sandbox reuse
- credential bleed through devcontainer- or Docker-specific config files

## UX and Safety Model

This proposal intentionally treats UX as part of the security boundary.
If the secure Docker path is slower, more confusing, or more fragmented than
the insecure path, users will bypass it.

Required UX properties:

- single mental model: "run Hazmat, not raw Docker"
- clear routing: Hazmat explains why a repo is using Tier 2 or Tier 3
- explicit override for code-only sessions on Docker repos
- clear diagnostics when backend setup is missing
- no suggestion that exposing host Docker is an acceptable shortcut

## Approval Model

Hazmat should require an explicit host-side approval the first time a project
is allowed to use Docker mode.

Approval should bind to:

- canonical project path
- backend type
- relevant network policy profile

Changing any of those should invalidate prior approval and require a new
operator decision.

This is not the primary security boundary, but it reduces accidental widening
of trust.

## Logging and Auditability

Hazmat should log enough information for an operator or auditor to answer:

- was this session Tier 2 or Tier 3
- which backend was used
- which network profile was applied
- which paths were mounted read-write and read-only
- whether the session was launched via auto-routing or explicit Docker mode

The logs should avoid storing secret values but should preserve enough structure
to reconstruct policy decisions.

## Rollout Plan

### Phase 0: Correct current UX

Before introducing Docker mode, Hazmat should fix current messaging so the
recommended path in errors is a real supported command rather than a placeholder
or stale reference.

### Phase 1: Backend setup and diagnostics

Ship:

- `hazmat docker setup`
- `hazmat docker doctor`
- version checks
- backend identity checks
- policy validation checks

Do not auto-route sessions yet.

### Phase 2: Explicit Docker mode

Ship:

- `hazmat docker claude`
- `hazmat docker exec`
- `hazmat docker shell`

This gives auditors and early adopters a concrete path to test without changing
the generic command behavior.

### Phase 3: Auto-routing

Teach `hazmat claude`, `hazmat exec`, and `hazmat shell` to route Docker
projects into Tier 3 when setup is complete and approval exists.

### Phase 4: Hardening and backend abstraction

Only after the primary path is stable should Hazmat consider:

- multiple backends
- devcontainer-aware UX
- policy profiles for different package ecosystems

## Open Design Questions

These should be resolved before implementation freeze:

1. Should Tier 3 sandboxes be ephemeral per session or stable per project?
2. Which package registries belong in the default allowlist, if any?
3. How much operator-facing network customization is acceptable without
   creating a footgun?
4. Should Docker mode have a separate snapshot and restore story from Tier 2?
5. How should Hazmat expose Docker-specific state reset to the operator?

## Red-Team Review Questions

An auditor or red team should explicitly try to answer the following:

1. Can the Tier 3 runtime reach the host Docker daemon directly or indirectly?
2. Can the agent cause Hazmat to mount more host paths than intended?
3. Can the agent influence the network policy before launch?
4. Can the agent trigger a launch before policy application completes?
5. Can environment inheritance leak host credentials into the runtime?
6. Can stable sandbox reuse leak data between unrelated projects?
7. Can a Docker-oriented project downgrade itself into Tier 2 or raw host
   execution through path tricks or marker suppression?
8. Can backend identity be spoofed so Hazmat believes it is talking to a
   private daemon when it is actually using the host daemon?
9. Can `.devcontainer/`, Compose files, or build scripts broaden mounts or
   network access outside Hazmat's control plane?
10. Can approval state be replayed or confused across projects or backends?

## Acceptance Criteria

Hazmat Docker support is ready for implementation only if all of the following
are true:

- the design preserves the current Tier 2 boundary
- the Docker-capable runtime is private to the agent session
- network policy is automatic and auditable
- the operator can use Docker workflows through Hazmat without touching raw host
  Docker access
- the red-team questions above have concrete test plans and clear expected
  outcomes

## Summary

The recommended path is not "let Tier 2 use Docker." The recommended path is to
make Docker a first-class Tier 3 runtime inside Hazmat and make that path
simple enough that users adopt it by default.

That is the only approach that offers a meaningful usability unlock without
abandoning Hazmat's existing security posture.
