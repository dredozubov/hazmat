# Hermes Managed Harness Design

Status: Implemented (Phase 1). Originally proposed and landed on master the same
day (741cefa model-first, f34f5c2 implementation, 450ca5b docs); the initial
landing ran the full TLA+ suite (`bash tla/check_suite.sh`) successfully. The
forward-looking "should" language below is preserved as the original proposal;
post-merge corrections are flagged inline.
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
- Support transparent modeled provider API-key delivery in v1: if the user has
  configured provider keys that Hermes already understands, Hazmat should inject
  those same env vars into Hermes without making the user learn Hermes-specific
  Hazmat choices. Hermes profile files, OAuth state, MCP secrets, messaging
  credentials, SSH keys, cloud credentials, and tool home secrets remain out of
  managed import.
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
- No bulk provider-key import from host `~/.hermes`; v1 provider env delivery
  should come from Hazmat's host-owned secret store or an explicit, auditable
  key import path.
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
registry only after the harness lifecycle model is updated. This is not merely
"simple code": the model has a closed harness set, bootstrap records harness
state versions, and `HarnessGemini` already exists in Go without appearing in
`tla/MC_HarnessLifecycle.tla`. The Hermes change should backfill that Gemini
drift at the same time, then add Hermes as a non-importable harness. This work
should not generalize into a user-provided harness plugin system.

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
- A provider key, if configured, is injected from Hazmat's secret store as an
  environment variable for this Hermes session only.

Phase 1 must not leave users at a dead end. If Hermes is not installed for the
agent user, the launcher should produce an actionable error pointing at
`hazmat bootstrap hermes`. The registry entry must still have a non-nil
`Installed` and `Bootstrap` path so `hazmat init` cannot expose a broken
bootstrap selection. The bootstrap command may be manual-instruction-only in
the first slice, but it must be present and verifiable.

## State Layout

Hermes' default state root is `~/.hermes`. In Hazmat, the first slice should use
a dedicated managed state root instead of importing host state:

