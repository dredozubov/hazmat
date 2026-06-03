# Package Split Architecture - Audit Draft

**Date:** 2026-06-02
**Status:** audit draft; not implementation approval
**Authoring bead:** `sandboxing-le2b`
**Related docs:**
[architecture](../architecture.md),
[modular architecture direction](2026-06-02-modular-architecture-direction.md),
[implementation roadmap](2026-06-03-package-split-implementation-roadmap.md),
[remote launch envelope schema](2026-06-02-remote-launch-envelope-schema.md),
[TLA+ verified areas](../../tla/VERIFIED.md)

This document turns the modular architecture direction into a concrete Go
package split for review. The goal is to make Hazmat a set of reusable
contract, planning, and runtime libraries that multiple frontends can call:
the current `hazmat` CLI and future local frontends such as a desktop UI or
local API.

Remote/orchestrated execution is a compatibility input, not the first runtime
target. This split should define what remote may need, take the low-hanging
package choices that keep those needs reachable, and defer hard properties such
as envelope signing, worker identity, replay, worker-local path mapping,
credential handles, cleanup proofs, and control-plane records until a
remote-specific design bead.

This document does not approve moving code, changing behavior, weakening
verified invariants, adding remote execution, or expanding credential delivery.
It is written so reviewers can audit package boundaries before implementation
beads are created.

## Thesis

Hazmat is not truly modular while `package main` owns session semantics. It can
have helper packages and still be hard to reuse if the CLI owns request
validation, contract construction, credential decisions, backend selection,
launch artifact preparation, and runtime cleanup.

The target architecture separates three layers:

- **Frontends** parse input, collect user intent, render output, and preserve UX
  compatibility.
- **Contracts and planners** construct validated, redaction-safe authority
  models without side effects.
- **Runtimes** perform host effects only after receiving validated contracts and
  prepared launch artifacts.

```text
frontend input
  -> validated request
  -> pure planner
  -> backend-neutral contract
  -> backend artifact
  -> runtime admission
  -> launch / cleanup / result records
```

## Design Principles

1. **Frontends do not own security policy.** The CLI can choose defaults and
   render warnings, but package constructors own invariants.
2. **Contracts are side-effect-free.** A contract package may parse, validate,
   normalize, sort, and serialize. It must not inspect the host, prompt, run
   commands, materialize credentials, or launch processes.
3. **Runtimes are effectful but narrow.** Runtime packages may mutate or
   launch only through explicit inputs: validated request, validated contract,
   prepared backend artifact, scoped credential delivery plan, and cleanup
   policy.
4. **Wire DTOs are never authority.** JSON structs and saved plans must parse
   into validated authority types before runtime code sees them.
5. **Capability gaps are first-class.** Unsupported policy must become a
   structured gap or hard error, not a silent approximation.
6. **Verified areas keep their governance.** Any package movement across setup,
   rollback, seatbelt, credential delivery, launch fd isolation, harness
   lifecycle, or session repair boundaries must follow `tla/VERIFIED.md`.
7. **Remote-compatible shape, local-first implementation.** Versioned plans,
   explicit facts, DTO-to-validated-type boundaries, capability gaps, and
   redaction-safe descriptors are cheap now. Worker admission, remote
   credentials, replay defense, and cleanup proofs are not.

## Target Package Tree

The exact names are open for audit. The dependency direction is not.

```text
hazmat/
  cmd/
    hazmat/                    # CLI frontend
    hazmat-launch/             # native launch helper

  internal/
    frontend/cli/              # Cobra commands, rendering, prompts
    legacy/                    # temporary shims during migration
    runtime/
      darwin/                  # native launch runtime and SBPL admission
      docker/                  # Docker Sandbox runtime
      linux/                   # future Linux native runtime
    credentialruntime/         # secret store, brokers, materialization effects
    agententry/                # hidden in-containment helper commands (no sudo primitives)
    harnessruntime/            # harness install/uninstall effects + state writes
    backupruntime/             # snapshot/restore execution and session trigger
    hookruntime/               # repo-local hook execution and cleanup effects
    diagnostics/               # check/stackcheck probes and security smoke tests
    state/                     # host-owned state.json IO
    hostexec/                  # shared sudo/asAgent host execution primitives
    setup/
      darwin/                  # host setup/rollback runtime after model approval
    testfixtures/              # shared package test fixtures, no production imports

  sessionrequest/              # validated user/API request builders
  sessionplanner/              # pure planning facade
  sessioncontract/             # redaction-safe session plan DTOs
  sessionmeta/                 # stable mode/network/metadata labels
  hostfacts/                   # explicit host/platform facts for planners
  configmodel/                 # config schema and pure validation
  pathpolicy/                  # canonical paths and deny-zone grant types
  containment/                 # backend-neutral authority contract
    darwin/                    # SBPL compiler package
    linux/                     # Linux native launch spec compiler
    docker/                    # Docker Sandbox spec compiler

  sessionbackend/              # backend kinds, gaps, prepared artifact variants
  credentials/                 # pure registry and scoped descriptor contracts
  harnesses/                   # harness registry and lifecycle metadata
  integrations/                # manifest schema and pure merge/detection rules
  hostmutations/               # permission repair and repo setup plans
  hooks/                       # sensitive repo-local hook contracts
  backup/                      # snapshot/restore plans

```

### Public vs internal packages

The reusable packages above may be public Go packages inside the module. They
do not all need stable external API guarantees on day one, but their import
rules should be enforced as if another frontend will call them.

