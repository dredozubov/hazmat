# Problem 17 — Service Harness Lifecycle

**Status:** design model for future OpenHands-style service harnesses,
service-shaped proxy frontends, and configuration-only attachment to externally
managed OpenAI-compatible endpoints. Related designs:
[Service-Oriented Harness Boundary](../docs/plans/2026-06-12-service-harness-boundary-design.md)
and
[OpenHands Harness Candidate Evaluation](../docs/plans/2026-06-12-openhands-harness-evaluation.md).

## Problem Statement

Foreground harnesses are one contained process with one stream. OpenHands-style
platforms are different: they may need a service process, local UI/API attach
point, WebSocket stream, Docker-backed workspace, health check, service logs,
typed credentials, and crash cleanup. Treating that as another quick CLI wrapper
would skip the hard parts.

This model defines the first Hazmat service-harness, service-proxy, and external
API endpoint lifecycle boundary before adapter code lands:

1. stale service residue from a prior Hazmat crash is recovered or recorded
   before a new service starts;
2. unsupported features fail closed before credentials, service start, attach
   authority, or user-visible attach details;
3. session metadata exists before any service-owned side effect;
4. typed credentials can be materialized only after a valid service plan;
5. service attach waits for readiness evidence;
6. attach points are local only, and localhost ports require a per-session
   token;
7. native containment never passes host Docker socket authority or starts a
   container-requiring service;
8. terminal service, attach, credential, or stale residue is removed or covered
   by a recorded cleanup failure.
9. an external API endpoint requires one complete typed URL/token
   configuration, reaches readiness without a host process start or local bind,
   and never creates service residue owned by Hazmat.

Stdio MCP is not service-shaped. It launches as a foreground child process and
reuses the launch/fd-isolation boundary rather than this service lifecycle.
A Hazmat-owned local API proxy and future HTTP MCP proxy are service-shaped and
use this model: they bind a local attach point only after metadata, token
policy, typed credential materialization, readiness, and cleanup obligations
are satisfied. An already-running OpenAI-compatible endpoint takes a separate
branch: Hazmat validates its explicit URL/token pair, records metadata,
materializes the typed token, and attaches the contained harness without
starting, probing, or owning an endpoint process.

## Model

`MC_ServiceHarnessLifecycle` uses a linear phase model:

```text
0 prior-residue recovery gate
1 unsupported-feature gate
2 credential materialization
3 service start
4 health check
5 ready
6 attached
7 service/attach cleanup
8 credential cleanup
10 terminal
```

The external endpoint branch skips phases 3-4 as side-effecting operations:
phase 3 accepts the already-complete configuration and moves directly to ready
at phase 5. It does not set `serviceStarted`, `serviceRunning`, or
`attachAuthorityActive`.

The request dimensions are deliberately small but cover the unsafe boundaries:

- service kind: ordinary service harness, Hazmat-owned API proxy, HTTP MCP
  proxy, or external API endpoint
- backend: native, Docker Sandbox, or VM
- attach kind: stdio, Unix-domain socket, localhost port, external HTTP
  endpoint, or LAN-visible port
- token policy: none or session token
- credential kind: none, typed, or untyped
- whether the service requires container authority
- whether the request asks for host Docker socket, host profile import,
  persistent daemon mode, browser automation, or integration env passthrough
- service start outcome and health outcome

The external endpoint shape is valid only when its attach kind is
`external-http`, its credential is typed, and its token policy is
`session-token`. Missing, partial, untyped, or structurally invalid
configuration is rejected at the unsupported-feature gate before metadata or
credential delivery. Service start and health outcomes are fixed to
non-side-effecting placeholders for this branch to avoid inflating the state
space with meaningless combinations.

The model also chooses prior residue at `Init`. Prior residue is always paired
with prior metadata in this model, because service residue without metadata is a
preexisting bug rather than a recoverable service-session state. Recovery can
succeed and let the new plan proceed, or fail and terminate with a recorded
cleanup failure before the new service starts.

## Invariants

