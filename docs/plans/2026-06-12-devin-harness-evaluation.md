# Devin Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.2`
Follow-up implemented: `sandboxing-2sn4`
Parent: `sandboxing-lg07.5`

Sources:

- Devin CLI quickstart: <https://docs.devin.ai/cli>
- Devin CLI commands and flags: <https://docs.devin.ai/cli/reference/commands>
- Devin CLI essential commands and modes: <https://docs.devin.ai/cli/essential-commands>
- Devin CLI configuration file: <https://docs.devin.ai/cli/reference/configuration/config-file>
- Devin CLI MCP configuration: <https://docs.devin.ai/cli/extensibility/mcp/configuration>
- Devin CLI JetBrains ACP integration: <https://docs.devin.ai/cli/acp/jetbrains>
- Devin Desktop ACP integration: <https://docs.devin.ai/desktop/acp>

## Decision

Do not add `hazmat devin` in the next release.

Devin is a strong future ACP/foreground harness candidate. The current upstream
surface is clearer than the original Open Design intake: official docs describe
`devin acp` as a JSON-RPC-over-stdio ACP server for ACP-aware clients, and the
CLI also has useful non-interactive entrypoints through `--print` /
`--prompt-file`, `--config`, and `--export`.

The Open Design candidate argv is still not acceptable as a Hazmat default:

```bash
devin --permission-mode dangerous --respect-workspace-trust false acp
```

Hazmat should not start by bypassing Devin's own permission prompts and
workspace-trust checks. A future adapter must choose a Hazmat-owned policy,
likely normal or explicitly sandboxed/autonomous mode where supported, and must
make every credential/config import explicit. The first-class implementation
also needs fake ACP protocol coverage before shipping. Until then, Devin stays
recipe-only through `hazmat exec` or `hazmat shell`.

The evaluation found one immediate hardening gap: Devin user config, rules,
MCP config, hooks, and auth-adjacent state live under `~/.config/devin`.
`sandboxing-2sn4` adds that root to Hazmat's credential deny floor and host
credential hardening specs while Devin remains recipe-only.

## Upstream Surface

Important surfaces for Hazmat:

- Devin CLI is a local terminal agent distinct from the cloud Devin product.
- Global flags include `--permission-mode`, `--print`, `--prompt-file`,
  `--config`, `--export`, `--continue`, `--resume`, and
  `--respect-workspace-trust`.
- Permission modes include normal/default behavior and bypass/dangerous modes
  that auto-approve tool calls.
- `devin acp` runs Devin as an ACP server over stdin/stdout and is intended for
  ACP-aware editors/IDEs, not interactive shell use.
- The ACP server reads `WINDSURF_API_KEY` when set, otherwise stored login
  credentials, and may also authenticate at runtime through ACP.
- User config lives under `~/.config/devin/config.json`; project config lives
  under `.devin/config.json` and `.devin/config.local.json`.
- Configuration can include permissions, MCP servers, hooks, and imports from
  other agent/editor configs.
- MCP config can launch local stdio commands, connect to remote HTTP servers,
  and carry env/header secrets.
- Devin Desktop can launch local ACP agents, including a sample Devin Local
  registry entry that invokes `devin acp`.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `devin acp` | Strong | Good future ACP-driver candidate after fake protocol tests |
| JSON-RPC over stdio | Strong | Matches the accepted ACP/RPC driver shape |
| `--print` / `--prompt-file` | Strong | Useful foreground smoke and automation entrypoints |
| `--config` | Strong | Future adapter can point at session-local config |
| `--export` | Strong | Useful transcript/debug artifact if session-scoped |
| Normal permission mode | Manageable | Candidate safe default, still contained by Hazmat |
| Dangerous/bypass mode | Risky | Never use as Hazmat's default |
| `--respect-workspace-trust false` | Risky | Do not disable upstream trust checks silently |
| User `~/.config/devin` | Risky | Deny and harden host state; future adapter must use session-local state |
| Project `.devin` config | Mixed | Treat as project input only after config/import/MCP policy exists |
| MCP/hooks/config imports | Risky | Require Hazmat-owned allowlists for command, env, HTTP, and inherited policy |
| Cloud/account policy | Mixed | Document inherited authority and respect enterprise restrictions |

## Recipe-Only Shape

Users who already have Devin installed and authenticated inside the contained
agent account can run a single-turn task:

```bash
hazmat exec -C ~/workspace/project -- devin --permission-mode normal -p "summarize the current git diff"
```

For interactive work:

```bash
hazmat shell -C ~/workspace/project
devin --permission-mode normal
```

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `devin acp` directly:

```json
{
  "agent_servers": {
    "devin-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/devin",
        "acp"
      ],
      "env": {}
    }
  }
}
```

This is not first-class support. Hazmat contains the Devin process, project
paths, network policy, and credential deny zones, but it does not manage Devin
auth, Devin config, MCP servers, hooks, imported rules, project `.devin`
semantics, export retention, or cloud account policy.

## First-Class Requirements

Before `hazmat devin` is supportable:

- implement a built-in ACP/foreground adapter entry; do not expose a generic
  repo-defined ACP plugin host
- default to a non-bypass permission posture; never use the Open Design
  dangerous/workspace-trust-bypass argv as the default
- use session-local Devin config and state; never import host
  `~/.config/devin`
- define typed credentials for `WINDSURF_API_KEY` or Devin auth and keep broad
  environment passthrough rejected by default
- decide whether first-class support is ACP-only, foreground CLI-only, or both
- point `--config` and `--export` at session-scoped paths and clean up terminal
  residue
- default MCP servers, hooks, skills/rules, and config imports to disabled or
  allowlisted by Hazmat policy before launch
- treat project `.devin/config.json` and `.devin/config.local.json` as project
  inputs only after policy review, not as implicit authority grants
- add fake ACP server/client coverage for initialize/authenticate/prompt,
  malformed JSON-RPC, provider failure, permission failure, denied MCP/hook
  launch, disabled config imports, host-state denial, export cleanup, and git
  dirty state
- add non-interactive CLI smoke coverage for `--print`, `--prompt-file`,
  `--config`, and `--export`
- document inherited Devin account, team, and enterprise policy interactions in
  `hazmat explain`, trace output, and the harness docs

## Follow-Up

Devin remains recipe-only until the ACP/RPC adapter infrastructure can own
launch policy, credential policy, session-local config, and fake protocol
tests. The immediate deny-list hardening from `sandboxing-2sn4` should ship
independently because it protects users even when Devin is only run through
`hazmat exec`.