`internal/frontend/cli` and `internal/legacy` are intentionally not reusable.
They can depend on Cobra, terminal UI, compatibility shims, and command-specific
rendering. Reusable packages must never import them.

Effectful runtime and setup packages should also start under `internal/`.
They can be shared by multiple in-module frontends, but they should not become
external API until a second frontend exists and the API shape has proved stable.

## Dependency Graph

```mermaid
flowchart TB
    subgraph frontends["Frontends"]
        cli["cmd/hazmat"]
        desktop["future desktop/local API"]
    end

    subgraph contracts["Contracts and planners"]
        request["sessionrequest"]
        planner["sessionplanner"]
        contract["sessioncontract"]
        meta["sessionmeta"]
        facts["hostfacts"]
        config["configmodel"]
        paths["pathpolicy"]
        contain["containment"]
        backend["sessionbackend"]
        creds["credentials descriptors"]
        harness["harnesses metadata"]
        integ["integrations"]
        mutations["hostmutations plans"]
        backupPlan["backup plans"]
        hooksPlan["hooks contracts"]
    end

    subgraph compilers["Backend compilers"]
        darwinC["containment/darwin"]
        linuxC["containment/linux"]
        dockerC["containment/docker"]
    end

    subgraph runtimes["Runtimes"]
        darwinR["internal/runtime/darwin"]
        dockerR["internal/runtime/docker"]
        linuxR["internal/runtime/linux"]
        credR["internal/credentialruntime"]
        agentEntry["internal/agententry"]
        harnessR["internal/harnessruntime"]
        backupR["internal/backupruntime"]
        hookR["internal/hookruntime"]
        diagR["internal/diagnostics"]
        stateR["internal/state"]
        hostexecR["internal/hostexec"]
        setupR["internal/setup/darwin"]
    end

    cli --> request
    cli --> planner
    cli --> facts
    cli --> config
    cli --> diagR
    cli --> agentEntry
    desktop --> request
    desktop --> planner
    desktop --> facts
    request --> paths
    request --> meta
    request --> harness
    request --> integ
    planner --> facts
    planner --> contract
    planner --> backend
    planner --> contain
    planner --> creds
    planner --> mutations
    darwinC --> contain
    linuxC --> contain
    dockerC --> contain
    darwinR --> darwinC
    dockerR --> dockerC
    linuxR --> linuxC
    credR --> creds
    credR --> config
    darwinR --> credR
    dockerR --> credR
    agentEntry --> credR
    backupR --> backupPlan
    hookR --> hooksPlan
    diagR --> darwinC
    diagR --> dockerC
    setupR --> stateR
    harnessR --> harness
    harnessR --> stateR
    harnessR --> hostexecR
    darwinR --> hostexecR
    setupR --> hostexecR
    diagR --> darwinR
    diagR --> backupR
    diagR --> planner
    diagR --> hostexecR
    cli --> darwinR
    cli --> dockerR
    cli --> setupR
    cli --> backupR
    cli --> hookR
    cli --> harnessR

    style contracts fill:#eef,stroke:#55c,color:#000
    style runtimes fill:#ffe,stroke:#a80,color:#000
```

Arrows in this diagram are import dependencies, not runtime call order. Backend
compiler packages import `containment` for the contract type; `containment`
must not import its compiler children. Effectful state writes are a runtime
concern: the pure `harnesses` metadata package never imports `internal/state`;
`internal/harnessruntime` owns install/uninstall effects and the state writes.
Privileged host execution (`sudo`/`asAgent` primitives) lives in
`internal/hostexec`, importable by every runtime that needs it, so the narrow
`internal/agententry` is not forced to host primitives that setup, backup,
harness, credentials, and diagnostics also call.

## Contract vs Runtime Boundary

```mermaid
flowchart LR
    dto["Wire DTO or CLI flags"]
    parse["Parse"]
    validate["Validate constructors"]
    plan["Pure plan"]
    contract["Authority contract"]
    artifact["Prepared artifact"]
    admission["Runtime admission"]
    effects["Host effects"]

    dto --> parse
    parse --> validate
    validate --> plan
    plan --> contract
    contract --> artifact
    artifact --> admission
    admission --> effects

    dto -. not authority .-> reject["No direct runtime access"]
    style reject fill:#fee,stroke:#c33,color:#000
```

The runtime boundary should be a small API surface. A runtime should not accept
raw strings for project roots, raw credential bytes, unvalidated JSON, or
partially constructed backend artifacts. It should receive values that have
already passed the constructors and gap checks.

## Proposed Package Responsibilities

