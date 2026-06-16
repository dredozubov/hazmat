# Pi Harness Candidate Evaluation

Status: Superseded by first-class foreground harness implementation
Date: 2026-06-13
Related issue: `sandboxing-lg07.5.8`
Follow-up implemented: `sandboxing-l12m`
Parent: `sandboxing-lg07.5`

Sources:

- Pi coding agent README:
  <https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md>
- Pi usage docs: <https://pi.dev/docs/latest/usage>
- Open Design Pi runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/pi.ts`
- Open Design adapter notes:
  `/Users/dr/workspace/opendesign/docs/agent-adapters.md`

## Decision

Add `hazmat pi` as a first-class foreground harness with a narrow v1 surface.

Pi is a credible future protocol-driver harness, but it is not an ACP
candidate. Open Design drives Pi through `pi --mode rpc`, sends prompts through
Pi's JSON-RPC command stream, and maps Pi-specific events such as message
updates, tool execution, compaction, extension errors, and auto-retry events
into its UI event model. That is a real adapter, not a thin foreground recipe.

Pi also has a wider persistent state surface than a one-shot CLI. Upstream docs
describe global settings, models, sessions, context files, skills, prompt
templates, extensions, project trust decisions, and provider auth under
`~/.pi/agent`. Non-interactive modes, including RPC mode, do not show a trust
prompt; they follow global trust settings unless the run passes an explicit
trust override. Hazmat should not inherit that host state or silently rely on a
user's global Pi trust decisions.

For now, first-class support is intentionally narrower than a full RPC adapter.
Hazmat owns the lifecycle, launch, explain, status, Docker routing, and smoke
coverage for `hazmat pi`, but does not import host `~/.pi/agent`, does not
materialize provider credentials into Pi, and does not drive Pi's JSON-RPC
prompt/event stream itself. Users configure Pi inside the contained agent
profile. The existing `sandboxing-l12m` hardening still protects host
`~/.pi/agent` when Pi is run through any contained path.

## Upstream Surface

Important surfaces for Hazmat:

- Pi supports interactive, print/JSON, RPC, and SDK modes.
- Open Design uses `pi --mode rpc` with prompts delivered over stdin as
  JSON-RPC, not as an argv string.
- Pi's RPC process can stay alive across prompts; Open Design explicitly closes
  stdin and terminates after a grace period to fit its single-shot chat route.
- Pi supports provider subscriptions and API-key auth for many providers.
- Global custom providers can be stored under `~/.pi/agent/models.json`.
- Sessions are saved under `~/.pi/agent/sessions`.
- Global settings and project trust decisions live under `~/.pi/agent`.
- Global context files and system prompt files can live under `~/.pi/agent`.
- Skills, extensions, prompt templates, themes, and packages are part of Pi's
  extension surface.
- Open Design auto-resolves extension UI prompts because its web UI has no
  interactive dialog surface for those Pi requests.
- Open Design forwards external skill/design-system roots through
  `--append-system-prompt`, which hints paths in prompt context but does not
  grant filesystem access.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `pi --mode rpc` | Strong later | Needs a Pi-specific RPC driver, not an ACP-only adapter |
| JSON-RPC prompt stream | Manageable | Requires fake protocol coverage and typed event mapping |
| Long-lived process lifecycle | Risky | Need bounded cleanup, cancel, and one-shot/session semantics |
| `~/.pi/agent` global root | Risky | Deny and harden host state; future adapter must use session-local state |
| Project trust | Risky | Require explicit Hazmat-owned trust policy; do not inherit host decisions |
| Provider auth | Risky | Requires typed provider credential materialization |
| Images | Manageable | Stage or validate image paths before RPC attachment |
| Skills/extensions/packages | Risky | Default to session-local, repo-visible, or allowlisted inputs only |
| `--append-system-prompt` | Weak | Treat as context hint only; stage needed assets into the session |
| Extension UI | Risky | Do not auto-approve dialogs without an explicit policy and audit trail |

## Recipe-Only Shape

Users who already have Pi installed and authenticated inside the contained
agent account can run Pi as a first-class contained foreground tool:

```bash
hazmat pi -C ~/workspace/project
```

RPC mode still expects a JSON-RPC client that sends a `prompt` command and
consumes Pi's event stream. `hazmat pi -- --mode rpc` can contain the Pi
process, but Hazmat v1 does not yet act as that JSON-RPC client and does not
manage Pi auth, trust, settings, extensions, skills, sessions, or provider
credentials beyond preserving contained agent-side state.

## First-Class Requirements

Before a full Pi RPC adapter is supportable:

- implement a built-in Pi RPC adapter entry separate from ACP
- define typed launch, prompt, image, cancel, error, and lifecycle contracts
- use a session-local Pi state root and never import host `~/.pi/agent`
- define typed provider credential materialization for Pi's supported auth
  modes instead of inheriting broad env passthrough or host config
- set explicit project-trust behavior for non-interactive runs and explain it
  in `hazmat explain`
- stage skills, design-system files, prompt templates, and image attachments
  into session-visible paths instead of relying on absolute-path prompt hints
- default extensions and packages to disabled or allowlisted until their
  execution and install surfaces are policy-owned
- surface extension UI requests as denied, audited, or explicitly user-driven;
  do not silently auto-confirm
- add fake Pi RPC coverage for startup, model selection, prompt acceptance,
  agent success/failure after acceptance, tool events, extension errors,
  auto-retry exhaustion, compaction events, image attachments, malformed
  JSON-RPC, cancel, process cleanup, and empty-output handling
- add fake CLI coverage for missing auth, session-local state isolation,
  trust-policy behavior, prompt/template loading, host-state denial, and git
  dirty state

## Follow-Up

Pi's foreground harness can ship before the protocol-driver architecture owns
Pi's RPC stream, provider credentials, trust policy, extension UI, and session
asset staging. The immediate deny-list hardening from `sandboxing-l12m` still
protects users when Pi is run through `hazmat pi`, `hazmat shell`, or another
contained wrapper.
