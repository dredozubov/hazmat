# Supported Harnesses

Hazmat runs eight agent CLIs in containment. Hermes, Qwen, Cursor Agent, and Pi
keep narrower foreground-only v1 surfaces. This page is the actionable
reference: pick your harness, pick your auth path, run the listed commands.

## Comparison matrix

Use this table to choose a setup path. Most harnesses support at least two auth
modes; Hermes, Qwen, Cursor Agent, and Pi deliberately keep narrower v1 surfaces.
The third column shows the **simplest** way to get a working session.

| Harness | Tested | Install | Subscription / OAuth | API key (env var) | Import from host |
|---|---|---|---|---|---|
| **Claude Code** | 2.1.118 | `hazmat harness update claude` | `/login` inside `hazmat claude` | `ANTHROPIC_API_KEY` via `hazmat config agent` | `hazmat config import claude` |
| **Codex** | 0.118.0 | `hazmat harness update codex` | Device Code in TUI (or import) | `OPENAI_API_KEY` via `hazmat config agent` | `hazmat config import codex` |
| **OpenCode** | 1.14.20 | `hazmat harness update opencode` | per-provider OAuth via `opencode auth login` | per-provider env vars | `hazmat config import opencode` |
| **Antigravity** | agy (pinned) | `hazmat harness update antigravity` | Google sign-in inside `hazmat antigravity` (Keychain OAuth via the agent login keychain adapter) | `ANTIGRAVITY_API_KEY` or `GEMINI_API_KEY` via `hazmat config agent` | not supported in v1 |
| **Hermes (experimental)** | manual install | `hazmat harness update hermes` verifies only | contained Hermes setup only | `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, or `OPENROUTER_API_KEY` via `hazmat config agent` | unsupported in v1 |
| **Qwen Code** | npm latest | `hazmat harness update qwen` | contained Qwen auth flow only | configure through contained Qwen profile / `.env` | unsupported in v1 |
| **Cursor Agent** | manual install | `hazmat harness update cursor-agent` verifies only | contained Cursor Agent login only | configure through contained Cursor Agent profile; no Hazmat `CURSOR_API_KEY` grant in v1 | unsupported in v1 |
| **Pi** | manual install | `hazmat harness update pi` verifies only | contained Pi setup only | configure through contained Pi profile; no Hazmat-managed Pi provider grant in v1 | unsupported in v1 |

Use `hazmat harness status` to inspect every built-in harness at once. For one
harness, `hazmat harness status codex` shows the agent binary path, version
probe, recorded Hazmat state, last curated import timestamp when supported,
credential hints, managed code artifacts, and the auth/profile/session data
that uninstall preserves.

`hazmat harness update <harness>` is the lifecycle-oriented spelling for
install/update. The older `hazmat bootstrap <harness>` commands remain
compatible aliases for the same paths. To remove agent-owned harness code, run
`hazmat harness uninstall <harness>`; by default it removes only declared
Hazmat-owned code artifacts plus the selected `~/.hazmat/state.json` metadata
entry. Auth, profile roots, sessions, provider keys, and imported basics stay in
place unless a future explicit purge flow models and documents a wider delete.
Hermes and Pi are detection-only in v1, so their uninstall clears Hazmat
metadata but does not remove manually installed executables.

Future harness additions follow the maintainer-owned adapter boundary in
[Harness Adapter RFC](plans/2026-06-12-harness-adapter-rfc.md). Hazmat does not
load arbitrary harness plugins or project-defined harness behavior.
OpenHands is tracked separately as a service-oriented candidate in
[OpenHands Harness Candidate Evaluation](plans/2026-06-12-openhands-harness-evaluation.md);
it is not a supported `hazmat <harness>` command today. For current
recipe-only use, see [OpenHands under Hazmat](recipes/openhands-recipe-only.md).

After install/update + auth: `hazmat <harness>` to launch a session, or
`hazmat <harness> -p "prompt"` (claude / antigravity) /
`hazmat <harness> exec "prompt"` (codex) /
`hazmat <harness> run "prompt"` (opencode) /
`hazmat hermes -- --version` or `hazmat hermes -- chat ...` (hermes) /
`hazmat qwen -p "prompt"` (qwen) /
`hazmat cursor-agent -- --version` or Cursor Agent headless flags
(cursor-agent) /
`hazmat pi -- --version` or `hazmat pi -- --mode rpc` (pi) for foreground use.

For Claude, Codex, and OpenCode, the fastest path for a new install is
usually the **import** column — it copies selected host credentials into
Hazmat's host-owned secret store, so there's nothing to re-enter inside the
sandbox. Antigravity, Hermes, Qwen, Cursor Agent, and Pi are intentionally
different in v1: Hazmat does not import host `~/.gemini/antigravity-cli`, host
`~/.hermes`, host `~/.qwen`, host `~/.cursor`, or host `~/.pi/agent`, and it does
not import Cursor IDE profile/auth state. Use the `ANTIGRAVITY_API_KEY` /
`GEMINI_API_KEY` env path or Hermes provider keys from `hazmat config agent`, or
configure the narrower harnesses inside their contained profiles.

## Credential storage summary

Hazmat's current credential map is registry-backed. Durable managed credentials
live under `~/.hazmat/secrets/`; the session sees only the delivery form needed
for the selected capability.

Users upgrading from older Hazmat versions can run
`hazmat migrate credentials` once, or with `--dry-run`, to move legacy
agent-home/provider/cloud/Git residue into the current store without printing
secret values.

| Surface | Durable owner | Session delivery |
|---|---|---|
| Claude credential file + Claude Keychain OAuth | `~/.hazmat/secrets/claude/...` plus the declared `Claude Code-credentials` item in the host/agent login Keychains | Host Keychain and store are reconciled before launch; the session gets a materialized file and, on macOS OAuth refresh, the agent Keychain value is harvested and written back to the host Keychain |
| Codex and OpenCode auth | `~/.hazmat/secrets/<harness>/...` mirrored only with registered host auth files | Host file and store are reconciled by mtime before launch; the session file is harvested to both store and host file on normal exit, then removed from `/Users/agent` |
| Provider API keys from `hazmat config agent` | `~/.hazmat/secrets/providers/*` | Redacted env grant only for explicitly allowed native harnesses, including Hermes when allowed for that provider |
| GitHub API token from `hazmat config github` | `~/.hazmat/secrets/github/token` | `GH_TOKEN` only when `--github` is passed; Docker Sandbox currently fails closed. Treat it as whole-process GitHub API authority, not a review-only grant. |
| Git HTTPS credentials | `~/.hazmat/secrets/git-https/credentials` | Per-session brokered credential helper |
| Git SSH provisioned keys | `~/.hazmat/secrets/git-ssh/provisioned/` | Per-session brokered Git SSH transport |
| Git SSH external keys/profiles | Host-owned private-key paths selected in project config | External references consumed by the broker; not imported into `/Users/agent` |
| Cloud backup credentials | `~/.hazmat/secrets/cloud/` | Host-side backup/restore only; not a harness-session grant |
| Claude agent Keychain OAuth | `/Users/agent/Library/Keychains/login.keychain-db` | Scoped adapter for the `Claude Code-credentials` item only; no broad Keychain linking or export |
| Antigravity Keychain OAuth | `/Users/agent/Library/Keychains/login.keychain-db` | Hazmat prepares + unlocks the agent login keychain so agy's in-session Google sign-in stores its OAuth item there with no password prompt. Non-syncable: the item is **not** harvested into `~/.hazmat/secrets`, the host Keychain, or backups, and is lost on `hazmat claude-keychain reset` / agent-user recreation. Use the `ANTIGRAVITY_API_KEY` / `GEMINI_API_KEY` env path for a portable/headless credential |
| Hermes profile state | `/Users/agent/.hazmat/hermes/projects/<project-hash>` | Contained-only project-scoped `HERMES_HOME`; host `~/.hermes` is not imported, copied, synced, or harvested |
| Qwen profile state | `/Users/agent/.qwen` | Contained-only Qwen auth/settings/sessions; host `~/.qwen` auth/settings are not imported. Portable `QWEN.md` and `extensions/` can sync separately as assets. |
| Cursor Agent profile state | `/Users/agent` default Cursor Agent paths such as `/Users/agent/.cursor` | Contained-only Cursor auth/settings/sessions; host Cursor IDE state, host `~/.cursor`, and host auth settings are not imported |
| Pi profile state | `/Users/agent/.pi/agent` | Contained-only Pi settings, trust decisions, sessions, skills, extensions, and auth; host `~/.pi/agent` is not imported, copied, synced, or harvested |

Provider API keys are configured once per provider. If more than one harness is
allowed to consume the same env var, Hazmat reuses the same stored key and
records the consuming harness in explain/session metadata.

## Per-harness reference

### Claude Code

- **Install / update:** `hazmat harness update claude`. Downloads the official Anthropic installer, verifies the pinned installer checksum, and installs or refreshes the agent-owned Claude Code CLI at `/Users/agent/.local/bin/claude`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. `hazmat bootstrap claude` remains a compatible alias.
- **Durable auth storage:** `~/.hazmat/secrets/claude/credentials.json` and `~/.hazmat/secrets/claude/state.json`. Hazmat materializes them to `/Users/agent/.claude/.credentials.json` and `/Users/agent/.claude.json` only while a Claude session is active.
- **Subscription / OAuth path:** run `hazmat claude`, type `/login`. Claude opens a browser for the OAuth handshake; the resulting credentials are harvested back into `~/.hazmat/secrets/claude/` when the session exits.
- **Agent Keychain path:** newer Claude Code releases may also read/write OAuth state through the agent account login keychain. Before non-`--bare` native Claude launches, Hazmat prepares `/Users/agent/Library/Keychains/login.keychain-db`, makes it the agent user's default/search-list keychain, best-effort sets the login keychain preference, and unlocks it with Hazmat's empty-password keychain profile. If that unlock fails because the existing agent keychain has a different password, run `hazmat claude-keychain reset` to back it up and recreate it. This does not touch your invoking user's keychain.
- **API key path:** `hazmat config agent` will offer to store `ANTHROPIC_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/anthropic-api-key`. Hazmat injects it only into explicitly allowed native sessions instead of keeping it in `/Users/agent/.zshrc`. Native Claude API-key sessions launch Claude Code with `--bare` so newer Claude builds do not read the agent account's Apple Keychain. Bare mode requires API-key or `apiKeyHelper` auth, so OAuth/imported-subscription sessions stay in normal Claude mode.
- **Import from host path:** `hazmat config import claude` stores `~/.claude/.credentials.json` and Claude auth state in `~/.hazmat/secrets/claude/`, and copies the user-level `commands/` and `skills/` portable basics plus your git identity into Hazmat's managed state. Doesn't import `settings.json`, hooks, MCP, or session history (those stay host-only).
- **Verify:** `hazmat claude -p "say OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/claude-import.md](claude-import.md).

### Codex

- **Install / update:** `hazmat harness update codex`. Downloads the official OpenAI installer, verifies the GitHub-published digest, and installs or refreshes the agent-owned Codex CLI at `/Users/agent/.local/bin/codex`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. Also prepares `/Users/agent/.codex` and `/Users/agent/.agents` shared dirs. `hazmat bootstrap codex` remains a compatible alias.
- **Durable auth storage:** `~/.hazmat/secrets/codex/auth.json`. Hazmat materializes it to `/Users/agent/.codex/auth.json` only while a Codex session is active. The file holds **both** ChatGPT subscription OAuth tokens and OpenAI API keys.
- **Subscription / OAuth path:** run `hazmat codex`, use the arrow keys (or type the option number directly) to pick **Sign in with Device Code** in the first-run picker, then press Enter. You complete the code on your host browser; the token is harvested into `~/.hazmat/secrets/codex/auth.json` when the session exits.
  - The import path bypasses this picker entirely.
- **API key path:** `hazmat config agent` can store `OPENAI_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/openai-api-key`. Hazmat injects it only into explicitly allowed native sessions. You can also paste an API key in the codex first-run picker (option `3`) or import `auth.json` from the host.
- **Import from host path:** `hazmat config import codex` stores `~/.codex/auth.json` (covers OAuth and API key) in `~/.hazmat/secrets/codex/auth.json` and imports your git identity. Prompts, rules, and `AGENTS.md` mirror automatically via the harness asset sync at session launch.
- **Advanced app-server path:** `hazmat codex-app-server` starts Codex as `codex app-server --listen stdio://` inside the normal Hazmat Codex containment path. `hazmat codex-app-shim` is the `CODEX_CLI_PATH` compatibility shim for the stock Codex desktop app. These commands are for app-server integration work, not the ordinary CLI quickstart; see [Codex app-server non-interference](codex-app-server-non-interference.md) before testing against a machine that also uses the stock desktop app.
- **Unsupported desktop GUI path:** `hazmat codex app` is intentionally rejected. A full desktop GUI launch under the non-GUI agent account is not a supported containment target yet; use the app-server commands above for contained backend work, or run the opt-in desktop attach smoke only when explicitly validating that integration path.
- **Verify:** `hazmat codex exec "Reply with only OK"` — runs the codex non-interactive subcommand; should print `OK` and exit cleanly.

### OpenCode

- **Install / update:** `hazmat harness update opencode`. Resolves the latest OpenCode GitHub release, verifies the published digest for the selected CLI archive, installs or refreshes the agent-owned OpenCode CLI, prepares the config dir, and links `/Users/agent/.local/bin/opencode`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. `hazmat bootstrap opencode` remains a compatible alias.
- **Durable auth storage:** `~/.hazmat/secrets/opencode/auth.json`. Hazmat materializes it to `/Users/agent/.local/share/opencode/auth.json` only while an OpenCode session is active. Provider-specific shape; OpenCode supports Anthropic, OpenAI, Google, OpenRouter, Groq, etc.
- **Subscription / OAuth path:** run `hazmat opencode`, then `opencode auth login` and pick a provider. Each provider has its own OAuth flow; what works in plain `opencode` works inside `hazmat opencode`. File-based auth is harvested into `~/.hazmat/secrets/opencode/auth.json` when the session exits.
- **API key path:** OpenCode reads provider keys from the same `auth.json`. Either paste them via `opencode auth login` inside the sandbox, or pre-seed them on the host with the OpenCode `auth login` flow and import.
- **Import from host path:** `hazmat config import opencode` stores `~/.local/share/opencode/auth.json` (all configured providers) in `~/.hazmat/secrets/opencode/auth.json`, and copies the user-level `commands/`, `agents/`, `skills/` portable basics plus your git identity.
- **Verify:** `hazmat opencode run "say only OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/opencode-import.md](opencode-import.md).

### Antigravity

> Antigravity (`agy`) is Google's successor to the Gemini CLI. Hazmat replaced the
> `antigravity` harness with `antigravity`; `hazmat antigravity` is gone. The `GEMINI_API_KEY`
> env path still works because `agy` honors it.

- **Install / update:** `hazmat harness update antigravity` (alias `agy`). Downloads the pinned, checksum-verified official `agy` installer and runs it as the agent user. `agy` is a flat native binary installed at `/Users/agent/.local/bin/agy`; no Node.js toolchain is required. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. `hazmat bootstrap antigravity` remains a compatible alias.
- **Durable auth storage:** Antigravity v1 has no curated import or file-backed OAuth sync. `agy` keeps its config and runtime state under `/Users/agent/.gemini/antigravity-cli`. Its interactive OAuth is macOS Keychain-backed; Hazmat now bridges it with the **agent login keychain adapter** (see Subscription / OAuth path) but, as a non-syncable external boundary, does not harvest the Keychain item into `~/.hazmat/secrets`, the host Keychain, or backups.
- **API key path:** `hazmat config agent` can store `ANTIGRAVITY_API_KEY` (or `GEMINI_API_KEY`, an AI Studio key) under `~/.hazmat/secrets/providers/`. Hazmat injects it only into explicitly allowed native sessions. This is the recommended headless/portable/contained path, and when set agy uses the key and skips the keychain entirely.
- **Subscription / OAuth path:** run `hazmat antigravity` and follow the **Sign in with Google** flow. When no API key is granted, Hazmat prepares and unlocks the agent account login keychain (empty-password profile) and grants the session scoped read-write to only that keychain DB before launch, so agy's token exchange and OAuth item storage complete with **no** SecurityAgent password prompt. The token lives in the agent login keychain and persists across sessions; it is not exported to the host. If the unlock fails because the existing agent keychain has a different password, run `hazmat claude-keychain reset` (the agent login keychain is shared across agent-user harnesses).
- **Verify:** `hazmat antigravity -p "say only OK"` — non-interactive prompt; should print `OK`.

### Hermes (experimental)

- **Install / update:** `hazmat harness update hermes` is detection-only in v1. It
  verifies an agent-owned executable at `/Users/agent/.local/bin/hermes` by
  running `hermes --version`, then records harness state. It does not run an
  upstream install script, curl pipe, npm latest install, pipx install, or host
  profile migration.
- **Managed profile root:** Hazmat creates
  `/Users/agent/.hazmat/hermes/projects/<project-hash>` with mode `0700` before
  launch and sets `HERMES_HOME` to that path. That state is durable agent
  profile state scoped by canonical project path; normal rollback boundaries are
  documented by the setup/rollback model, and Hazmat does not treat host
  `~/.hermes` as a source.
- **Reset / uninstall boundary:** ordinary `hazmat rollback` removes host-owned
  Hazmat metadata but preserves `/Users/agent/.hazmat/hermes` with the rest of
  the agent home. After running untrusted Hermes skills, MCP servers, hooks, or
  cron-like experiments, the supported full reset for all Hermes project state is
  `hazmat rollback --delete-user` followed by `hazmat init` and
  `hazmat harness update hermes`. A narrower Hermes-only reset command needs a
  model-first cleanup design before
  it can be supported.
- **Provider API key path:** `hazmat config agent` can store
  `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, and
  `OPENROUTER_API_KEY` once in `~/.hazmat/secrets/providers/*`. Hermes receives
  only the provider env vars allowed by Hazmat's credential registry for the
  session.
- **Contained setup path:** run `hazmat hermes` or
  `hazmat hermes -- chat ...` and let Hermes write any local profile state under
  the project-scoped managed `HERMES_HOME`.
- **Unsupported in v1:** `hazmat config import hermes`, host `~/.hermes`
  import, harness asset sync, gateway/dashboard/API/server modes, persistent
  cron/service entrypoints, and Hermes MCP/skill/profile migration.
- **Verify:** `hazmat hermes -- --version` checks the foreground launch path.
  `hazmat explain --for hermes -C /tmp` previews the session contract.
  `hazmat hermes --network none --metadata-json -- --version` verifies that
  Hermes composes with native network-none sessions.

### Qwen Code

- **Install / update:** `hazmat harness update qwen`. Installs or refreshes `@qwen-code/qwen-code@latest` into the agent's `~/.local` prefix via npm. Requires Node.js 20 or newer on the agent's PATH. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. `hazmat bootstrap qwen` remains a compatible alias.
- **Contained profile root:** Qwen uses `/Users/agent/.qwen` for auth, settings, extensions, and session state. Hazmat prepares that directory during install/update and does not import host `~/.qwen` auth/settings in v1.
- **Approval mode:** when `session.skip_permissions` is enabled, `hazmat qwen` prepends `--yolo` unless you already passed `--yolo` or `-y` after `--`. Hazmat remains the containment boundary.
- **Auth path:** run `hazmat qwen` and use Qwen's own auth/config flow inside the contained session. For API-key setups, keep provider keys in Qwen's contained profile or `.env`; do not rely on host `~/.qwen` being copied.
- **Asset sync:** Hazmat can sync host `~/.qwen/QWEN.md` and `~/.qwen/extensions/` into the contained profile on launch. It does not sync Qwen settings, auth, sessions, MCP config, or other executable/profile state.
- **Unsupported in v1:** `hazmat config import qwen`, host `~/.qwen` auth/settings import, daemon mode, SDK/server mode, and broad Qwen profile migration.
- **Verify:** `hazmat qwen -p "say only OK"` — single-shot prompt; should print `OK`.

### Cursor Agent

- **Install / update:** `hazmat harness update cursor-agent` is detection-only in
  v1. It verifies an agent-owned executable at
  `/Users/agent/.local/bin/cursor-agent` by running `cursor-agent --version`,
  then records harness state. It does not run an upstream install script or
  copy a host Cursor install.
- **Contained profile boundary:** Cursor Agent runs as `/Users/agent` and uses
  its own agent-side state. Hazmat does not import host Cursor IDE state, host
  `~/.cursor`, host auth settings, or host workspace trust/profile data.
- **Auth path:** run `hazmat cursor-agent -- login` or use Cursor Agent's own
  contained setup flow. If your setup uses `CURSOR_API_KEY`, configure it
  through Cursor Agent's contained profile or a future typed credential design;
  `hazmat config agent` does not grant `CURSOR_API_KEY` in v1.
- **Headless path:** Hazmat forwards Cursor Agent flags exactly as provided.
  For Open Design-style automation, pass the Cursor flags yourself, for example
  `hazmat cursor-agent --print --output-format stream-json
  --stream-partial-output --force --trust`.
- **Unsupported in v1:** `hazmat config import cursor-agent`, host Cursor IDE
  auth/profile import, host `~/.cursor` sync, automatic `--force`/`--trust`
  injection, and service/daemon/browser-control modes.
- **Verify:** `hazmat cursor-agent -- --version` checks the foreground launch
  path. `hazmat explain --for cursor-agent -C /tmp` previews the session
  contract.

### Pi

- **Install / update:** `hazmat harness update pi` is detection-only in v1. It
  verifies an agent-owned executable at `/Users/agent/.local/bin/pi` by running
  `pi --version`, prepares `/Users/agent/.pi/agent`, then records harness
  state. It does not run an upstream installer or import host `~/.pi/agent`.
- **Contained profile boundary:** Pi uses `/Users/agent/.pi/agent` for
  settings, trust decisions, sessions, skills, extensions, and auth. Configure
  Pi inside the contained profile; host Pi state remains outside Hazmat.
- **RPC path:** Hazmat can contain a Pi RPC process, for example
  `hazmat pi -- --mode rpc`, but v1 does not drive Pi's JSON-RPC prompt/event
  stream. A compatible editor or daemon must still be the JSON-RPC client.
- **Unsupported in v1:** `hazmat config import pi`, host `~/.pi/agent` import,
  Hazmat-managed Pi provider env grants, host skill/extension sync, extension
  UI approval policy, and Pi-specific JSON-RPC prompt driving.
- **Verify:** `hazmat pi -- --version` checks the foreground launch path.
  `hazmat explain --for pi -C /tmp` previews the session contract.

## Choosing an auth mode

Three rules of thumb:

1. **You're the only user, and you've already auth'd this CLI on the host.** Use the **Import** column when the harness supports it. One command, no re-entry.
2. **You have a subscription (Claude Pro / ChatGPT Plus / Google AI Pro / OpenCode-supported subscription).** Use the **Subscription / OAuth** column. The agent's first-run picker handles the browser handoff and Hazmat harvests file-backed tokens into its host-owned store when the session exits.
3. **You only have an API key (or you're scripting CI).** Use the **API key** column. Persistent, scriptable, no browser dance.

Mixing is fine for importable harnesses: you can import once and switch to API
key later by setting the env var, or vice versa. Hermes, Qwen, Cursor Agent,
and Pi exclude host-profile import in v1, so mix provider keys or contained
setup according to the harness-specific section above.

## Session modes

Harness auth and harness session mode are separate decisions:

- **Native containment:** available on all eight harnesses (`claude`, `codex`, `opencode`, `antigravity`, `hermes`, `qwen`, `cursor-agent`, `pi`).
- **Docker Sandbox:** available on all eight harnesses, plus the generic `hazmat shell` and `hazmat exec` entrypoints.
- **`--docker=auto`:** works the same way on every harness. On repos that actually need a private Docker daemon, Hazmat routes that harness into Docker Sandbox mode; on code-only repos, the harness stays in native containment.

Native containment also supports a per-session network mode:

```bash
hazmat claude --network none --metadata-json -p "offline review"
hazmat codex --network none --metadata-json exec "offline review"
hazmat opencode --network none run "offline review"
hazmat antigravity --network none -p "offline review"
hazmat hermes --network none --metadata-json -- --version
hazmat qwen --network none -p "offline review"
hazmat cursor-agent --network none -- --version
hazmat pi --network none -- --version
```

`--network none` denies outbound IPv4, outbound IPv6, and DNS for that native
session's Seatbelt identity. It composes with concurrent Hazmat sessions because
it does not touch global `pf` state. Use `--metadata-json` when an automation
needs to verify the requested policy was enforced; the JSON line is written to
stderr so the harness stdout remains usable for non-interactive capture.

## Harness tracing

When a supported harness behaves differently under containment, developers can
install Hazmat's debug trace tools and collect a timestamped debug bundle around
the normal launch path:

```bash
sudo -v
make hazmat-debug TRACE_ACK=1
```

For interactive Claude Code sessions, run the installed wrapper from the project
that reproduces the issue:

```bash
cd ~/workspace/project-that-reproduces
~/.hazmat/bin/hazmat-trace-claude --i-understand-this-runs-sudo-dtrace-probes --name claude-interactive-repro
```

```bash
~/.hazmat/bin/hazmat-debug trace claude --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace codex --name baseline -- --no-backup exec "say ok"
~/.hazmat/bin/hazmat-debug trace opencode --name baseline -- --no-backup run "say ok"
~/.hazmat/bin/hazmat-debug trace antigravity --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace hermes --name baseline -- --no-backup -- --version
~/.hazmat/bin/hazmat-debug trace qwen --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace cursor-agent --name baseline -- --no-backup -- --version
~/.hazmat/bin/hazmat-debug trace pi --name baseline -- --no-backup -- --version
```

The bundle includes the planned session contract, harness metadata, before/after
state snapshots, process samples, unified logs, sandbox denials, and required
platform trace probes. Release builds do not include this command. See
[Harness tracing](claude-tracing.md) for the comparison workflow.

## GitHub API Access

GitHub API access is harness-agnostic and explicit. Configure a token once with
`hazmat config github` or `GH_TOKEN=... hazmat config github --token-from-env`,
then pass `--github` to the session that needs it:

```bash
hazmat claude --github -p "review this PR"
hazmat codex --github exec "review this PR"
hazmat opencode --github run "review this PR"
hazmat antigravity --github -p "review this PR"
hazmat hermes --github -- chat "review this PR"
hazmat qwen --github -p "review this PR"
hazmat cursor-agent --github --print --output-format stream-json --force --trust
hazmat pi --github -- --help
```

Hazmat stores the token in `~/.hazmat/secrets/github/token`, injects only
`GH_TOKEN`, and shows a redacted `github.api-token` grant in the session
contract and `hazmat explain --json`. Integrations and repo recommendations
cannot request this capability, and Docker Sandbox sessions currently reject
`--github` instead of silently dropping it.

`--github` is intentionally coarse. The token reaches the whole harness process
and any tool, hook, MCP server, or child process it spawns. If the token has
write scopes, the agent can use GitHub's API or local tooling to create refs,
push branches, open or update PRs, edit issues, or otherwise modify the review
path. Use a least-scoped token for the task and omit `--github` for sessions
that must not be able to self-push or change repository state remotely.

## Session integrations

Session integrations (language toolchain extensions like `go`, `rust`, `python-uv`, `tla-java`, etc.) apply uniformly across **every** harness — claude, codex, opencode, antigravity, hermes, qwen, cursor-agent, and pi all flow through the same `applyIntegrations` path in `resolvePreparedSession`. The HarnessID does not gate which integrations activate; auto-detection (e.g. `go.mod` triggers the `go` integration) and the `--integration <name>` CLI flag work identically per harness.

Preview the planned session contract for any harness with `hazmat explain --for <harness>`:

```bash
hazmat explain --for codex --integration go    # codex session, force-activate go integration
hazmat explain --for antigravity -C ~/my-rust-app    # antigravity session, auto-detect rust from Cargo.toml
hazmat explain --for opencode --json            # machine-readable preview
hazmat explain --for hermes --network none       # Hermes foreground contract
hazmat explain --for qwen --docker=auto          # Qwen foreground contract
hazmat explain --for cursor-agent --docker=auto  # Cursor Agent foreground contract
hazmat explain --for pi --docker=auto            # Pi foreground contract
```

Integrations are documented in [docs/integrations.md](integrations.md) — the trust model, allowed env passthrough set, and built-in list are all there.

## Session asset sync

For harnesses with an asset spec, hazmat keeps a small set of "portable
basics" in sync from your host to the agent on every session launch. This is
harness-aware and runs automatically (toggle with `session.harness_assets` in
`hazmat config`):

| Harness | Synced from host on launch |
|---|---|
| Claude | `~/.claude/CLAUDE.md`, `commands/`, `skills/`, `agents/` |
| Codex | `~/.codex/AGENTS.md`, `prompts/`, `rules/`, `~/.agents/skills/` |
| OpenCode | `~/.config/opencode/commands/`, `agents/`, `skills/` |
| Antigravity | none in v1; `agy` state under `~/.gemini/antigravity-cli` is contained-only and not synced from the host |
| Qwen | `~/.qwen/QWEN.md`, `extensions/` |
| Hermes | none in v1; host `~/.hermes`, skills, MCP, cron, and service config are not synced |
| Cursor Agent | none in v1; host Cursor IDE state, host `~/.cursor`, auth, and workspace trust/profile data are not synced |
| Pi | none in v1; host `~/.pi/agent`, skills, extensions, trust decisions, sessions, and auth are not synced |

For the rows with synced paths, these are managed copies — if you edit them
inside the sandbox, the next session will overwrite your edits with the host
version. Edit on the host instead.

## Troubleshooting

- **Bootstrap sees an existing harness binary:** Hazmat still runs the harness installer/update path. Existing config files, hooks, auth state, and shared directories remain idempotent and are not overwritten unless their step explicitly says so.
- **Import says "no basics found to import":** the host doesn't have any of the expected files in its standard locations. Check the **Auth file location** above for the harness — that's the path the import scans.
- **Import says "Codex auth imported" but `hazmat codex` still asks for sign-in:** check that `~/.hazmat/secrets/codex/auth.json` exists. If an older Hazmat left a stale `/Users/agent/.codex/auth.json`, current Hazmat should recover it automatically on launch. If the stale copy differs from the host-owned copy, the previous host-owned copy is preserved under `~/.hazmat/secrets/codex/auth.json.conflicts/`.
- **Codex chat hangs on "Reconnecting…":** if you're on a hazmat older than commit `eaaaa1c`, the seatbelt was missing several Security framework allowances. Update and rebuild.
- **`hazmat harness update hermes` says Hermes is not installed:** install or link the Hermes executable as the agent user at `/Users/agent/.local/bin/hermes`, then rerun the update. Hazmat records Hermes as installed only after `hermes --version` succeeds.
- **`hazmat hermes -- gateway` / `dashboard` / `server` / `cron` is rejected:** v1 supports foreground Hermes sessions only. Run an interactive or prompt-driven foreground command under `hazmat hermes`, or track service supervision as a separate design.
- **`hazmat qwen` still asks for auth:** run Qwen's auth flow inside `hazmat qwen`, or configure the contained `/Users/agent/.qwen` profile. Hazmat does not import host `~/.qwen` auth/settings in v1.
- **`hazmat harness update cursor-agent` says Cursor Agent is not installed:** install or link the Cursor Agent executable as the agent user at `/Users/agent/.local/bin/cursor-agent`, then rerun the update. Hazmat records Cursor Agent as installed only after `cursor-agent --version` succeeds.
- **`hazmat cursor-agent` still asks for auth:** run `hazmat cursor-agent -- login`, or configure the contained agent-side Cursor Agent profile. Hazmat does not import host Cursor IDE state, host `~/.cursor`, or host auth settings in v1.
- **`hazmat harness update pi` says Pi is not installed:** install or link the Pi executable as the agent user at `/Users/agent/.local/bin/pi`, then rerun the update. Hazmat records Pi as installed only after `pi --version` succeeds.
- **`hazmat pi` still asks for auth or trust:** configure Pi inside the contained `/Users/agent/.pi/agent` profile. Hazmat does not import host `~/.pi/agent` in v1.

For deeper containment behavior (what the agent can and can't see), [docs/usage.md](usage.md) is the canonical reference. To verify any of the setup paths above end-to-end (per-harness checklists, regression scenarios, recovery), see [docs/manual-testing.md](manual-testing.md).