| Package | Current anchors | Proposed ownership |
| --- | --- | --- |
| `sessionrequest` | `session.go` `sessionConfig`, `harnessSessionOpts`, `resolveSessionConfig` | Validated request builder for project/read/write paths, harness target, network mode, integrations, backend preference, preview/launch mode. |
| `pathpolicy` | existing `pathpolicy/`, `path_policy.go` shims | Canonical path authority and deny-zone grant constructors. No frontend or runtime imports. |
| `hostfacts` | scattered globals and probes in `session.go`, `agent_user.go`, `sandbox.go`, integration and harness checks | Explicit host/platform facts collected by frontends and passed into planners. Includes agent home, invoker home, target GOOS/platform, Docker availability, kernel probes, harness installed status, and integration marker facts. No planner should read `$HOME`, inspect Docker, call `runtime.GOOS`, probe kernels, or check harness installation directly. |
| `sessionplanner` | existing `sessionplanner/`, `explain_json.go`, `session_backend.go` | Single pure facade producing contract plan, backend plan, host mutation preview, credential descriptors, and warnings from validated request plus explicit facts. |
| `sessioncontract` | existing `sessioncontract/` | Redaction-safe plan DTOs and versioned JSON shapes for preview/output. |
| `sessionmeta` | existing `sessionmeta/` | Stable labels for mode, network, backend, and platform metadata shared by frontends and planners. |
| `configmodel` | validation portions of `config.go` | Pure config schema and validation, including `ValidateProjectSSHConfig()` and `NormalizedKeys()` routing invariants. `config.go`'s Cobra command handlers move to `internal/frontend/cli`; its cloud-credential persistence (`saveCloudStoredCredential`, `migrateCloudCredentialsIntoSecretStore`, `loadCloud*Key`) moves to `internal/credentialruntime`; neither stays in `configmodel`. |
| `containment` | existing `containment/`, `native_session_policy.go` | Backend-neutral authority contract, structural credential floor, grant overlap validation, comparable core policy. |
| `containment/darwin` | `session_policy_sbpl.go`, `native_session_policy.go` | SBPL compiler from `containment.Contract`; no launch execution. |
| `containment/linux` | existing `containment/linux/` | Plan-only Linux launch spec compiler until runtime is modeled and implemented. |
| `containment/docker` | `sandbox.go` launch spec/profile builders | Docker Sandbox spec compiler from contract/backend plan; no Docker CLI execution. |
| `sessionbackend` | existing `sessionbackend/` | Backend kinds, gap taxonomy, lifecycle artifact expectations, prepared artifact variants. Target platform is an explicit input from `hostfacts`; no `runtime.GOOS` fallback in pure planning paths after Phase C/E. |
| `credentials` | `credential_registry.go`, descriptor portions of `session_credentials.go`, grant/request metadata | Pure registry descriptors, support status, grant requests, scoped delivery handles, redaction contracts, and cleanup plan DTOs. No secret-store, broker, materialization, or file-copy runtime imports. |
| `internal/credentialruntime` | `secret_store.go`, effectful portions of `session_credentials.go`, `config_agent.go`, cloud-credential persistence in `config.go`, `github_capability.go`, `git_https_credentials.go`, `git_ssh.go` | Secret-store access, credential brokers, scoped materialization, file-copy delivery, and cleanup application. Consumes descriptor-package validated plans. |
| `harnesses` | metadata/plan portions of `harness.go`, `harness_lifecycle.go`, `harness_assets.go` | Harness registry metadata, managed artifacts, preserved artifacts, and install/update/uninstall *plans*. Pure: no state writes, no install effects, no `internal/state` import. |
| `internal/harnessruntime` | effectful install/update/uninstall portions of `harness.go`, `harness_lifecycle.go`, bootstrap files; `update/removeHarnessState` calls | Harness install/update/uninstall execution and the `internal/state` writes those flows perform. Consumes `harnesses` plans; imports `internal/state` and `internal/hostexec`. |
| `integrations` | existing `integrations/`, `integration_manifest.go`, `integration_resolver.go` | Manifest schema, safe merge, detection, read-dir/env validation, repo recommendations. Host tool repair plans go through `hostmutations`. |
| `hostmutations` | `session_mutation.go`, `workspace_acl.go`, `git_preflight.go`, `repo_setup.go` | Previewable host mutation plans and proof-scope metadata. Applying mutations is runtime-only. |
| `backup` | plan/schema portions of `backup.go`, `snapshots.go`, `restore.go` | Snapshot and restore plan contracts. Any split is gated by `MC_BackupSafety`, especially the prior-snapshot-before-overwrite invariant. |
| `internal/backupruntime` | `kopia_wrapper.go`, effectful backup/restore portions, `session.go:preSessionSnapshot()` | Snapshot/restore execution and the pre-session snapshot trigger. Must preserve snapshot-before-session ordering across runtime movement. |
| `hooks` | `hook_manifest.go`, approval metadata portions of `hook_approval.go` | Sensitive repo-local hook contracts and approval records. Do not treat as a routine extraction; `MC_GitHookApproval` governs approval, pinned hooksPath, snapshot execution, drift refusal, and rollback cleanup. |
| `internal/hookruntime` | `hook_runtime.go`, effectful portions of `hook_cli.go`, hook wrapper dispatch/fallback | Repo-local hook installation, wrapper dispatch, approved snapshot execution, and rollback cleanup effects. The hook hidden-command shells stay here; do not add a hookruntime/agententry edge. |
| `internal/runtime/darwin` | `native_launch*.go`, `agent_launch.go`, `runner.go`, `cmd/hazmat-launch` interface | Native session admission, policy file lifecycle, launch-helper invocation, cleanup. |
| `internal/runtime/docker` | `sandbox.go` runtime portions | Docker Sandbox readiness, approval, creation, launch, cleanup. |
| `internal/runtime/linux` | future Linux native launch runtime | Empty or plan-only until `MC_LinuxNativeLaunch` covers concrete runtime behavior and a Linux helper implementation exists. |
| `internal/agententry` | `main.go` hidden commands `_connect`, `_git_ssh_transport`, `_git_https_credential`; minimal helper command shells in `git_ssh.go`, `git_https_credentials.go` | Agent-side in-containment helper entrypoints. They are not frontend rendering code and must not import frontend, setup, backup, diagnostics, or broad host-runtime packages. Host-side agent execution goes through `internal/hostexec`. |
| `internal/hostexec` | host execution primitives in `exec.go` (`sudo`, `newSudoCommand`, `sudoOutput`, `sudoWriteFile`, `sudoAppendFile`, `newAgentCommand`, `asAgent*`) | Shared low-level host execution and privilege transition. Importable by setup, native/Docker runtime, backup runtime, harness runtime, credential runtime, and diagnostics. Owns no policy; never imports frontend, planners, or `internal/agententry`. |
| `internal/diagnostics` | `test.go`, `stackcheck.go`, `exec.go:agentTCPConnect()` | Effectful check and stackcheck probes: SBPL builds, pfctl/DNS/firewall checks, fd-isolation tests, and contained smoke workflows. Governed by the specs for the probes they exercise. |
| `internal/state` | `state.go` | Host-owned `state.json` read/write, setup/migration/harness lifecycle metadata persistence, and rollback/migration state transitions. |
| `internal/setup/darwin` | `init*.go`, `rollback*.go`, `sudoers.go`, `native_account*.go`, `native_service*.go` | Host setup and rollback runtime after model-approved package split. |
| `internal/frontend/cli` | `main.go`, command files, `config.go` Cobra command handlers, rendering, prompts | CLI commands, status text, explain rendering, compatibility flags, shell completion. |
| `internal/legacy` | temporary wrappers in `package main` | Compatibility shims during movement. Must shrink phase by phase and stay behavior-equivalent. |
| `internal/testfixtures` | golden fixtures and shared test fixture helpers | Test-only fixtures. No production imports. |

