# Problem 5 — Tier 3 Launch Containment

## Problem Statement

Hazmat's Tier 3 path in `hazmat/sandbox.go` launches Docker-capable sessions
through Docker Sandboxes instead of the Tier 2 host-side seatbelt boundary.
That changes where the security argument lives:

1. **Mount planner exclusions** — the host-side launcher must never mount
   credential paths or broad parent directories that cover them.
2. **Read-only mount planning** — redundant read-only mounts should be filtered
   the same way Tier 2 filters overlapping `ReadDirs`, so the effective mount
   set is minimal and deterministic.
3. **Backend identity gate** — launch must fail closed unless the configured
   backend is supported, detected as the same backend, healthy, and using the
   expected policy profile.
4. **Policy-before-launch** — the network policy must be applied before the
   agent process starts inside the sandbox.
5. **Minimal env passthrough** — the current Tier 3 path must not forward
   stack-pack environment variables into the sandbox launch path.

Unlike Tier 2, these properties are not enforced by SBPL deny rules. They are
enforced by the host-side launch sequence and mount planner before `docker
sandbox run` ever starts.

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/sandbox.go` | `buildSandboxLaunchSpec()`, `prepareSandboxLaunch()`, `loadHealthySandboxLaunchBackend()`, `dockerSandboxesBackend.PrepareLaunch()` |
| `hazmat/pack.go` | `isCredentialDenyPath()` |
| `hazmat/session.go` | `isWithinDir()` |

## TLA+ Model

### Abstract Path Model

Eight abstract host paths with a containment relation:

| Path | Represents | Contains |
|------|-----------|----------|
| `workspaceRoot` | `/Users/dr/workspace` | `projectRoot`, `projectSub` |
| `projectRoot` | `/Users/dr/workspace/app` | `projectSub` |
| `projectSub` | `/Users/dr/workspace/app/subdir` | (nothing) |
| `safeRef` | `/Users/dr/reference` | `safeRefChild` |
| `safeRefChild` | `/Users/dr/reference/nested` | (nothing) |
| `invokerHome` | `/Users/dr` | `sshDir`, `awsDir` |
| `sshDir` | `/Users/dr/.ssh` | (nothing) |
| `awsDir` | `/Users/dr/.aws` | (nothing) |

Credential deny paths are modeled by `{sshDir, awsDir}`. A path is unsafe to
mount if it is either a credential path itself or a proper parent of one
(`invokerHome`).

### Nondeterministic Inputs

- `ProjectDir ∈ {projectRoot, projectSub, invokerHome, sshDir}`
- `ReadDirs ⊆ {workspaceRoot, projectSub, safeRef, safeRefChild, invokerHome}`
- `Agent ∈ {"claude", "shell"}`
- `PackEnvRequested ∈ BOOLEAN`
- `BackendReady ∈ BOOLEAN` — abstracts the conjunction of:
  - configured backend supported
  - detected backend matches configured backend
  - backend doctor report is healthy
  - detected policy profile matches configured policy profile
- Approval granted or denied
- Version gates:
  - shell sandbox support present or absent
  - extra read-only workspace support present or absent

### Launch Sequence

The model follows the host-side Tier 3 launch order:

1. Reject stack-pack env passthrough
2. Validate backend readiness (support, identity, health, and policy profile)
3. Validate mount inputs against credential deny zones
4. Check compatibility gates (`shell` support, extra read-only workspace support)
5. Require operator approval
6. Create sandbox with the planned mount set
7. Apply network policy
8. Launch the agent process

### Mount Planner

The planned read-only mount set is:

- every `ReadDir` that is not a credential deny path
- excluding any `ReadDir` already covered by the project directory
- excluding any `ReadDir` covered by another broader `ReadDir`

The resulting mount set is:

- `ProjectDir` mounted read-write
- each planned read directory mounted read-only

### Environment Model

The current Tier 3 implementation rejects stack-pack env passthrough and does
not add any explicit extra launch environment variables in `hazmat/sandbox.go`.
The model therefore treats the Hazmat-controlled launch environment as empty.
If explicit API-key injection is added later, this spec must be updated first.

## What TLC Checks

### Invariants That Must Pass

| Invariant | Meaning |
|-----------|---------|
| `CredentialPathsNeverMounted` | No mounted path is a credential deny path or a parent of one |
| `ProjectMountedRW` | Successful launch always mounts the project directory read-write |
| `PlannedReadDirsMountedRO` | Successful launch mounts every planned reference directory read-only |
| `CoveredReadDirsOmitted` | Read-only dirs covered by the project or another read dir are not mounted |
| `NoUnexpectedLaunchEnv` | Hazmat-controlled Tier 3 launch env is empty |
| `BackendValidationBeforeLaunch` | Successful launch implies backend readiness was established before launch |
| `PolicyBeforeLaunch` | The network policy is applied before the agent launches |
| `ApprovalBeforeLaunch` | Successful launch requires approval |
| `PackEnvRejected` | Requesting stack-pack env passthrough prevents launch |
| `ShellVersionGate` | Shell launches only succeed when shell sandbox support is present |
| `ExtraWorkspaceVersionGate` | Extra read-only mounts only succeed when that Docker Desktop capability is present |

## What This Found

This model drove a concrete code change in Tier 3:

1. The initial Docker Sandboxes implementation mounted `ProjectDir` and
   `ReadDirs` directly, without a Tier 3 equivalent of the credential deny-zone
   check used for packs.
2. The initial mount path also did not filter read directories already covered
   by the project or by another broader read-only directory, even though Tier 2
   already applies that filtering in `generateSBPL()`.

The fix was to add `buildSandboxLaunchSpec()` as the explicit Tier 3 mount
planner and to base compatibility checks and sandbox naming on the effective
mount set rather than raw `ReadDirs`.

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Paths | 8 | Project root/child, safe ref root/child, credential parent/leaves |
| ProjectChoices | 4 | Safe project paths plus adversarial credential parent/leaf |
| ReadChoices | 5 | Parent-of-project, child-of-project, safe redundant, and credential-parent read dirs |
| Launch gate booleans | 5 | Pack env, backend readiness, approval, shell support, extra-workspace support |

**Confirmed state space:** 33,876 states generated, 23,580 distinct. Depth: 9.
Runtime: ~1s.
