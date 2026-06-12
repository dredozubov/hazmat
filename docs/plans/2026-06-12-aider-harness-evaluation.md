# Aider Harness Candidate Evaluation

Status: Compatibility decision
Date: 2026-06-12
Related issue: `sandboxing-lg07.7.6`
Parent: `sandboxing-lg07.7`

Sources:

- Aider documentation: <https://aider.chat/docs/>
- Aider installation: <https://aider.chat/docs/install.html>
- Aider usage: <https://aider.chat/docs/usage.html>
- Aider scripting: <https://aider.chat/docs/scripting.html>
- Aider API keys: <https://aider.chat/docs/config/api-keys.html>
- Aider options reference: <https://aider.chat/docs/config/options.html>
- Aider `.env` config: <https://aider.chat/docs/config/dotenv.html>
- Aider GitHub README: <https://github.com/aider-ai/aider>

## Decision

Do not add `hazmat aider` in the next release.

Aider is a good future foreground harness candidate, not a service harness. Its
useful shape is a contained subprocess that edits files in the current git repo
and may auto-commit those edits. It has a meaningful non-interactive mode via
`--message`, so a future adapter can have hermetic smoke coverage once Hazmat
owns install, credential, profile, and fake-provider boundaries.

For now, keep Aider as recipe-only through `hazmat exec` or `hazmat shell`.
First-class support should wait for the closed harness adapter registry to own
Aider-specific install/update, credential grants, config/profile policy,
default flags, and smoke tests.

## Upstream Surface

Aider is a terminal pair-programming CLI. The docs describe it as editing code
in a local git repo, adding selected files to chat context, and committing
changes so users can review or undo them with normal git tools.

Important surfaces for Hazmat:

- install paths include `aider-install`, curl/wget one-liners, `uv tool install`,
  `pipx install`, and pip
- provider credentials can come from command-line flags, environment variables,
  `.env`, or `.aider.conf.yml`
- `.env` loading searches the home directory, git root, current directory, or a
  caller-specified env file, with later files taking priority
- config can live in home or repo `.aider.conf.yml`
- git behavior is central: repo discovery, auto-commits by default, dirty repo
  commits by default, optional hook verification, and `.aider*` gitignore
  management
- scripting mode accepts `--message` / `--message-file` and exits after one
  request
- chat modes include code, ask, architect, and help; architect mode may use a
  second editor model
- Aider has browser/web/documentation features and optional Playwright/web
  scraping behavior that should not be silently enabled by Hazmat
- Aider can connect to many providers, including local model endpoints, not just
  the providers Hazmat already grants to built-in harnesses

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Interactive terminal CLI | Strong | Suitable for future foreground harness adapter |
| Non-interactive `--message` | Strong | Good future smoke-test entrypoint |
| Git edit/commit workflow | Manageable | Session snapshot and git status provide recovery; adapter must document auto-commit behavior |
| Provider API keys | Mixed | Needs typed registry mapping for Aider's provider names and `.env` / config-file precedence |
| Host `.aider.conf.yml` / `.env` | Risky | Do not import host config by default; repo config is visible only through normal project scope |
| Browser/web/Playwright features | Poor for v1 | Disable or document as unsupported until explicitly modeled |
| Aider Docker mode | Not a Hazmat boundary | Use Hazmat native or Docker Sandbox containment instead |
| Install/update | Mixed | Avoid curl-pipe installers; future adapter should prefer a reviewed uv/pipx-style install path with managed artifacts |

## Recipe-Only Shape

Users who already have Aider installed in the contained agent account can run:

```bash
hazmat exec -C ~/workspace/project -- aider --message "summarize this repo" --no-analytics
```

For interactive work:

```bash
hazmat shell -C ~/workspace/project
aider --no-analytics
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Aider auth,
import host Aider state, validate Aider config, or promise versioned lifecycle
commands.

## First-Class Requirements

Before `hazmat aider` is supportable:

- define a built-in adapter entry with install/update/status/uninstall scope
- choose a managed install path that does not run curl-pipe installers
- add typed credential grants for the provider keys Hazmat intentionally
  supports, including Aider provider-name mapping
- decide whether Aider receives provider keys as env vars or config entries;
  do not write durable secrets into repo `.env` or `.aider.conf.yml`
- deny or explicitly model host `.aider.conf.yml`, home `.env`, browser mode,
  Playwright/web scraping, and arbitrary repo-requested env passthrough
- pin default safety flags, likely including no analytics and no browser/web
  automation by default
- add fake-provider smoke coverage for `--message` success, provider failure,
  malformed response, git dirty state, auto-commit/no-commit modes, and cleanup
- document how Aider auto-commits interact with Hazmat snapshots and rollback

## Follow-Up

Create a future implementation bead only after the adapter registry work is
ready to accept another foreground harness. Until then, Aider remains a strong
recipe candidate and a useful comparison point for foreground subprocess
harness design.
