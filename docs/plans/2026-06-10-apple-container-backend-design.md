# Apple Container Backend Design For macOS

**Date:** 2026-06-10
**Status:** Design spec. No implementation approval.
**Owns bead:** `sandboxing-4tsx`
**Related docs:** [architecture](../architecture.md),
[Linux-ready backend architecture](2026-05-28-linux-ready-backend-architecture.md),
[Tier 3 Docker Sandboxes](../tier3-docker-sandboxes.md),
[Tier 4 VM isolation](../tier4-vm-isolation.md),
[TLA+ verified areas](../../tla/VERIFIED.md)

This document specifies how Hazmat should add support for Apple's
`container` runtime on macOS. The target is Apple's open-source
Containerization-backed `container` CLI, not Docker Desktop and not a
replacement for Hazmat's native macOS Seatbelt backend.

The short version:

- Add an executable `apple-container` backend for short-lived Linux agent
  sessions built on `container run`.
- Keep `container machine` as an explicit future mode, not the default backend.
- Run the Apple `container` CLI as Hazmat's dedicated macOS `agent` user.
- Mount only the planned project and explicit read/write grants.
- Reject broad home exposure, host credential paths, registry credential
  inheritance, SSH forwarding, socket publishing, and unsupported network
  downgrades.
- Treat network allowlists and persistent machines as later work that needs
  model updates before launch code lands.

## Upstream Facts

As of 2026-06-10, the upstream facts that matter for Hazmat are:

- `apple/container` says `container` creates and runs Linux containers as
  lightweight virtual machines on Mac, consumes and produces OCI-compatible
  images, requires Apple silicon, and is supported on macOS 26 because it uses
  new virtualization and networking features.
- `container run` exposes the backend shape Hazmat needs for an MVP: bind
  mounts, read-only rootfs, tmpfs, user/UID/GID, workdir, env/env-file,
  CPU/memory limits, Linux capability add/drop, network selection, DNS control,
  labels, and explicit socket/SSH forwarding flags.
- User-defined network commands are macOS 26+ and include `container network
  create --internal`, but the public docs do not expose a domain allowlist or a
  hard "no network namespace" equivalent.
- `container machine` is a persistent Linux environment, not an application
  container. It automatically maps the host username and home directory into
  the Linux environment. Its `home-mount` setting defaults to `rw`, with `ro`
  and `none` available.
- The upstream network allowlist discussion for AI agents is still unsettled:
  users are building proxy and PF experiments, while maintainers have pointed
  out capability changes and have not documented a built-in Hazmat-grade
  egress allowlist.

Sources:

- [apple/container README](https://github.com/apple/container/tree/main)
- [container-machine.md](https://github.com/apple/container/blob/main/docs/container-machine.md)
- [command-reference.md](https://github.com/apple/container/blob/main/docs/command-reference.md)
- [discussion #719](https://github.com/apple/container/discussions/719)

## Product Positioning

Apple Container support should be presented as a Linux microVM backend for
Hazmat, not as "macOS containerization" for native Mac processes.

Hazmat still needs the native backend because Claude Code, Codex, Cursor
Agent, shell scripts, local MCP servers, Homebrew tools, Keychain-adjacent
flows, and normal macOS CLIs often run as Mach-O processes on the host. Apple
Container does not confine those processes. It runs Linux workloads in VMs.

The product promise for this backend is narrower:

```text
Hazmat can run a Linux-compatible agent harness in an Apple Container VM while
preserving the same Hazmat session contract: explicit project authority,
explicit extra grants, no host-user credential inheritance, scoped credential
delivery, resource limits, and cleanup metadata.
```

This complements Docker Sandbox mode. Docker Sandbox remains the right answer
when the project needs Docker-compatible workflows and Docker's sandbox UX.
Apple Container becomes a first-party, Apple-silicon-only alternative for
Linux agent sessions that do not require Docker daemon semantics.

## Approach Options

### Option A: Shell Out To `container run` For MVP

This is the recommended first implementation.

Hazmat adds a pure compiler package that turns `containment.Contract` into an
Apple Container launch spec. The runtime package then invokes
`/usr/local/bin/container` through the existing host execution path, as the
dedicated macOS `agent` user. The runner creates one named container per
Hazmat session and removes it during cleanup.

Pros:

- Uses the upstream supported CLI rather than binding to an unstable internal
  Swift API.
- Fits the existing Docker compiler/runtime split.
- Keeps the first feature testable through generated launch specs and command
  argv snapshots.
- Preserves Hazmat's host-side user boundary if the CLI runs as `agent`.

Cons:

- CLI output and version compatibility need careful parsing.
- Some safety claims depend on empirical probes because `container` is still
  young.
- Network allowlisting is not solved by the CLI alone.

### Option B: Build A Swift Containerization Helper

This should be deferred.

A small Swift helper using Apple's Containerization framework could eventually
give Hazmat more structured API access and fewer CLI parsing seams. It also
adds a second implementation language, signing/build concerns, and a new
launch-helper surface. That is too much for the first backend.

### Option C: Make `container machine` The Default

This should not be the default.

`container machine` is designed for a persistent Linux development environment
with strong host integration. That is useful, but the defaults conflict with
Hazmat's untrusted-agent posture: automatic home sharing, long-lived mutable
state, and service persistence. It can become an explicit future mode after
the mount, credential, cleanup, and session-resume semantics are modeled.

## Backend Shape

Add a new backend kind:

```go
const KindAppleContainer sessionbackend.Kind = "apple-container"
const ArtifactAppleContainer = "apple-container-launch-spec"
```

Add a matching session mode:

```go
const ModeAppleContainer sessionmeta.Mode = "apple-container"
```

The first user-facing CLI should be explicit rather than automatic:

```bash
hazmat claude --backend=apple-container --image ghcr.io/example/hazmat-claude:latest
hazmat codex --backend=apple-container --image ghcr.io/example/hazmat-codex:latest
hazmat exec --backend=apple-container --image ubuntu:24.04 -- bash -lc 'make test'
hazmat explain --backend=apple-container --image ghcr.io/example/hazmat-codex:latest
```

Do not overload `--docker`. Docker routing and Apple Container routing are
different products with different backend readiness checks. If a generic
`--backend` flag is too large for the first patch, use
`--apple-container=<image>` as a temporary explicit flag and keep the internal
mode name stable.

The backend should not participate in `--docker=auto`. A later
`--backend=auto` may route to Apple Container only after repo markers, image
availability, harness compatibility, and network policy support are stable.

## Package Plan

Add packages in the same direction as the existing architecture:

```text
session request / CLI
  -> sessionplanner
  -> containment.Contract
  -> containment/applecontainer.Compile(...)
  -> internal/runtime/applecontainer.PrepareLaunch(...)
  -> internal/runtime/applecontainer.Run(...)
```

Proposed package responsibilities:

| Package | Owns | Must not own |
| --- | --- | --- |
| `containment/applecontainer` | Pure conversion from `containment.Contract` to Apple Container launch spec, mount plan, labels, network capability gaps, and argv DTO. | Running `container`, probing host state, writing files, materializing credentials. |
| `internal/runtime/applecontainer` | Tool discovery, version/status checks, named container lifecycle, generated env-file cleanup, process launch, best-effort cleanup. | Authority decisions that belong in `containment`, `pathpolicy`, or `sessionplanner`. |
| `sessionbackend` | Backend kind, lifecycle artifact kind, capability gaps. | Apple-specific command construction. |
| `sessionmeta` | Mode label and network metadata. | Backend readiness probing. |
| `hostfacts` or new `containerfacts` | Explicit inspected facts: macOS version, Apple silicon, CLI path/version, system service status, network command support. | Mutating the Apple Container system service. |

The compiler should return a JSON-friendly launch spec, not just an argv:

```go
type LaunchSpec struct {
    FormatVersion int
    Backend       string
    Phase         string
    ContainerName string
    Image         string
    Workdir       string
    User          UserSpec
    Mounts        []MountSpec
    Tmpfs         []string
    Rootfs        RootfsSpec
    Network       NetworkSpec
    Resources     ResourceSpec
    Capabilities  CapabilitySpec
    Environment   EnvironmentSpec
    Labels        map[string]string
    Cleanup       CleanupSpec
    CapabilityGaps []CapabilityGap
}
```

The argv should be derived from the validated `LaunchSpec` at the runtime
boundary. Tests should snapshot both the spec JSON and the resulting command
arguments.

## Host Admission

The Apple Container backend is executable only when all of these are true:

1. Host is macOS on Apple silicon.
2. macOS major version is 26 or newer.
3. `container` CLI is present at an approved path, initially
   `/usr/local/bin/container` or a configured absolute path.
4. `container system status --format json` reports a healthy API server.
5. `container system version --format json` reports a supported CLI/API
   version, initially `>= 1.0.0`.
6. Hazmat can run the CLI as the dedicated `agent` macOS user.
7. The selected image is explicit, pinned or policy-approved, and Linux/arm64
   compatible.
8. Requested network policy is enforceable by the selected Apple Container
   strategy.

Hazmat must not install Apple Container, run `container system start`, or
repair launchd/system service state automatically in the MVP. Those operations
may require administrator authority and are outside the session launch
contract. Diagnostics can print exact commands for the user to run.

## Identity Model

The runtime must invoke `container` as Hazmat's dedicated `agent` macOS user,
not as the invoking host user.

This gives the backend the same first host-side boundary as native Hazmat:

- Apple Container user data, registry state, generated files, logs, and cache
  state belong to `agent`.
- Host bind mounts are limited by the ACLs and path grants Hazmat already
  prepares for `agent`.
- The invoking user's home directory and registry credentials are not visible
  to the Apple Container CLI.

Inside the Linux VM, Hazmat should run the agent process as a non-root numeric
UID/GID that preserves write behavior on bind mounts. The first implementation
must probe how Apple's VirtioFS ownership mapping behaves when the host CLI is
run as `agent`; do not guess in launch code. Until that probe is pinned by
tests, the backend remains behind an experimental gate.

Root inside the Linux VM is not equivalent to root on macOS, but root can
weaken guest-side firewall and file protections. The default harness process
should therefore be non-root. Images that require root for package
installation should do that at image build time, not during the agent session.

## Filesystem Authority

The Apple Container compiler should follow Docker Sandbox's mount planner
rules, not macOS SBPL rule shape.

Required rules:

- Project directory is mounted read-write.
- Explicit read-only grants are mounted read-only.
- Explicit read-write grants are mounted read-write.
- Redundant read-only grants covered by the project or broader read-only
  grants are omitted.
- Ancestor grants that would cover credential deny paths are rejected or split
  before launch, using the same safety posture as Docker Sandbox.
- Host credential deny zones and parents of deny zones are never mounted.
- The invoking user's home is never mounted.
- The `agent` user's home is not mounted wholesale.
- Anonymous volumes are not used, because upstream docs say anonymous volumes
  are not removed automatically with `--rm`.
- Socket publishing, SSH agent forwarding, and virtualization exposure are
  disabled unless a later model explicitly adds them.

The first target mapping should preserve absolute host paths inside the Linux
VM where possible, for example:

```text
/Users/dr/workspace/app -> /Users/dr/workspace/app
/Users/dr/reference    -> /Users/dr/reference
```

This keeps tool output and session contracts understandable. If Apple
Container path semantics make host-absolute targets awkward, use a deterministic
Linux namespace root:

```text
/workspace/project
/workspace/read-only/<stable-name>
/workspace/read-write/<stable-name>
```

That fallback must be reflected in session metadata and in agent launch cwd.

Rootfs policy:

- Default MVP: writable ephemeral rootfs, removed during cleanup. This supports
  package managers inside the container without granting host authority.
- Strict option: `--read-only` rootfs plus explicit tmpfs for `/tmp`, `/run`,
  and any harness-required scratch paths.

The strict option should come after harness smoke tests prove which images can
run under it.

## Credential Delivery

Credential delivery must start narrower than native Hazmat.

Allowed in MVP:

- Provider credential material that Hazmat already owns for the selected
  harness, delivered through a generated env-file or generated secret file
  under `agent`-owned temp state.
- Session-scoped files with mode `0600`, consumed by `container run`, then
  deleted by Hazmat after launch or during cleanup.
- Redacted session metadata that records credential descriptor names, not
  payloads.

Rejected in MVP:

- Integration env passthrough.
- Host shell environment inheritance.
- SSH agent forwarding via `--ssh`.
- Host registry credentials from the invoking user.
- Broad mounts of `~/.config`, `~/.ssh`, `~/.aws`, `~/Library`, or agent home.
- Beadpost or other host broker sockets, unless a separate broker boundary
  model proves the socket path and attestation flow for this backend.

If a harness cannot run without broad host credentials, it is not compatible
with `apple-container` mode yet.

## Network Policy

The backend must report the effective network policy honestly.

MVP support:

- `--network default`: allowed, reported as outbound allowed through Apple
  Container networking.
- Named explicit network with default egress: allowed only after host admission
  can inspect that the network exists and is not marked as a Hazmat deny-mode
  approximation.

MVP rejects:

- `--network none`
- DNS/domain allowlists
- "deny all except package registry/API hosts"
- Claims that PF/DNS policy from native Hazmat applies to Apple Container
  traffic

The reason is simple: Apple Container networking is VM-backed and the public
surface does not yet give Hazmat a proven deny-by-default egress mechanism.
An internal network plus proxy may be useful later, but discussion #719 shows
that host reachability and PF/vmnet behavior need careful proof before Hazmat
can call it equivalent to native or Docker Sandbox network policy.

Phase 2 can add a named egress profile:

```text
container network create --internal hazmat-<session>
proxy sidecar on internal + default networks
agent container on internal only
guest firewall or host PF rule blocks host gateway/private ranges
agent HTTPS_PROXY/HTTP_PROXY points at proxy
```

That phase needs new model work. The model must prove policy-before-launch,
host gateway blocking, cleanup of network/proxy artifacts, and fail-closed
behavior when proxy or PF setup fails.

## Container Machine Policy

`container machine` should be documented and exposed later as a separate mode:

```bash
hazmat claude --backend=apple-container-machine --machine hazmat-ubuntu
```

It must not be used as the default Apple Container backend.

Reasons:

- It is persistent by design.
- It runs an init system and long-lived services.
- It maps the host username and home by default.
- Its `home-mount` default is `rw`.
- It does not provide the same obvious per-session cleanup boundary as
  `container run --rm`.

A safe future machine mode needs one of these authority strategies:

1. Run the machine as `agent` with `home-mount=none`, clone/materialize the
   repo inside the machine, and deliver credentials through scoped session
   files.
2. Run the machine as `agent` with `home-mount=ro` only for reference
   inspection, with writes going to a machine-local workspace that Hazmat
   exports after review.
3. Wait for upstream support for explicit machine mounts equivalent to
   `container run --mount`, then reuse the same mount planner as the ephemeral
   backend.

Do not support `home-mount=rw` for untrusted agent sessions without a separate
design and a blunt session contract warning.

## Session Lifecycle

The runtime sequence should be:

1. Resolve and validate request, image, harness, project, grants, network mode,
   and credentials.
2. Build the backend-neutral `containment.Contract`.
3. Compile the Apple Container `LaunchSpec`.
4. Run host admission checks as the invoking user where safe and as `agent`
   where needed.
5. Materialize session-scoped credential files under an `agent`-owned temp
   root.
6. Create a deterministic container name:

   ```text
   hazmat-<harness>-<project-hash>-<session-id-short>
   ```

7. Start the container with labels tying it to the Hazmat session ID and bead
   or trace ID when available.
8. Stream stdio to the user.
9. On normal exit, remove the container and generated env/secret files.
10. On interrupted exit, best-effort stop/remove the container and record any
    cleanup failure in session metadata.

The runtime should never call `container prune`, `container volume prune`, or
network-wide cleanup commands. Cleanup is by exact session-owned artifact names
and labels only.

## UX And Session Contract

The session contract should make the backend distinction visible:

```text
Mode:                 Apple Container
Backend:              apple-container
Image:                ghcr.io/example/hazmat-codex:sha256-...
Host identity:        agent macOS user
Guest identity:       uid 502 gid 20 (non-root)
Project:              /Users/dr/workspace/app (rw bind mount)
Read-only grants:     /Users/dr/reference (ro bind mount)
Read-write grants:    none
Credential delivery:  provider env-file, session-scoped
Network:              default (outbound allowed, Apple Container VM network)
Unsupported policy:   network none, egress allowlist
Cleanup:              remove named container + generated credential files
```

If the user requests `--network none`, the error should say:

```text
--network none is not implemented for Apple Container backend yet.
Apple Container networking is VM-backed and Hazmat does not have a proved
deny-all egress mechanism for this backend.
Use native containment for network-none macOS sessions, Docker Sandbox for
Hazmat-managed deny-mode profiles, or omit --network none.
```

If the user tries to use container machine defaults, the error should say:

```text
container machine defaults expose the host home directory read-write.
Hazmat will not use that as an untrusted-agent boundary.
Create a machine with home-mount=none or use apple-container ephemeral mode.
```

## TLA+ Governance

This backend touches verified areas:

- launch containment
- credential delivery
- session permission repair if new ACL or user setup is required
- launch fd isolation if a new helper path is introduced
- setup/rollback if `hazmat init` starts managing Apple Container resources

Implementation must start with model work before persistent mutations or
executable launch code land.

Required model work:

1. Extend or add a launch containment model for Apple Container mount planning.
   It should be closest to `MC_Tier3LaunchContainment`, not the SBPL model.
2. Prove credential deny paths and parents are never mounted.
3. Prove generated env/secret files are session-scoped and cleaned up or
   recorded as cleanup failures.
4. Prove integration env passthrough is rejected.
5. Prove backend admission occurs before launch.
6. Prove unsupported network policies fail closed.
7. If network profiles are added, prove policy-before-launch and network
   artifact cleanup.
8. If `container machine` support is added, model persistent state separately
   from ephemeral containers.

Until that model exists, code may add plan-only specs, docs, and tests for
pure compilers. It must not start Apple Container sessions.

## Testing Plan

Pure tests:

- Compile a minimal contract into an Apple Container launch spec.
- Reject project/read/write grants that are credential deny paths or parents.
- Omit covered read-only mounts.
- Preserve read-only vs read-write mount access in generated spec.
- Reject integration env passthrough.
- Reject unsupported network modes as capability gaps or hard errors.
- Snapshot generated argv for common harnesses.

Host admission tests:

- Parse `container system version --format json`.
- Parse `container system status --format json`.
- Reject unsupported macOS versions, Intel Macs, missing CLI, unhealthy API,
  old CLI versions, and non-absolute custom CLI paths.

Smoke tests, gated on a macOS 26 Apple silicon host with `container` installed:

- `hazmat exec --backend=apple-container --image alpine:latest -- true`
- read project file from container
- write project file from container
- fail to read a credential deny path
- verify generated env-file is deleted
- verify container is removed after normal exit
- interrupt a running session and verify best-effort cleanup
- verify `--network none` fails closed

Manual security checks:

- Run the CLI as `agent` and confirm registry state does not use the invoking
  user's credentials.
- Confirm mounted paths are exactly the launch spec's paths.
- Confirm the guest process is non-root.
- Confirm host services bound to `0.0.0.0` are not claimed to be blocked in
  session metadata.

## Rollout Plan

Phase 0: Research and proof

- Update stale research docs with `container` 1.0.0 and `container machine`.
- Add beads for TLA+ launch containment model work.
- Add host probes for installed Apple Container without changing launch
  behavior.

Phase 1: Plan-only backend

- Add `KindAppleContainer`, `ModeAppleContainer`, and metadata labels.
- Add `containment/applecontainer` compiler.
- Add launch spec JSON snapshots.
- Make `hazmat explain --backend=apple-container` render capability gaps, but
  do not execute.

Phase 2: Experimental execution

- Implement `internal/runtime/applecontainer` behind an explicit experimental
  gate.
- Support only `hazmat exec` and one harness with a known Linux image.
- Support only `network default`.
- Reject integration env passthrough and SSH/Git socket flows.
- Run gated smoke tests on macOS 26 Apple silicon.

Phase 3: Harness expansion

- Add curated Linux images for Codex, Claude, and selected shell workflows.
- Add image pinning and provenance guidance.
- Add resume/history policy.
- Add optional strict rootfs mode after smoke tests.

Phase 4: Network profiles or machine mode

- Choose one path, not both in the same implementation slice.
- Network profiles require proxy/PF/guest-firewall model work.
- Machine mode requires persistent-state and home-mount model work.

## Open Questions

- Does Apple Container's bind mount ownership behavior preserve writes cleanly
  when the CLI is run as the macOS `agent` user and the guest process uses the
  same numeric UID/GID?
- Can `container run` support a true no-network mode that is stronger than an
  internal network plus `--no-dns`?
- Which harness images should Hazmat own, and how should image tags be pinned
  or verified?
- Should Apple Container support be configured by `--backend`, a narrower
  `--apple-container`, or a project config field first?
- How should session history and transcript sync work when the agent runs in a
  Linux image rather than in the native agent home?
- Is Rosetta acceptable for any x86_64-only Linux toolchains, or should the
  backend require arm64 images only?

## Non-Goals

- Do not replace native macOS containment.
- Do not implement Docker compatibility or Compose semantics through Apple
  Container.
- Do not launch `container machine` by default.
- Do not mount the invoking user's home.
- Do not forward the host SSH agent.
- Do not inherit arbitrary host env vars.
- Do not claim network deny/allowlist support before it is modeled and tested.
- Do not auto-install Apple Container or start its system service from Hazmat.
