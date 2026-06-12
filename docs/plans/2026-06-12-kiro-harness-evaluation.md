# Kiro Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.4`
Follow-up implemented: `sandboxing-nv0n`
Parent: `sandboxing-lg07.5`

Sources:

- Kiro CLI docs: <https://kiro.dev/docs/cli/>
- Kiro CLI ACP docs: <https://kiro.dev/docs/cli/acp/>
- Kiro CLI command reference: <https://kiro.dev/docs/cli/reference/cli-commands/>
- Kiro CLI settings reference: <https://kiro.dev/docs/cli/reference/settings/>
- Kiro CLI headless mode: <https://kiro.dev/docs/cli/headless/>
- Kiro CLI MCP docs: <https://kiro.dev/docs/cli/mcp/>
- Kiro CLI custom agents: <https://kiro.dev/docs/cli/custom-agents/>
- Kiro CLI tool permissions: <https://kiro.dev/docs/cli/chat/permissions/>
- Kiro CLI privacy and security: <https://kiro.dev/docs/cli/privacy-and-security/>
- Kiro CLI code intelligence: <https://kiro.dev/docs/cli/code-intelligence/>

## Decision

Do not add `hazmat kiro` in the next release.

Kiro is a strong future ACP/foreground harness candidate. Open Design's entry
matches current upstream docs: `kiro-cli acp` runs an ACP agent over JSON-RPC
stdin/stdout, editor examples use that exact subprocess shape, and Kiro has a
documented ACP method/capability surface. Kiro also has useful non-interactive
headless automation through API-key-backed prompts and a documented settings
surface with `KIRO_HOME` for state relocation.

First-class support still needs more than a wrapper. Kiro has global agents,
prompts, skills, steering, settings, sessions, tool-permission decisions,
MCP servers, custom agents, headless API-key auth, code-intelligence background
LSP processes, hooks-like workflow automation, checkpointing, and subscription
policy. Hazmat should not inherit those from the host profile or editor config.

For now, keep Kiro recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation found one immediate hardening gap: Kiro stores global state under
`~/.kiro`, with `KIRO_HOME` available to relocate it. `sandboxing-nv0n` adds
`~/.kiro` to Hazmat's credential deny floor and host credential hardening specs
while Kiro remains recipe-only. In TLA+, this is covered by the existing
`agentCliStateDir` abstraction for Kimi/Kiro-style external agent CLI state
roots, avoiding one finite-model dimension per vendor.

## Upstream Surface

Important surfaces for Hazmat:

- Kiro CLI can run interactive terminal sessions, headless automation, and ACP
  subprocess mode.
- `kiro-cli acp` communicates over JSON-RPC 2.0 on stdin/stdout and can be
  spawned by Zed, JetBrains, or any ACP-compatible editor.
- ACP examples recommend the full path to `kiro-cli` because editors may not
  inherit shell `PATH`.
- Headless mode requires `KIRO_API_KEY` and is aimed at CI/CD tasks such as
  review, test generation, and build troubleshooting.
- `KIRO_HOME` overrides the default `~/.kiro` directory used for global agents,
  prompts, skills, steering, settings, and sessions.
- Global custom agents live under `~/.kiro/agents/`; project-local agents live
  under `.kiro/agents/` and take precedence.
- Custom agents can include built-in tools, MCP servers, allowed tools, and
  OAuth-backed remote MCP entries.
- Tool permissions are persisted as trusted/per-request decisions and can be
  changed from the session.
- MCP servers can provide local or remote tools, including OAuth-backed
  services.
- Kiro code intelligence starts background LSP processes that maintain
  workspace indexes.
- Checkpointing creates a shadow bare Git repository for session file changes.
- Kiro privacy/security docs explicitly warn that the agent may access local
  files, environment variables, AWS credentials in the environment, and other
  sensitive configuration files.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `kiro-cli acp` | Strong | Good future ACP-driver candidate |
| JSON-RPC over stdio | Strong | Matches the accepted ACP/RPC driver shape |
| `KIRO_HOME` | Strong | Future adapter can force session-local state |
| Headless mode | Mixed | Useful smoke entrypoint, but requires typed `KIRO_API_KEY` and policy |
| User `~/.kiro` | Risky | Deny and harden host state; future adapter must use session-local state |
| Tool permissions | Risky | Do not import persisted trusted-tool decisions from host state |
| MCP servers | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and OAuth |
| Custom agents | Mixed | Treat project `.kiro/agents` as project input only after policy review |
| Global skills/prompts/steering | Risky | Disable or session-localize by default |
| Code intelligence | Mixed | Model background subprocesses and index state before first-class support |
| Checkpointing | Mixed | Keep shadow repos session-scoped and document overlap with Hazmat snapshots |
| Subscription policy | External | Explain inherited Kiro account/team/product restrictions |

## Recipe-Only Shape

Users who already have Kiro installed and authenticated inside the contained
agent account can run a foreground session:

```bash
hazmat shell -C ~/workspace/project
kiro-cli
```

For headless automation, pass an explicit API key only when the user intends to
grant Kiro account authority to that contained run:

```bash
hazmat exec -C ~/workspace/project -- env KIRO_API_KEY="$KIRO_API_KEY" kiro-cli chat --no-interactive --trust-tools=read,grep "summarize the current git diff"
```

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `kiro-cli acp` directly:

```json
{
  "agent_servers": {
    "kiro-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/kiro-cli",
        "acp"
      ],
      "env": {}
    }
  }
}
```

This is not first-class support. Hazmat contains the Kiro process, project
paths, network policy, and credential deny zones, but it does not manage Kiro
auth, `KIRO_HOME`, global agents/skills/prompts/steering, MCP servers, trusted
tool permissions, code-intelligence subprocesses, checkpoint state, headless
API-key policy, or subscription restrictions.

## First-Class Requirements

Before `hazmat kiro` is supportable:

- implement a built-in ACP/foreground adapter entry; do not expose a generic
  repo-defined ACP plugin host
- choose whether first-class support is ACP-only, foreground CLI-only, or both
- set `KIRO_HOME` to a session-local state root and never import host `~/.kiro`
- define typed `KIRO_API_KEY` materialization for headless mode; reject broad
  environment passthrough by default
- generate or validate a minimal Kiro settings/profile state with no inherited
  trusted-tool decisions
- default user/global custom agents, prompts, skills, steering, MCP servers,
  and checkpoint state to disabled, session-local, or explicitly allowlisted
- treat project `.kiro/agents`, `.kiro` config, and MCP references as project
  inputs only after policy review
- model or disable code-intelligence background LSP/index subprocesses before
  launch
- keep ACP trace recording, logs, sessions, and checkpoints session-scoped
- add fake ACP coverage for initialize/session/model/prompt/cancel/list,
  malformed JSON-RPC, tool-permission paths, MCP forwarding rejected/allowed
  cases, stderr/stdout isolation, and cleanup
- add fake CLI/headless coverage for API-key absence, API-key materialization,
  provider failure, permission failure, denied MCP/custom-agent loading,
  host-state denial, session-local cleanup, and git dirty state
- document inherited Kiro account, subscription, and enterprise policy
  interactions in `hazmat explain`, trace output, and harness docs

## Follow-Up

Kiro remains recipe-only until the ACP/RPC adapter infrastructure can own
launch policy, credential policy, session-local `KIRO_HOME`, MCP/custom-agent
policy, tool permissions, and fake protocol tests. The immediate deny-list
hardening from `sandboxing-nv0n` should ship independently because it protects
users even when Kiro is only run through `hazmat exec`.