## Remote Scope For This Split

Remote/orchestrated execution is not in the first implementation wave, but the
package split should be shaped so the future remote envelope does not need to
fork Hazmat's local planning model.

The remote envelope plan in
[2026-06-02-remote-launch-envelope-schema.md](2026-06-02-remote-launch-envelope-schema.md)
names the broad future needs:

- schema versioning and producer/consumer skew rules
- canonical serialization
- integrity verification
- replay defense
- worker identity binding
- worker-local path mapping
- capability-gap admission
- credential handle lifecycle
- cleanup proof shape
- record classification

This split should take the low-hanging pieces that help both local and remote
without claiming remote execution:

| Low-hanging choice | Why it helps local Hazmat | Why it helps future remote |
| --- | --- | --- |
| Versioned plan DTOs | Makes explain/golden output explicit. | Gives envelope producers stable input shapes. |
| Explicit host facts input | Keeps planners pure and easier to test. | Lets control planes provide scheduler/worker facts later. |
| DTO-to-validated-type rule | Prevents saved JSON from bypassing constructors. | Prevents remote API JSON from becoming authority. |
| Redaction-safe descriptors | Keeps CLI JSON safe. | Keeps control-plane records from carrying raw secrets. |
| Capability-gap taxonomy | Gives CLI precise failure messages. | Gives worker admission a future shared rejection vocabulary. |
| Prepared artifact variants | Keeps local runtimes from accepting mixed artifacts. | Gives remote envelopes a future single-artifact rule. |

The first package split should not implement remote signing, worker admission,
remote credential handles, replay storage, worker cleanup proofs, or a
`runtime/remote` runner. It can reserve naming and keep `KindRemoteEnvelope`
gap-gated and non-executable. A later remote bead can then add the missing
model and runtime without changing the local planner's authority semantics.

```mermaid
flowchart LR
    local["Local hazmat frontend"]
    request["Validated request"]
    planner["Pure planner"]
    contract["Containment contract"]
    backend["Backend gaps/artifacts"]
    remoteFuture["Future remote envelope"]
    localRuntime["Local runtime"]

    local --> request
    request --> planner
    planner --> contract
    planner --> backend
    contract --> localRuntime
    backend --> localRuntime
    planner -. future compatible .-> remoteFuture
    backend -. gap-gated .-> remoteFuture

    style remoteFuture fill:#ffe,stroke:#a80,color:#000
```

## Frontend API Shape

The reusable API should let a frontend do this without knowing SBPL ordering,
Docker routing internals, or credential materialization details:

```go
req, err := sessionrequest.New("codex", projectRoot).
    WithReadOnly(extraDocs).
    WithNetwork(sessionmeta.NetworkDefault).
    WithIntegrationHints(hints).
    Build()
if err != nil {
    return err
}

plan, err := sessionplanner.Plan(ctx, req, facts)
if err != nil {
    return err
}

if preview {
    return renderer.Render(plan.Contract, plan.Backend)
}

prepared, err := runtime.Prepare(ctx, plan, runtimeTarget)
if err != nil {
    return err
}

return runtime.Launch(ctx, prepared)
```

`facts` must be explicit. A pure planner should not reach into `$HOME`, call
`os/user`, inspect Docker, read `runtime.GOOS`, or read global config by itself.
The frontend or a host-facts package can collect those values before calling
the planner. Target GOOS/platform is a required fact, even for local explain
rendering, so cross-platform planning cannot silently depend on the host running
the command.

## Runtime Admission Shape

```mermaid
sequenceDiagram
    participant Frontend
    participant Planner
    participant Compiler
    participant Runtime
    participant Harness

    Frontend->>Planner: ValidatedRequest + explicit facts
    Planner-->>Frontend: Plan + Contract + BackendPlan
    Frontend->>Compiler: containment.Contract
    Compiler-->>Frontend: one backend artifact
    Frontend->>Runtime: PreparedLaunch + credential delivery plan
    Runtime->>Runtime: admission checks
    Runtime->>Harness: launch contained process
    Harness-->>Runtime: exit status
    Runtime-->>Frontend: result + cleanup proof
```