```text
/Users/agent/.hazmat/hermes/projects/<project-hash>/
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

Hazmat sets `HERMES_HOME` to a project-scoped path under
`/Users/agent/.hazmat/hermes/projects/`, keyed by the canonical project path.
Keeping the root under a Hazmat-owned prefix makes it easier to explain that the
state is not the user's normal Hermes profile; keying by project avoids sharing
Hermes memories, sessions, and skills across unrelated `-C` values.

Hazmat should create the root before launch as the agent user, with restrictive
ownership and permissions. This is a non-secret harness environment setting; the
implementation should either use a dedicated non-secret harness env channel or
update the existing `HarnessEnv` comment so `HERMES_HOME` is not mislabeled as a
credential/capability grant.

The current native policy has a broad `/Users/agent` read/write grant, so this
state root does not require a new seatbelt allow rule in v1. That fact cuts both
ways: Hazmat's existing credential-deny anchors cover paths such as
`/Users/agent/.ssh`, but they do not automatically cover nested Hermes tool-home
paths such as `/Users/agent/.hazmat/hermes/projects/<project-hash>/home/.ssh`.
The v1 design should not claim otherwise.

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

Two foreground Hermes sessions for the same project still share the same managed
`HERMES_HOME`; cross-project sessions do not. V1 can defer locking if it matches
the rest of Hazmat's shared-agent-home posture, but the same-project limitation
should be documented. Secrets or credentials manually written into
`<HERMES_HOME>/.env` by the user or by Hermes setup also survive ordinary Hazmat
rollback unless the agent user is deleted; that is correct by inheritance, but
it should be made visible.

The supported Phase 1 reset is the existing destructive agent-home reset:
`hazmat rollback --delete-user`, followed by `hazmat init` and
`hazmat bootstrap hermes`. Ordinary `hazmat rollback` removes host-owned Hazmat
metadata but intentionally preserves `/Users/agent/.hazmat/hermes` with the
rest of the agent home. A narrower `hazmat hermes reset` or uninstall command
would be a new persistent cleanup path and must start with the harness-lifecycle
model and rollback design note before implementation.

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

V1 should separate provider-key env delivery from Hermes profile import. The
right user model is transparent provider reuse: a user who has already stored an
OpenAI, Anthropic, Gemini, or OpenRouter key in Hazmat should not have to learn a
new Hermes-specific Hazmat key choice. Hazmat should inject the same env var
names Hermes already understands, subject to an explicit registry allowlist for
the Hermes harness.

That transparency requires a small credential-registry schema change. Today,
`credentialDescriptor.Harness` is a single `HarnessID`,
`providerCredentialDescriptorForEnvVar` is env-var-only, and the provider
secret-store path is selected through that env var. `OPENAI_API_KEY` already
belongs to the Codex descriptor. Adding a second Hermes-scoped
`OPENAI_API_KEY` row would make descriptor lookup and grant attribution
ambiguous. Avoiding the collision by telling users to use only
`OPENROUTER_API_KEY` is technically simpler, but it leaks Hazmat internals and
provider choices into the user experience.

The preferred v1 fix is therefore:

1. model provider API keys as shared or multi-consumer credentials, not
   one-harness credentials;
2. make env-descriptor lookup harness-aware or descriptor-ID-aware;
3. keep a single host-store entry for a shared env var such as
   `OPENAI_API_KEY`;
4. record which harnesses may receive each provider env var;
5. attribute session env grants to both the credential ID and consuming harness;
6. make `hazmat config agent` prompt in provider terms, for example "OpenAI API
   key, used by Codex and Hermes", instead of duplicating a harness-specific
   prompt.

The concrete registry design is tracked in
`docs/plans/2026-05-30-shared-provider-credentials-design.md`.

Delivery is whole-process. Every provider key whose registry allowlist includes
Hermes (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`,
`OPENROUTER_API_KEY`) is injected into the session environment and is therefore
inherited by every skill, MCP server, hook, and subprocess Hermes spawns; there
is no per-tool scoping inside the process. A shared env var such as
`OPENAI_API_KEY` is a single host-store entry consumed by both Codex and Hermes,
so rotating or compromising it affects both harnesses. This is the intended
account-isolation model, but it is broader than "one provider key": assume any
in-process Hermes component can read every delivered provider key.

This still avoids unmanaged Hermes profile import. It only means Hazmat's own
provider secret store can deliver provider keys transparently to every harness
that is explicitly allowed to consume them. If product wants zero managed
secrets for a purer containment story, the design should state that as a policy
choice, not as a technical necessity. Nous Portal remains the smoother
Hermes-native auth path, but it is OAuth/device-code state under the Hermes
profile rather than a simple v1 env key.

The v1 harness should otherwise avoid managed Hermes credential import. Users
can run a fresh contained Hermes setup inside the agent profile if they
deliberately want credentials to exist only in the contained agent account. That
is compatible with Hazmat's current account-isolation story, but it is not the
same as host-secret-store materialization.

The credential-deny posture for Hermes' tool home must be explicit. With the
current native policy, credential denies anchored at `/Users/agent/.ssh`,
`/Users/agent/.aws`, and similar paths do not deny
`<HERMES_HOME>/home/.ssh` or `<HERMES_HOME>/home/.aws`. V1 may accept this
because those nested credentials are agent-created rather than host-imported, so
host-*imported* credential confidentiality is preserved.

