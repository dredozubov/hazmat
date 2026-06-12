# Cline Harness Candidate Evaluation

Status: Compatibility decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.3`
Parent: `sandboxing-lg07.7`

Sources:

- Cline overview: <https://docs.cline.bot/cline-overview>
- Cline installation: <https://docs.cline.bot/getting-started/installing-cline>
- Cline CLI overview: <https://docs.cline.bot/usage/cli-overview>
- Cline CLI reference: <https://docs.cline.bot/cli/cli-reference>
- Cline tools reference: <https://docs.cline.bot/tools-reference/all-cline-tools>
- Cline MCP overview: <https://docs.cline.bot/mcp/mcp-overview>
- Cline plugins: <https://docs.cline.bot/customization/plugins>
- Cline GitHub README: <https://github.com/cline/cline>

## Decision

Do not add `hazmat cline` in the next release.

Cline is a high-fit future foreground harness candidate. Unlike several
market-priority candidates, it has a mature CLI story: interactive terminal use,
headless automation, JSON output, provider/model flags, explicit config and data
directory flags, ACP mode, SDK support, and tool policy controls. It also maps
well to Hazmat's security story because Hazmat already denies `~/.cline` as an
AI-agent credential/config root.

The blocker is scope, not lack of fit. Cline is now an SDK, CLI, IDE extension,
Kanban app, hub daemon, scheduler, plugin system, MCP client, ACP surface, and
multi-agent runtime. First-class Hazmat support should enter through the closed
foreground adapter registry with a narrow CLI profile first. Service/hub,
Kanban, scheduled agents, ACP editor integration, and IDE/plugin surfaces need
separate lifecycle policy before support.

## Upstream Surface

Important surfaces for Hazmat:

- CLI install is documented as `npm install -g cline`; Node 20+ is required.
- `cline auth` configures provider/model authentication.
- headless mode activates for `--json`, piped stdin, or stdout redirection.
- CLI flags include `--config <path>` and `--data-dir <path>`, making
  session-local state possible without relying on host `~/.cline`.
- CLI global `--auto-approve` defaults to `true`; autonomous execution can
  modify files and run commands without further prompts.
- `CLINE_COMMAND_PERMISSIONS` can restrict shell command execution patterns.
- built-in ClineCore tools include shell command execution, file edit/read,
  patch application, code search, web fetch, and ask-question.
- MCP configuration can live at `~/.cline/mcp.json`; MCP servers can include
  command/env secrets or remote URL/header credentials.
- plugins can be global under `~/.cline/plugins/` or project-scoped under
  `.cline/plugins/`, and can add custom tools, hooks, commands, and behavior.
- `--acp` runs Cline in Agent Client Protocol mode for editor integration.
- hub, schedule, and Kanban commands introduce persistent service or task-board
  lifecycle concerns outside a foreground CLI run.
- SDK `ClineCore` exposes local, hub, and remote backends, optional plugin
  paths/extensions, tool policies, scheduling, and automation APIs.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Headless `--json` | Strong | Good future fake-provider smoke entrypoint |
| `--data-dir` / `--config` | Strong | Enables session-local state by design |
| Interactive CLI/TUI | Strong | Suitable for foreground adapter registry |
| SDK `ClineCore` | Strong | Useful adapter-test harness, not first release surface |
| Default auto-approve | Risky | Future adapter must pin policy deliberately |
| `~/.cline` | Covered | Already denied and hardened as AI-agent credential state |
| Project `.cline/*` | Mixed | Treat as project input only after plugin/MCP policy exists |
| MCP servers | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and remote authority |
| Plugins/hooks | Risky | Disable or allowlist before first-class support |
| ACP editor mode | Mixed | Reuse ACP driver design; do not combine with initial CLI adapter |
| Hub/schedule/Kanban | Service-like | Require service lifecycle model and cleanup policy |

## Recipe-Only Shape

Users who already have Cline installed in the contained agent account can run a
headless review with explicit local state paths:

```bash
hazmat exec -C ~/workspace/project -- cline --data-dir /tmp/hazmat-cline-data --config /tmp/hazmat-cline-config --auto-approve false --json "summarize the current git diff"
```

For autonomous contained work:

```bash
hazmat exec -C ~/workspace/project -- cline --data-dir /tmp/hazmat-cline-data --config /tmp/hazmat-cline-config --auto-approve true "run tests and fix failures"
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Cline auth,
MCP servers, plugins, command permissions, hub/daemon state, scheduled agents,
Kanban worktrees, ACP editor attach, or provider-specific login flows.

## First-Class Requirements

Before `hazmat cline` is supportable:

- define a built-in foreground CLI adapter with install/update/status scope
- always launch with session-local `--data-dir` and `--config` paths
- keep host `~/.cline` denied; do not import host auth, MCP, plugin, hook, or
  history state by default
- add typed credential grants for explicitly supported providers, and keep
  `cline auth` outside automation until provider auth storage is modeled
- pin `--auto-approve` and command permission defaults explicitly instead of
  inheriting Cline defaults
- disable or allowlist project `.cline` plugins, hooks, and MCP servers before
  launch
- leave `--acp`, hub, schedule, Kanban, and IDE attach out of the first adapter
  unless their lifecycle is modeled separately
- add fake-provider smoke coverage for JSON headless output, provider failure,
  malformed JSON stream, denied shell command, denied file edit, isolated data
  dir cleanup, host-state denial, plugin/MCP disabled behavior, and git dirty
  state
- document how Cline checkpoints/history interact with Hazmat snapshots,
  rollback, and git status

## Follow-Up

Cline should be near the top of the future foreground adapter queue once the
adapter registry and session-local tool-state policy are ready. No new
credential deny path is needed from this evaluation because `~/.cline` is
already in the credential deny and host-hardening lists.