Admission checks differ by runtime, but the common contract is the same:

- validate the containment contract again
- verify backend/artifact kind match
- reject unaccepted capability gaps
- materialize only scoped credential grants
- build cleanup before launch
- emit only classified result and cleanup records
- fail closed on missing runtime capability

`PreparedLaunch` must become a real authority type before Phase G runtimes
consume it. Its authority-bearing fields should be unexported, with a separate
JSON DTO for rendering or persistence, so callers cannot hand-assemble multiple
artifact variants or bypass `NewPreparedLaunch` gap checks.

The separate DTO must also define its disclosure scope. The current Darwin
artifact can contain full SBPL policy text and resolved host paths; serialization
for explain, logs, saved plans, or future envelopes must be reviewed under the
record-classification decision instead of automatically mirroring authority
fields.

## Import Boundary Rules

The package split needs automated guards, not just documentation. Add a
structural import-boundary test before large movement begins.

| Package class | Forbidden imports |
| --- | --- |
| Pure contracts/planners | `os/exec`, `net/http`, Cobra, terminal UI, `sudo`, host probes including `runtime.GOOS`, runtime packages, setup packages, backup runtime, `cmd/*`. |
| DTO/schema packages | Runtime packages, credential delivery, host mutation apply code. |
| Compilers | Frontend packages, prompts, Cobra, direct process launch. |
| Runtimes | Frontend rendering, Cobra, unvalidated DTO packages as authority. |
| Credential descriptors | Secret-store, broker, materialization, or file-copy runtime packages. |
| Agent entrypoints | Frontend rendering, prompts, broad setup/backup runtimes, unvalidated config as authority. |
| Diagnostics | Reusable pure planners as effect sinks; diagnostics may call planners, but planners must never import diagnostics. |
| Frontends | No direct use of low-level compiler internals when a planner/runtime facade exists. |

The guard should use `go list -deps -json` from the start so it catches
transitive imports and aliases:

```text
scripts/check-import-boundaries.sh  # or a small Go test wrapper
  pure packages cannot import effect packages
  frontend packages cannot be imported by libraries
  compilers cannot import runtime launchers
  credential descriptor packages cannot reach secret materialization code
  pure planner packages cannot read runtime.GOOS or other host facts directly
  hidden agent entrypoints cannot import frontend rendering or setup packages
```

## Invariant Ownership After Split

| Invariant | Owning package after split | Governed specs/gates |
| --- | --- | --- |
| Deny-zone project/read/write rejection | `pathpolicy` + `sessionrequest` | `MC_TierPolicyEquivalence`, `MC_Tier3LaunchContainment`, path/session tests. |
| Non-omittable credential-deny floor | `containment` | `MC_SeatbeltPolicy`, containment tests, backend compiler tests. |
| SBPL section order | `containment/darwin` | `MC_SeatbeltPolicy`, SBPL goldens. |
| Native launch fd cleanup | `internal/runtime/darwin` + `cmd/hazmat-launch` | `MC_LaunchFDIsolation`, helper tests. |
| Preview-vs-launch mutation behavior | `hostmutations` | `MC_SessionPermissionRepairs`, explain/launch tests. |
| Setup/rollback agent containment and firewall/sudoers ordering | `internal/setup/darwin` + `internal/state` | `MC_SetupRollback`, setup/rollback tests, TLC before step-order changes. |
| Version migration state safety | `internal/setup/darwin` + `internal/state` | `MC_Migration`, migration tests, TLC before version graph changes. |
| Credential descriptor/delivery mode matching | `credentials` + `internal/credentialruntime` | `MC_SecretStoreRecovery`, `MC_CredentialCapabilityLifecycle`, credential registry tests, credential-regression hook. |
| Credential descriptors cannot reach materialization | `credentials` descriptor package plus import-boundary guard | `MC_SecretStoreRecovery`, `MC_CredentialCapabilityLifecycle`, `go list -deps` import guard, credential-regression hook. |
| Git SSH routing is deterministic and does not cross keys | `configmodel` + `internal/credentialruntime` + `internal/agententry` | `MC_GitSSHRouting`, config/git-ssh tests. |
| Harness lifecycle metadata cleanup and state persistence | `harnesses` (pure metadata/plans) + `internal/harnessruntime` + `internal/state` (effect/persistence) | `MC_HarnessLifecycle`, state tests, harness lifecycle tests. The pure `harnesses` package must not import `internal/state`. |
| Backend capability gaps | `sessionbackend` | backend plan goldens, `NewPreparedLaunch` tests. |
| Prepared launch artifacts cannot be forged before runtime admission | `sessionbackend` authority type plus separate DTO | `NewPreparedLaunch` tests, import/API review before Phase G. |
| Prepared launch DTOs do not over-disclose policy/path details | `sessionbackend` DTO plus record-classification review | DTO golden review, future remote model before envelope serialization. |
| Docker launch containment | `containment/docker` + `internal/runtime/docker` | `MC_Tier3LaunchContainment`, launch-spec goldens. |
| Linux native launch remains plan-only until modeled runtime exists | `containment/linux` + `internal/runtime/linux` | `MC_LinuxNativeLaunch`, compile-only tests, no effectful runtime before model update. |
| Restore never overwrites without a prior snapshot and sessions snapshot before launch | `backup` + `internal/backupruntime` | `MC_BackupSafety`, backup/restore tests, launch-path snapshot tests. |
| Repo-local hook execution uses approved immutable content and pinned paths | `hooks` + `internal/hookruntime` | `MC_GitHookApproval`, hook approval/runtime tests. |
| Check and stackcheck probes exercise governed boundaries without owning policy | `internal/diagnostics` | `MC_SeatbeltPolicy`, `MC_LaunchFDIsolation` (seatbelt/fd probes), `MC_SetupRollback`, `MC_Migration` (pf-firewall/DNS-blocklist boundary probes), diagnostics smoke tests. Note: live network-*enforcement* correctness (pf block, DNS resolution) has no governing TLA spec; only step ordering is modeled, so the live probes are the sole check there. |
| In-containment helper commands stay agent-side and narrow | `internal/agententry` | `MC_GitSSHRouting` (`_git_ssh_transport`), `MC_CredentialCapabilityLifecycle` (`_git_https_credential`), credential helper tests. Hook dispatch/fallback stays in `internal/hookruntime` under `MC_GitHookApproval`; no hookruntime/agententry import edge is allowed. |
| Remote compatibility stays non-executable | `sessionbackend` + versioned plan DTOs | `GapRemoteLaunch`; new remote model before execution. |

