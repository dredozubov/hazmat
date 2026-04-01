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
- `tla/VERIFIED.md` for the current formal-verification scope boundary

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

Hazmat should ship a first-class Tier 3 sandbox mode for Docker-capable
sessions. The operator continues to use `hazmat claude`, `hazmat exec`, and
`hazmat shell` as the primary entrypoints. Hazmat detects whether the current
project requires Tier 3 and routes the session to the correct runtime.

Recommended behavior:

- Non-Docker repository: launch the current Tier 2 runtime.
- Docker-oriented repository with a healthy Docker backend configured: launch
  Tier 3 automatically.
- Docker-oriented repository with no Docker backend configured: show a setup
  path that keeps the user inside Hazmat instead of pushing them to external
  docs.
- Docker-oriented repository where the user only wants code editing:
  `--ignore-docker` remains available as an explicit override.
- Operator override: `--sandbox` forces the canonical command to use Tier 3
  rather than introducing a parallel session command surface.

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

Canonical session commands:

- `hazmat claude`
- `hazmat exec`
- `hazmat shell`

Explicit Tier 3 selection:

- `hazmat claude --sandbox`
- `hazmat exec --sandbox`
- `hazmat shell --sandbox`

Tier 3 management commands:

- `hazmat sandbox setup`
- `hazmat sandbox doctor`
- `hazmat sandbox reset`

The namespace is intentionally backend-agnostic. Docker Sandboxes are the v1
backend, but the product surface should describe the abstraction rather than
bind itself permanently to a single vendor name.

## Architecture Overview

### Components

1. Tier detector
2. Tier 3 backend manager
3. Tier 3 network policy manager
4. Mount planner
5. Runtime launcher
6. Session recorder and export layer

### Tier detector

The tier detector is responsible for deciding whether a project should run in:

- Tier 2 host-side containment
- Tier 3 Docker-capable containment
- blocked state with setup guidance

Inputs:

- hard Docker markers such as `Dockerfile`, `compose.yaml`,
  `docker-compose.yml`, and `Containerfile`
- soft container-workflow markers such as `.devcontainer/`
- explicit user override such as `--ignore-docker` or `--sandbox`
- backend readiness state from `hazmat sandbox doctor`

Routing rule for v1:

- hard Docker markers are sufficient to auto-route into Tier 3 when the backend
  is configured
- `.devcontainer/` alone is not sufficient to auto-route by itself
- `.devcontainer/` alone should produce an advisory unless Hazmat positively
  determines that the devcontainer config itself requires Docker or Compose

This distinction exists because IDE-oriented devcontainer metadata is not the
same thing as proof that a CLI session must be Docker-capable.

### Tier 3 backend manager

The backend manager is responsible for:

- verifying backend availability and version floor
- creating or reusing a per-project sandbox
- associating stable project identity with stable sandbox identity
- ensuring the runtime uses its own Docker daemon rather than the host daemon

The backend manager is a trust boundary. It must fail closed if it cannot prove
that the runtime is using a private daemon or microVM boundary.

### Tier 3 network policy manager

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
- `ANTHROPIC_API_KEY` passed from the parent process environment at launch
  time via explicit env injection
- no persistence of model API credentials inside the sandbox between sessions

For v1, the launcher should pass the minimum viable environment:

- `ANTHROPIC_API_KEY`
- fixed runtime variables Hazmat itself requires

It should not pass through arbitrary host shell state.

### Session recorder and export layer

Hazmat should preserve its current session model as much as possible:

- user sees a Hazmat session, not an unrelated Docker workflow
- session export and resume behavior stays consistent where possible
- audit logs identify whether the session ran in Tier 2 or Tier 3

## Session Lifecycle

### 1. Setup

`hazmat sandbox setup` verifies the presence and version of the supported Docker
backend and records approved configuration. It should not grant broad trust to
arbitrary Docker installations; it should validate the exact backend type and
minimum version.

### 2. Preflight

Before launching a Docker-oriented session, Hazmat runs:

