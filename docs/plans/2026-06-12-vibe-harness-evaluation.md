# Mistral Vibe Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.5.6`
Follow-up implemented: `sandboxing-3bzx`
Parent: `sandboxing-lg07.5`

Sources:

- Mistral Vibe CLI install/setup: <https://docs.mistral.ai/vibe/code/cli/install-setup>
- Mistral Vibe CLI quickstart: <https://docs.mistral.ai/getting-started/quickstarts/vibe-code/install-cli>
- Mistral Vibe repository: <https://github.com/mistralai/mistral-vibe>
- Mistral Vibe ACP setup: <https://github.com/mistralai/mistral-vibe/blob/main/docs/acp-setup.md>
- Zed Mistral Vibe ACP page: <https://zed.dev/acp/agent/mistral-vibe>
- Mistral Vibe VS Code authentication: <https://docs.mistral.ai/vibe/code/vs-code-extension/install-authenticate>

## Decision

Do not add `hazmat vibe` in the next release.

Mistral Vibe is a strong future ACP/foreground harness candidate. Open Design's
entry matches current upstream docs: `vibe-acp` is the ACP subprocess command,
and Mistral's ACP setup docs show editors launching that command directly.
Vibe also has a normal foreground CLI (`vibe`) with setup wizard, local model
support, Mistral-hosted model support, and shell command execution on approval.

First-class support still needs more than wrapping `vibe-acp`. The install path
uses a curl-pipe installer that installs or upgrades Python tooling through
`uv`; first-run setup stores API keys in the Vibe home directory; the ACP
server assumes Vibe is already configured; and model/provider credentials can
come from stored config or `MISTRAL_API_KEY`. Hazmat needs to own install
trust, credential materialization, and session-local state before exposing
`hazmat vibe`.

For now, keep Vibe recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation found one immediate hardening gap: Vibe creates `~/.vibe/config.toml`
and stores API keys under the Vibe home directory. `sandboxing-3bzx` adds
`~/.vibe` to Hazmat's credential deny floor and host credential hardening specs
while Vibe remains recipe-only. In TLA+, this is covered by the existing
`agentCliStateDir` abstraction for Kilo/Kimi/Kiro/Vibe-style external agent CLI
state roots, avoiding one finite-model dimension per vendor.

## Upstream Surface

Important surfaces for Hazmat:

- `vibe` is the foreground CLI coding agent; `vibe-acp` is the ACP server entry.
- The one-line installer runs `curl -LsSf https://mistral.ai/vibe/install.sh |
  bash`, checks/installs `uv`, and installs or upgrades `mistral-vibe`.
- On first launch, `vibe` creates `~/.vibe/config.toml` and runs a setup wizard
  to register an API key.
- Mistral-hosted models can use account/plan access; API-plan users paste a
  key from Studio; local models or compatible providers can also be used.
- `MISTRAL_API_KEY` can be used instead of the interactive prompt.
- `vibe-acp` is intended for ACP-aware editors after the normal `vibe` setup
  has already configured credentials.
- The foreground CLI can read project context, generate code, edit files, and
  run shell commands when approved.
- Programmatic mode supports `vibe --prompt ...`; trust enforcement still
  applies, but interactive confirmation is unavailable, so `--trust` is an
  explicit per-invocation grant.
- Vibe remembers trusted folders in `~/.vibe/trusted_folders.toml`, and config
  includes auto-update, notifications, telemetry, agents, and skills.
- The VS Code extension invokes the bundled Vibe agent with `--setup` and
  stores credentials in the Vibe home directory.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| `vibe-acp` | Strong | Good future ACP-driver candidate |
| `vibe` foreground CLI | Strong | Useful recipe and future manual smoke entrypoint |
| `~/.vibe` | Risky | Deny and harden host state; future adapter must use session-local state |
| `MISTRAL_API_KEY` | Manageable | Needs typed credential materialization |
| Setup wizard | Mixed | Keep out of first-class automation until session-local state is owned |
| Curl/uv installer | Risky | Requires managed install/update trust before bootstrap support |
| ACP-only command | Mixed | Adapter must define cwd/workspace and config path explicitly |
| Local models/providers | Mixed | Treat as separate integration/provider policy, not implicit passthrough |

## Recipe-Only Shape

Users who already have Vibe installed and authenticated inside the contained
agent account can run a foreground session:

```bash
hazmat shell -C ~/workspace/project
vibe
```

For an ACP-aware editor, use the generic ACP recipe shape and launch Hazmat as
the subprocess wrapper rather than launching `vibe-acp` directly:

```json
{
  "agent_servers": {
    "vibe-contained": {
      "command": "/usr/local/bin/hazmat",
      "args": [
        "exec",
        "--no-backup",
        "-C",
        "/Users/dr/workspace/example-project",
        "--",
        "/Users/agent/.local/bin/vibe-acp"
      ],
      "env": {}
    }
  }
}
```

For automation where the user intentionally grants a Mistral API key:

```bash
hazmat exec -C ~/workspace/project -- env MISTRAL_API_KEY="$MISTRAL_API_KEY" vibe --prompt "summarize the current git diff"
```

This is not first-class support. Hazmat contains the Vibe process, project
paths, network policy, and credential deny zones, but it does not manage Vibe
auth, stored API keys, setup wizard behavior, install/update trust, local model
config, or ACP trace retention.

## First-Class Requirements

Before `hazmat vibe` is supportable:

- implement a built-in ACP/foreground adapter entry; do not expose a generic
  repo-defined ACP plugin host
- choose whether first-class support is ACP-only, foreground CLI-only, or both
- force a session-local Vibe home/config root; never import host `~/.vibe`
- define typed `MISTRAL_API_KEY` and any supported provider/local-model
  credential grants; reject broad environment passthrough by default
- decide how `--trust`, trusted-folder persistence, and `--auto-approve`
  interact with Hazmat's outer containment before using programmatic mode as a
  release gate
- replace curl-pipe/implicit upgrade behavior with a managed install/update
  story before adding bootstrap support
- decide how the setup wizard is represented, skipped, or contained when
  first-class support generates session-local config
- add fake ACP coverage for initialize/session/model/prompt/cancel/list,
  malformed JSON-RPC, stdout/stderr isolation, provider failure, missing auth,
  and cleanup
- add fake CLI coverage for foreground launch, setup-required failure,
  typed credential materialization, host-state denial, session-local cleanup,
  and git dirty state
- document inherited Mistral account/plan/API-key authority in
  `hazmat explain`, trace output, and harness docs

## Follow-Up

Mistral Vibe remains recipe-only until the ACP/RPC adapter infrastructure can
own launch policy, credential policy, session-local Vibe state, install trust,
and fake protocol tests. The immediate deny-list hardening from
`sandboxing-3bzx` should ship independently because it protects users even when
Vibe is only run through `hazmat exec`.