## Migration Strategy

This must move in small, equivalence-proved beads. Every phase must keep
package-main compatibility shims green where they exist, preserve current CLI
entrypoints until their release path is proven, and be independently revertible
without leaving package imports half-moved.

The intended sequence is:

### Phase A: Guardrails before movement

- Add the `go list -deps -json` import-boundary guard for current pure
  packages.
- Add a package dependency diagram check to docs or CI output.
- Record remote compatibility as a non-runtime constraint: versioned DTOs,
  explicit facts, redaction-safe descriptors, and gap taxonomy only.
- Keep all existing goldens green.
- No behavior changes.

### Phase B: Frontend shell split

- Move Cobra commands and renderers into `internal/frontend/cli`.
- Move the binary entrypoint to `cmd/hazmat`.
- Keep compatibility wrappers if release tooling expects the old module-root
  shape during transition.
- Update operational touchpoints that goldens do not cover: `Makefile`,
  `scripts/release.sh`, `scripts/e2e*.sh`, pre-push CLI smoke, install paths,
  and release build commands.
- No session semantics move in this phase.

### Phase C: Host facts package

- Extract explicit host/platform fact collection into `hostfacts`.
- Collect facts in the frontend or host inspection layer, then pass them into
  planners.
- Cover agent home, invoker home, target GOOS/platform, Docker availability,
  kernel/platform probes, harness installed status, and integration marker
  facts.
- No planner may call `os.UserHomeDir`, `os/user`, `runtime.GOOS`, Docker
  probes, or harness installation checks directly.

### Phase D: Validated request package

- Extract `sessionrequest` around existing `pathpolicy` constructors.
- Route `resolveSessionConfig` through it as a compatibility shim.
- Preserve the exact rejected-input set unless the model is updated first.
- Re-run `MC_TierPolicyEquivalence` and `MC_Tier3LaunchContainment`.

### Phase E: Planner expansion

- Expand `sessionplanner` to own the full side-effect-free plan:
  contract plan, backend plan, mutation preview, credential descriptors,
  harness requirements, and warnings.
- Remove planner/rendering fallbacks to `runtime.GOOS`; target platform comes
  from `hostfacts`.
- Ensure planner outputs are versioned and canonical enough for local goldens
  and future remote envelope inputs, without adding remote execution.
- Keep launch and explain goldens byte-identical.
- Do not move credential delivery or mutation apply code yet.

### Phase F: Backend compiler packages

- Move Darwin SBPL compilation into `containment/darwin`.
- Move Docker spec compilation into `containment/docker`.
- Keep runtime execution in `package main` or internal runtime adapters until
  prepared artifact boundaries are audited.
- Re-run governed specs and all goldens.

### Phase G: Prepared launch authority

- Make `PreparedLaunch` an authority type with unexported artifact fields.
- Add a separate DTO for JSON/rendering/persistence.
- Decide and test the DTO disclosure scope for SBPL policy text and resolved
  host paths before using it in logs, saved plans, or future envelopes.
- Require all runtimes to receive values constructed by `NewPreparedLaunch`.
- This phase must land before any runtime package accepts `PreparedLaunch`.

### Phase H: Config, credentials, and harnesses

- Move pure config schema and SSH routing validation into `configmodel`; move
  `config.go`'s Cobra command handlers to `internal/frontend/cli` and its
  cloud-credential persistence to `internal/credentialruntime`.
- Split credential registry descriptors into `credentials` and credential
  delivery effects into `internal/credentialruntime`.
- Enforce that descriptor packages cannot import secret store, broker,
  materialization, or file-copy runtime code.
- Split harness metadata/plans (`harnesses`, pure) from install/update/uninstall
  effects and state writes (`internal/harnessruntime`). The pure `harnesses`
  package must not import `internal/state`; the state writes
  (`update/removeHarnessState`) move with the runtime half.
- Preserve `MC_HarnessLifecycle`, `MC_GitSSHRouting`,
  `MC_SecretStoreRecovery`, and `MC_CredentialCapabilityLifecycle`.
- Add explicit DTO-to-validated-type tests for any serialized credential or
  harness lifecycle artifact.

### Phase I: Runtime and agent entry packages

- Move effectful native and Docker launch code behind `internal/runtime/darwin`
  and `internal/runtime/docker`. The frontend invokes them via an explicit
  runtime-selection facade (`cli -> runtime`); the launch runtimes are not
  orphan nodes.