That is the correct test for confidentiality but not for integrity. The seatbelt
grants `file-write*` and `process-exec` across all of `/Users/agent`, so an
in-process Hermes skill or MCP server can write, read, and execute material under
`<HERMES_HOME>/home` itself — the effective control against that staging/exfil
path is network mode, not the deny list. Prefer `--network none` for Hermes runs
that enable untrusted skills, plugins, or MCP servers. Separately, the managed
state root survives ordinary `hazmat rollback` (it is removed only by
`--delete-user`), and Hazmat does not pin or verify `config.yaml`, `skills/`,
`hooks/`, or `cron/` between runs, so a compromised session can plant state that
the next `hazmat hermes` launch silently honors. Recommend a documented state
reset (or `--delete-user`) after untrusted work. If Hazmat wants defense-in-depth
for the nested Hermes tool-home credentials themselves, it must extend deny
generation to the Hermes root, with a seatbelt-policy model update and tests.

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
- pin and verify the source where upstream makes that possible
- verify the installed `hermes --version`
- make the install idempotent
- avoid importing host `~/.hermes`
- report whether optional dependencies such as browser tooling are absent

Phase 1 should include a minimal non-broken bootstrap path even if full
automated installation is deferred:

- user-installed `hermes` on the agent account, detected by `PATH`, or
- a bootstrap command that prints exact manual install instructions, checks for
  the binary afterward, and records harness state only when `hermes --version`
  succeeds.

The later automated bootstrap should be stricter than the loosest existing
precedent. Hazmat's current harnesses are mixed: Claude pins a script checksum,
Codex and OpenCode verify published GitHub digests, and Gemini installs an
unpinned `@latest` package. Hermes has a richer in-process surface than a
typical coding CLI, so the preferred target is the Codex/Claude/OpenCode end
of that spectrum, not the Gemini end.

The consistent rule is narrower: install is explicit, agent-owned, and never
performed implicitly during `hazmat hermes` launch.

## Launch Pipeline

The Hermes foreground launch should reuse the existing session setup pipeline:

1. parse Hazmat flags and split Hermes args after `--`
2. resolve project root and requested read/write directories
3. apply explicit integrations for the project stack, not Hermes itself
4. apply network mode
5. apply optional Git SSH and GitHub capabilities
6. prepare backup/snapshot state
7. create the managed Hermes state root as the agent user
8. set Hermes-specific environment, including `HERMES_HOME` and the selected
   provider key env grant
9. reject deferred service-mode entrypoints such as `gateway`, dashboard/API,
   and persistent cron commands with explicit guidance
10. launch the Hermes process as the agent user under native policy
11. clean up session-scoped materialized credentials after exit
12. leave durable Hermes agent-profile state only where the design explicitly
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
- Hermes is Python-based and should not need the wider macOS Security framework policy
  used by Claude and Codex. The implementation should set `harnessUsesMacOSSecurityFramework` to
  false for Hermes and validate that with a live network probe.

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
user forwards `gateway`, dashboard/API, or persistent cron entrypoints manually,
Hazmat should reject the command with clear guidance. Do not add
`--allow-foreground-gateway` in v1; enforcing the deferral requires inspecting
the Hermes passthrough args after `--`, not only Hazmat's own flags.

There are two cron risks to keep distinct. A live cron worker process is
contained by the foreground process tree and should die when the session exits.
Durable cron definitions under `<HERMES_HOME>/cron` can survive and reactivate
on a later Hermes launch. V1 should reject persistent cron management commands
or document that persistent cron state is possible but unsupported.

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

> **Implementation status: done.** `MC_HarnessLifecycle` now models
> `{claude, codex, opencode, gemini, hermes}` (the prior `HarnessGemini` drift is
> backfilled) with Hermes excluded from `ImportableHarnesses`, and
> `MC_CredentialCapabilityLifecycle` models OpenCode file-backed auth and Hermes
> as a multi-consumer of the shared provider keys. The credential lifecycle
> follow-up passed standalone TLC (`MC_CredentialCapabilityLifecycle.cfg` →
> 25,623,297 generated states, 6,963,327 distinct, depth 32, no errors). The
> "should" steps below record the original plan.

`HarnessState.state_version` remains part of the lifecycle contract. Phase 1
keeps it and reads it from `hazmat status` to surface missing or stale harness
metadata; it is not used as a migration gate yet.

Adding `HarnessHermes` is itself a model-first change. The current
`MC_HarnessLifecycle` model has a closed `Harnesses` set, while Go already ships
`HarnessGemini` outside that set. The Hermes implementation should update the
model before the Go registry change:

- add `"gemini"` to `Harnesses` and `HarnessVersion`
- add `"hermes"` to `Harnesses` and `HarnessVersion`
- keep Hermes out of `ImportableHarnesses` for v1, matching the no host-profile
  import decision
- keep only Hermes out of `ImportableHarnesses`; Codex *is* importable
  (`CodexHarness.ImportBasics`) and must stay in the set — do not generalize this
  bullet to Codex
- update `MC_CredentialCapabilityLifecycle` for Hermes and the shared-provider
  credential model: provider env credentials may be delivered to multiple
  explicitly allowed harnesses, while `NoCrossHarnessExposure` still forbids
  exposure to unlisted harnesses
- prove `RecordedHarnessVersionsMatchSpec`, dry-run state preservation, and the
  rollback properties still hold
- run the TLA+ suite before changing Go harness registry code

The following changes also touch verified areas and require model-first work:

- adding managed Hermes credential delivery
- adding session-scoped Hermes state materialization and harvest
- adding persistent mutation to setup/init
- changing rollback behavior for Hermes state
- adding service/gateway lifecycle
- changing launch file-descriptor behavior for Hermes IPC or API sockets
- changing seatbelt rules to deny nested Hermes tool-home credential paths

At minimum, the implementation plan should review:

- harness lifecycle
- credential capability lifecycle
- secret-store crash recovery
- setup/rollback
- seatbelt policy structure
- launch fd isolation

The design boundary for v1 should keep runtime scope small while accepting the
credential model work needed for transparency: foreground harness launch,
shared provider-key env delivery, no managed Hermes profile import, no gateway
service, no new daemon setup, and no host-state sync.

## Testing Plan

Unit tests:

- `HarnessHermes` appears in the built-in harness registry.
- `hazmat hermes -- ...` forwards Hermes args after Hazmat flag parsing.
- `HERMES_HOME` points at the Hazmat-managed agent root.
- the managed Hermes state root is created as the agent user before launch.
- the registry entry has non-nil `Installed` and `Bootstrap` functions.
- host `~/.hermes` paths are not selected as asset-sync roots.
- `--skip-harness-assets-sync` remains accepted but has no Hermes host assets in
  v1.
- Docker socket paths are not granted by the Hermes harness.
- gateway/dashboard/persistent-cron commands are rejected after passthrough arg
  parsing.
- `hazmat explain` or its selected preview surface accepts `hermes` as a target
  and reports disabled host-profile import, service-mode deferral, Docker socket
  denial, and transparent provider-key env delivery.

Policy tests:

- native policy allows the selected Hermes state root only as intended.
- current credential denies are asserted for `/Users/agent/.ssh` and similar
  roots, and tests explicitly document whether nested
  `<HERMES_HOME>/home/.ssh` is accepted or denied.
- `--network none` is preserved through Hermes launch.
- optional Git/GitHub capabilities are still explicit.
- Hermes does not receive the macOS Security framework widened policy unless the live
  probe proves it needs one.

Smoke tests:

- `hazmat hermes -- --version`
- `hazmat hermes -- chat --help`
- `hazmat hermes --network none -- --version`
- a fake `hermes` binary fixture that records `HERMES_HOME`, cwd, env, and args
  without requiring upstream installation
- a scratch project where Hermes can read/write only within the expected project
  and session write roots
- provider-key fixtures proving shared keys such as `OPENAI_API_KEY` can be
  injected into Codex and Hermes when both are allowed, while the same key is
  not exposed to unrelated harnesses
- a default-network live probe proving the selected provider endpoint is
  reachable under Hazmat's default DNS/PF posture, and that `--network none`
  fails closed with an actionable message
- first-run diagnostics for missing binary, missing provider key, blocked
  egress, and TLS/certificate failure

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

The docs should also warn that manually configured Hermes credentials under the
managed agent profile survive ordinary rollback, that two foreground sessions
share the same managed profile in v1, and that host-profile migration is
deliberately absent.

