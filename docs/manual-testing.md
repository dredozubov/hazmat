# Manual Testing

Hazmat ships an automated test suite (see [docs/testing.md](testing.md)), but the things the test suite can't reach — interactive prompts, real network calls, browser-based OAuth, terminal UI input — need a human at a keyboard.

This page is the human-driven verification checklist. Use it before cutting a release, after touching the harness / seatbelt / init code paths, or when validating a fresh install on a new machine.

## How to use this doc

Each item is a checkbox. Tick it when the **Expected** outcome lands. If something fails, the **On failure** column points at what to inspect next.

You don't have to run the whole list every time. The shortest meaningful pass is **§1 (preconditions) + the harness section for whichever harness you touched**. Run the cross-cutting and recovery sections before a release.

Steps assume macOS, a real TTY (not a piped shell), and a working `hazmat init` from a recent commit. Snippets that need substitution use `<placeholder>` markers.

---

## 1. Preconditions

Before you touch any harness flow.

- [ ] **Latest binary installed**
  - Steps:
    ```bash
    cd ~/workspace/hazmat
    make && make install
    hazmat --version
    ```
  - Expected: version string matches the commit you're testing (e.g. `v0.7.0-NN-g<sha>`).
  - On failure: check `make install` output; ensure `~/.local/bin/hazmat` is on `PATH`.

- [ ] **Init has been run on this machine**
  - Steps:
    ```bash
    hazmat status
    ```
  - Expected: every check is green; agent user exists; sudoers + launch helper present.
  - On failure: run `hazmat init` (interactive) and re-check.

- [ ] **No leaked agent processes from earlier sessions**
  - Steps:
    ```bash
    ps -u agent -o pid,etime,comm | grep -v 'PID\|^$'
    ```
  - Expected: no output, or only short-lived processes you started intentionally. Long-running `hazmat-launch` processes from hours ago indicate a leak (see §6 recovery).
  - On failure: `sudo -n -u agent /usr/bin/pkill -9 -f hazmat-launch`, then re-check.

- [ ] **SSH config is the multi-key shape (not single-key)**
  - Steps:
    ```bash
    grep -A 5 'projects:' ~/.hazmat/config.yaml | head -30
    ```
  - Expected: every project entry under `ssh:` uses the `keys:` list shape (with `name`, `private_key`, `known_hosts`, `hosts`). The single-key shape (`private_key:` directly under `ssh:`) is rejected at config-load time and blocks every harness launch.
  - On failure: migrate any offender to the multi-key shape per [docs/usage.md](usage.md) §"Reusable SSH profiles".

---

## 2. Per-harness flows

Run **one path** (subscription / API key / host import) per harness for a smoke pass; run **all three** before a release.

### 2.1 Claude Code

- [ ] **Bootstrap**
  - Steps: `hazmat bootstrap claude`
  - Expected: each step ✓; `/Users/agent/.local/bin/claude` exists; `claude --version` (run as agent: `sudo -n -u agent -H /Users/agent/.local/bin/claude --version`) prints a version.
  - On failure: re-run with `-v`; check the bootstrap script error.

- [ ] **Subscription path** (`/login`)
  - Preconditions: a Claude Pro/Max subscription on the host.
  - Steps: `hazmat claude` → type `/login` → complete browser OAuth → exit Claude.
  - Expected: `~/.hazmat/secrets/claude/credentials.json` exists after exit; `/Users/agent/.claude/.credentials.json` is gone again once the session ends.
  - Verify: `hazmat claude -p "say only OK"` round-trips.

- [ ] **API key path** (env var)
  - Preconditions: `ANTHROPIC_API_KEY` set in your invoking shell.
  - Steps: `hazmat config agent` → accept the offer to copy the host key → press Enter through git identity.
  - Expected: `~/.hazmat/secrets/providers/anthropic-api-key` exists with mode `0600`; `hazmat claude -p "say only OK"` round-trips.

- [ ] **Host import path**
  - Preconditions: claude already authed on host (`~/.claude/.credentials.json` exists).
  - Steps: `hazmat config import claude --dry-run` → review plan → `hazmat config import claude` (no `--dry-run`).
  - Expected: "Sign-in: yes" in the plan; `~/.hazmat/secrets/claude/credentials.json` and/or `state.json` are populated; subsequent `hazmat claude -p "say OK"` round-trips without a `/login` prompt.

### 2.2 Codex

- [ ] **Bootstrap**
  - Steps: `hazmat bootstrap codex`
  - Expected: each step ✓; `/Users/agent/.codex` and `/Users/agent/.agents` both prepared as `agent:dev 2770`.
  - On failure: check that the GitHub installer URL is reachable; `curl --head` it manually.