- Split `exec.go`: privileged host and agent maintenance primitives
  (`sudo*`, `newSudoCommand`, `newAgentCommand`, `asAgent*`) move to
  `internal/hostexec`, which setup, runtime, backup runtime, harness runtime,
  credential runtime, and diagnostics import. Diagnostic helpers such as
  `agentTCPConnect()` move with diagnostics. Do not assign all of `exec.go` to
  the narrow `internal/agententry`.
- Move the agent-side re-exec helpers `_connect`, `_git_ssh_transport`, and
  `_git_https_credential` into `internal/agententry`.
- **Resolved hook-home decision.** Keep `_git-hook-wrapper`,
  `_git-hook-dispatch`, `_git-hook-fallback`, and their dispatch logic in
  `internal/hookruntime`. They depend on hookruntime-local
  approval/snapshot/drift validation and remain governed by
  `MC_GitHookApproval`; do not create a hookruntime/agententry import edge.
- Keep agent entrypoints narrow: no frontend rendering, setup runtime, backup
  runtime, or unvalidated config authority imports.
- Runtimes accept only `PreparedLaunch`, scoped credential delivery plans, and
  cleanup policy.
- Create `internal/runtime/linux` as an empty/plan-only package; it stays
  plan-only until separately modeled under `MC_LinuxNativeLaunch`. Remote
  runtime is outside this split and needs its own design/model bead.

### Phase J: Backup and hooks

- Split backup plans from `internal/backupruntime` only with
  `MC_BackupSafety` re-run and explicit preservation of both "snapshot before
  overwrite" and `preSessionSnapshot()` ordering before session launch.
- Split repo-local hook contracts from `internal/hookruntime` only with
  `MC_GitHookApproval` re-run and explicit preservation of approved immutable
  snapshot execution, pinned `core.hooksPath`, drift refusal, and rollback
  cleanup.
- These are governed effect surfaces, not routine data-package extractions.

### Phase K: Setup and rollback

- Split setup/rollback only after a model-aware design bead. This code is
  too sensitive to move opportunistically.
- Move `state.go` behind `internal/state` as part of, or before, this phase
  only when setup, migration, and harness lifecycle callers have compatibility
  shims.
- `MC_SetupRollback`, `MC_Migration`, and `MC_HarnessLifecycle` govern the move.

### Phase L: Diagnostics and stackcheck

- Move `test.go` and `stackcheck.go` into `internal/diagnostics` after the
  probed boundaries have stable facades.
- Diagnostics may call planners, compilers, and runtimes as a client; those
  packages must never import diagnostics.
- Re-run the governed specs for any probe whose setup or ordering changes,
  especially `MC_SeatbeltPolicy` and `MC_LaunchFDIsolation`.

## Data Flow After Split

```mermaid
flowchart LR
    flags["CLI/API input"]
    request["sessionrequest.ValidatedRequest"]
    facts["Explicit host facts"]
    plan["sessionplanner.Plan"]
    contract["containment.Contract"]
    backend["sessionbackend.Plan"]
    selector["Frontend/runtime compiler selection"]
    compiler["Backend compiler"]
    prepared["sessionbackend.PreparedLaunch"]
    runtime["runtime admission"]
    result["Result + cleanup proof"]

    flags --> request
    facts --> plan
    request --> plan
    plan --> contract
    plan --> backend
    contract --> selector
    backend --> selector
    selector --> compiler
    compiler --> prepared
    backend --> prepared
    prepared --> runtime
    runtime --> result
```

## Frontend Reuse Flow

```mermaid
flowchart TB
    subgraph frontendA["hazmat CLI"]
        cobra["Cobra commands"]
        text["Text/JSON renderers"]
    end

    subgraph frontendB["Future desktop/local API"]
        ui["UI or RPC handlers"]
        apiRender["Structured responses"]
    end

    common["Shared request/planner/contracts"]
    runtimes["Local runtimes"]

    cobra --> common
    text --> common
    ui --> common
    apiRender --> common
    common --> runtimes
```

The frontend-specific code may render different output, but it should not fork
policy. The same validated request and planner APIs should determine authority.

## Package Split Risks