## Phased Delivery

### Phase 0: Recipe Only

Document how to run a user-installed Hermes binary through `hazmat exec`, with
concrete warnings about host state and credentials. If this recipe launches
before the managed harness exists, it should set `HERMES_HOME` explicitly;
otherwise `hazmat exec hermes` writes to the agent user's ordinary `~/.hermes`
and creates a different state contract from the proposed harness.

### Phase 1: Experimental Foreground Harness

Add `hazmat hermes` as a built-in harness with:

- no host profile import
- managed `HERMES_HOME`
- no harness asset sync
- transparent modeled provider API-key delivery for Hermes-recognized env vars,
  including shared keys such as `OPENAI_API_KEY`
- no managed Hermes profile or file credential import
- no gateway/service support
- rejection of gateway/dashboard/persistent-cron entrypoints
- minimal non-broken `hazmat bootstrap hermes` and install detection
- normal Hazmat network, project, Git, GitHub, backup, and native launch policy

### Phase 2: Automated Bootstrap

Replace the Phase 1 manual-instruction bootstrap with automated installation
once the supply-chain posture is acceptable. Prefer a pinned, auditable
installation path over running upstream's install script directly.

### Phase 3: Additional Credential Capabilities

Add typed Hermes credential support beyond shared provider API-key env delivery
only after the TLA+ and registry design are updated. Tool gateway credentials
may be the next candidate. Defer messaging, MCP OAuth, SSH, and cloud
credentials unless each has a precise descriptor and cleanup story.

### Phase 4: Service Mode Evaluation

Evaluate whether Hazmat should own long-running assistant services at all. If
yes, design a generic service lifecycle first, then consider Hermes gateway,
dashboard, cron, and profile supervision.

## Resolved Audit Decisions

- Use `/Users/agent/.hazmat/hermes/projects/<project-hash>` as the preferred
  managed state root unless an implementation fact-check finds Hermes hardcodes
  `~/.hermes` in a way that ignores `HERMES_HOME`.
- Reject `hermes gateway`, persistent cron management, and dashboard/API
  entrypoints in v1 with guidance.
- Is it acceptable for users to configure credentials manually inside the agent
  profile before Hazmat has managed Hermes credential import? Yes, but docs and
  diagnostics must say those credentials are agent-profile state and survive
  ordinary rollback.
- Is there a Hermes-specific reset or uninstall command in Phase 1? No. The
  supported reset boundary is destructive agent-user removal
  (`hazmat rollback --delete-user`). A Hermes-only reset is future work because
  it would change the modeled cleanup contract for agent-home harness state.
- Keep Hermes skills as Hermes-profile state in v1; do not join harness asset
  sync.
- Adding the new state root does not require a new seatbelt allow under the
  current broad `/Users/agent` rule, but extending credential denies into nested
  Hermes tool-home paths would require model work.
- The explain/preview surface should include Hermes-specific notes for disabled
  host-profile import, service-mode deferral, Docker socket denial, and
  transparent provider-key env delivery.

## Remaining Questions

- Which exact automated install source is acceptable for Phase 2?
- Should v1 add profile locking, or is documented shared-state behavior
  sufficient for the first release?
- Should nested `<HERMES_HOME>/home` credential denies be added immediately, or
  should v1 explicitly accept agent-created credentials there?
- Should same-project foreground Hermes sessions share one project-scoped
  profile root, or should Hazmat add locking or per-session roots?

## Recommended Decision

Proceed with Phase 1 after the model-first harness lifecycle update and the
shared-provider credential registry update. Keep the runtime implementation
small: foreground process, fresh contained Hermes profile, transparent provider
key delivery, manual/minimal bootstrap, no Hermes profile import, no asset sync,
no daemon, no Docker socket, and explicit rejection of service-mode entrypoints.

This gives Hazmat a useful Hermes integration point while preserving the core
security claim: Hazmat contains assistant runtimes as whole processes instead
of trusting their in-process guardrails.
