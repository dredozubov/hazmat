# Supported Harnesses

Hazmat runs six agent CLIs in containment. Hermes and Qwen keep narrower
foreground-only v1 surfaces. This page is the actionable reference: pick your
harness, pick your auth path, run the listed commands.

## Comparison matrix

Use this table to choose a setup path. Most harnesses support at least two auth
modes; Hermes and Qwen deliberately keep narrower v1 surfaces. The third column
shows the **simplest** way to get a working session.

| Harness | Tested | Install | Subscription / OAuth | API key (env var) | Import from host |
|---|---|---|---|---|---|
| **Claude Code** | 2.1.118 | `hazmat bootstrap claude` | `/login` inside `hazmat claude` | `ANTHROPIC_API_KEY` via `hazmat config agent` | `hazmat config import claude` |
| **Codex** | 0.118.0 | `hazmat bootstrap codex` | Device Code in TUI (or import) | `OPENAI_API_KEY` via `hazmat config agent` | `hazmat config import codex` |
| **OpenCode** | 1.14.20 | `hazmat bootstrap opencode` | per-provider OAuth via `opencode auth login` | per-provider env vars | `hazmat config import opencode` |
| **Gemini** | 0.38.2 | `hazmat bootstrap gemini` | Google sign-in inside `hazmat gemini` | `GEMINI_API_KEY` via `hazmat config agent` | `hazmat config import gemini` |
| **Hermes (experimental)** | manual install | `hazmat bootstrap hermes` verifies only | contained Hermes setup only | `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, or `OPENROUTER_API_KEY` via `hazmat config agent` | unsupported in v1 |
| **Qwen Code** | npm latest | `hazmat bootstrap qwen` | contained Qwen auth flow only | configure through contained Qwen profile / `.env` | unsupported in v1 |

After bootstrap + auth: `hazmat <harness>` to launch a session, or
`hazmat <harness> -p "prompt"` (claude / gemini) /
`hazmat <harness> exec "prompt"` (codex) /
`hazmat <harness> run "prompt"` (opencode) /
`hazmat hermes -- --version` or `hazmat hermes -- chat ...` (hermes) /
`hazmat qwen -p "prompt"` (qwen) for foreground use.

For Claude, Codex, OpenCode, and Gemini, the fastest path for a new install is
usually the **import** column — it copies selected host credentials into
Hazmat's host-owned secret store, so there's nothing to re-enter inside the
sandbox. Hermes and Qwen are intentionally different in v1: Hazmat does not
import host `~/.hermes` or host `~/.qwen`. Use Hermes provider keys from
`hazmat config agent`, or configure either harness inside its contained profile.

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
| Claude, Codex, OpenCode, file-backed Gemini auth | `~/.hazmat/secrets/<harness>/...` | Materialized into `/Users/agent` only for the matching harness session, then harvested/removed on normal exit |
| Provider API keys from `hazmat config agent` | `~/.hazmat/secrets/providers/*` | Redacted env grant only for explicitly allowed native harnesses, including Hermes when allowed for that provider |
| GitHub API token from `hazmat config github` | `~/.hazmat/secrets/github/token` | `GH_TOKEN` only when `--github` is passed; Docker Sandbox currently fails closed. Treat it as whole-process GitHub API authority, not a review-only grant. |
| Git HTTPS credentials | `~/.hazmat/secrets/git-https/credentials` | Per-session brokered credential helper |
| Git SSH provisioned keys | `~/.hazmat/secrets/git-ssh/provisioned/` | Per-session brokered Git SSH transport |
| Git SSH external keys/profiles | Host-owned private-key paths selected in project config | External references consumed by the broker; not imported into `/Users/agent` |
| Cloud backup credentials | `~/.hazmat/secrets/cloud/` | Host-side backup/restore only; not a harness-session grant |
| Gemini Keychain OAuth | macOS Keychain item owned by Gemini CLI | Adapter required; Hazmat reports the boundary and does not import it yet |
| Hermes profile state | `/Users/agent/.hazmat/hermes` | Managed agent-side `HERMES_HOME`; host `~/.hermes` is not imported, copied, synced, or harvested |
| Qwen profile state | `/Users/agent/.qwen` | Contained agent-side Qwen auth/settings/sessions; host `~/.qwen` auth/settings are not imported. Portable `QWEN.md` and `extensions/` can sync separately as assets. |

Provider API keys are configured once per provider. If more than one harness is
allowed to consume the same env var, Hazmat reuses the same stored key and
records the consuming harness in explain/session metadata.

## Per-harness reference

### Claude Code

- **Install / update:** `hazmat bootstrap claude`. Downloads the official Anthropic installer, verifies the pinned installer checksum, and installs or refreshes the agent-owned Claude Code CLI at `/Users/agent/.local/bin/claude`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself.
- **Durable auth storage:** `~/.hazmat/secrets/claude/credentials.json` and `~/.hazmat/secrets/claude/state.json`. Hazmat materializes them to `/Users/agent/.claude/.credentials.json` and `/Users/agent/.claude.json` only while a Claude session is active.
- **Subscription / OAuth path:** run `hazmat claude`, type `/login`. Claude opens a browser for the OAuth handshake; the resulting credentials are harvested back into `~/.hazmat/secrets/claude/` when the session exits.
- **API key path:** `hazmat config agent` will offer to store `ANTHROPIC_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/anthropic-api-key`. Hazmat injects it only into explicitly allowed native sessions instead of keeping it in `/Users/agent/.zshrc`.
- **Import from host path:** `hazmat config import claude` stores `~/.claude/.credentials.json` and Claude auth state in `~/.hazmat/secrets/claude/`, and copies the user-level `commands/` and `skills/` portable basics plus your git identity into Hazmat's managed state. Doesn't import `settings.json`, hooks, MCP, or session history (those stay host-only).
- **Verify:** `hazmat claude -p "say OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/claude-import.md](claude-import.md).

### Codex

- **Install / update:** `hazmat bootstrap codex`. Downloads the official OpenAI installer, verifies the GitHub-published digest, and installs or refreshes the agent-owned Codex CLI at `/Users/agent/.local/bin/codex`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself. Also prepares `/Users/agent/.codex` and `/Users/agent/.agents` shared dirs.
- **Durable auth storage:** `~/.hazmat/secrets/codex/auth.json`. Hazmat materializes it to `/Users/agent/.codex/auth.json` only while a Codex session is active. The file holds **both** ChatGPT subscription OAuth tokens and OpenAI API keys.
- **Subscription / OAuth path:** run `hazmat codex`, use the arrow keys (or type the option number directly) to pick **Sign in with Device Code** in the first-run picker, then press Enter. You complete the code on your host browser; the token is harvested into `~/.hazmat/secrets/codex/auth.json` when the session exits.
  - The import path bypasses this picker entirely.
- **API key path:** `hazmat config agent` can store `OPENAI_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/openai-api-key`. Hazmat injects it only into explicitly allowed native sessions. You can also paste an API key in the codex first-run picker (option `3`) or import `auth.json` from the host.
- **Import from host path:** `hazmat config import codex` stores `~/.codex/auth.json` (covers OAuth and API key) in `~/.hazmat/secrets/codex/auth.json` and imports your git identity. Prompts, rules, and `AGENTS.md` mirror automatically via the harness asset sync at session launch.
- **Verify:** `hazmat codex exec "Reply with only OK"` — runs the codex non-interactive subcommand; should print `OK` and exit cleanly.

### OpenCode

- **Install / update:** `hazmat bootstrap opencode`. Downloads via the official OpenCode installer, installs or refreshes the agent-owned OpenCode CLI, prepares the config dir, and links `/Users/agent/.local/bin/opencode`. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself.
- **Durable auth storage:** `~/.hazmat/secrets/opencode/auth.json`. Hazmat materializes it to `/Users/agent/.local/share/opencode/auth.json` only while an OpenCode session is active. Provider-specific shape; OpenCode supports Anthropic, OpenAI, Google, OpenRouter, Groq, etc.
- **Subscription / OAuth path:** run `hazmat opencode`, then `opencode auth login` and pick a provider. Each provider has its own OAuth flow; what works in plain `opencode` works inside `hazmat opencode`. File-based auth is harvested into `~/.hazmat/secrets/opencode/auth.json` when the session exits.
- **API key path:** OpenCode reads provider keys from the same `auth.json`. Either paste them via `opencode auth login` inside the sandbox, or pre-seed them on the host with the OpenCode `auth login` flow and import.
- **Import from host path:** `hazmat config import opencode` stores `~/.local/share/opencode/auth.json` (all configured providers) in `~/.hazmat/secrets/opencode/auth.json`, and copies the user-level `commands/`, `agents/`, `skills/` portable basics plus your git identity.
- **Verify:** `hazmat opencode run "say only OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/opencode-import.md](opencode-import.md).

### Gemini

- **Install / update:** `hazmat bootstrap gemini`. Installs or refreshes `@google/gemini-cli@latest` into the agent's `~/.local` prefix via npm. Requires Node.js on the agent's PATH (Homebrew node at `/opt/homebrew/bin/node` works). Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself.
- **Durable auth storage:** `~/.hazmat/secrets/gemini/oauth_creds.json` and `~/.hazmat/secrets/gemini/google_accounts.json` for file-based Gemini auth. Hazmat materializes them to `/Users/agent/.gemini/...` only while a Gemini session is active. Modern Keychain-backed Gemini OAuth is an explicit external backend in Hazmat's credential registry; Hazmat does not import or harvest that Keychain item yet.
- **Subscription / OAuth path:** run `hazmat gemini`, follow the **Sign in with Google** flow. Browser-based on the host; if Gemini writes file-backed auth, Hazmat harvests it into `~/.hazmat/secrets/gemini/` when the session exits. If Gemini stores OAuth only in Keychain, use the API-key path or re-auth in the contained Gemini session until Hazmat has a Keychain adapter.
- **API key path:** `hazmat config agent` can store `GEMINI_API_KEY` (AI Studio key) in `~/.hazmat/secrets/providers/gemini-api-key`. Hazmat injects it only into explicitly allowed native sessions. Vertex-style `GOOGLE_API_KEY` + `GOOGLE_GENAI_USE_VERTEXAI=true` remains a manual path for now.
- **Import from host path:** `hazmat config import gemini` stores `~/.gemini/oauth_creds.json` and `google_accounts.json` in `~/.hazmat/secrets/gemini/`, and copies `settings.json`, `GEMINI.md`, and your git identity. If your host stores OAuth in Keychain, `oauth_creds.json` won't exist on the host and that item is skipped because Hazmat does not import Keychain-backed Gemini OAuth yet.
- **Verify:** `hazmat gemini -p "say only OK"` — non-interactive prompt; should print `OK`.

### Hermes (experimental)

- **Install / update:** `hazmat bootstrap hermes` is detection-only in v1. It
  verifies an agent-owned executable at `/Users/agent/.local/bin/hermes` by
  running `hermes --version`, then records harness state. It does not run an
  upstream install script, curl pipe, npm latest install, pipx install, or host
  profile migration.
- **Managed profile root:** Hazmat creates `/Users/agent/.hazmat/hermes` with
  mode `0700` before launch and sets `HERMES_HOME` to that path. That state is
  durable agent profile state; normal rollback boundaries are documented by the
  setup/rollback model, and Hazmat does not treat host `~/.hermes` as a source.
- **Reset / uninstall boundary:** ordinary `hazmat rollback` removes host-owned
  Hazmat metadata but preserves `/Users/agent/.hazmat/hermes` with the rest of
  the agent home. After running untrusted Hermes skills, MCP servers, hooks, or
  cron-like experiments, the supported full reset is
  `hazmat rollback --delete-user` followed by `hazmat init` and
  `hazmat bootstrap hermes`. A narrower Hermes-only reset command needs a
  model-first cleanup design before
  it can be supported.
- **Provider API key path:** `hazmat config agent` can store
  `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, and
  `OPENROUTER_API_KEY` once in `~/.hazmat/secrets/providers/*`. Hermes receives
  only the provider env vars allowed by Hazmat's credential registry for the
  session.
- **Contained setup path:** run `hazmat hermes` or
  `hazmat hermes -- chat ...` and let Hermes write any local profile state under
  the managed `HERMES_HOME`.
- **Unsupported in v1:** `hazmat config import hermes`, host `~/.hermes`
  import, harness asset sync, gateway/dashboard/API/server modes, persistent
  cron/service entrypoints, and Hermes MCP/skill/profile migration.
- **Verify:** `hazmat hermes -- --version` checks the foreground launch path.
  `hazmat explain --for hermes -C /tmp` previews the session contract.
  `hazmat hermes --network none --metadata-json -- --version` verifies that
  Hermes composes with native network-none sessions.

### Qwen Code

- **Install / update:** `hazmat bootstrap qwen`. Installs or refreshes `@qwen-code/qwen-code@latest` into the agent's `~/.local` prefix via npm. Requires Node.js 20 or newer on the agent's PATH. Re-running this command updates the Hazmat copy; upgrading a host install does not change the isolated agent binary by itself.
- **Contained profile root:** Qwen uses `/Users/agent/.qwen` for auth, settings, extensions, and session state. Hazmat prepares that directory during bootstrap and does not import host `~/.qwen` auth/settings in v1.
- **Approval mode:** when `session.skip_permissions` is enabled, `hazmat qwen` prepends `--yolo` unless you already passed `--yolo` or `-y` after `--`. Hazmat remains the containment boundary.
- **Auth path:** run `hazmat qwen` and use Qwen's own auth/config flow inside the contained session. For API-key setups, keep provider keys in Qwen's contained profile or `.env`; do not rely on host `~/.qwen` being copied.
- **Asset sync:** Hazmat can sync host `~/.qwen/QWEN.md` and `~/.qwen/extensions/` into the contained profile on launch. It does not sync Qwen settings, auth, sessions, MCP config, or other executable/profile state.
- **Unsupported in v1:** `hazmat config import qwen`, host `~/.qwen` auth/settings import, daemon mode, SDK/server mode, and broad Qwen profile migration.
- **Verify:** `hazmat qwen -p "say only OK"` — single-shot prompt; should print `OK`.

## Choosing an auth mode

Three rules of thumb:

1. **You're the only user, and you've already auth'd this CLI on the host.** Use the **Import** column when the harness supports it. One command, no re-entry.
2. **You have a subscription (Claude Pro / ChatGPT Plus / Google AI Pro / OpenCode-supported subscription).** Use the **Subscription / OAuth** column. The agent's first-run picker handles the browser handoff and Hazmat harvests file-backed tokens into its host-owned store when the session exits.
3. **You only have an API key (or you're scripting CI).** Use the **API key** column. Persistent, scriptable, no browser dance.

Mixing is fine for importable harnesses: you can import once and switch to API
key later by setting the env var, or vice versa. Hermes and Qwen exclude
host-profile import in v1, so mix provider keys or contained setup according to
the harness-specific section above.

## Session modes

Harness auth and harness session mode are separate decisions:

- **Native containment:** available on all six harnesses (`claude`, `codex`, `opencode`, `gemini`, `hermes`, `qwen`).
- **Docker Sandbox:** available on all six harnesses, plus the generic `hazmat shell` and `hazmat exec` entrypoints.
- **`--docker=auto`:** works the same way on every harness. On repos that actually need a private Docker daemon, Hazmat routes that harness into Docker Sandbox mode; on code-only repos, the harness stays in native containment.

Native containment also supports a per-session network mode:

```bash
hazmat claude --network none --metadata-json -p "offline review"
hazmat codex --network none --metadata-json exec "offline review"
hazmat opencode --network none run "offline review"
hazmat gemini --network none -p "offline review"
hazmat hermes --network none --metadata-json -- --version
hazmat qwen --network none -p "offline review"
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
make hazmat-debug
```

For interactive Claude Code sessions, run the installed wrapper from the project
that reproduces the issue:

```bash
cd ~/workspace/project-that-reproduces
~/.hazmat/bin/hazmat-trace-claude --name claude-interactive-repro
```

```bash
~/.hazmat/bin/hazmat-debug trace claude --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace codex --name baseline -- --no-backup exec "say ok"
~/.hazmat/bin/hazmat-debug trace opencode --name baseline -- --no-backup run "say ok"
~/.hazmat/bin/hazmat-debug trace gemini --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace hermes --name baseline -- --no-backup -- --version
~/.hazmat/bin/hazmat-debug trace qwen --name baseline -- --no-backup -p "say ok"
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
hazmat codex --github "review this PR"
hazmat opencode --github -p "review this PR"
hazmat gemini --github -p "review this PR"
hazmat hermes --github -- chat "review this PR"
hazmat qwen --github -p "review this PR"
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

Session integrations (language toolchain extensions like `go`, `rust`, `python-uv`, `tla-java`, etc.) apply uniformly across **every** harness — claude, codex, opencode, gemini, hermes, and qwen all flow through the same `applyIntegrations` path in `resolvePreparedSession`. The HarnessID does not gate which integrations activate; auto-detection (e.g. `go.mod` triggers the `go` integration) and the `--integration <name>` CLI flag work identically per harness.

Preview the planned session contract for any harness with `hazmat explain --for <harness>`:

```bash
hazmat explain --for codex --integration go    # codex session, force-activate go integration
hazmat explain --for gemini -C ~/my-rust-app    # gemini session, auto-detect rust from Cargo.toml
hazmat explain --for opencode --json            # machine-readable preview
hazmat explain --for hermes --network none       # Hermes foreground contract
hazmat explain --for qwen --docker=auto          # Qwen foreground contract
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
| Gemini | `~/.gemini/GEMINI.md`, `extensions/` |
| Qwen | `~/.qwen/QWEN.md`, `extensions/` |
| Hermes | none in v1; host `~/.hermes`, skills, MCP, cron, and service config are not synced |

For the rows with synced paths, these are managed copies — if you edit them
inside the sandbox, the next session will overwrite your edits with the host
version. Edit on the host instead.

## Troubleshooting

- **Bootstrap sees an existing harness binary:** Hazmat still runs the harness installer/update path. Existing config files, hooks, auth state, and shared directories remain idempotent and are not overwritten unless their step explicitly says so.
- **Import says "no basics found to import":** the host doesn't have any of the expected files in its standard locations. Check the **Auth file location** above for the harness — that's the path the import scans.
- **Import says "Codex auth imported" but `hazmat codex` still asks for sign-in:** check that `~/.hazmat/secrets/codex/auth.json` exists. If an older Hazmat left a stale `/Users/agent/.codex/auth.json`, current Hazmat should recover it automatically on launch. If the stale copy differs from the host-owned copy, the previous host-owned copy is preserved under `~/.hazmat/secrets/codex/auth.json.conflicts/`.
- **Codex chat hangs on "Reconnecting…":** if you're on a hazmat older than commit `eaaaa1c`, the seatbelt was missing several Security framework allowances. Update and rebuild.
- **`hazmat bootstrap hermes` says Hermes is not installed:** install or link the Hermes executable as the agent user at `/Users/agent/.local/bin/hermes`, then rerun bootstrap. Hazmat records Hermes as installed only after `hermes --version` succeeds.
- **`hazmat hermes -- gateway` / `dashboard` / `server` / `cron` is rejected:** v1 supports foreground Hermes sessions only. Run an interactive or prompt-driven foreground command under `hazmat hermes`, or track service supervision as a separate design.
- **`hazmat qwen` still asks for auth:** run Qwen's auth flow inside `hazmat qwen`, or configure the contained `/Users/agent/.qwen` profile. Hazmat does not import host `~/.qwen` auth/settings in v1.

For deeper containment behavior (what the agent can and can't see), [docs/usage.md](usage.md) is the canonical reference. To verify any of the setup paths above end-to-end (per-harness checklists, regression scenarios, recovery), see [docs/manual-testing.md](manual-testing.md).
