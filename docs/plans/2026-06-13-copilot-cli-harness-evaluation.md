# GitHub Copilot CLI Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.4.4`
Follow-up implemented: `sandboxing-6qyj`
Parent: `sandboxing-lg07.4`

Sources:

- Copilot CLI overview:
  <https://docs.github.com/en/copilot/how-tos/copilot-cli/use-copilot-cli/overview>
- Copilot CLI command reference:
  <https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference>
- Copilot CLI config directory reference:
  <https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference>
- Copilot CLI authentication:
  <https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli>
- Copilot CLI tool permissions:
  <https://docs.github.com/en/copilot/how-tos/copilot-cli/use-copilot-cli/allowing-tools>
- Agent skills for Copilot CLI:
  <https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills>
- Open Design Copilot runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/copilot.ts`
- Open Design adapter notes:
  `/Users/dr/workspace/opendesign/docs/agent-adapters.md`

## Decision

Do not add `hazmat copilot` in the next release.

GitHub Copilot CLI is a plausible future foreground harness. It is a terminal
agent with programmatic JSON output, stdin prompt delivery, model selection,
MCP support, skills, trusted folders, and a documented permission model. Open
Design already drives it as a one-request adapter by piping the prompt to stdin
and parsing JSONL output.

The current shape is still too risky for a quick Hazmat wrapper. Official docs
state that Copilot CLI stores configuration, session history, logs, MCP config,
skills, trusted folders, and customizations under `~/.copilot` by default, and
that auth can fall back to plaintext config there when no credential store is
available. Authentication can also use `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or
`GITHUB_TOKEN`, which overlaps with Hazmat's GitHub token authority. Programmatic
runs require broad approval such as `--allow-all-tools`; `--allow-all`/`--yolo`
also widens paths and URLs.

For now, keep Copilot CLI recipe-only through `hazmat exec` or `hazmat shell`.
`sandboxing-6qyj` adds `~/.copilot` to Hazmat's credential deny floor and host
credential hardening specs while Copilot CLI remains unsupported as a
first-class harness. In TLA+, this is covered by the existing
`agentCliStateDir` representative for external agent CLI/service state roots.

## Upstream Surface

Important surfaces for Hazmat:

- Copilot CLI can answer questions, write/debug code, execute commands, modify
  files, and interact with GitHub.
- It asks users to trust the current folder because it may read, modify, and
  execute files in and below that folder.
- The default config directory is `~/.copilot`; `COPILOT_HOME` can override it.
- `~/.copilot` can contain settings, trusted folders, MCP config, sessions,
  logs, skills, custom agents, and auth fallback files.
- Credential lookup order includes `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`,
  `GITHUB_TOKEN`, system keychain, and GitHub CLI fallback.
- If a credential store is unavailable, auth may be stored in plaintext under
  the Copilot home.
- Copilot CLI supports local and remote MCP servers, with organization and
  enterprise policy controls outside Hazmat.
- Personal skills can live under `~/.copilot/skills` or `~/.agents/skills`;
  project skills can live under repo-local skill directories.
- `--allow-all-tools` allows all tools without confirmation and is required by
  GitHub docs for programmatic use.
- `--allow-all` and `--yolo` are broader than tool approval because they also
  allow all paths and URLs.
- Deny/allow tool options can target shell commands, write tools, and MCP
  tools, but Hazmat still needs fake CLI coverage before relying on that
  surface.
- Open Design uses `--add-dir` to widen Copilot's path-level sandbox for
  external skills and design-system files.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Stdin prompt + JSON output | Strong | Good future fake-binary smoke path |
| `COPILOT_HOME` | Strong | Use for session-local state in a future adapter |
| `~/.copilot` | Risky | Deny and harden host state by default |
| GitHub auth env | Risky | Requires typed, consumer-scoped credential grants |
| GitHub CLI fallback | Risky | Do not inherit host `gh` auth implicitly |
| `--allow-all-tools` | Risky | Needs explicit Hazmat permission posture |
| `--allow-all` / `--yolo` | Too broad | Never use as a default |
| MCP servers | Risky | Require command/env/network allowlisting |
| Skills | Mixed | Prefer repo/session-staged skills over host personal skills |
| Trusted folders | Risky | Generate session-local trust; do not import host decisions |
| `--add-dir` | Mixed | Stage or validate external asset paths before forwarding |

## Recipe-Only Shape

Users who already installed and authenticated Copilot CLI inside the contained
agent account can run an interactive session:

```bash
hazmat shell -C ~/workspace/project
copilot
```

Programmatic use is possible only when the user explicitly accepts Copilot's
permission posture:

```bash
hazmat exec -C ~/workspace/project -- copilot --allow-all-tools --output-format json "summarize the current git diff"
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage GitHub auth,
GitHub CLI fallback, Copilot home, trusted folders, MCP servers, skills,
allowed tools, path/URL permissions, organization policy, or JSONL schema drift.

## First-Class Requirements

Before `hazmat copilot` is supportable:

- add a built-in `HarnessCopilot` entry with explicit metadata and explain
  output
- set `COPILOT_HOME` to a session-local directory; never import host
  `~/.copilot`
- define typed GitHub/Copilot credential grants that do not silently reuse broad
  `GH_TOKEN`, `GITHUB_TOKEN`, host keychain, or `gh auth token`
- decide the exact non-interactive permission posture; prefer narrow
  allow/deny tool policy over `--allow-all-tools`, and never default to
  `--allow-all` or `--yolo`
- generate or validate trusted-folder state for the session/project only
- default MCP servers and personal skills to disabled unless explicitly staged
  or allowlisted by Hazmat policy
- validate or stage external skill/design-system paths instead of forwarding
  arbitrary host `--add-dir` grants
- add JSONL parser tests for deltas, reasoning, tool events, result usage,
  malformed records, errors, empty output, and schema drift
- add fake CLI coverage for missing auth, typed token materialization, host
  state denial, session-local cleanup, trust decisions, MCP rejection, skill
  staging, permission flags, and git dirty state
- document GitHub organization/enterprise Copilot policy inheritance in
  `hazmat explain`

## Follow-Up

Copilot CLI remains recipe-only until Hazmat owns its state root, GitHub auth
boundary, permission posture, MCP/skill policy, trusted folders, `--add-dir`
policy, and JSONL parser contract. The immediate deny-list hardening from
`sandboxing-6qyj` should ship independently because it protects users even
when Copilot CLI is only run through `hazmat exec`.
