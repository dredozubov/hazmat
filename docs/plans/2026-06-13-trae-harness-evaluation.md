# Trae CLI Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.5.7`
Follow-up implemented: `sandboxing-jrdt`
Parent: `sandboxing-lg07.5`

Sources:

- Trae CLI quickstart: <https://www.volcengine.com/docs/86677/2227861>
- Trae CLI global settings: <https://www.volcengine.com/docs/86677/2227869>
- Trae CLI MCP docs: <https://docs.trae.cn/cli/model-context-protocol>
- Trae CLI skills docs: <https://docs.trae.cn/cli/skills>
- Trae CLI permission mode: <https://docs.trae.cn/cli/permission-mode>
- Trae CLI login token: <https://www.volcengine.com/docs/86677/2227863>
- Trae CLI ACP docs: <https://www.volcengine.com/docs/86677/2227871>
- Open Design Trae CLI runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/trae-cli.ts`

## Decision

Do not add `hazmat trae` in the next release.

Trae is a plausible future ACP harness candidate. Open Design launches it as
`traecli acp serve --yolo`, with ACP JSON-RPC over stdio and mature ACP MCP
discovery. Current vendor docs also expose first-class Trae CLI sections for
global settings, MCP, skills, memory, tool permissions, permission modes, ACP,
and CLI login tokens.

The Open Design argv is not acceptable as a Hazmat default because it enables
`--yolo`. Trae's own docs make permission mode and tool permissions explicit
configuration surfaces, and Hazmat should not silently inherit a host Trae
profile or start with the least restrictive mode. A first-class adapter needs
session-local config, typed token/account handling, explicit MCP/tool policy,
and fake ACP coverage before shipping.

For now, keep Trae recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation found one immediate hardening gap: Trae CLI stores global state for
login tokens, settings, MCP, skills, memory, permissions, and custom commands
under its local Trae CLI root. `sandboxing-jrdt` adds `~/.traecli` to Hazmat's
credential deny floor and host credential hardening specs while Trae remains
recipe-only. In TLA+, this is covered by the existing `agentCliStateDir`
abstraction for Kilo/Kimi/Kiro/Vibe/Trae-style external agent CLI state roots,
avoiding one finite-model dimension per vendor.

## Upstream Surface

Important surfaces for Hazmat:

- `traecli acp serve` is the ACP server shape used by Open Design.
- Open Design adds `--yolo` to the ACP launch argv; Hazmat must not use that
  as a default.
- Trae CLI has explicit docs for global settings, custom agents, models, MCP,
  skills, memory, tool permissions, permission modes, response language, ACP,
  and CLI login tokens.
- The docs describe editing global config with `traecli config edit`, including
  fields such as model selection and permission mode.
- MCP config is editable through Trae CLI global config and can include local
  command-based servers.
- Skills are stored under a Trae CLI local root such as `traecli/skills`.
- Custom slash commands can live in project `.traecli/commands`.
- The quickstart documents automatic upgrade checks on startup and manual
  updates through `traecli update`.
- CLI login tokens and `TRAECLI_HOST` are part of enterprise/private-domain
  setup, so account/team/domain policy is inherited from Trae.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `traecli acp serve` | Strong | Good future ACP-driver candidate |
| ACP JSON-RPC over stdio | Strong | Matches the accepted ACP/RPC driver shape |
| `--yolo` | Risky | Never use as Hazmat's default |
| Global Trae CLI root | Risky | Deny and harden host state; future adapter must use session-local state |
| CLI login token | Risky | Requires typed credential/account grant |
| MCP servers | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and account scope |
| Skills and memory | Mixed | Allow only repo-visible/session-local assets after policy review |
| Project `.traecli` commands | Mixed | Treat as project input only after command policy review |
| Auto-update | Risky | Disable or route through managed install/update trust |
| Enterprise controls | External | Explain inherited Trae account/domain/team restrictions |

## Recipe-Only Shape

Users who already have Trae installed and authenticated inside the contained
agent account can run a foreground session:

```bash
hazmat shell -C ~/workspace/project
traecli
```

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `traecli acp serve` directly:

```json
{
  "agent_servers": {
    "trae-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/traecli",
        "acp",
        "serve"
      ],
      "env": {}
    }
  }
}
```

This is not first-class support. Hazmat contains the Trae process, project
paths, network policy, and credential deny zones, but it does not manage Trae
auth, global Trae config, CLI login tokens, MCP servers, skills, memory,
permission modes, `--yolo`, auto-update behavior, or enterprise-domain policy.

## First-Class Requirements

Before `hazmat trae` is supportable:

- implement a built-in ACP adapter entry; do not expose a generic repo-defined
  ACP plugin host
- use a session-local Trae CLI config/state root and never import host
  `~/.traecli`
- define typed Trae CLI login-token/account materialization and reject broad
  environment passthrough by default
- choose a non-`--yolo` default permission posture and document any explicit
  escape hatch separately
- generate or validate minimal config with auto-update disabled or routed
  through a managed install/update path
- default MCP servers, skills, memory, custom agents, and project
  `.traecli/commands` to disabled or allowlisted by Hazmat policy
- add fake ACP coverage for initialize/model/session/prompt/cancel/list,
  malformed JSON-RPC, MCP forwarding rejected/allowed cases, permission-mode
  behavior, stdout/stderr isolation, and cleanup
- add fake CLI coverage for missing auth, typed token materialization,
  auto-update disabled, host-state denial, session-local cleanup, and git dirty
  state
- document inherited Trae account, enterprise domain, MCP/tool-permission, and
  CLI token authority in `hazmat explain`, trace output, and harness docs

## Follow-Up

Trae remains recipe-only until the ACP/RPC adapter infrastructure can own
launch policy, credential policy, session-local Trae state, MCP/skills/memory
policy, `--yolo` defaults, and fake protocol tests. The immediate deny-list
hardening from `sandboxing-jrdt` should ship independently because it protects
users even when Trae is only run through `hazmat exec`.
