# Local ACP Agent Under Hazmat

Use this recipe when you are developing or testing an Agent Client Protocol
(ACP) agent that runs as a local subprocess over stdio.

This is recipe-only support. Hazmat does not provide a generic `hazmat acp`
wrapper, does not install ACP registry entries, and does not import host agent
profiles or credentials for arbitrary ACP servers.

## Fit

Good fit:

- a local ACP server that an editor can launch as a subprocess
- JSON-RPC over stdin/stdout
- project paths passed as absolute paths
- fake-agent or development-agent testing where provider credentials are not
  needed

Poor fit:

- remote ACP over HTTP or WebSocket
- an ACP server that must run as a daemon before the editor starts
- an agent that requires the host Docker socket from native containment
- a client/agent setup that depends on importing host MCP, profile, or auth
  state
- browser automation or GUI control as the containment boundary

## Decision

Docs are enough for the current release. A thin `hazmat acp` wrapper is not
justified yet.

The safe Phase 0 path is to let the editor launch `hazmat exec`, and let Hazmat
contain the ACP agent subprocess exactly like any other local command. Hazmat
status and session-contract output goes to stderr; stdout stays reserved for
the agent protocol stream.

A first-class wrapper becomes worthwhile only when Hazmat owns a specific
built-in adapter. That adapter would need typed launch argv, request shape,
credentials, profile policy, fake-server tests, and docs. A generic wrapper
that accepts repo-defined ACP descriptors would be a plugin host, which is
outside Hazmat's trust model.

## Zed Custom Agent Shape

Zed's custom External Agent settings let you specify a command, args, and env.
For a contained local agent, make the command Hazmat and put the real ACP server
after `hazmat exec`.

```json
{
  "agent_servers": {
    "contained-local-agent": {
      "type": "custom",
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "--docker=none",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/dr/workspace/example-project/target/debug/my-acp-agent",
        "--stdio"
      ],
      "env": {}
    }
  }
}
```

Adjust the project path and ACP server path for your machine. Prefer absolute
paths; ACP itself requires protocol file paths to be absolute.

For a pure fake-server test, add `--network none` before `-C` so the contained
process cannot make outbound network connections:

```json
"args": [
  "exec",
  "--no-backup",
  "--network",
  "none",
  "-C",
  "/Users/dr/workspace/example-project",
  "--",
  "/Users/dr/workspace/example-project/target/debug/fake-acp-agent",
  "--stdio"
]
```

## Credential Boundary

Do not put provider API keys, OAuth tokens, GitHub tokens, or MCP secrets in
the editor `env` block for a generic ACP recipe. Hazmat cannot type-check,
redact, materialize, or clean up arbitrary ACP credentials from that field.

Use one of these paths instead:

- test with a fake model/provider while developing the ACP protocol surface
- configure the agent inside the contained agent account if the agent has its
  own safe local setup flow
- promote the agent to a built-in Hazmat adapter only after defining typed
  credential registry entries and fake-server tests

The recipe also does not import host MCP config. If the ACP agent starts MCP
servers, they run inside the same contained process tree and inherit only what
the session intentionally provides.

## Docker Boundary

Native Hazmat containment must not expose the host Docker socket to a generic
ACP agent. If the project itself needs private Docker-daemon semantics, evaluate
Docker Sandbox mode separately:

```json
"args": [
  "exec",
  "--no-backup",
  "--docker=sandbox",
  "-C",
  "/Users/dr/workspace/example-project",
  "--",
  "/Users/dr/workspace/example-project/target/debug/my-acp-agent",
  "--stdio"
]
```

Do not use this to run an ACP platform that manages long-lived containers,
browser sessions, or service ports. Those belong to the service-harness boundary
and need lifecycle, readiness, local-only attach, cleanup, and credential
modeling before first-class support.

## Validation Checklist

- The ACP client launches `hazmat exec`, not the raw agent directly.
- Hazmat messages appear on stderr; stdout remains valid ACP JSON-RPC traffic.
- The agent receives an absolute project path and runs with the expected cwd.
- No host profile, MCP config, auth file, or Docker socket is imported.
- Provider credentials are absent or typed through a future built-in adapter.
- `--network none` works for fake-server tests that do not call a model
  provider.

## Sources

- ACP introduction: <https://agentclientprotocol.com/get-started/introduction>
- ACP protocol overview: <https://agentclientprotocol.com/protocol/v1/overview>
- ACP repository and versioning: <https://github.com/agentclientprotocol/agent-client-protocol>
- Zed External Agents custom-agent settings:
  <https://zed.dev/docs/ai/external-agents>
