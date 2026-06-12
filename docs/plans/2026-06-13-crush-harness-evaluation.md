# Crush Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.7.8`
Follow-up implemented: `sandboxing-jm49`
Parent: `sandboxing-lg07.7`

Sources:

- Crush repository README: <https://github.com/charmbracelet/crush>
- Vercel AI Gateway Crush guide:
  <https://vercel.com/docs/agent-resources/coding-agents/crush>
- DeepSeek Crush integration guide:
  <https://api-docs.deepseek.com/quick_start/agent_integrations/crush>
- Charm Crush announcement:
  <https://charm.land/blog/crush-comes-home/>

## Decision

Do not add `hazmat crush` in the next release.

Crush is a good future foreground-harness candidate, but current support should
stay recipe-only. It is a power-user terminal client with multi-provider auth,
LSP integration, MCP servers, project/global config, local data stores, global
context files, skills, hooks, metrics, permission prompts, `--yolo`, and
shared workspace semantics through `crush serve`. A thin wrapper would inherit
too much authority from the user's global Crush configuration.

The immediate security gap is host state exposure. Crush's README documents
global config at `~/.config/crush/crush.json` and application state at
`~/.local/share/crush/crush.json`, with overrides available through
`CRUSH_GLOBAL_CONFIG` and `CRUSH_GLOBAL_DATA`. The config surface can include
providers, LSP commands, MCP servers, headers, env expansion, and shell command
substitution. `sandboxing-jm49` adds `~/.config/crush` and
`~/.local/share/crush` to Hazmat's credential deny floor and host credential
hardening specs while Crush remains recipe-only. In TLA+, this is covered by
the existing `agentCliStateDir` abstraction for
Kilo/Kimi/Kiro/Vibe/Trae/Pi/Crush-style external agent CLI state roots.

## Upstream Surface

Important surfaces for Hazmat:

- Crush is a TUI coding agent with model/provider selection, sessions, LSP,
  MCP, skills, hooks, permissions, and metrics.
- Installation paths include Homebrew, npm, Go install, distro packages, Nix,
  and platform package managers; some documented package-manager setup commands
  are sudo-adjacent and must not be automated by Hazmat without explicit user
  action.
- Crush can read provider keys from env vars including Anthropic, OpenAI,
  Vercel AI Gateway, Gemini, OpenRouter, Bedrock, Azure OpenAI, and others.
- Global config priority includes project `.crush.json`, project `crush.json`,
  and `$HOME/.config/crush/crush.json`.
- Application data is documented under `$HOME/.local/share/crush`.
- `CRUSH_GLOBAL_CONFIG` and `CRUSH_GLOBAL_DATA` can redirect global config and
  data roots, which is the right primitive for a future session-local adapter.
- LSP config can spawn language servers with custom command, args, and env.
- MCP config supports stdio, HTTP, and SSE servers.
- MCP/provider fields support shell-style expansion, including command
  substitution, in config-loaded values.
- The docs explicitly warn that `crush.json` is trusted code because command
  substitution runs while loading config.
- Crush can run with allowed tools or skip permission prompts through `--yolo`.
- Skills can be loaded from global and project paths, including shared
  `~/.config/agents/skills`, `~/.agents/skills`, and `~/.claude/skills`.
- Crush records pseudonymous metrics by default unless disabled through env or
  config, and it respects `DO_NOT_TRACK`.
- Shared backends/workspaces can share session list, message history,
  permission queue, LSP state, and MCP state by resolved cwd.
- The project is licensed under FSL-1.1-MIT, which is not a technical blocker
  for running an external user-installed binary but is relevant to vendoring or
  redistributing integration code.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Foreground TUI | Strong | Good future interactive harness after adapter work |
| Headless/script mode | Unclear | No stable documented one-shot prompt path found in current docs |
| `CRUSH_GLOBAL_CONFIG` / `CRUSH_GLOBAL_DATA` | Strong | Use for session-local state in future adapter |
| Global config/data roots | Risky | Deny and harden host state by default |
| Provider env keys | Risky | Requires typed credential materialization |
| MCP config | Risky | Require Hazmat-owned command, env, HTTP, SSE, and header policy |
| LSP config | Mixed | Allow only project/toolchain-scoped commands after policy review |
| Shell expansion in config | Risky | Never load unreviewed project/global config as trusted host code |
| Skills/global context | Mixed | Prefer repo/session-local inputs; avoid inheriting host skill stores |
| `--yolo` | Risky | Never use as Hazmat's default |
| Metrics | Manageable | Default recipe guidance should disable metrics unless user opts in |
| Shared workspace backend | Service-like | Route through service-harness lifecycle if used |
| FSL-1.1-MIT | External | Avoid vendoring/redistribution assumptions without license review |

## Recipe-Only Shape

Users who already have Crush installed and configured inside the contained
agent account can run it interactively:

```bash
hazmat shell -C ~/workspace/project
env CRUSH_DISABLE_METRICS=1 DO_NOT_TRACK=1 crush
```

For a stricter contained run, keep config and data under the session/project
area instead of the persistent agent home:

```bash
hazmat shell -C ~/workspace/project
mkdir -p .hazmat/crush-config .hazmat/crush-data
env CRUSH_DISABLE_METRICS=1 DO_NOT_TRACK=1 \
  CRUSH_GLOBAL_CONFIG="$PWD/.hazmat/crush-config" \
  CRUSH_GLOBAL_DATA="$PWD/.hazmat/crush-data" \
  crush
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Crush auth,
provider keys, config trust, LSP/MCP commands, skills, hooks, permissions,
metrics, shared workspaces, or data cleanup.

## First-Class Requirements

Before `hazmat crush` is supportable:

- define whether the adapter is foreground TUI only, service-backed via
  `crush serve`, or both
- use `CRUSH_GLOBAL_CONFIG` and `CRUSH_GLOBAL_DATA` to create session-local
  Crush state; never import host `~/.config/crush` or `~/.local/share/crush`
- define typed provider credential grants and reject broad `*_API_KEY`, cloud,
  and Bedrock env passthrough by default
- default metrics off unless the user explicitly grants telemetry
- generate minimal Hazmat-owned config with no `--yolo`, no pre-approved tools,
  no unreviewed hooks, and no inherited project/global config command
  substitution
- add an explicit MCP policy for stdio commands, env, HTTP/SSE endpoints,
  headers, network authority, and secrets
- add an explicit LSP policy for allowed project/toolchain language servers and
  environment
- decide whether global context files and skills are disabled, session-staged,
  or repo-visible only
- if `crush serve` is used, model shared workspace lifecycle, cwd identity,
  permission queue, LSP/MCP state, cancel, and teardown under the service
  harness boundary
- add fake CLI/service tests for missing auth, typed credential injection,
  denied host state, session-local config/data cleanup, trusted config command
  substitution rejection, disabled metrics, MCP/LSP allowlist failures,
  permission prompts, `--yolo` rejection, and git dirty state
- document license/distribution posture before any managed install or vendoring
  work

## Follow-Up

Crush remains recipe-only until Hazmat can own its config/data root, provider
credentials, config trust, MCP/LSP surfaces, metrics, and any service backend
lifecycle. The immediate deny-list hardening from `sandboxing-jm49` should
ship independently because it protects users even when Crush is only run
through `hazmat shell`.