- [ ] **Subscription path** (Device Code)
  - Preconditions: a ChatGPT Plus/Pro/Business/Enterprise subscription.
  - Steps: `hazmat codex` → at the auth picker, press `↓` once to highlight **Sign in with Device Code** (or type `2`) → press Enter → complete the device-code flow on host browser.
  - Expected: the highlight moves from option `1` to option `2`; after exit `~/.hazmat/secrets/codex/auth.json` exists and `/Users/agent/.codex/auth.json` has been cleaned up.
  - Verify: `hazmat codex exec "Reply with only OK and nothing else."` returns `OK` with a token-count footer.

- [ ] **API key path** (env var)
  - Preconditions: `OPENAI_API_KEY` set in your invoking shell.
  - Steps: `hazmat config agent` → accept the OpenAI key prompt.
  - Expected: `~/.hazmat/secrets/providers/openai-api-key` exists with mode `0600`; `hazmat codex exec "say OK"` round-trips.

- [ ] **Host import path**
  - Preconditions: codex already authed on host.
  - Steps: `hazmat config import codex` → accept.
  - Expected: `~/.hazmat/secrets/codex/auth.json` exists; subsequent `hazmat codex exec "say OK"` skips the auth picker entirely.

### 2.3 OpenCode

- [ ] **Bootstrap**
  - Steps: `hazmat bootstrap opencode`
  - Expected: each step ✓; PATH shim at `/Users/agent/.local/bin/opencode` → `/Users/agent/.opencode/bin/opencode`; `opencode.json` written.

- [ ] **Subscription / per-provider auth path**
  - Preconditions: a provider account that OpenCode supports (Anthropic, OpenAI, Google, OpenRouter, etc.).
  - Steps: `hazmat opencode` → at the OpenCode prompt, run `/auth` (or exit and use `opencode auth login` outside Hazmat then import).
  - Expected: provider entry is available during the session and harvested into `~/.hazmat/secrets/opencode/auth.json` when the session exits.

- [ ] **API key path** (per provider, no single env var)
  - Note: OpenCode is exempt from the `hazmat config agent` env-var prompts (multi-provider — no single key). Use the host import path or set per-provider keys with `opencode auth login` inside the sandbox.

- [ ] **Host import path**
  - Preconditions: opencode authed on host.
  - Steps: `hazmat config import opencode --dry-run` → `hazmat config import opencode`.
  - Expected: "Sign-in: yes"; `~/.hazmat/secrets/opencode/auth.json` exists; `hazmat opencode run "say OK"` round-trips.

### 2.4 Gemini

- [ ] **Bootstrap**
  - Preconditions: Node.js available on agent PATH (Homebrew node satisfies this).
  - Steps: `hazmat bootstrap gemini`
  - Expected: each step ✓; `/Users/agent/.local/bin/gemini` linked from npm prefix; `/Users/agent/.gemini` prepared.
  - On failure: check `node --version` works for the agent: `sudo -n -u agent -H bash -lc 'node --version'`.

- [ ] **Subscription path** (Google sign-in)
  - Preconditions: a Google account with Gemini access.
  - Steps: `hazmat gemini` → "Sign in with Google" flow.
  - Expected: if Gemini writes file-backed auth, Hazmat harvests that data into `~/.hazmat/secrets/gemini/` when the session exits. If Gemini stores OAuth only in Keychain, Hazmat reports that as an adapter-required external backend and does not copy the Keychain item into `/Users/agent`.

