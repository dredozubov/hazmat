# Proxy Core Refactor Design

Status: Proposed decision
Date: 2026-07-01
Related issue: `sandboxing-atx5`

## Decision

Hazmat should add a protocol-neutral proxy substrate before implementing MCP
containment wrappers or inference API proxying. The substrate should expose
clean library surfaces for:

- planning and preparing a contained session;
- starting contained child processes or session-scoped services;
- materializing typed credentials and cleaning them up;
- binding local attach points with explicit authority;
- emitting redaction-safe proxy evidence.

MCP and inference API handling should be thin protocol frontends over that
substrate. They should not call Cobra command internals, build ad hoc session
configs, or duplicate credential/runtime cleanup code.

The first implementation should be a facade over the existing launch path, not a
flag-day rewrite. Existing commands should keep behavior while the reusable
library surfaces become available to proxy commands.

## Problem

Two near-term proxy use cases need the same lower-level machinery:

1. MCP containment wrappers. A local MCP server can be contained when Hazmat
   controls the server process launch. For stdio MCP, the strongest shape is a
   wrapper command that the contained harness starts as its MCP server. The
   wrapper launches the real server as a child, proxies JSON-RPC, and records or
   enforces policy on MCP messages.
2. Inference API proxying through Muginn. A contained harness should talk to a
   local session-scoped endpoint, while host-side Hazmat or Muginn handles
   upstream provider routing, credentials, logging policy, and streaming.

Today, Hazmat has the core ingredients, but they are not exposed as a clean
surface for proxy code:

- `sessionConfig` and `preparedSession` are root-package implementation details
  in `hazmat/session.go`.
- pure planning exists in `hazmat/sessionplanner` and `hazmat/sessionbackend`;
- runtime preparation and cleanup are buried in launch execution paths;
- service lifecycle exists in `hazmat/internal/serviceharness`;
- MCP configuration is deliberately manual-only and not imported wholesale.

Adding MCP or API proxy code directly on top of root command glue would create a
second launch stack. That would weaken the contract vocabulary and make future
extraction of Hazmat core libraries harder.

## Goals

- Give proxy features one reusable way to request a contained session.
- Keep side-effect-free planning separate from host mutation and launch.
- Keep protocol parsing out of containment and credential packages.
- Support both process-shaped proxies and service-shaped proxies.
- Preserve current launch behavior while migrating internals.
- Make the eventual open-source core package boundary clearer.

## Non-Goals

- Do not import host MCP configuration wholesale.
- Do not claim remote MCP execution is contained.
- Do not build a general persistent proxy daemon.
- Do not solve all provider-specific inference APIs in the first pass.
- Do not expose host Docker socket, browser automation, or profile import.
- Do not make MCP annotations trusted policy.

## Target Layers

### `sessionlaunch`

`sessionlaunch` should become the reusable launch-preparation surface. It should
wrap the current `sessionConfig` path first, then absorb implementation details
incrementally.

Responsibilities:

- normalize a launch request;
- resolve session planning inputs;
- select the backend;
- prepare session runtime artifacts;
- prepare native policy or backend artifacts;
- expose env pairs and cleanup handles;
- return redaction-safe plan/evidence DTOs.

It should not know about MCP, OpenAI, Muginn, HTTP, JSON-RPC, Cobra, terminal UI,
or harness config file formats.

Sketch:

```go
type LaunchRequest struct {
	Target      string
	ProjectDir  string
	ReadOnly    []string
	ReadWrite   []string
	Network     sessionmeta.NetworkMode
	Mode        sessionmeta.Mode
	Credentials []CredentialGrant
	Options     LaunchOptions
}

type PreparedSession struct {
	Plan        sessionplanner.Plan
	Backend     sessionbackend.Kind
	RuntimeEnv  []string
	SessionID   string
	RuntimeDir  string
	cleanup     func()
}

type Launcher interface {
	Prepare(context.Context, LaunchRequest) (PreparedSession, error)
	StartProcess(context.Context, PreparedSession, ProcessSpec) (Process, error)
	StartService(context.Context, PreparedSession, ServiceSpec) (Service, error)
}
```

The first version can use unexported adapters into existing root-package
functions. The important constraint is that new proxy code depends on this
surface rather than on command-specific launch helpers.

### `proxyruntime`

`proxyruntime` should be protocol-neutral. It owns the mechanics shared by MCP
and API proxying:

- proxy session metadata;
- evidence event shape;
- local bind policy;
- stdio process plumbing;
- localhost or Unix-domain-socket service attach;
- session token generation and redaction;
- lifecycle cleanup.

Responsibilities:

- start a contained child process for stdio-style proxies;
- start a session-scoped local service for HTTP-style proxies;
- fail closed on unsupported attach shapes;
- emit structured proxy events;
- expose only redaction-safe data to callers.

