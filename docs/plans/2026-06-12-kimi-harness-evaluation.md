# Kimi Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.3`
Follow-up implemented: `sandboxing-f71b`
Parent: `sandboxing-lg07.5`

Sources:

- Kimi Code CLI repository: <https://github.com/MoonshotAI/kimi-code>
- Legacy Kimi CLI repository: <https://github.com/MoonshotAI/kimi-cli>
- Kimi Code CLI getting started: <https://moonshotai.github.io/kimi-code/en/guides/getting-started.html>
- Kimi Code `kimi` command reference: <https://moonshotai.github.io/kimi-code/en/reference/kimi-command.html>
- Kimi Code `kimi acp` reference: <https://moonshotai.github.io/kimi-code/en/reference/kimi-acp.html>
- Kimi Code configuration files: <https://moonshotai.github.io/kimi-code/en/configuration/config-files.html>
- Kimi Code data locations: <https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/data-locations.html>
- Kimi Code MCP customization: <https://moonshotai.github.io/kimi-code/en/customization/mcp>
- Kimi Code hooks: <https://moonshotai.github.io/kimi-code/en/customization/hooks>
- Kimi Code agent skills: <https://moonshotai.github.io/kimi-code/en/customization/skills>

## Decision

Do not add `hazmat kimi` in the next release.

Kimi is a strong future ACP/foreground harness candidate. Open Design's entry
matches current upstream docs: `kimi acp` speaks JSON-RPC over stdin/stdout,
keeps protocol logs on stderr, advertises a documented ACP capability matrix,
and accepts MCP servers from ACP clients. The foreground CLI also has useful
automation surfaces through `kimi -p`, `--output-format stream-json`, `--model`,
`--continue`, `--session`, `kimi doctor`, and `kimi export`.

First-class support is still not a thin wrapper. Kimi Code is a new
TypeScript/Node rewrite that is replacing legacy Python `kimi-cli`, it has a
stateful local data root, provider credentials, OAuth, MCP config and OAuth,
plugins, Kimi-specific and generic Skills, hooks, subagents, background tasks,
auto-update behavior, telemetry, and multiple permission modes. Hazmat needs
to own those authority boundaries before exposing `hazmat kimi`.

For now, keep Kimi recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation found one immediate hardening gap: Kimi Code stores sensitive
config, credentials, sessions, logs, MCP, plugins, and skills under
`~/.kimi-code`, while legacy `kimi-cli` stored equivalent state under
`~/.kimi`. `sandboxing-f71b` adds both roots to Hazmat's credential deny floor
and host credential hardening specs while Kimi remains recipe-only.

## Upstream Surface

Important surfaces for Hazmat:

- Kimi Code CLI is a terminal coding agent that can read and edit files, run
  shell commands, search files, fetch web pages, and dispatch subagents.
- Installation is available through a one-line script, Homebrew, npm, or pnpm;
  `kimi upgrade` can run install/update commands based on the installation
  method.
- `kimi acp` is an ACP subprocess entrypoint over JSON-RPC stdin/stdout; it
  waits for `initialize` and writes logs to stderr plus
  `~/.kimi-code/logs/`.
- ACP support includes initialize/authenticate/session/prompt/cancel/list,
  client-side file read/write reverse-RPC, and MCP forwarding for HTTP and
  stdio servers supplied by the client.
- `kimi -p` runs one prompt non-interactively; docs state that it does not ask
  for human approval and handles regular tool calls under auto permission
  policy.
- `--output-format stream-json` emits JSONL for non-interactive output, while
  tool progress and notices stay on stderr.
- The data root defaults to `~/.kimi-code` and can be relocated with
  `KIMI_CODE_HOME`; it contains config, sessions, logs, OAuth credentials,
  Kimi-specific skills, global `AGENTS.md`, MCP config, plugins, and update
  state.
- Legacy `kimi-cli` stores sensitive state under `~/.kimi`; Kimi Code can
  migrate legacy configuration and sessions.
- User config can set `default_permission_mode` to `manual`, `auto`, or
  `yolo`, define provider credentials and env fallbacks, enable telemetry, set
  background task behavior, and register hooks.
- MCP config lives in user `~/.kimi-code/mcp.json` or project
  `.kimi-code/mcp.json`; stdio entries execute local commands on session start.
- Plugins can declare MCP servers and are enabled by default once installed.
- Hooks run local commands for lifecycle events and are fail-open on most
  script errors/timeouts.