- [ ] **API key path** (env var)
  - Preconditions: `GEMINI_API_KEY` set in your invoking shell (get one from https://aistudio.google.com/apikey).
  - Steps: `hazmat config agent` → accept the Gemini key prompt.
  - Expected: `~/.hazmat/secrets/providers/gemini-api-key` exists with mode `0600`; `hazmat gemini -p "say OK"` round-trips.

- [ ] **Host import path** (file-fallback only)
  - Preconditions: host stores creds in `~/.gemini/oauth_creds.json` (file fallback). If host uses macOS Keychain, this item is N/A — the import skips the OAuth file because Keychain-backed Gemini OAuth is not imported yet.
  - Steps: `hazmat config import gemini`.
  - Expected: imported file-backed auth lands in `~/.hazmat/secrets/gemini/`; `hazmat gemini -p "say OK"` round-trips.

---

## 3. Cross-cutting

These exercise the per-harness scaffolding rather than any one harness.

- [ ] **`hazmat init --bootstrap-agent <harness>` end-to-end**
  - Steps: on a clean (rolled-back) machine: `hazmat rollback --yes` → `hazmat init --bootstrap-agent gemini` (try each of `claude / codex / opencode / gemini` in turn).
  - Expected: agent user created; bootstrap step runs for the chosen harness; `hazmat config agent` prompt appears; the optional "Import basics?" prompt appears for the bootstrapped harness; the "Ready to use" guidance ends with `cd your-project && hazmat <harness>`.

- [ ] **`hazmat explain --for <harness>`** for each of `claude / codex / opencode / gemini / shell / exec`
  - Steps: `hazmat explain --for <each> -C /tmp` (or any project dir without an SSH-config gate)
  - Expected: each prints a session contract; integrations section updates if `--integration go` is added; no errors.

- [ ] **Docker Sandbox support across harnesses**
  - Preconditions: repo with a `Dockerfile`.
  - Steps: `hazmat codex --docker=auto -C <repo>` (repeat for `opencode` and `gemini`), then explicitly `hazmat codex --docker=sandbox -C <repo>` (repeat for `opencode` / `gemini`).
  - Expected: `--docker=auto` routes the matching harness into Docker Sandbox mode on Docker-heavy private-daemon repos; explicit `--docker=sandbox` launches the same harness in Docker Sandbox mode without redirecting you to Claude.

- [ ] **Per-harness seatbelt scoping**
  - Steps: snapshot the generated SBPL for claude vs codex:
    ```bash
    hazmat explain --for claude --json -C /tmp/x | jq -r '.session.script_path' || true
    # Or eyeball: launch a session, then inspect /tmp/hazmat-<pid>.sb while it's live.
    ```
  - Expected: the codex policy contains `com.apple.SystemConfiguration.configd`, `com.apple.SecurityServer`, `/Library/Keychains`, and the `apple.shm.notification_center` IPC; the claude policy does **not** contain any of those (least-privilege gating from `sandboxing-m7f7`).

- [ ] **Session integrations apply uniformly per harness**
  - Steps: in a Go project, `hazmat explain --for codex` and `hazmat explain --for gemini`.
  - Expected: both show `Integrations: go` with the same `Integration sources` line; both auto-add the Go module cache to read-only.

- [ ] **Harness asset sync**
  - Preconditions: edit a file in your host `~/.codex/prompts/` (or `~/.claude/commands/` for claude, `~/.gemini/extensions/` for gemini, `~/.config/opencode/commands/` for opencode).
  - Steps: launch the matching `hazmat <harness>` session; observe the "host changes" line.
  - Expected: a "<Harness> asset sync" entry; the agent-side file matches the host-side after launch.

- [ ] **Pre-session snapshot**
  - Steps: any `hazmat <harness>` launch; before chatting, scroll up to the snapshot line.
  - Expected: `Snapshot: <project> ... done (X.Xs)`; `hazmat snapshots list` shows the new entry.

- [ ] **Cloud credential storage**
  - Preconditions: disposable S3-compatible test credentials, or run only the config/migration checks without `hazmat backup --cloud`.
  - Steps: configure with `HAZMAT_CLOUD_SECRET_KEY=<secret> HAZMAT_CLOUD_PASSWORD=<recovery> hazmat config cloud --endpoint <endpoint> --bucket <bucket> --access-key <access> --secret-key-from-env`.
  - Expected: `~/.hazmat/secrets/cloud/s3-access-key-id`, `s3-secret-key`, and `kopia-recovery-key` exist with mode `0600`; `~/.config/hazmat/config.yaml` contains endpoint/bucket but not the access key, secret key, recovery key, `access_key:`, `recovery_key:`, or `password:`. If an old `~/.hazmat/cloud-credentials` file is present, the next cloud read migrates it into `~/.hazmat/secrets/cloud/s3-secret-key` and removes the legacy file.

- [ ] **Status bar visible during an interactive session**
  - Steps: `hazmat claude` (or any harness) in a fullscreen terminal; check the bottom row.
  - Expected: `☢ HAZMAT │ <integrations> ... <project>` rendered in the bottom row, doesn't scroll.

---

## 4. Self-heal scenarios (regression coverage)

These verify that earlier-fixed bugs stay fixed.

- [ ] **Legacy auth file migrates into host-owned storage**
  - Setup: simulate an older Hazmat release that still left auth under `/Users/agent`:
    ```bash
    rm -f ~/.hazmat/secrets/codex/auth.json
    sudo -u agent rm -f /Users/agent/.codex/auth.json
    sudo -u agent install -d -m 700 /Users/agent/.codex
    sudo -u agent cp ~/.codex/auth.json /Users/agent/.codex/auth.json
    ```
  - Steps: `hazmat codex exec "Reply with only OK."`
  - Expected: the run succeeds; after exit `~/.hazmat/secrets/codex/auth.json` exists and `/Users/agent/.codex/auth.json` is gone again.
  - On failure: check the session notes for a migration warning or mismatch warning from the harness auth runtime.

- [ ] **Codex bootstrap creates `~/.agents` shared dir** (regression: `sandboxing-3u4a`)
  - Steps: `sudo -u agent rm -rf /Users/agent/.agents` → `hazmat bootstrap codex`
  - Expected: re-creates `/Users/agent/.agents` as `agent:dev 2770`; subsequent `hazmat codex` does not fail with `mkdir /Users/agent/.agents: permission denied`.

- [ ] **`hazmat-launch` does not hang in non-TTY shells** (regression: `sandboxing-qfv6`)
  - Steps: from a non-TTY shell (e.g. an SSH session piped to `tee`, or a script):
    ```bash
    hazmat codex exec "Reply with only OK." 2>&1 | tee /tmp/codex-noninteractive.log
    ```
  - Expected: completes within tens of seconds; `tee` shows the OK response. **Never hangs** spinning at 70%+ CPU.
  - On failure: check `closeInheritedFDs` is using `/dev/fd` enumeration (not iterating to RLIMIT_NOFILE); `ps -u agent` should not show stuck `hazmat-launch exec ...` processes after the run.

- [ ] **Config-agent with multiple harnesses installed**
  - Preconditions: claude + codex + gemini all bootstrapped.
  - Steps: `hazmat config agent`
  - Expected: three separate "API key" steps, one per installed harness, in order Claude → OpenAI → Gemini. OpenCode is intentionally skipped (no single env var).

---

## 5. Rollback / cleanup

- [ ] **`hazmat rollback`** unwinds init cleanly
  - Steps: `hazmat rollback --yes` → `hazmat status`
  - Expected: agent user removed; sudoers + DNS blocklist + pf anchor + launch helper all removed; status shows "not initialized".

- [ ] **Idempotent re-bootstrap after rollback**
  - Steps: after a rollback, `hazmat init --bootstrap-agent claude` (or any harness)
  - Expected: works without manual cleanup of leftover files.

---

## 6. Recovery (when things are stuck)

Reference card for fixing common stuck states. Not a checklist — these are the moves to make when something in §1–§5 fails.

- **Stuck `hazmat-launch` agent processes** (high CPU, won't exit):
  ```bash
  sudo -n -u agent /usr/bin/pkill -9 -f hazmat-launch
  ```

- **Single-key SSH config blocking all launches**: edit `~/.hazmat/config.yaml`, convert each project entry to the multi-key shape ([docs/usage.md](usage.md) §"Reusable SSH profiles"). The error message includes the exact YAML to paste.


- **Codex chat fails with `no native root CA certificates`**: rebuild against latest master (commit `eaaaa1c` and later). Several Security framework / mach-service allowances were added.

- **Stale auth file left under `/Users/agent` from an older build**: relaunch the matching harness with current Hazmat. Startup recovery should promote the leftover agent copy into `~/.hazmat/secrets`, archive any divergent host-owned copy under a sibling `.conflicts/` directory, and remove the agent copy. Manual removal should only be needed if recovery reports a filesystem error:
  ```bash
  hazmat <harness>
  ```

---

## 7. After-test cleanup

- [ ] **No leaked agent processes**: `ps -u agent` shows nothing surprising.
- [ ] **No stray `/tmp/hazmat-*.sb` files older than your session**: `ls -lt /tmp/hazmat-*.sb 2>/dev/null | head` — anything older than your last session was leaked by an earlier crash.
- [ ] **Snapshot count is what you expected**: `hazmat snapshots list | wc -l` — sanity check that pre-session snapshots aren't accumulating without bound.
- [ ] **bd issues reflect the test result**: if a checklist item failed in a way that points at a bug, file it (`bd create --type=bug ...`) before moving on.

---

## What this doc deliberately doesn't cover

- **Linux**: there's no `hazmat init` on Linux yet; `sandboxing-pk5x` tracks adding it.
- **Docker Sandbox mode**: see [docs/tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) — separate test discipline.
- **CVE coverage**: see [docs/cve-audit.md](cve-audit.md).
- **TLA+ verification**: `cd tla && bash check_suite.sh` — no human-in-loop component.
- **Performance**: there's no perf test yet; if a session takes noticeably longer to start than the last release, file a bug.

If something feels wrong but isn't covered above, file an issue with `bd create --type=bug` rather than expanding this doc indefinitely. This doc should stay short enough to skim before a release.