It should not parse MCP messages or provider API payloads. Protocol frontends
should pass normalized policy events into it.

Sketch:

```go
type ProxyKind string

const (
	ProxyKindMCPStdio ProxyKind = "mcp-stdio"
	ProxyKindLLMHTTP  ProxyKind = "llm-http"
)

type ProxySessionRequest struct {
	Kind       ProxyKind
	Session    sessionlaunch.LaunchRequest
	Downstream Downstream
	Attach     Attach
	Policy     PolicyRef
}

type Event struct {
	SessionID  string
	ProxyKind  ProxyKind
	Direction  string
	Operation  string
	Decision   string
	Redactions []string
}
```

### Protocol Frontends

Protocol frontends should be small packages:

- `mcpproxy` for MCP JSON-RPC;
- `llmproxy` for OpenAI-compatible HTTP/SSE first, with Muginn as the initial
  upstream routing target.

Each frontend owns protocol validation and forwarding. Neither owns session
launch, credential materialization, cleanup, or backend policy compilation.

## MCP Proxy Design

The first MCP proxy should support local stdio servers only.

User-facing shape:

```bash
hazmat mcp proxy --stdio -- <real-mcp-server> [args...]
```

Installed MCP config should point the harness at the wrapper command, not at the
real server directly. When the harness starts the wrapper inside a Hazmat
session, the wrapper and real MCP server inherit the Hazmat OS boundary. The
wrapper adds protocol evidence and policy on top of that boundary.

MVP behavior:

- read JSON-RPC from stdin;
- launch downstream MCP server as a contained child process;
- forward stdin/stdout faithfully;
- inspect and log `initialize`, `tools/list`, `tools/call`;
- allow or deny by server identity and tool name;
- preserve cancellation, errors, and server stderr behavior;
- fail closed on malformed JSON-RPC;
- never treat MCP annotations as enforcement.

The wrapper should distinguish three claims:

| Shape | Claim |
| --- | --- |
| stdio wrapper launches local downstream inside Hazmat | containment plus protocol mediation |
| local HTTP MCP server launched by Hazmat and exposed through local proxy | containment plus protocol mediation |
| remote HTTP MCP server behind Hazmat proxy | mediation only, not containment |

HTTP MCP should come later. It needs service lifecycle, local bind authority,
session token policy, and streaming fidelity. Remote MCP should be described as
credential/policy mediation only.

## Inference API Proxy Design

The first API proxy should be a session-scoped HTTP/SSE proxy for
OpenAI-compatible inference traffic routed through Muginn.

User-facing shape is still open, but the runtime shape should be:

1. Hazmat starts a host-side local proxy for one session.
2. The proxy binds only to a local attach point, preferably `127.0.0.1` with a
   per-session token or a Unix-domain socket where the harness supports it.
3. The contained harness receives only proxy base URL and session token.
4. Provider credentials stay host-side or Muginn-side.
5. Direct provider egress is denied or made unnecessary where the selected
   backend can enforce that honestly.

MVP behavior:

- OpenAI-compatible request forwarding;
- streaming SSE pass-through;
- bearer/session-token validation on inbound calls;
- upstream base URL configured as Muginn;
- request/response metadata logging with body logging disabled by default;
- redaction of auth headers, cookies, tokens, and provider keys;
- provider-specific unsupported endpoints fail closed with clear errors.

This is not MCP containment. It is inference egress brokering. It controls model
traffic and credentials, but it does not constrain arbitrary local tools unless
the session boundary and network policy also do that.

## Config Adapter Design

Proxy config adapters should be separate from proxy runtime:

- MCP adapters install or render harness-specific MCP config entries.
- API adapters inject base URLs, token env vars, or provider config for harnesses
  that support local API indirection.

Adapters must be explicit per harness. They should not copy host MCP config or
host provider config. They should render a new Hazmat-owned config entry from a
typed request.

Initial MCP adapter targets:

- Claude Code project/user MCP config;
- Codex MCP config;
- one editor harness after the stdio proxy is stable.

Initial API adapter targets:

- harnesses that accept OpenAI-compatible base URL and API key env vars;
- Muginn-controlled provider routing;
- no OAuth/profile import in the first pass.

## Evidence And Policy

Proxy evidence should be distinct from runtime authority. Evidence describes
what the proxy saw and decided; the session contract remains the host-enforced
boundary.

Minimum evidence fields:

- session ID;
- proxy kind;
- downstream identity;
- backend kind;
- attach kind;
- normalized operation;
- decision;
- reason;
- timestamp;
- redaction markers.

MCP operation examples:

- `initialize`;
- `tools/list`;
- `tools/call:<tool-name>`;
- `resources/list`;
- `resources/read`;
- `prompts/list`;
- `prompts/get`.

