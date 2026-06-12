# Grok Build Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.4.6`
Follow-up implemented: `sandboxing-izm9`
Parent: `sandboxing-lg07.4`

Sources:

- xAI Grok Build landing page: <https://x.ai/cli>
- xAI Grok Build announcement: <https://x.ai/news/grok-build-cli>
- xAI Grok Build getting started docs: <https://docs.x.ai/build/overview>
- xAI Grok Build headless docs:
  <https://docs.x.ai/build/cli/headless-scripting>
- xAI Grok Build enterprise docs: <https://docs.x.ai/build/enterprise>
- xAI Grok Build skills and plugins docs:
  <https://docs.x.ai/build/features/skills-plugins-marketplaces>
- Open Design Grok Build runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/grok-build.ts`
- Open Design adapter notes:
  `/Users/dr/workspace/opendesign/docs/agent-adapters.md`

## Decision

Do not add `hazmat grok` in the next release.

Grok Build is a strong future foreground harness candidate. It has an
interactive TUI, headless mode, JSON and streaming JSON output, model
selection, an ACP server, explicit permission modes, and an enterprise policy
surface. Open Design already has a runtime definition for the `grok` binary and
uses prompt-file transport because recent CLI builds no longer reliably accept
large prompts on stdin.

The current surface is still too broad for a quick Hazmat wrapper. Grok Build
is in beta, owns authentication and session state under `~/.grok`, can use a
browser session, device auth, an external auth provider command, API keys, or
model-specific config keys, and resolves credentials with multiple precedence
layers. It also has auto-update, skills, plugins, hooks, MCP servers,
marketplace installs, Claude compatibility, user-level `~/.agents` discovery,
remote session sync, share links, and always-approve mode. It ships its own
sandbox profiles, but Hazmat cannot rely on a nested tool sandbox to define the
outer containment boundary.

For now, keep Grok Build recipe-only through `hazmat exec` or `hazmat shell`.
`sandboxing-izm9` adds `~/.grok` to Hazmat's credential deny floor and host
credential hardening specs while Grok Build remains unsupported as a
first-class harness. In TLA+, this is covered by the existing
`agentCliStateDir` representative for external agent CLI/service state roots.

## Upstream Surface

Important surfaces for Hazmat:

- Official install uses a shell script from `x.ai`; npm install is documented
  as an enterprise alternative.
- First launch can open browser auth; headless environments can use device auth
  or `XAI_API_KEY`.
- Credential resolution can use model-specific config keys, model env keys,
  active session tokens, or `XAI_API_KEY`.
- Config loads from `/etc/grok` and `~/.grok` layers, including managed and
  requirement files with policy precedence.
- Headless mode supports `-p`, model selection, session resume, continue,
  `--cwd`, `--output-format`, `--always-approve`, and `--no-alt-screen`.
- Headless and ACP scripts should pass `--no-auto-update` or disable updates
  in config.
- Output formats include `plain`, final `json`, and newline-delimited
  `streaming-json`.
- ACP is available through `grok agent stdio` with cached-token or API-key auth
  methods.
- Grok stores headless sessions in `~/.grok/sessions`.
- Skills and plugins can load from project `.grok` roots, user `~/.grok`
  roots, plugin roots, marketplaces, and explicit plugin directories.
- Hooks can execute scripts from user, project, and plugin locations.
- Grok reads Claude Code marketplaces, plugins, skills, MCPs, agents, hooks,
  and instruction files for compatibility.
- It also reads user-level `~/.agents/skills` and `~/.agents/commands`.
- Enterprise docs list remote session sync and sharing through `code.grok.com`
  as optional but available.
- Grok's own sandbox supports macOS Seatbelt and Linux Landlock profiles, but
  defaults to off.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Headless `-p` mode | Strong | Good future fake-binary smoke path |
| JSON and streaming JSON | Strong | Needs parser tests and schema drift handling |
| ACP stdio | Strong | Route through ACP driver architecture if selected |
| `~/.grok` state | Risky | Deny and harden host state by default |
| Browser/device/session auth | Risky | Requires typed auth and session-local state |
| `XAI_API_KEY` | Risky | Requires typed credential materialization |
| External auth provider command | Risky | Disable or allowlist explicitly |
| Always-approve | Risky | Needs explicit Hazmat permission posture |
| Auto-update | Risky | Disable or route through managed update policy |
| Skills/plugins/hooks/MCP | Risky | Require staging and allowlisting |
| Remote sync/share | Backend | Disable for local containment by default |
| Nested Grok sandbox | Mixed | Useful defense-in-depth, not a substitute for Hazmat |

## Recipe-Only Shape

Users who already installed and authenticated Grok Build inside the contained
agent account can run an interactive session:

```bash
hazmat shell -C ~/workspace/project
grok
```

A one-shot contained run is possible when the user explicitly chooses the
tool's approval and update posture:

```bash
hazmat exec -C ~/workspace/project -- env XAI_API_KEY="$XAI_API_KEY" grok --no-auto-update -p "summarize the current git diff" --output-format json --always-approve
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Grok auth,
session state, auto-update, skills, plugins, hooks, MCP servers, Claude
compatibility imports, user-level agent assets, remote sync, share links,
approval semantics, or output parser drift.

## First-Class Requirements

Before `hazmat grok` is supportable:

- add a built-in `HarnessGrok` entry with explicit metadata and explain output
- redirect Grok state to a session-local root; never import host `~/.grok`
- define typed `XAI_API_KEY` materialization and reject broad env passthrough
- decide whether browser/device auth is supported, and if so model the exact
  credential and callback boundaries
- disable external auth provider commands unless explicitly allowlisted
- disable auto-update by default or route updates through managed
  install/update policy
- decide and document the permission posture for `--always-approve`
- prefer prompt-file or stdin transport over prompt-as-argv and test long
  prompt behavior
- add JSON and streaming-json parser tests for deltas, tool events, errors,
  usage, malformed records, empty output, and schema drift
- add fake CLI coverage for auth resolution, session-local state cleanup,
  host-state denial, auto-update disabled, permission flags, model selection,
  external auth provider rejection, skill/plugin/hook/MCP policy, remote sync
  disabled, and git dirty state
- decide whether ACP support belongs in the foreground harness or a separate
  ACP driver and add protocol tests before exposing it
- document that Grok's nested sandbox is optional defense-in-depth and does not
  replace Hazmat's outer containment

## Follow-Up

Grok Build remains recipe-only until Hazmat owns its state root, credential
path, auth modes, update behavior, approval posture, plugin/hook/MCP policy,
remote sync defaults, ACP boundary, and output parser contract. The immediate
deny-list hardening from `sandboxing-izm9` should ship independently because it
protects users even when Grok Build is only run through `hazmat exec`.
