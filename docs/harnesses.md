# Supported Harnesses

Hazmat runs four agent CLIs in containment. This page is the actionable reference: pick your harness, pick your auth path, run the listed commands.

## Comparison matrix

Use this table to choose a setup path. Every harness supports two of the three auth modes; the third column shows the **simplest** way to get a working session.

| Harness | Tested | Install | Subscription / OAuth | API key (env var) | Import from host |
|---|---|---|---|---|---|
| **Claude Code** | 2.1.118 | `hazmat bootstrap claude` | `/login` inside `hazmat claude` | `ANTHROPIC_API_KEY` via `hazmat config agent` | `hazmat config import claude` |
| **Codex** | 0.118.0 | `hazmat bootstrap codex` | Device Code in TUI (or import) | `OPENAI_API_KEY` via `hazmat config agent` | `hazmat config import codex` |
| **OpenCode** | 1.14.20 | `hazmat bootstrap opencode` | per-provider OAuth via `opencode auth login` | per-provider env vars | `hazmat config import opencode` |
| **Gemini** | 0.38.2 | `hazmat bootstrap gemini` | Google sign-in inside `hazmat gemini` | `GEMINI_API_KEY` via `hazmat config agent` | `hazmat config import gemini` |

After bootstrap + auth: `hazmat <harness>` to launch a session, or `hazmat <harness> -p "prompt"` (claude / gemini) / `hazmat <harness> exec "prompt"` (codex) / `hazmat <harness> run "prompt"` (opencode) for non-interactive use.

The fastest path for a new install is almost always the **import** column — it copies whatever credentials you already have on the host into the agent's home, so there's nothing to re-enter inside the sandbox.

## Per-harness reference

### Claude Code