API operation examples:

- `POST /v1/responses`;
- `POST /v1/chat/completions`;
- `POST /v1/embeddings`;
- `stream:start`;
- `stream:end`;
- `upstream:error`.

Policy should start narrow:

- allow or deny by downstream identity;
- allow or deny by MCP tool name;
- allow or deny by HTTP route;
- require session token for local API proxy;
- redact request bodies by default.

Semantic policy can come later. The first version should prove the plumbing and
authority boundaries.

## Model And Governance Impact

This work touches governed areas:

- launch fd isolation;
- credential delivery;
- service lifecycle;
- local attach authority;
- network policy;
- cleanup obligations.

Implementation should therefore start with model/design updates where the
change crosses an existing proof boundary.

Recommended model path:

1. For stdio MCP, add a small design note that classifies it as a contained
   child-process proxy. If it uses existing foreground launch semantics and no
   new service attach, this may not require a new TLA+ service model.
2. For API proxy and HTTP MCP, extend `MC_ServiceHarnessLifecycle` or add
   `MC_ProxySessionLifecycle`. The model should prove:
   - metadata exists before bind;
   - credentials materialize only after a valid plan;
   - attach details print only after readiness;
   - local bind requires a token;
   - cleanup covers service, attach, and credential residue;
   - unsupported remote/daemon/profile-import requests fail before side effects.
3. If credential delivery gains proxy-mode credentials, update the credential
   capability lifecycle model before implementation.

Live smokes are not proof. They should be release gates only after hermetic fake
protocol tests and model alignment exist.

## Migration Plan

### Phase 1: Library Facade

Add `sessionlaunch` as a facade around existing launch preparation. It should
support plan-only and launch-capable modes, but initially delegate to current
root-package functions.

Acceptance:

- no behavior change in existing CLI commands;
- unit tests verify defensive copies and cleanup ordering;
- existing `hazmat explain` and launch tests still pass;
- no protocol-specific imports in `sessionlaunch`.

### Phase 2: Runtime Extraction

Move reusable runtime preparation into a package-level surface:

- temp/session runtime;
- harness auth runtime;
- Git HTTPS broker runtime;
- Git SSH broker runtime;
- cleanup composition;
- redaction-safe runtime env reporting.

Acceptance:

- existing native launch path uses the extracted surface;
- runtime cleanup still runs in reverse preparation order;
- no long-lived credentials become readable session files unless already modeled.

### Phase 3: Proxy Runtime

Add `proxyruntime` with process and service runners.

Acceptance:

- fake stdio process runner test;
- fake localhost service runner test;
- token redaction tests;
- cleanup-on-start-failure tests;
- no MCP or OpenAI imports.

### Phase 4: MCP Stdio Proxy

Add the stdio MCP wrapper and one explicit config renderer.

Acceptance:

- fake MCP server fixture;
- malformed JSON-RPC fails closed;
- `tools/list` and `tools/call` evidence is emitted;
- deny-by-tool works;
- downstream stderr is not mixed into JSON-RPC stdout;
- remote HTTP MCP is documented as out of scope.

### Phase 5: API Proxy Through Muginn

Add OpenAI-compatible HTTP/SSE proxy support.

Acceptance:

- fake Muginn/upstream server;
- streaming pass-through test;
- auth header redaction test;
- unsupported endpoint rejection test;
- contained harness receives only local proxy env;
- direct provider key is not exposed to the agent env in supported mode.

### Phase 6: Additional Adapters

Add more MCP and API config adapters only after the first two proxy paths are
stable.

Acceptance:

- each adapter has a narrow config renderer;
- no adapter imports host config wholesale;
- each adapter has golden config output tests.

## Testing

Default tests should remain hermetic and non-sudo:

- pure request normalization tests;
- planner DTO and disclosure-scope tests;
- prepared-launch defensive-copy tests;
- fake process runner tests;
- fake service lifecycle tests;
- JSON-RPC forwarding tests;
- SSE streaming tests;
- redaction tests;
- config renderer golden tests.

Approval-gated tests should be separate:

- live local stdio MCP wrapper smoke;
- live API proxy to Muginn smoke;
- native helper-backed launch smoke;
- any `hazmat check --full` or helper-backed validation.

Do not add these to the default unit suite.

## Open Questions

- Should the public command be `hazmat mcp proxy` or `hazmat proxy mcp`?
- Should the API proxy command be user-facing, or only an implementation detail
  activated by a harness adapter?
- Which first API-compatible harness should receive Muginn proxy injection?
- Does the first API proxy bind to localhost HTTP, Unix-domain socket, or both?
- Should proxy evidence reuse runtime authority trace storage, or get a separate
  event log namespace?
