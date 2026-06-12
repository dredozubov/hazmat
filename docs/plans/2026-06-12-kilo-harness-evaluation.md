# Kilo Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.5`
Follow-up implemented: `sandboxing-wx6t`
Parent: `sandboxing-lg07.5`

Sources:

- Kilo Code CLI docs: <https://kilo.ai/docs/code-with-ai/platforms/cli>
- Kilo CLI command reference: <https://kilo.ai/docs/code-with-ai/platforms/cli-reference>
- Kilo settings docs: <https://kilo.ai/docs/getting-started/settings>
- Kilo setup and authentication: <https://kilo.ai/docs/getting-started/setup-authentication>
- Kilo built-in provider docs: <https://kilo.ai/docs/ai-providers/kilocode>
- Kilo CLI MCP docs: <https://kilo.ai/docs/automate/mcp/using-in-cli>
- Kilo MCP overview: <https://kilo.ai/docs/automate/mcp/overview>
- Kilo custom instructions: <https://kilo.ai/docs/customize/custom-instructions>
- Kilo Gateway authentication: <https://kilo.ai/docs/gateway/authentication>
- Kilo Gateway SDKs/frameworks: <https://kilo.ai/docs/gateway/sdks-and-frameworks>

## Decision

Do not add `hazmat kilo` in the next release.

Kilo is a strong future ACP/foreground harness candidate. Open Design's entry
matches current upstream docs: `kilo acp` is a documented top-level command,
and Kilo's command reference exposes `kilo run`, `kilo auth`, `kilo mcp`,
`kilo session`, `kilo export`, `kilo remote`, and local daemon operations. The
market-research note that Kilo is broader than a CLI is accurate: Kilo Code
spans VS Code, JetBrains, CLI, Kilo Gateway, Cloud Agents, remote connections,
MCP, custom agents, and an OpenAI-compatible Gateway.

First-class support should not duplicate Hazmat's existing OpenCode support or
blindly import Kilo's host config. Kilo can edit projects, run terminal
commands, manage provider credentials, load global/project config, launch
local and remote MCP servers, enable remote cloud control of local CLI
sessions, and run local daemons. Hazmat needs to own those authority boundaries
before exposing `hazmat kilo`.

For now, keep Kilo recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation found one immediate hardening gap: Kilo CLI and extension surfaces
share global config under `~/.config/kilo`, where provider options, API keys,
MCP servers, remote-control settings, and global preferences can live.
`sandboxing-wx6t` adds `~/.config/kilo` to Hazmat's credential deny floor and
host credential hardening specs while Kilo remains recipe-only. In TLA+, this
is covered by the existing `agentCliStateDir` abstraction for Kilo/Kimi/Kiro
style external agent CLI state roots, avoiding one finite-model dimension per
vendor.

## Upstream Surface

Important surfaces for Hazmat:

- `kilo acp` starts an ACP server; `kilo run [message..]` runs a prompt from
  the terminal; `kilo [project]` starts the TUI.
- `kilo auth` manages providers and credentials, including login/logout flows.
- Kilo Gateway API keys are JWT-like account tokens; Gateway is
  OpenAI-compatible and can also route BYOK provider keys.
- Global config is documented under `~/.config/kilo/kilo.jsonc`, with other
  supported global filenames such as `~/.config/kilo/kilo.json` and
  `~/.config/kilo/config.json`.
- Project config can be `kilo.json`, `kilo.jsonc`, or `.kilo/kilo.json[c]`,
  and project config takes precedence over global settings.
- MCP config can launch local stdio commands with environment variables or
  connect to remote transports.
- Tool permissions use allow/ask/deny style policy for built-in and MCP tools.
- Remote mode can expose a local CLI session to Cloud Agents; docs warn that
  anyone with the Kilo account can send messages to the local computer when it
  is enabled.
- Kilo includes local daemon commands with configurable hostname/port/mDNS.
- Kilo supports sessions, snapshots/debug diff utilities, export/import, PR
  checkout helpers, custom agents, custom instructions, skills, workflows, and
  telemetry/experimental settings.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `kilo acp` | Strong | Good future ACP-driver candidate |
| `kilo run` | Strong | Useful future fake CLI smoke entrypoint |
| `~/.config/kilo` | Risky | Deny and harden host state; future adapter must use session-local config |
| Kilo Gateway | Mixed | Requires typed account/API-key grant and clear inherited authority docs |
| BYOK provider config | Risky | Reject broad provider/env import; use typed credentials only |
| MCP servers | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and OAuth |
| Remote mode | Risky | Disable by default; do not allow cloud-to-local control silently |
| Local daemon | Service-like | Needs service lifecycle modeling if first-class support uses it |
| Project `.kilo` config | Mixed | Treat as project input only after policy review |
| Custom agents/skills/workflows | Mixed | Allow only repo-visible/session-local assets after policy review |
| Existing OpenCode support | Overlap | Avoid duplicating a covered harness unless Kilo-specific value is explicit |

## Recipe-Only Shape

Users who already have Kilo installed and authenticated inside the contained
agent account can run a foreground session:

```bash
hazmat shell -C ~/workspace/project
kilo
```

For a single-turn task:

```bash
hazmat exec -C ~/workspace/project -- kilo run "summarize the current git diff"
```

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `kilo acp` directly:

```json
{
  "agent_servers": {
    "kilo-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/kilo",
        "acp"
      ],
      "env": {}
    }
  }
}
```

This is not first-class support. Hazmat contains the Kilo process, project
paths, network policy, and credential deny zones, but it does not manage Kilo
auth, global Kilo config, Gateway tokens, provider/BYOK keys, MCP servers,
remote-control mode, daemon lifecycle, custom agents, workflows, snapshots, or
telemetry.

## First-Class Requirements

Before `hazmat kilo` is supportable:

- implement a built-in ACP/foreground adapter entry; do not expose a generic
  repo-defined ACP plugin host
- decide whether first-class support is ACP-only, foreground CLI-only, daemon
  backed, or more than one adapter
- force a session-local Kilo config/state root; never import host
  `~/.config/kilo`
- define typed Kilo Gateway and provider/BYOK credentials; reject broad
  environment passthrough by default
- generate or validate minimal config with remote mode disabled, telemetry
  explicit, and no inherited trusted-tool/MCP decisions
- default global MCP, local MCP commands, custom agents, skills, workflows, and
  remote transports to disabled or allowlisted by Hazmat policy
- treat project `kilo.json[c]` and `.kilo/` config as project inputs only
  after policy review
- decide how Kilo sessions, snapshots, debug artifacts, export/import, and PR
  checkout helpers interact with Hazmat snapshots and git hygiene
- model local daemon lifecycle before using daemon mode in first-class support
- add fake ACP coverage for initialize/session/model/prompt/cancel/list,
  malformed JSON-RPC, MCP forwarding rejected/allowed cases, stdout/stderr
  isolation, and cleanup
- add fake CLI coverage for `kilo run`, auth absence, typed credential
  materialization, provider failure, denied MCP/custom-agent loading, remote
  mode disabled, host-state denial, session-local cleanup, and git dirty state
- document inherited Kilo Gateway account, organization, Cloud Agent, and
  provider-policy interactions in `hazmat explain`, trace output, and harness
  docs

## Follow-Up

Kilo remains recipe-only until the ACP/RPC adapter infrastructure can own
launch policy, credential policy, session-local Kilo config, MCP/custom-agent
policy, remote-control defaults, and fake protocol tests. The immediate
deny-list hardening from `sandboxing-wx6t` should ship independently because
it protects users even when Kilo is only run through `hazmat exec`.
