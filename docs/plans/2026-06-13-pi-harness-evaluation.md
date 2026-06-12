# Pi Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
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

Do not add `hazmat pi` in the next release.

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

For now, keep Pi recipe-only through generic containment. The immediate
hardening gap is independent of full adapter support: `sandboxing-l12m` adds
`~/.pi/agent` to Hazmat's credential deny floor and host credential hardening
specs while Pi remains unsupported as a first-class harness. In TLA+, this is
covered by the existing `agentCliStateDir` abstraction for
Kilo/Kimi/Kiro/Vibe/Trae/Pi-style external agent CLI state roots, avoiding one
finite-model dimension per vendor.

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
agent account can run Pi as a contained foreground tool:

```bash
hazmat shell -C ~/workspace/project
pi
```

There is no stable manual `hazmat exec -- pi --mode rpc` recipe for ordinary
users. RPC mode expects a JSON-RPC client that sends a `prompt` command and
consumes Pi's event stream. A compatible editor or daemon can still launch Pi
through Hazmat as a subprocess wrapper, but that is not first-class Hazmat
support and Hazmat does not yet manage Pi auth, trust, settings, extensions,
skills, sessions, or provider credentials.

## First-Class Requirements

Before `hazmat pi` is supportable:

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

Pi remains recipe-only until the protocol-driver architecture can own Pi's RPC
stream, state root, provider credentials, trust policy, extension UI, and
session asset staging. The immediate deny-list hardening from `sandboxing-l12m`
should ship independently because it protects users even when Pi is only run
through `hazmat shell` or another contained wrapper.
