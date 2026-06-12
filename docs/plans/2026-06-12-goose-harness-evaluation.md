# Goose Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.5`
Follow-up implemented: `sandboxing-vp3k`
Parent: `sandboxing-lg07.7`

Sources:

- Goose documentation: <https://goose-docs.ai/>
- Goose installation guide: <https://goose-docs.ai/docs/getting-started/installation/>
- Goose CLI commands: <https://goose-docs.ai/docs/guides/goose-cli-commands/>
- Goose running tasks guide: <https://goose-docs.ai/docs/guides/running-tasks/>
- Goose headless mode guide: <https://goose-docs.ai/docs/tutorials/headless-goose/>
- Goose configuration files: <https://goose-docs.ai/docs/guides/config-files/>
- Goose environment variables: <https://goose-docs.ai/docs/guides/environment-variables/>
- Goose extension allowlist: <https://goose-docs.ai/docs/guides/allowlist>
- Goose ACP clients guide: <https://goose-docs.ai/docs/guides/acp-clients/>
- Goose usage data: <https://goose-docs.ai/docs/guides/usage-data/>
- Goose logging system: <https://goose-docs.ai/docs/guides/logs/>

## Decision

Do not add `hazmat goose` in the next release.

Goose is a strong future harness candidate, but it is broader than a normal
foreground coding CLI. It has CLI, Desktop, headless automation, recipes, MCP
extensions, ACP client/provider surfaces, local session history, and optional
Goose-side sandboxing. A quick wrapper would leave too many authority decisions
to Goose config that Hazmat does not own.

For now, keep Goose recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation did identify one immediate Hazmat security gap: Goose stores
credential-bearing and transcript-bearing state under local config/data/log
roots. `sandboxing-vp3k` adds those roots to Hazmat's credential deny floor and
host credential hardening specs while Goose remains recipe-only.

## Upstream Surface

Important surfaces for Hazmat:

- Goose supports CLI, Desktop, headless `goose run`, recipes, and API-style
  integration surfaces.
- `goose run` can take text, files, stdin, JSON output, `--no-session`, named
  sessions, resume, provider/model overrides, built-in extensions, custom MCP
  commands, remote extensions, and streamable HTTP extensions.
- Headless mode is built for automation and uses configured defaults where an
  interactive session would ask the user.
- Configuration lives under `~/.config/goose`, including `config.yaml`,
  `permission.yaml`, optional `secrets.yaml`, permission decisions, and prompts.
- Sessions and logs are documented under `~/.local/share/goose` and
  `~/.local/state/goose`.
- Goose can store provider/model settings in config or environment variables.
- The Developer extension can edit files, run shell commands, analyze code, and
  perform project setup tasks.
- Goose is MCP-heavy. Extensions can be built-in, command-based, HTTP/remote,
  recipe-defined, or loaded through ACP clients.
- Goose has an extension allowlist mechanism, but it fetches/caches a URL-based
  allowlist and checks extension installation commands; Hazmat still needs its
  own launch-time authority model.
- Goose has optional anonymous usage telemetry controlled through config or
  `GOOSE_TELEMETRY_ENABLED`.
- `.gooseignore` is useful defense-in-depth for the Developer extension, but it
  is not a Hazmat containment boundary and does not constrain every extension.
- Goose Desktop has its own macOS sandbox feature, which is separate from
  Hazmat's native session boundary.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Headless `goose run` | Strong | Good future fake-provider smoke entrypoint |
| Interactive CLI | Strong | Supportable as a foreground harness after adapter registry work |
| Desktop | Mixed | Requires app/server lifecycle thinking; not a quick CLI wrapper |
| Recipes | Mixed | Treat as project inputs only after extension and credential policy exists |
| MCP extensions | Risky | Require Hazmat-owned allowlisting for command, HTTP, network, and env authority |
| ACP surfaces | Mixed | Reuse the ACP driver design; do not special-case Goose first |
| Local config/secrets | Risky | Deny and harden host Goose state by default |
| Sessions/logs | Sensitive | Deny host stores; future adapter should use session-local stores |
| Goose-side sandbox | Not sufficient | Hazmat must keep owning the outer boundary |
| Telemetry | Manageable | Default recipe guidance should disable telemetry unless user opts in |

## Recipe-Only Shape

Users who already have Goose installed in the contained agent account can run
headless tasks:

```bash
hazmat exec -C ~/workspace/project -- env GOOSE_TELEMETRY_ENABLED=false goose run --no-session -t "summarize the current git diff"
```

For interactive work:

```bash
hazmat shell -C ~/workspace/project
env GOOSE_TELEMETRY_ENABLED=false goose session
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Goose auth,
global Goose state, MCP extensions, recipes, permission caches, session
history, telemetry preference, Desktop state, or ACP lifecycle.

## First-Class Requirements

Before `hazmat goose` is supportable:

- define whether the adapter is foreground CLI-only, service/API-backed, ACP,
  Desktop, or more than one adapter
- choose a managed install/update path that avoids unreviewed bootstrap scripts
- use a session-local Goose config/data/state home; never import host
  `~/.config/goose`, `~/.local/share/goose`, or `~/.local/state/goose`
- add typed credential grants for explicitly supported providers and reject
  broad environment passthrough
- default telemetry off unless the user explicitly grants it
- model Goose permissions separately from Hazmat permissions; do not import
  persisted host tool approvals
- default MCP and recipe extensions to disabled unless repo-visible and
  allowlisted by Hazmat policy
- decide how Goose's `.gooseignore` combines with Hazmat project access without
  treating it as a security boundary
- add fake-provider smoke coverage for `goose run --no-session`, JSON output,
  provider failure, malformed model response, denied Developer edits, denied
  shell commands, disabled extensions, session-local cleanup, and host-state
  denial
- if Desktop/API/ACP support is selected, route through the service-harness or
  ACP lifecycle model before implementation
- document how Goose edits and sessions interact with Hazmat snapshots,
  rollback, and git status

## Follow-Up

Goose remains recipe-only until the adapter registry and session-local
tool-state policy are ready. The immediate deny-list hardening from
`sandboxing-vp3k` should ship independently because it protects users even
when Goose is only run through `hazmat exec`.