- **Install (one-time):** `hazmat bootstrap claude`. Downloads the official Anthropic installer, verifies the published checksum, installs to `/Users/agent/.local/bin/claude`. Idempotent.
- **Auth file location:** `/Users/agent/.claude/.credentials.json` (mode `0600`).
- **Subscription / OAuth path:** run `hazmat claude`, type `/login`. Claude opens a browser for the OAuth handshake; the resulting credentials are written to `.credentials.json` inside the sandbox.
- **API key path:** `hazmat config agent` will offer to store `ANTHROPIC_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/anthropic-api-key`. Hazmat injects it only into matching native sessions instead of keeping it in `/Users/agent/.zshrc`.
- **Import from host path:** `hazmat config import claude` copies `~/.claude/.credentials.json`, the user-level `commands/` and `skills/` portable basics, and your git identity from host → agent. Doesn't import `settings.json`, hooks, MCP, or session history (those stay host-only).
- **Verify:** `hazmat claude -p "say OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/claude-import.md](claude-import.md).

### Codex

- **Install (one-time):** `hazmat bootstrap codex`. Downloads the official OpenAI installer, verifies the GitHub-published digest, installs to `/Users/agent/.local/bin/codex`. Also prepares `/Users/agent/.codex` and `/Users/agent/.agents` shared dirs.
- **Auth file location:** `/Users/agent/.codex/auth.json` (mode `0600`). Holds **both** ChatGPT subscription OAuth tokens and OpenAI API keys — same file regardless of which auth mode you use.
- **Subscription / OAuth path:** run `hazmat codex`, use the arrow keys (or type the option number directly) to pick **Sign in with Device Code** in the first-run picker, then press Enter. You complete the code on your host browser; the token persists in `auth.json` inside the sandbox.
  - The import path bypasses this picker entirely.
- **API key path:** `hazmat config agent` can store `OPENAI_API_KEY` from your invoking shell in `~/.hazmat/secrets/providers/openai-api-key`. Hazmat injects it only into matching native sessions. You can also paste an API key in the codex first-run picker (option `3`) or import `auth.json` from the host.
- **Import from host path:** `hazmat config import codex` copies `~/.codex/auth.json` (covers OAuth and API key) plus your git identity. Prompts, rules, and `AGENTS.md` mirror automatically via the harness asset sync at session launch.
- **Verify:** `hazmat codex exec "Reply with only OK"` — runs the codex non-interactive subcommand; should print `OK` and exit cleanly.

### OpenCode

- **Install (one-time):** `hazmat bootstrap opencode`. Downloads via the official OpenCode installer, prepares config dir, links `/Users/agent/.local/bin/opencode`.
- **Auth file location:** `/Users/agent/.local/share/opencode/auth.json` (mode `0600`). Provider-specific shape; OpenCode supports Anthropic, OpenAI, Google, OpenRouter, Groq, etc.
- **Subscription / OAuth path:** run `hazmat opencode`, then `opencode auth login` and pick a provider. Each provider has its own OAuth flow; what works in plain `opencode` works inside `hazmat opencode`.
- **API key path:** OpenCode reads provider keys from the same `auth.json`. Either paste them via `opencode auth login` inside the sandbox, or pre-seed them on the host with the OpenCode `auth login` flow and import.
- **Import from host path:** `hazmat config import opencode` copies `~/.local/share/opencode/auth.json` (all configured providers) plus the user-level `commands/`, `agents/`, `skills/` portable basics and your git identity.
- **Verify:** `hazmat opencode run "say only OK"` — single-shot prompt; should print `OK`.
- **Detailed import scope:** [docs/opencode-import.md](opencode-import.md).

### Gemini

- **Install (one-time):** `hazmat bootstrap gemini`. Installs `@google/gemini-cli@latest` into the agent's `~/.local` prefix via npm. Requires Node.js on the agent's PATH (Homebrew node at `/opt/homebrew/bin/node` works).
- **Auth file location:** `/Users/agent/.gemini/oauth_creds.json` (file fallback, mode `0600`) **or** macOS Keychain (modern default — file isn't created when Keychain is used).
- **Subscription / OAuth path:** run `hazmat gemini`, follow the **Sign in with Google** flow. Browser-based on the host; tokens persist inside the sandbox.
- **API key path:** `hazmat config agent` can store `GEMINI_API_KEY` (AI Studio key) in `~/.hazmat/secrets/providers/gemini-api-key`. Hazmat injects it only into matching native sessions. Vertex-style `GOOGLE_API_KEY` + `GOOGLE_GENAI_USE_VERTEXAI=true` remains a manual path for now.
- **Import from host path:** `hazmat config import gemini` copies `~/.gemini/oauth_creds.json` (when host stored creds in the file fallback rather than Keychain), `google_accounts.json`, `settings.json`, `GEMINI.md`, and your git identity. If your host stores OAuth in Keychain, `oauth_creds.json` won't exist on the host and that item is silently skipped — re-auth inside `hazmat gemini` or use the env-var path.
- **Verify:** `hazmat gemini -p "say only OK"` — non-interactive prompt; should print `OK`.

## Choosing an auth mode

Three rules of thumb:

1. **You're the only user, and you've already auth'd this CLI on the host.** Use the **Import** column. One command, no re-entry.
2. **You have a subscription (Claude Pro / ChatGPT Plus / Google AI Pro / OpenCode-supported subscription).** Use the **Subscription / OAuth** column. The agent's first-run picker handles the browser handoff and persists the tokens.
3. **You only have an API key (or you're scripting CI).** Use the **API key** column. Persistent, scriptable, no browser dance.

Mixing is fine: you can import once and switch to API key later by setting the env var, or vice versa. Hazmat doesn't track which mode you're using.

## Session modes

Harness auth and harness session mode are separate decisions:

- **Native containment:** available on all four harnesses (`claude`, `codex`, `opencode`, `gemini`).
- **Docker Sandbox:** available on all four harnesses, plus the generic `hazmat shell` and `hazmat exec` entrypoints.
- **`--docker=auto`:** works the same way on every harness. On repos that actually need a private Docker daemon, Hazmat routes that harness into Docker Sandbox mode; on code-only repos, the harness stays in native containment.

## Session integrations

Session integrations (language toolchain extensions like `go`, `rust`, `python-uv`, `tla-java`, etc.) apply uniformly across **every** harness — claude, codex, opencode, and gemini all flow through the same `applyIntegrations` path in `resolvePreparedSession`. The HarnessID does not gate which integrations activate; auto-detection (e.g. `go.mod` triggers the `go` integration) and the `--integration <name>` CLI flag work identically per harness.

Preview the planned session contract for any harness with `hazmat explain --for <harness>`:

```bash
hazmat explain --for codex --integration go    # codex session, force-activate go integration
hazmat explain --for gemini -C ~/my-rust-app    # gemini session, auto-detect rust from Cargo.toml
hazmat explain --for opencode --json            # machine-readable preview
```

Integrations are documented in [docs/integrations.md](integrations.md) — the trust model, allowed env passthrough set, and built-in list are all there.

## Session asset sync

Independent of the auth mode you pick, hazmat keeps a small set of "portable basics" in sync from your host to the agent on every session launch. This is harness-aware and runs automatically (toggle with `session.harness_assets` in `hazmat config`):

| Harness | Synced from host on launch |
|---|---|
| Claude | `~/.claude/CLAUDE.md`, `commands/`, `skills/`, `agents/` |
| Codex | `~/.codex/AGENTS.md`, `prompts/`, `rules/`, `~/.agents/skills/` |
| OpenCode | `~/.config/opencode/commands/`, `agents/`, `skills/` |
| Gemini | `~/.gemini/GEMINI.md`, `extensions/` |

These are managed copies — if you edit them inside the sandbox, the next session will overwrite your edits with the host version. Edit on the host instead.

## Troubleshooting

- **Bootstrap step says "already installed":** that's the idempotent path. If you want a fresh install, uninstall first (`hazmat <harness> uninstall` if the CLI provides one, or remove the binary at `/Users/agent/.local/bin/<harness>`).
- **Import says "no basics found to import":** the host doesn't have any of the expected files in its standard locations. Check the **Auth file location** above for the harness — that's the path the import scans.
- **Import says "Codex auth imported" but `hazmat codex` still asks for sign-in:** the import wrote with the wrong ownership in early versions. Fixed in commit `6a466e7`; if you're on an older binary, `sudo -u agent rm /Users/agent/.codex/auth.json` and re-run the import.
- **Codex chat hangs on "Reconnecting…":** if you're on a hazmat older than commit `eaaaa1c`, the seatbelt was missing several Security framework allowances. Update and rebuild.

For deeper containment behavior (what the agent can and can't see), [docs/usage.md](usage.md) is the canonical reference. To verify any of the setup paths above end-to-end (per-harness checklists, regression scenarios, recovery), see [docs/manual-testing.md](manual-testing.md).
