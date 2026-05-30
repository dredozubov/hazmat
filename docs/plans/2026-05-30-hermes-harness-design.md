# Hermes Managed Harness Design

Status: Proposed
Date: 2026-05-30
Related research:
- https://github.com/NousResearch/hermes-agent
- https://github.com/NousResearch/hermes-agent/blob/main/SECURITY.md
- https://hermes-agent.nousresearch.com/docs/
- https://hermes-agent.nousresearch.com/docs/user-guide/security
- https://hermes-agent.nousresearch.com/docs/user-guide/features/tools
- https://hermes-agent.nousresearch.com/docs/user-guide/docker
- https://hermes-agent.nousresearch.com/docs/user-guide/features/skills
- https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp

## Position

Hazmat should support Hermes as an **experimental managed foreground harness**,
not as a session integration and not initially as an always-on gateway service.

The core reason is alignment of security models. Hermes' own security policy
states that the only load-bearing boundary against adversarial LLM behavior is
OS-level isolation, and that terminal-backend isolation does not confine code
running inside the Hermes Python process: skills, plugins, hooks, MCP
subprocesses, code execution, and other in-process behavior still run with the
agent's authority. Hazmat's native session model is a whole-process wrapper
around a harness process tree, so it is the right layer to contain Hermes when
Hermes is used for autonomous coding work.

This should not be implemented as a normal Hazmat integration. Integrations are
ergonomic overlays for stacks already launched through a harness: read-only
tool/cache grants, backup excludes, safe environment selectors, commands, and
warnings. Hermes is itself a harness runtime with state, credentials, skills,
memory, MCP servers, subprocesses, and optional gateway surfaces. Treating it as
an integration would blur the existing integration contract and create a path
for daemon, credential, and runtime behavior through the wrong abstraction.

The first supported shape should be:

```text
hazmat hermes [hazmat session flags] -- [hermes args...]
```

The command launches the Hermes CLI in the existing Hazmat session pipeline:
agent user, native seatbelt policy where available, credential deny rules,
network mode, optional Git/GitHub capabilities, backup/snapshot behavior, and
session cleanup.

Gateway mode, cron mode, dashboard/API exposure, and long-running supervised
Hermes services should be explicitly deferred until Hazmat has a service
lifecycle model.

## Goals

- Add a clear path for running Hermes as a contained autonomous assistant.
- Preserve Hazmat's existing harness/session contract.
- Avoid importing the user's host `~/.hermes` state by default.
- Keep Hermes credentials out of `/Users/agent` until a modeled credential
  surface exists.
- Make Hermes' own terminal backend a secondary concern; Hazmat should wrap the
  whole Hermes process, not only commands Hermes emits through its terminal
  tool.
- Keep Docker, MCP, skills, cron, and gateway behavior visible as capability
  decisions instead of incidental side effects.

## Non-Goals

- No `hazmat integration hermes`.
- No automatic migration from host `~/.hermes`.
- No automatic import from OpenClaw state through `hermes claw migrate`.
- No `hermes gateway` daemon management in the first slice.
- No dashboard/API port exposure in the first slice.
- No host Docker socket grant.
- No automatic passthrough of `~/.ssh`, `gh`, cloud SDKs, MCP tokens, provider
  keys, or messaging-platform credentials.
- No support for Hermes cron jobs that survive past the Hazmat session.
- No user-supplied harness plugin manifest.

## Fit With Current Hazmat Architecture

Hazmat currently has a fixed built-in harness set and a launch pipeline that is
aware of harness auth, provider API-key environment delivery, asset sync, Git
capabilities, GitHub token grants, Docker routing, network mode, snapshots, and
native launch policy.

Hermes fits the **harness** side of that architecture:

- It is a user-facing assistant runtime launched as a CLI command.
- It can perform code edits, shell execution, web/browser work, memory access,
  and delegation.
- Its highest-risk behavior happens inside its own process tree, not only in
  project build tools.
- It benefits directly from a dedicated macOS user and seatbelt policy.

Hermes does not fit the **integration** side:

- It owns durable state under `~/.hermes`, including `.env`, `config.yaml`,
  sessions, memories, skills, cron jobs, hooks, logs, and profile-specific home
  state.
- Its skills and MCP servers can execute code or spawn subprocesses.
- Its gateway and dashboard create network-exposed surfaces.
- Its terminal backends can introduce a second containment layer, including
  Docker and remote execution.