| Risk | Mitigation |
| --- | --- |
| Moving code hides a semantic change. | Golden fixtures, affected TLC specs, and small commits per boundary. |
| Public DTO fields let callers bypass constructors. | Separate DTOs from authority types; runtime accepts only validated types. |
| Runtimes import frontend state or globals. | Import-boundary guard and explicit runtime inputs. |
| Pure planners silently depend on the machine running the command. | Target GOOS/platform is a required `hostfacts` field; guard against `runtime.GOOS` in pure packages. |
| Credential delivery leaks into planner. | `credentials` split into descriptors/plans vs delivery runtime. |
| Credential descriptors reach materialization code through transitive imports. | `go list -deps -json` guard for `credentials` against `internal/credentialruntime`, secret-store, broker, and file-copy code. |
| Hidden agent-side re-exec commands are mistaken for CLI frontend code. | Home `_connect`, `_git_ssh_transport`, and `_git_https_credential` in `internal/agententry`; keep hook dispatch/fallback in `internal/hookruntime` under `MC_GitHookApproval` and forbid a hookruntime/agententry edge. |
| Backup snapshot ordering gets split across runtime boundaries. | Keep `preSessionSnapshot()` with `internal/backupruntime` and re-run `MC_BackupSafety` before movement. |
| Harness state writes (`update/removeHarnessState`) land in the pure `harnesses` package, forcing a contract->runtime import. | Home install/uninstall effects and state writes in `internal/harnessruntime`; keep `harnesses` pure; draw `harnessRuntime -> harnesses`/`internal/state`. |
| Broad `sudo`/`asAgent` host primitives get swept into the narrow `internal/agententry`, inverting layering. | Split `exec.go`: privileged `sudo*` and `asAgent*` primitives in shared `internal/hostexec`; diagnostics-only helpers stay with diagnostics; `internal/agententry` owns hidden command handlers only. |
| Launch runtimes have no modeled inbound caller, so the graph never shows the CLI launching a session. | Frontend invokes `internal/runtime/darwin`/`docker` via a runtime-selection facade; draw `cli -> runtime`. |
| Diagnostics/check probes become a policy owner by accident. | Keep diagnostics as effectful clients of planners/compilers/runtimes; never import diagnostics from reusable packages. |
| State persistence is stranded between harness, setup, and migration packages. | Move `state.go` behind `internal/state` only with `MC_HarnessLifecycle`, `MC_SetupRollback`, and `MC_Migration` coverage. |
| Remote scope creeps into the local split. | Keep `GapRemoteLaunch`; define compatibility data only; model admission before runtime code. |
| Setup/rollback order changes during package movement. | Do not split setup/rollback until a model-first bead. |
| Tests pass but backend equivalence weakens. | Keep launch/SBPL/backend goldens and re-run affected TLA specs. |

## First Implementation Beads To Consider

These are proposed only after audit feedback, in phase order:

1. **Import boundary guard.** Add `scripts/check-import-boundaries.sh` or a Go
   test using `go list -deps -json`. No code movement.
2. **CLI frontend shell.** Move Cobra/rendering into `internal/frontend/cli`
   while keeping behavior and command output unchanged, including release and
   smoke-test touchpoints.
3. **`hostfacts` extraction.** Make target GOOS/platform and other host probes
   explicit inputs; remove pure-planner `runtime.GOOS` fallbacks.
4. **`sessionrequest` extraction.** Create the validated request package around
   existing `pathpolicy` constructors and compatibility shims.
5. **Planner facade expansion.** Centralize side-effect-free plans and add
   versioned DTO fixtures suitable for local explain and future envelope input.
6. **Compiler package split.** Move Darwin SBPL and Docker launch-spec
   compilation to `containment/*` packages with byte-identical goldens.
7. **`PreparedLaunch` authority type.** Make artifacts unforgeable and define
   separate DTO disclosure scope before any runtime consumes it.
8. **Config/credential/harness split.** Move pure SSH routing validation to
   `configmodel`, `config.go` handlers to `internal/frontend/cli` and its cloud-
   credential persistence to `internal/credentialruntime`, descriptors to
   `credentials`, delivery effects to `internal/credentialruntime`, harness
   metadata/plans to `harnesses`, and harness install/state-write effects to
   `internal/harnessruntime`.
9. **Runtime and agent-entry split.** Move native/Docker runtimes under
   `internal/runtime/*` (invoked via a runtime facade), split `exec.go` into
   `internal/hostexec` (sudo/asAgent primitives), `internal/diagnostics`
   (diagnostic probes), and `internal/agententry` (hidden command handlers).
   Hook hidden-command handlers stay in `internal/hookruntime`; no
   hookruntime/agententry edge is introduced.
10. **Setup/rollback, backup, hooks, state, diagnostics follow-ups.** Split these
   only under their governing specs (`MC_SetupRollback`, `MC_Migration`,
   `MC_BackupSafety`, `MC_GitHookApproval`, `MC_HarnessLifecycle`) once the
   earlier facades are stable. Setup/rollback requires a model-aware design bead.

## Audit Questions

- When, if ever, should internal runtime packages become public for another
  frontend?
- Should `sessionrequest` be separate, or should it live inside
  `sessioncontract` until the API stabilizes?
- What disclosure scope should the `PreparedLaunch` DTO allow for SBPL policy
  text and resolved host paths?
- Which remote-compatible choices are cheap enough for v0: versioned DTOs,
  explicit facts, redaction-safe descriptors, gap taxonomy, or canonical test
  fixtures?
- Which remote properties must stay deferred: signing, replay, worker identity,
  worker-local path mapping, credential handles, cleanup proof format, record
  classification, or all of them?
- Should setup/rollback stay in `package main` longer than other runtime code
  because it is strongly model-governed?
- Should `internal/state` move with harness lifecycle first or wait for the
  setup/rollback phase?
- What is the minimum import-boundary guard that reviewers trust before large
  movement begins?
- Where should the package API stability line be drawn for external users:
  all top-level packages, only contracts/planners, or none until v1?

## Acceptance Bar For The Split

A package movement is acceptable only when all of these are true:

- Existing CLI behavior and user-visible output are unchanged unless the bead
  explicitly declares a behavior change.
- Pure packages remain side-effect-free and cross-platform.
- Target platform and other host facts are explicit inputs, not hidden reads.
- Runtime packages fail closed when given invalid or gap-bearing artifacts.
- Hidden agent entrypoints, diagnostics, backup runtime, hook runtime, and state
  IO stay under `internal/` until reviewed as stable APIs.
- Wire DTOs are converted through parse/validate constructors before authority
  use.
- Governed specs are updated or re-run according to `tla/VERIFIED.md`.
- Golden fixtures are unchanged or the diff is reviewed line-by line.
- Documentation identifies the new invariant owner before code lands.