- Skill discovery includes `$KIMI_CODE_HOME/skills`, `~/.agents/skills`,
  project `.kimi-code/skills`, and project `.agents/skills`; `--skills-dir`
  can replace automatically discovered user/project directories for a launch.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `kimi acp` | Strong | Good future ACP-driver candidate |
| JSON-RPC over stdio | Strong | Matches the accepted ACP/RPC driver shape |
| ACP capability docs | Strong | Useful for fake protocol coverage |
| `kimi -p` + stream-json | Strong but risky | Useful smoke entrypoint; auto-permission behavior must be modeled |
| `KIMI_CODE_HOME` | Strong | Future adapter can force session-local state |
| `kimi doctor` | Strong | Good preflight check for generated config |
| User `~/.kimi-code` / legacy `~/.kimi` | Risky | Deny and harden host state; future adapter must use session-local state |
| User MCP/plugins/hooks | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and lifecycle authority |
| Project `.kimi-code` config | Mixed | Treat as project input only after policy review |
| Generic `~/.agents` Skills | Mixed | Must be disabled, modeled, or explicitly allowed per adapter |
| YOLO/auto modes | Risky | Never inherit from host config without a Hazmat policy decision |
| Auto-update | Risky | Disable or route through a managed install/update path |

## Recipe-Only Shape

Users who already have Kimi installed and authenticated inside the contained
agent account can run a foreground session:

```bash
hazmat shell -C ~/workspace/project
kimi
```

For a single-turn task:

```bash
hazmat exec -C ~/workspace/project -- kimi -p "summarize the current git diff" --output-format stream-json
```

This non-interactive form does not ask for human tool approvals. Hazmat still
contains filesystem/network authority at the outer boundary, but users should
expect Kimi to be able to edit the project and run permitted shell commands
inside that boundary.

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `kimi acp` directly:

```json
{
  "agent_servers": {
    "kimi-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/kimi",
        "acp"
      ],
      "env": {}
    }
  }
}
```

This is not first-class support. Hazmat contains the Kimi process, project
paths, network policy, and credential deny zones, but it does not manage Kimi
auth, Kimi config, MCP servers, hooks, plugins, skills, project `.kimi-code`
semantics, auto-update behavior, telemetry, export/log retention, or provider
policy.

## First-Class Requirements

Before `hazmat kimi` is supportable:

- implement a built-in ACP/foreground adapter entry; do not expose a generic
  repo-defined ACP plugin host
- choose whether first-class support is ACP-only, foreground CLI-only, or both
- set `KIMI_CODE_HOME` to a session-local state root and never import host
  `~/.kimi-code` or legacy `~/.kimi`
- generate a minimal config with `default_permission_mode` and telemetry/update
  behavior chosen by Hazmat, not inherited from host config
- define typed credentials for Kimi OAuth, Kimi/Open Platform API keys, and
  other supported provider keys; reject broad env passthrough by default
- decide how to handle `kimi -p` auto-permission behavior before using it as a
  non-interactive smoke/release gate
- default user MCP, plugin MCP, hooks, and generic user Skills to disabled or
  allowlisted by Hazmat policy before launch
- review project `.kimi-code/mcp.json`, `.kimi-code/skills`, `.agents/skills`,
  and `AGENTS.md` as project inputs, not implicit authority grants
- use `--skills-dir` or generated config when necessary to avoid loading
  unmodeled user skill directories
- keep `kimi export` and diagnostic logs session-scoped, and exclude global
  logs unless explicitly requested
- add fake ACP coverage for initialize/authenticate/session/new/session/load,
  prompt streaming, permission reverse-RPC, MCP forwarding rejected/allowed
  cases, malformed JSON-RPC, stderr log isolation, and cleanup
- add fake CLI coverage for `-p`, `--output-format stream-json`, `kimi doctor`,
  permission failure, denied MCP/hook/plugin loading, host-state denial,
  session-local cleanup, and git dirty state
- document inherited Kimi account/provider policy and ACP capability limits in
  `hazmat explain`, trace output, and harness docs

## Follow-Up

Kimi remains recipe-only until the ACP/RPC adapter infrastructure can own launch
policy, credential policy, session-local config, MCP/plugin/hook policy, and
fake protocol tests. The immediate deny-list hardening from `sandboxing-f71b`
should ship independently because it protects users even when Kimi is only run
through `hazmat exec`.