- Docker marker detection
- backend health check
- version floor check
- approval check for the project/backend/profile tuple
- network policy materialization
- mount plan generation

If any step fails, launch must fail closed.

### 3. Policy application

Hazmat applies the required network policy to the specific sandbox or runtime
instance before starting the agent process.

### 4. Launch

Hazmat launches the requested tool within the Tier 3 runtime using the prepared
mounts and environment. The operator should not need to invoke raw Docker
commands, and the canonical `hazmat claude|exec|shell` commands remain the
entrypoint even when Tier 3 is selected explicitly with `--sandbox`.

### 5. Active session

During the session, the agent can use Docker workflows inside its private
runtime, including `docker build`, `docker run`, and `docker compose`, without
access to the host daemon.

### 6. Cleanup

The lifecycle decision is:

- stable per-project sandbox identity
- ephemeral per-session agent process or container layer inside that sandbox
- explicit reset via `hazmat sandbox reset`
- cleanup on `hazmat rollback`

This preserves warm per-project state where intended while avoiding ambiguous
cross-project reuse.

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
9. Model API credentials are delivered only as explicit launch-time env
   injection and are not persisted inside the sandbox by default.

If any one of these invariants cannot be enforced, Docker mode should not ship.

## Formal Verification Scope Gap

Hazmat's current TLA+ proofs cover Tier 2 containment properties built around
Seatbelt policy generation, rollback ordering, backup safety, and migration
logic. They do not currently cover Tier 3 container containment.

This matters because the Tier 2 proof of credential isolation is grounded in
SBPL deny rules. In Tier 3, the analogous security property is enforced by:

- what the mount planner refuses to mount
- what environment variables the launcher refuses to pass through
- what backend identity the control plane accepts

Until a Tier 3 model exists, `tla/VERIFIED.md` should not be read as proving
Tier 3 containment. A future model should cover at least mount-planner
exclusions, backend identity, and pre-launch network-policy application.

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
- explicit Tier 3 selection on the canonical commands via `--sandbox`
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
- whether the session was launched via auto-routing or explicit `--sandbox`

The logs should avoid storing secret values but should preserve enough structure
to reconstruct policy decisions.

## Rollout Plan

### Phase 0: Correct current UX

Fix the existing Tier 3 hint immediately anywhere it points to a non-existent
command surface. This is not a design-phase defer; it is an immediate hygiene
fix required for the current Docker gate to remain credible.

### Phase 1: Backend setup and diagnostics

Ship:

- `hazmat sandbox setup`
- `hazmat sandbox doctor`
- `hazmat sandbox reset`
- version checks
- backend identity checks
- policy validation checks

Do not auto-route sessions yet.

### Phase 2: Explicit Tier 3 selection

Ship:

- `--sandbox` flag on `hazmat claude`
- `--sandbox` flag on `hazmat exec`
- `--sandbox` flag on `hazmat shell`

This gives auditors and early adopters a concrete path to test without creating
two parallel session command surfaces.

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

1. Which package registries belong in the default allowlist, if any?
2. How much operator-facing network customization is acceptable without
   creating a footgun?
3. Should Docker mode have a separate snapshot and restore story from Tier 2?
4. How much `.devcontainer` parsing is acceptable in v1 before the detector
   becomes too implicit or too error-prone?

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
- canonical session commands remain the primary UX, with `--sandbox` as the
  explicit Tier 3 override
- model API credentials are passed only at launch time and are not stored in
  the sandbox by default
- the operator can use Docker workflows through Hazmat without touching raw host
  Docker access
- the red-team questions above have concrete test plans and clear expected
  outcomes

## Summary

The recommended path is not "let Tier 2 use Docker." The recommended path is to
make Docker a first-class Tier 3 runtime inside Hazmat, keep the operator on
the canonical `hazmat claude|exec|shell` entrypoints, and make that path simple
enough that users adopt it by default.

That is the only approach that offers a meaningful usability unlock without
abandoning Hazmat's existing security posture.
