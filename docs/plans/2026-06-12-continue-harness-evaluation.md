# Continue Harness Candidate Evaluation

Status: Compatibility decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.7`
Parent: `sandboxing-lg07.7`

Sources:

- Continue homepage: <https://www.continue.dev/>
- Continue CLI guide: <https://docs.continue.dev/guides/cli>
- Continue `config.yaml` reference: <https://docs.continue.dev/reference>
- Continue models, rules, and tools guide: <https://docs.continue.dev/guides/configuring-models-rules-tools>
- Continue local secrets FAQ: <https://docs.continue.dev/faqs#managing-local-secrets-and-environment-variables>
- Continue documentation MCP reference: <https://docs.continue.dev/reference/continue-mcp>
- Continue documentation automation guide: <https://docs.continue.dev/guides/doc-writing-agent-cli>
- Continue GitHub README: <https://github.com/continuedev/continue>

## Decision

Do not add `hazmat continue` in the next release.

Continue is a plausible future foreground/headless harness because `cn -p`
gives a clean scripted entrypoint. It is also more config and CI/check oriented
than most terminal coding agents: the current product positioning emphasizes
source-controlled PR checks, while the CLI docs position `cn` as an interactive
or headless coding agent.

For now, keep Continue as recipe-only through `hazmat exec` or `hazmat shell`.
First-class support should wait for the closed harness adapter registry to own
Continue-specific install, config, permission, credential, MCP, and smoke-test
boundaries.

## Upstream Surface

Important surfaces for Hazmat:

- `cn` has interactive and headless modes; `cn -p` prints only the final
  response and is suitable for shell automation.
- the CLI can edit files, run terminal commands, and use repository context,
  subject to Continue's own tool permission flow.
- `cn` uses the same `config.yaml` model/rule/tool format as Continue.
- configuration can come from local workspace/global state or Continue Mission
  Control/Hub references.
- models and MCP servers can carry secrets through `${{ secrets.NAME }}`.
- local secret resolution includes project `.env`, project `.continue/.env`,
  global `~/.continue/.env`, and process environment variables for the CLI.
- tool permission approvals are persisted in `~/.continue/permissions.yaml`.
- MCP servers can be configured under workspace `.continue/mcpServers`.
- automation docs show `cn` used inside git workflows to inspect diffs, update
  files, commit, and push.
- the public GitHub README states that the `continuedev/continue` repository is
  read-only/no longer actively maintained, while the product site now centers
  PR-check workflows.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Headless `cn -p` | Strong | Good future fake-provider smoke entrypoint |
| Interactive CLI | Moderate | Supportable as a foreground harness after adapter registry work |
| CI/PR checks | Mixed | Better as recipe/integration guidance first; Hazmat should not own hosted GitHub app behavior |
| Local/global `~/.continue` state | Risky | Keep host `~/.continue` denied; use session-local managed state only |
| Workspace `.continue/*` | Mixed | Treat as project input, but do not import global state or unreviewed MCP servers silently |
| Secret resolution | Risky | Requires typed grants and explicit env/config precedence; never write durable provider secrets into repo files |
| MCP servers | Risky | Requires allowlisted command/network policy before first-class support |
| Tool permissions cache | Manageable | Future adapter should set explicit flags/defaults and treat permission cache as session-local residue |
| Install/update | Mixed | Avoid curl-pipe bootstrap in Hazmat-managed flows; prefer reviewed package/runtime path |

## Recipe-Only Shape

Users who already have Continue installed in the contained account can run
headless prompts:

```bash
hazmat exec -C ~/workspace/project -- cn -p "summarize the current git diff"
```

For interactive use:

```bash
hazmat shell -C ~/workspace/project
cn
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Continue auth,
global Continue state, model configuration, MCP servers, permission caches, or
hosted Mission Control behavior.

## First-Class Requirements

Before `hazmat continue` is supportable:

- define a built-in foreground adapter entry with install/update/status and
  uninstall scope
- choose a managed install path compatible with Node 20+ without running
  upstream curl-pipe bootstrap scripts
- decide whether Continue receives model credentials as process env vars,
  session-local `.continue/.env`, or generated config; do not write durable
  secrets into repository `.env` files
- deny host `~/.continue` by default and create a session-local Continue home
  for config, permissions, indexes, and cache residue
- add typed credential grants for explicitly supported providers and
  `CONTINUE_API_KEY` only when the user grants Continue cloud access
- default MCP servers to disabled unless they are repo-visible and allowlisted
  by Hazmat policy
- model tool permission defaults with explicit `allow` / `ask` / `exclude`
  policy instead of importing persisted host approvals
- add fake-provider smoke coverage for `cn -p` success, model failure,
  malformed responses, tool-denied edits, terminal command denial, workspace
  `.continue` handling, global-state denial, and cleanup
- document how Continue-generated edits interact with Hazmat snapshots,
  rollback, and git status

## Follow-Up

Create a future implementation bead only after the adapter registry and
session-local tool-state policy are ready to accept another foreground harness.
Until then, Continue remains a strong recipe candidate and a useful reference
for headless `-p` smoke-test design.