| Invariant | Meaning |
|-----------|---------|
| `PriorResidueHasMetadata` | stale service, credential, or attach residue is recoverable by recorded metadata |
| `SideEffectsHaveMetadata` | current service start, credential materialization, attach authority, and attach details never happen before current metadata exists |
| `AttachAuthorityHasMetadata` | local bind/attach authority cannot become active before metadata |
| `StartOnlyAfterPriorResidueHandled` | no new service starts while prior residue is still active |
| `UnsupportedRequestsFailClosed` | unsupported requests never reach current service side effects |
| `CredentialMaterializationGated` | only typed credentials can materialize, and only after a valid plan |
| `ReadyRequiresHealth` | readiness evidence implies a passed health check, and active ready/attached phases still have a running service |
| `ExternalEndpointNeverStartsService` | external endpoint attachment never starts or owns a service process or local bind authority |
| `ExternalEndpointReadinessIsConfigurationOnly` | external readiness requires metadata, the typed credential, and the complete URL/token attach shape |
| `AttachOnlyAfterReady` | attach cannot happen unless readiness evidence exists |
| `AttachDetailsAfterReady` | user-visible attach details are printed only after readiness |
| `AttachPolicyLocalOnly` | printed attach details are never LAN-visible |
| `ExternalEndpointAttachPolicy` | external attach details exist only for a complete typed external HTTP configuration |
| `LocalhostPortRequiresToken` | localhost-port attach requires a session token |
| `ProxyServiceAttachPolicy` | API proxy and HTTP MCP proxy service shapes use only UDS or localhost-port attach, with local attach policy satisfied |
| `NoHostDockerSocketExposure` | no started service ever received host Docker socket authority |
| `NoNativeContainerStart` | native containment does not start a service that requires container authority |
| `NoProfileDaemonBrowserOrEnvStart` | profile import, persistent daemon, browser automation, and integration env passthrough cannot reach service start |
| `TerminalResidueHandled` | terminal service/attach/credential/prior residue is gone or a cleanup failure is recorded |
| `RejectedRequestsHaveNoCurrentSideEffects` | feature-gate rejections are side-effect free |
| `CredentialRemovedOnlyAfterTypedPlan` | credential removal is only recorded for typed planned credentials |

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers auto \
  -config MC_ServiceHarnessLifecycle.cfg \
  MC_ServiceHarnessLifecycle.tla
```

- `Model checking completed. No error has been found.`
- `6,624,536 states generated`
- `2,702,168 distinct states found`
- `depth 10`
- `Finished in 13s`

## Interpretation

This proof is intentionally narrower than "OpenHands or proxy support is
implemented." It proves the lifecycle shape future service and service-proxy
adapters must preserve:

- recovery precedes new service start;
- service side effects are impossible before metadata;
- attach follows readiness;
- bind and token policy are part of the model, not doc-only advice;
- typed credential delivery and cleanup are accounted for;
- unsupported service features fail closed before side effects.

Recipe-only use through `hazmat exec` does not need this model because Hazmat is
only launching an ordinary foreground process. A first-class service adapter
does need this model and the later fake-service smoke suite.

Likewise, stdio MCP proxying can reuse foreground child-process launch semantics
plus the `MC_LaunchFDIsolation` boundary. Hazmat-owned API and future HTTP MCP
proxies need the service-start branch because they create a session-scoped
local attach point and bind cleanup obligations. An externally managed
OpenAI-compatible endpoint uses only the configuration/credential/attach branch
and is proved unable to reach service-start or bind-authority state.

## Change Rules

- Adding a first-class service harness ID, service-proxy kind, service lifecycle
  phase, service metadata field, port/socket attach policy, or crash-cleanup
  rule must update `MC_ServiceHarnessLifecycle.tla` first and re-run TLC before
  implementation.
- Changing the external API endpoint input pair, endpoint policy, credential
  delivery, or ownership boundary must update the external branch and re-run
  TLC before implementation.
- Supporting host Docker socket access, LAN-visible binds, browser automation,
  host profile import, persistent daemon mode, or untyped credentials requires a
  deliberate model change. The current model proves those requests fail closed.
- Adding container-requiring native services requires a separate backend model
  change; native sessions currently cannot satisfy that authority honestly.
- Live OpenHands smokes are not proof. They become release gates only after the
  fake-service lifecycle suite exists and this model still matches the intended
  adapter behavior.