The implementation should therefore add `HarnessHermes` to the built-in harness
registry only after its lifecycle and credential surfaces are modeled. It should
not generalize into a user-provided harness plugin system as part of this work.

## Recommended User Model

The initial UX should be intentionally narrow:

```bash
hazmat hermes
hazmat hermes --network none
hazmat hermes -C /path/to/project -- chat --toolsets terminal,file
hazmat hermes --github -C /path/to/project -- chat
```

Hazmat flags remain Hazmat flags. Everything after `--` is forwarded to Hermes.
The command should make these facts obvious in the session contract:

- Hermes is running as the `agent` user.
- The host user's `~/.hermes` was not imported.
- Hermes is using a Hazmat-managed profile/state root.
- Host Docker is not available unless the user selected a containment mode that
  deliberately provides Docker semantics.
- Gateway, dashboard, and cron persistence are not part of the foreground
  harness contract.

If Hermes is not installed for the agent user, Hazmat should either print a
bootstrap hint or run an explicit `hazmat bootstrap hermes` command. It should
not run Hermes' curl-piped installer implicitly during a harness launch.

## State Layout

Hermes' default state root is `~/.hermes`. In Hazmat, the first slice should use
a dedicated managed state root instead of importing host state:

```text
/Users/agent/.hazmat/hermes/
  config.yaml
  .env                  # absent in v1 unless explicitly configured inside agent
  sessions/
  memories/
  skills/
  cron/
  hooks/
  logs/
  home/
```

Hazmat should set `HERMES_HOME=/Users/agent/.hazmat/hermes` for the session.
Keeping the root under a Hazmat-owned prefix makes it easier to explain that the
state is not the user's normal Hermes profile.

The first slice should not sync:

- host `~/.hermes`
- host `~/.agents/skills`
- host OpenClaw state
- host MCP config
- host provider keys
- host messaging credentials
- host command allowlists
- host persistent memories

Hermes skills are not simple prompt assets. The Hermes docs say skills live
under `~/.hermes/skills`, can include scripts, and can be modified or deleted by
the agent. They should therefore not join the existing harness asset sync
system until Hazmat has a Hermes-specific asset policy.

## Credential Model

Hermes credentials should be treated as a new credential surface, not as generic
files copied from the host. Relevant Hermes surfaces include:

- provider API keys in `.env`
- Nous Portal OAuth/token state
- messaging-platform bot tokens and allowlists
- MCP server env and OAuth token caches
- tool gateway keys
- browser/search/image/TTS provider keys
- skill-declared required environment variables
- SSH, GitHub, cloud, and database credentials inside Hermes' per-tool home

The v1 harness should avoid managed Hermes credential import. Users can run a
fresh contained Hermes setup inside the agent profile if they deliberately want
credentials to exist only in the contained agent account. That is compatible
with Hazmat's current account-isolation story, but it is not the same as
host-secret-store materialization.

Managed import/export should be a later slice with:

1. typed `credentialDescriptor` entries for Hermes credential groups
2. host-store locations under `~/.hazmat/secrets`
3. session-scoped materialization into the Hermes state root
4. harvest and cleanup after launch
5. crash recovery for residue
6. diagnostics that redact secret values
7. conflict archive behavior when host and agent copies diverge

Because this touches credential delivery and harness lifecycle, it must start
with the relevant TLA+ model and design note before implementation.

## Bootstrap Model

Hermes' public installer is a shell script fetched over HTTPS. Hazmat should not
silently run that installer as part of `hazmat hermes`.

Recommended bootstrap shape:

```bash
hazmat bootstrap hermes
```

The bootstrap command should:

- install into the agent user's managed tool area, not the host user's home
- avoid mutating host shell profiles
- pin or record the source version
- verify the installed `hermes --version`
- make the install idempotent
- avoid importing host `~/.hermes`
- report whether optional dependencies such as browser tooling are absent

If upstream does not publish signed releases or stable checksums, the bootstrap
design should say so plainly and initially support either:

- user-installed `hermes` on the agent account, detected by `PATH`, or
- a pinned Git clone plus deterministic Python environment created by Hazmat.

The exact supply-chain policy should match the existing bootstrap posture for
other harnesses: install is explicit, agent-owned, and auditable.

## Launch Pipeline

The Hermes foreground launch should reuse the existing session setup pipeline:

1. parse Hazmat flags and split Hermes args after `--`
2. resolve project root and requested read/write directories
3. apply explicit integrations for the project stack, not Hermes itself
4. apply network mode
5. apply optional Git SSH and GitHub capabilities
6. prepare backup/snapshot state
7. set Hermes-specific environment, including `HERMES_HOME`
8. launch the Hermes process as the agent user under native policy
9. clean up session-scoped materialized credentials after exit
10. leave durable Hermes agent-profile state only where the design explicitly
    allows it

The first slice should not configure Hermes' own Docker terminal backend
automatically. Running Hermes inside Hazmat already wraps the whole process
tree. If the user also enables Hermes' Docker backend, Hazmat must not grant the
host Docker socket by accident.

## Docker And Remote Backends

Hermes supports local, Docker, SSH, Singularity, Modal, and Daytona terminal
backends. This should be documented as an inner execution choice, not Hazmat's
outer boundary.

Recommended policy:

- `terminal.backend: local` is acceptable because the whole Hermes process is
  already inside Hazmat.
- `terminal.backend: docker` is allowed only when Docker access is explicitly
  available in the selected containment tier. Hazmat should not pass the host
  Docker socket into a native session.
- SSH, Modal, Daytona, and other remote backends are network capabilities. They
  require network access and credentials; Hazmat should not special-case them in
  v1.
- `--network none` should make remote/backend setup fail closed rather than
  being silently bypassed.

The docs should warn that inner Hermes terminal-backend isolation is not a
substitute for Hazmat's whole-process wrapper when skills, plugins, MCP, hooks,
or code execution are enabled.

## Gateway, Dashboard, And Cron

Hermes can run as a messaging gateway and can schedule cron jobs. Those are
valuable features, but they are not foreground harness behavior.

Hazmat should defer:

- `hazmat hermes gateway`
- supervised background gateways
- dashboard/API port exposure
- persistent cron jobs
- messaging-platform delivery
- multi-profile gateway orchestration

Supporting those safely requires a service lifecycle model:

- explicit `start`, `stop`, `status`, and `logs`
- process supervision that cannot outlive Hazmat unexpectedly
- port binding policy
- allowlist and auth-token policy
- state locking to prevent two gateways sharing one Hermes state root
- crash recovery
- teardown guarantees
- session contract output for listening surfaces
- network policy that distinguishes outbound provider traffic from inbound API
  or dashboard exposure

Until that exists, `hazmat hermes` should be a foreground command only. If the
user forwards `gateway` manually, Hazmat should either reject it with a clear
message or require an explicit escape flag such as `--allow-foreground-gateway`
that still exits when the foreground process exits.

## MCP, Skills, Hooks, And Plugins

Hermes' MCP and skills systems are powerful enough to deserve explicit audit
attention.

Policy for v1:

- Do not sync host MCP config.
- Do not sync host skills.
- Do not enable external skill directories by default.
- Do not pass host env wholesale to Hermes.
- Do not auto-install Hermes MCP catalog entries.
- Do not treat Hermes skill declarations as Hazmat env-grant authority.
- Keep Hazmat's normal project write boundaries as the filesystem boundary for
  file operations that escape through Hermes tools.

This preserves a simple rule: Hermes may use the tools and state that exist
inside the contained Hermes profile and the Hazmat session policy. Host user
assistant assets remain outside the v1 contract.

## TLA+ And Verification Requirements

Adding the command name and bootstrap stub alone may be simple code, but the
following changes touch verified areas and require model-first work:

- adding managed Hermes credential delivery
- adding session-scoped Hermes state materialization and harvest
- adding persistent mutation to setup/init
- changing rollback behavior for Hermes state
- adding service/gateway lifecycle
- changing launch file-descriptor behavior for Hermes IPC or API sockets
- changing seatbelt rules for new agent-home state roots

At minimum, the implementation plan should review:

- harness lifecycle
- credential capability lifecycle
- secret-store crash recovery
- setup/rollback
- seatbelt policy structure
- launch fd isolation

The design boundary for v1 should be chosen to avoid model churn where possible:
foreground harness launch, no managed credential import, no gateway service, no
new daemon setup, and no host-state sync.

## Testing Plan

Unit tests:

- `HarnessHermes` appears in the built-in harness registry.
- `hazmat hermes -- ...` forwards Hermes args after Hazmat flag parsing.
- `HERMES_HOME` points at the Hazmat-managed agent root.
- host `~/.hermes` paths are not selected as asset-sync roots.
- `--skip-harness-assets-sync` remains accepted but has no Hermes host assets in
  v1.
- Docker socket paths are not granted by the Hermes harness.
- gateway/dashboard/cron commands are rejected or warned according to the final
  CLI policy.

Policy tests:

- native policy allows the selected Hermes state root only as intended.
- credential deny rules still override any broad read grants.
- `--network none` is preserved through Hermes launch.
- optional Git/GitHub capabilities are still explicit.

Smoke tests:

- `hazmat hermes -- --version`
- `hazmat hermes -- chat --help`
- `hazmat hermes --network none -- --version`
- a fake `hermes` binary fixture that records `HERMES_HOME`, cwd, env, and args
  without requiring upstream installation
- a scratch project where Hermes can read/write only within the expected project
  and session write roots

Manual audit tests:

- run Hermes setup inside the contained profile and verify host `~/.hermes` is
  unchanged
- verify no host provider keys appear in Hermes subprocess env
- verify gateway/dashboard commands do not leave background processes after
  Hazmat exits
- verify MCP stdio server config is absent unless created inside the contained
  profile

## Documentation Plan

Add or update:

- `docs/harnesses.md`: list Hermes as experimental once implemented.
- `docs/usage.md`: show foreground CLI examples.
- `docs/compatibility.md`: record gateway, dashboard, cron, Docker backend, and
  MCP limitations.
- `docs/design-assumptions.md`: state that Hermes host-profile import and
  service mode are explicitly unsupported.
- `docs/recipes/`: optional recipe for contained Hermes setup without host
  profile migration.

The docs should consistently say that Hazmat wraps Hermes as a whole process.
They should not imply that Hermes' own approval prompts, env filtering, or
terminal backends are Hazmat security boundaries.

## Phased Delivery

### Phase 0: Recipe Only

Document how to run a user-installed Hermes binary through `hazmat exec`, with
warnings about host state and credentials. No code changes beyond docs.

### Phase 1: Experimental Foreground Harness

Add `hazmat hermes` as a built-in harness with:

- no host profile import
- managed `HERMES_HOME`
- no harness asset sync
- no managed Hermes credential import
- no gateway/service support
- normal Hazmat network, project, Git, GitHub, backup, and native launch policy

### Phase 2: Bootstrap

Add explicit `hazmat bootstrap hermes` once the supply-chain posture is
acceptable. Prefer a pinned, auditable installation path over running upstream's
install script implicitly.

### Phase 3: Credential Capability

Add typed Hermes credential support only after the TLA+ and registry design are
updated. Start with provider keys and tool gateway credentials. Defer messaging,
MCP OAuth, SSH, and cloud credentials unless each has a precise descriptor and
cleanup story.

### Phase 4: Service Mode Evaluation

Evaluate whether Hazmat should own long-running assistant services at all. If
yes, design a generic service lifecycle first, then consider Hermes gateway,
dashboard, cron, and profile supervision.

## Open Questions For Audit

- Should the managed state root be `/Users/agent/.hazmat/hermes` or
  `/Users/agent/.hermes` with `HERMES_HOME` set only when necessary?
- Should v1 reject `hermes gateway`, `hermes cron`, and dashboard-related flags,
  or allow them as foreground commands with loud warnings?
- Is it acceptable for users to configure credentials manually inside the agent
  profile before Hazmat has managed Hermes credential import?
- Should Hermes skills ever join harness asset sync, or should they remain
  Hermes-profile state only?
- Which Hermes install source is acceptable for `hazmat bootstrap hermes`?
- Does adding a new durable app state root under `/Users/agent/.hazmat/hermes`
  require seatbelt model work before even the no-credential v1?
- Should `hazmat explain hermes` include a Hermes-specific section enumerating
  disabled host-profile imports, service-mode deferrals, and Docker-socket
  denials?

## Recommended Decision

Proceed with Phase 1 only after a short audit of the state-root and command
rejection policy. Keep the implementation small: foreground process, fresh
contained Hermes profile, no credential import, no asset sync, no daemon, no
Docker socket.

This gives Hazmat a useful Hermes integration point while preserving the core
security claim: Hazmat contains assistant runtimes as whole processes instead
of trusting their in-process guardrails.
