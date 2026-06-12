# Amp Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.4`
Follow-up implemented: `sandboxing-uyab`
Parent: `sandboxing-lg07.7`

Sources:

- Amp Owner's Manual: <https://ampcode.com/manual>
- Amp SDK manual: <https://ampcode.com/manual/sdk>
- Amp CLI examples and guides: <https://github.com/ampcode/amp-examples-and-guides/blob/main/guides/cli/README.md>

## Decision

Do not add `hazmat amp` in the next release.

Amp is a strong future foreground harness candidate, and its SDK makes it a
good reference point for programmatic harness adapters. It should stay in the
harness queue, not be filed away as a generic provider integration. The SDK
still depends on the Amp CLI, and the CLI has clean non-interactive entrypoints
through `amp -x` / `--execute` plus stream-JSON output.

However, first-class support is not a small wrapper. Amp runs tools and shell
commands without approval by default, supports user and workspace settings,
loads skills/plugins/MCP servers, integrates with IDEs, syncs threads to
ampcode.com, supports remote control of running CLI threads, and has enterprise
managed settings. Hazmat needs to own those authority boundaries rather than
inherit the user's host Amp configuration.

For now, keep Amp recipe-only through `hazmat exec` or `hazmat shell`. The
evaluation identified one immediate hardening gap: Amp local config can contain
settings, plugins, skills, MCP server definitions, and login-adjacent state.
`sandboxing-uyab` adds `~/.config/amp` to Hazmat's credential deny floor and
host credential hardening specs while Amp remains recipe-only.

## Upstream Surface

Important surfaces for Hazmat:

- installation is documented through a curl-pipe installer, Homebrew, and npm
  alternatives; updates can run through `amp update` or automatic update mode.
- Amp CLI has interactive mode, stdin-prompt behavior, execute mode with
  `-x` / `--execute`, stream JSON output, thread continuation, and prompt
  history.
- non-interactive and SDK flows can use `AMP_API_KEY`; interactive login can
  use `amp login`.
- Amp user settings live under `~/.config/amp/settings.json`; workspace
  settings live under `.amp/settings.json`, and workspace settings override
  user settings.
- `AMP_SETTINGS_FILE` can point to a custom settings file.
- user guidance can come from project `AGENTS.md`, `~/.config/amp/AGENTS.md`,
  `~/.config/AGENTS.md`, `/etc/ampcode/AGENTS.md`, and
  `/Library/Application Support/ampcode/AGENTS.md`.
- Amp-native skills include user-wide and project-specific directories, and
  skills can bundle MCP servers.
- plugins can register commands, tools, custom agents, subagents, UI prompts,
  and shell-backed tools.
- Amp MCP permissions default to allowing servers when no rule matches; a
  block-all policy requires explicit rules.
- CLI/IDE integration can inspect open files/selections and edit through the
  IDE with undo support.
- threads can be synced/shared through ampcode.com, and remote control allows
  continuing a running CLI thread from the web.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Headless `amp -x` | Strong | Good future fake-provider smoke entrypoint |
| Stream JSON | Strong | Useful for adapter protocol and hermetic tests |
| TypeScript/Python SDK | Strong | Treat as adapter implementation aid, not a separate provider class |
| Interactive CLI | Strong | Supportable as foreground harness after adapter registry work |
| Default tool execution | Risky | Hazmat must supply containment and explicit policy defaults |
| User `~/.config/amp` | Risky | Deny and harden host state; future adapter must use session-local state |
| Workspace `.amp` | Mixed | Treat as project input only after MCP/plugin policy exists |
| Plugins/skills/MCP | Risky | Require Hazmat-owned allowlisting for command, env, HTTP, and remote authority |
| IDE integration | Poor for v1 | Do not silently attach to host IDEs from contained sessions |
| Thread sync/remote control | Risky | Disable or model as an explicit cloud/session capability |
| Managed settings | Enterprise-only | Do not assume system managed settings are present or safe |

## Recipe-Only Shape

Users who already have Amp installed in the contained agent account can run
headless tasks:

```bash
hazmat exec -C ~/workspace/project -- env AMP_SKIP_UPDATE_CHECK=1 amp -x "summarize the current git diff"
```

For interactive work:

```bash
hazmat shell -C ~/workspace/project
env AMP_SKIP_UPDATE_CHECK=1 amp
```

For CI-like automation where the user intentionally grants a token:

```bash
hazmat exec -C ~/workspace/project -- env AMP_SKIP_UPDATE_CHECK=1 AMP_API_KEY="$AMP_API_KEY" amp -x "review this patch" --stream-json
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Amp auth,
host Amp settings, plugins, skills, MCP servers, thread sharing, remote
control, IDE attach, or update behavior.

## First-Class Requirements

Before `hazmat amp` is supportable:

- define a built-in foreground adapter entry backed by the CLI and optionally
  reused by SDK-based tests
- choose a managed install/update path that avoids unreviewed bootstrap scripts
  and disables automatic update side effects by default
- use session-local Amp config and state; never import host `~/.config/amp`
- add a typed `AMP_API_KEY` credential grant and keep interactive Amp login
  outside first-class automation until the auth store is understood
- disable or explicitly gate thread sync, sharing, and remote control
- disable host IDE attach unless a separate IDE/app lifecycle model owns it
- default MCP servers, plugins, and workspace `.amp` settings to disabled or
  allowlisted by Hazmat policy before launch
- decide how user-wide and project skills interact with Hazmat's existing
  `.agents/skills` posture
- add fake-provider smoke coverage for `amp -x`, stream JSON, SDK execution,
  provider failure, malformed stream event, denied shell/tool call, disabled
  MCP/plugin loading, host-state denial, session-local cleanup, and git dirty
  state
- document how Amp threads, edits, and cloud sync interact with Hazmat
  snapshots, rollback, and git status

## Follow-Up

Amp remains recipe-only until the foreground adapter registry and session-local
tool-state policy are ready. The immediate deny-list hardening from
`sandboxing-uyab` should ship independently because it protects users even
when Amp is only run through `hazmat exec`.
