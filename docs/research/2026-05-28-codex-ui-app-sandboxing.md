# Codex UI App Sandboxing Feasibility

**Date**: 2026-05-28
**Scope**: Research whether the Codex desktop UI can be sandboxed similarly to Hazmat's CLI harnesses, using the current Hazmat Codex harness, the installed Codex app, OpenAI Codex source/docs, and Ollama's Codex App integration as evidence.
**Status**: Research only. No implementation changes in this pass.

## Executive answer

Running the stock `/Applications/Codex.app` itself inside Hazmat's current CLI-style native sandbox is technically worth a spike, but it is not a good first implementation target. The desktop app is an Electron GUI application with LaunchServices, WindowServer, TCC, keychain, Sparkle/update, crashpad, Chromium helper, browser-use, native addon, and app-server process surfaces. That is a much larger and less terminal-shaped target than the CLI harnesses Hazmat currently launches.

The most feasible Hazmat-shaped design is to sandbox the Codex execution backend, not the whole GUI:

1. Launch `codex app-server` under Hazmat as the `agent` user, with the same outer user isolation, generated SBPL, network policy, credential materialization, and cleanup lifecycle used for the CLI.
2. Treat the stock desktop app, if used at all, as a host-side client that must connect to a contained backend or contained remote environment.
3. If the stock app cannot attach to an externally launched app-server or remote execution environment, build a Hazmat-owned app-server client/proxy first and leave full GUI containment as a stretch item.

Configuring the stock app's `~/.codex/config.toml` can improve the app's own sandbox/approval defaults, but that is Tier 0 hardening. It is not equivalent to Hazmat's CLI boundary because the app still runs as the host user and its local app-server exposes host-side filesystem/process utilities.

## Current Hazmat CLI boundary

Hazmat's Codex CLI harness is already meaningfully stronger than Codex's built-in sandbox because it adds an outer boundary:

- The harness runs as the dedicated `agent` user, not as the invoking user.
- `hazmat-launch` closes non-stdio descriptors, validates the generated policy file, calls `sandbox_init()`, then `exec`s the target process.
- The generated SBPL is deny-by-default, grants project/session paths, optionally blocks network, and applies last-match credential denies.
- Codex credentials are stored under `~/.hazmat/secrets/codex/auth.json`, materialized to `/Users/agent/.codex/auth.json` only during a Codex session, harvested on exit, then removed from the agent home.
- Hazmat imports only curated Codex basics from the host: `~/.codex/auth.json` and git identity. It does not import the host Codex config, plugins, session history, rules, prompts, or runtime caches as a blind migration.
- For Codex specifically, the native policy carries extra macOS TLS/Security framework allowances. Those are gated to Codex and covered by tests to avoid broadening other harnesses.

Relevant local code/docs:

- `docs/harnesses.md`
- `hazmat/session.go`
- `hazmat/native_launch.go`
- `hazmat/native_launch_darwin.go`
- `hazmat/cmd/hazmat-launch/main.go`
- `hazmat/session_policy_sbpl.go`
- `hazmat/bootstrap_codex.go`
- `hazmat/config_import_codex.go`
- `hazmat/credential_registry.go`
- `hazmat/harness_auth_runtime.go`
- `tla/VERIFIED.md`
- `tla/09_launch_fd_isolation.md`
- `tla/13_credential_capability_lifecycle.md`

This is the standard any Codex UI work should be compared against.

## Observed Codex App surface

Installed app inspected: `/Applications/Codex.app`

- Bundle ID: `com.openai.codex`
- Version observed: `26.519.41501`
- Signed by: `Developer ID Application: OpenAI OpCo, LLC (2DC432GLL2)`
- Hardened runtime: enabled
- Notarization ticket: stapled
- App Sandbox entitlement: `false`

Notable entitlements on the app bundle:

```json
{
  "com.apple.security.app-sandbox": false,
  "com.apple.security.automation.apple-events": true,
  "com.apple.security.cs.allow-jit": true,
  "com.apple.security.cs.allow-unsigned-executable-memory": true,
  "com.apple.security.device.audio-input": true,
  "com.apple.security.files.user-selected.read-write": true,
  "com.apple.security.network.client": true,
  "keychain-access-groups": ["2DC432GLL2.*"]
}
```

The app process tree on this machine is not running as `agent`; it runs as the host user. It includes:

- `/Applications/Codex.app/Contents/MacOS/Codex`
- Electron GPU/network/renderer helpers
- renderer helpers with Chromium `--enable-sandbox`
- `/Applications/Codex.app/Contents/Resources/codex app-server --analytics-default-enabled`
- multiple `/Applications/Codex.app/Contents/Resources/node_repl` children, each with `codex app-server --listen stdio://` grandchildren from local Codex tool sessions
- `native/bare-modifier-monitor`
- crashpad handlers writing under `~/Library/Application Support/Codex/Crashpad`

The resources bundle contains executable/native components beyond the Rust `codex` binary:

- `Resources/codex`
- `Resources/node`
- `Resources/node_repl`
- `Resources/rg`
- `Resources/codex_chronicle`
- `Resources/native/*.node`
- `Resources/native/bare-modifier-monitor`
- `Resources/native/launch-services-helper`

Observed state/socket locations include:

- `~/Library/Application Support/Codex`
- `~/Library/Preferences/com.openai.codex.plist`
- `~/Library/Caches/com.openai.codex`
- `~/Library/HTTPStorages/com.openai.codex`
- `~/Library/Logs/com.openai.codex`
- `~/.codex/sqlite/codex-dev.db`
- `/tmp/codex-browser-use/*.sock`
- `/var/folders/.../T/codex-ipc/ipc-501.sock`

Implication: the app is not an App Sandbox-contained bundle. Chromium renderer processes have their own sandboxing, but the product as a whole is an unsandboxed signed Electron app plus Rust and Node helper processes. Putting an external SBPL around the parent should constrain inherited children if launched directly through `hazmat-launch`, but making the full GUI work would require a much broader policy than the CLI harness.

## Codex's own sandbox is command-scoped

OpenAI's current Codex docs say local commands in the Codex app, IDE extension, and CLI run inside constrained platform-native environments by default. On macOS, Codex uses Seatbelt; on Linux/WSL2, bubblewrap/seccomp; on Windows, the native Windows sandbox or WSL2 path. The docs also distinguish the technical sandbox from the approval policy, and identify `workspace-write` with no command network as the default low-friction mode.

Official docs referenced:

- https://developers.openai.com/codex/concepts/sandboxing
- https://developers.openai.com/codex/app/features#approvals-and-sandboxing
- https://developers.openai.com/codex/config-reference#configtoml

The app-server source clarifies the boundary:

- `command/exec` runs through Codex's sandbox manager. It accepts a permission profile or legacy sandbox policy and calls `codex_core::exec::build_exec_request(...)`, which selects the platform sandbox and network proxy behavior.
- `turn/start` can carry sandbox/permission overrides for agent turns.
- `thread/shellCommand` is documented as an unsandboxed user-initiated shell command.
- `process/spawn` is explicitly documented as spawning a standalone process without the Codex sandbox on the host where app-server runs. The implementation builds environment from `std::env::vars()` and calls PTY/pipe spawn helpers directly.
- `fs/readFile`, `fs/writeFile`, `fs/createDirectory`, `fs/remove`, and related app-server filesystem methods pass `/*sandbox*/ None` to the executor filesystem and operate on absolute host paths.
- `environment/add` can register remote execution environments by `environmentId` and `execServerUrl`.
- `remoteControl/*` exists, but is experimental and enables control of the current app-server process rather than proving that the stock GUI can attach to an arbitrary Hazmat-owned backend.

Relevant upstream source/docs inspected in the local clone at `/Users/dr/workspace/codex`:

- `codex-rs/app-server/README.md`
- `codex-rs/app-server/src/request_processors/command_exec_processor.rs`
- `codex-rs/app-server/src/request_processors/process_exec_processor.rs`
- `codex-rs/app-server/src/request_processors/fs_processor.rs`
- `codex-rs/app-server/src/request_processors/environment_processor.rs`
- `codex-rs/app-server/src/request_processors/remote_control_processor.rs`
- `codex-rs/sandboxing/src/manager.rs`
- `codex-rs/sandboxing/src/seatbelt.rs`

This matters for Hazmat: Codex's inner sandbox is valuable, but it is not a complete boundary around every app-server capability. If the app-server runs as the host user, app-server filesystem/process utilities have host-user reach. If the app-server runs as `agent` under Hazmat, even its unsandboxed utilities are still inside Hazmat's outer user/SBPL/network boundary.

## Ollama comparison

Ollama's `codex-app` integration is a configuration and launch integration, not a containment integration.

In `/Users/dr/workspace/ollama`:

- `cmd/launch/codex_app.go` configures the desktop app by writing a Codex profile into `~/.codex/config.toml`.
- It writes a model catalog at `~/.codex/ollama-launch-models.json`.
- It opens/restarts the app using LaunchServices (`open`, bundle id `com.openai.codex`) and AppleScript/TERM for quit/restart flows.
- It detects Codex App processes by matching `Codex.app/Contents/MacOS/Codex` and `Codex.app/Contents/Resources/codex app-server`.
- The docs describe model/profile integration, local server/browser features, review mode, and restore backups under `~/.ollama/backup/codex-app/`.

Ollama therefore proves that external tooling can configure and relaunch the desktop app by manipulating Codex config and LaunchServices. It does not prove the app can be safely contained, and it does not try to run Codex App under a different user or SBPL.

Relevant local files:

- `/Users/dr/workspace/ollama/cmd/launch/codex_app.go`
- `/Users/dr/workspace/ollama/cmd/launch/codex.go`
- `/Users/dr/workspace/ollama/docs/integrations/codex-app.mdx`
- `/Users/dr/workspace/ollama/docs/integrations/codex.mdx`

## Option analysis

| Option | Boundary | Feasibility | Security value | Recommendation |
|---|---|---:|---:|---|
| A. Launch stock `Codex.app` as `agent` under Hazmat SBPL | Full GUI process tree, if it launches correctly | Low to medium | High if correct | Keep as stretch spike, not v1 |
| B. Launch `codex app-server` under Hazmat | Backend process and all backend side effects | High | High | Best first implementation path |
| C. Host stock GUI attaches to Hazmat app-server or remote env | GUI remains host-side, execution backend contained | Unknown | Medium to high | Research next |
| D. Manage stock app `config.toml` only | Codex inner sandbox/approval defaults | High | Low to medium | Useful hardening, not Hazmat containment |
| E. Run full app in VM/container | Entire app and OS state | Medium to low | Very high | Heavy fallback, not near-term CLI-equivalent |

## Option A: full GUI under Hazmat

This would mean installing or locating Codex.app, then launching its GUI process as the `agent` user through `hazmat-launch` with a generated SBPL.

Expected blockers:

- **GUI login session**: Hazmat's native launcher is terminal-shaped. A normal macOS GUI app expects a WindowServer/Aqua login session, LaunchServices integration, and user-session services. The `agent` user is not logged into the desktop.
- **LaunchServices vs direct exec**: `open -b com.openai.codex` launches via host user session machinery. Directly execing `Contents/MacOS/Codex` through `hazmat-launch` preserves the outer sandbox but may bypass assumptions the Electron app and updater expect.
- **TCC**: The app has Apple Events, audio input, user-selected read/write, browser-use, and related privacy-sensitive surfaces. TCC grants are per user and app identity. Granting them to an `agent`-run GUI app is operationally awkward and can weaken the user-isolation story.
- **Keychain and auth**: The app uses the signed app's keychain access group and `~/Library/Application Support/Codex` state. Hazmat's current Codex credential model is file-backed `auth.json` materialized into `/Users/agent/.codex/auth.json`. Desktop app auth may not map cleanly to that model.
- **Electron/Chromium helper policy**: The app uses renderer/GPU/network helpers, JIT, unsigned executable memory, shared memory, Mach services, crashpad, local IPC sockets, and native addons. A working SBPL would need many non-CLI allowances.
- **Lifecycle**: Hazmat's session lifecycle expects a foreground command, PTY/status bar, exit cleanup, and credential harvest. GUI focus, relaunch, Sparkle update, crashpad, helper cleanup, stale app-server children, and app windows need new lifecycle code.
- **Formal scope**: setup/rollback, seatbelt policy, launch fd isolation, credential delivery, and session repair are all verified areas. A persistent app install, new GUI policy, or new auth state would start with TLA+ model/design work.

Verdict: keep `sandboxing-oulj` as the full-GUI stretch issue, but do not build this first. The chance of spending the first implementation cycle on macOS GUI plumbing rather than useful containment is too high.

## Option B: contained Codex app-server harness

This is the best first target.

Shape:

- Add a Hazmat launch mode that runs `codex app-server --listen stdio://` or a controlled Unix socket as `agent`.
- Reuse the existing Codex bootstrap where possible, because the bundled Codex CLI already provides `app-server`.
- Reuse existing Codex credential materialization/harvest.
- Reuse the existing native SBPL and network policy first; only add app-server-specific allowances if tests prove they are needed.
- Expose a Hazmat-owned client/proxy protocol that can start turns, stream events, and run app-server `command/exec`/thread APIs.
- Treat `process/spawn`, app-server `fs/*`, and `thread/shellCommand` as allowed only because the whole backend process is already contained by Hazmat. If a client needs finer policy, it must use `command/exec` and Codex permission profiles.
- Ensure any Unix socket transport is created inside a directory only the intended user/client can access. Do not expose app-server websocket transport on a network listener for this mode.

Security properties:

- Even app-server APIs that are unsandboxed relative to Codex are still sandboxed relative to the host by Hazmat's `agent` user, SBPL, and network policy.
- Codex's inner command sandbox can remain active for defense in depth, or use `externalSandbox` when Hazmat is intentionally the outer sandbox and the client model prompt should reflect that.
- Existing Hazmat credential lifecycle remains the source of truth.

Open questions:

- Which app-server transport is best for Hazmat: stdio for a managed session, or a Unix socket under an agent-owned runtime dir for a longer-lived backend?
- Whether `codex app-server` requires additional macOS TLS/Security allowances beyond the current Codex CLI policy.
- Whether the existing stock desktop app can connect to this contained backend directly, or whether Hazmat needs its own client/UI first.

## Option C: stock GUI as a thin client to contained execution

This is the most attractive user experience if the stock app supports it.

The upstream app-server protocol has `environment/add`, `remoteControl/*`, app-server daemon support, Unix socket transport, websocket transport, and remote execution concepts. That suggests a possible path where the desktop app remains host-side but the execution environment is a Hazmat-contained backend.

Unknowns to resolve:

- Can the current Codex desktop app attach to an externally launched app-server process, app-server daemon, or remote environment without also using its own host-side local app-server for filesystem/process side effects?
- Can the desktop app be configured to trust only that contained environment for shell, filesystem, browser-use, and app/tool execution?
- Does the app expose enough UI to select or enroll a remote environment, or is this currently mobile/daemon-only infrastructure?
- What local state remains host-readable by the GUI even if execution is remote?

Static probe result on 2026-05-28:

- The public app-server command supports stdio, Unix socket, and experimental websocket transports for custom clients. That does not by itself prove the stock desktop app has a setting for an already-running external app-server URL.
- The installed desktop bundle's standard local connection path resolves a Codex CLI executable, then spawns it with `app-server --analytics-default-enabled`. The resolver honors `CODEX_CLI_PATH`, and remote SSH host records have a `codex_cli_command` field. This is an app-server command substitution surface, not a direct socket/websocket attach surface.
- The same bundle has SSH and `remote-control` host kinds. SSH host records include `terminal_command`, `codex_cli_command`, and default workspaces. `remote-control` is account/device-key mediated, not a raw Hazmat app-server listener.
- No documented desktop setting was found for selecting an arbitrary pre-launched `unix://...` or `ws://...` app-server. The missing upstream capability, if that strict shape is required, is "desktop app selects an explicit external app-server endpoint and disables its local host-user sidecar."
- The practical candidate is to launch the desktop app with `CODEX_CLI_PATH` pointing at a Hazmat-owned shim, or to configure an SSH remote host with `codex_cli_command` pointing at Hazmat on the remote host. The desktop side would still use stdio, but the spawned command can route into `hazmat codex-app-server` under the existing outer containment.
- If the shim owns the stdio app-server process and that process runs as `agent` under Hazmat, app-server `command/exec`, `process/spawn`, `fs/*`, and `thread/shellCommand` should land on the contained backend because those APIs execute where the app-server runs. `browser-use` and computer-use need a separate proof because the host Electron UI remains local and tool runtime paths are configured through the app-server.
- Residual host-side surfaces remain: GUI auth and settings, keychain access, app support/cache/HTTPStorage/log/crashpad paths, deeplinks, LaunchServices, remote-control enrollment state, and the initial host-user spawn of the shim. The contained backend now uses an agent-owned session temp root rather than implicit broad `/private/tmp` access. This is backend containment, not full GUI containment.
- The live desktop attach proof was not run because it would require launching or reconfiguring the user's active Codex App. That work is tracked separately as an explicit opt-in smoke. The guarded harness is `scripts/check-codex-desktop-attach-smoke.sh`; it defaults to a host-state disclosure/dry run and requires explicit approval before launching the stock app.

Verdict: Option C is plausible through CLI command substitution, not through a proven arbitrary external app-server endpoint. Build the shim path first because it can be tested autonomously without touching the live desktop app; keep the live desktop proof opt-in.

## Option D: harden stock app config

Ollama's approach shows that configuring `~/.codex/config.toml` and relaunching the app is straightforward. Hazmat could add a host-side helper that writes a guarded profile:

```toml
sandbox_mode = "workspace-write"
approval_policy = "on-request"

[sandbox_workspace_write]
network_access = false

[features]
network_proxy = true

[apps._default]
destructive_enabled = false
open_world_enabled = false
```

This is not a substitute for Hazmat containment:

- The app still runs as the host user.
- The app's local app-server still has host-side `fs/*`, `process/spawn`, and `thread/shellCommand` surfaces.
- The app can still access its host App Support, keychain, HTTPStorage, caches, browser-use sockets, and TCC-granted capabilities.

Verdict: useful as a "make stock Codex safer" command, not as the answer to this research task.

## Option E: VM/container boundary

A macOS VM or a remote contained environment can isolate the whole desktop app and user session. This gives the cleanest story for GUI containment because WindowServer, TCC, keychain, app support, and browser state all live in the guest.

Cost:

- Heavy setup and runtime overhead.
- Separate app install/update/auth state.
- Not equivalent to the low-friction CLI harness.
- More likely a Tier 4 product direction than a near-term harness extension.

Verdict: viable if the product goal is "run arbitrary GUI agents safely", not the first Codex-specific step.

## TLA+ and governance impact

The repo's governance requires model-first changes for setup/init, rollback, seatbelt policy, credential delivery, session permission repair, launch fd isolation, and related verified areas.

Option B can likely stay close to existing verified shapes if it is implemented as another Codex harness launch mode:

- no new persistent setup step beyond existing Codex bootstrap
- no new credential store path if it uses `credentialHarnessCodexAuth`
- no new broad SBPL rules unless tests prove app-server needs them
- no new GUI/TCC state

Still, a durable implementation should check whether the harness lifecycle model needs a new app-server session state, especially if the backend is socket-based or long-lived.

Option A almost certainly requires model/design work before implementation:

- setup/rollback for app install or agent Library state
- seatbelt policy changes for GUI/Mach/TCC surfaces
- credential capability changes if app keychain/App Support auth is introduced
- launch fd isolation changes if LaunchServices or an alternate helper is used
- lifecycle semantics for a GUI process tree rather than a foreground terminal process

## Recommendation

Do not start by trying to make `/Applications/Codex.app` itself a first-class Hazmat-contained GUI harness. That path may be possible, but it has too many unrelated macOS GUI, TCC, keychain, and Electron lifecycle traps.

Start with a contained app-server backend:

1. Prototype `codex app-server` under Hazmat as `agent`, using stdio first.
2. Prove basic app-server requests work under the existing Codex SBPL: initialize, start a thread, run `command/exec`, exercise `fs/readFile` against allowed and denied paths, and confirm network policy behavior.
3. Prototype a `CODEX_CLI_PATH`/`codex_cli_command` shim that accepts the desktop app's `app-server` argv shape and routes stdio to the managed Hazmat backend.
4. Separately, with explicit human approval, test whether the stock Codex desktop app uses that contained backend without falling back to a host-user local app-server.
5. If stock app attachment works, design a broker that keeps execution in Hazmat while leaving UI on the host.
6. If attachment does not work, build or reuse a Hazmat-owned client before revisiting full GUI containment.

This preserves Hazmat's core security property: execution and filesystem side effects happen inside the outer Hazmat boundary, regardless of what the inner agent app-server does.

## Follow-up beads

- `sandboxing-oulj` already tracks the stretch "hazmat codex app" GUI containment idea. This research narrows its expected role: full GUI containment should come after app-server/remote-environment work, not before it.
- `sandboxing-txz6` prototypes a contained Codex app-server harness.
- `sandboxing-lsn2` probes whether the stock Codex desktop app can attach to a Hazmat-contained app-server or remote environment.
- `sandboxing-wsd1` classifies Codex App host-state paths before any future integration grants parent `Library` or `.codex` paths.
- `sandboxing-zz6k.5` prototypes the autonomous `CODEX_CLI_PATH`/`codex_cli_command` shim path without launching the desktop app.
- `sandboxing-zz6k.6` tracks the explicit opt-in live desktop attach smoke and its guarded launcher/proxy harness.
- `sandboxing-zz6k.8` modeled and narrowed native temp policy for the contained app-server path.

## Sources

Local:

- Hazmat repo: `/Users/dr/workspace/hazmat`
- OpenAI Codex clone: `/Users/dr/workspace/codex`, commit observed `46946bb91c`
- Ollama clone: `/Users/dr/workspace/ollama`, commit observed `f63eea3d`
- Installed app: `/Applications/Codex.app`, version `26.519.41501`

Official:

- OpenAI Codex sandboxing: https://developers.openai.com/codex/concepts/sandboxing
- OpenAI Codex app approvals/sandboxing: https://developers.openai.com/codex/app/features#approvals-and-sandboxing
- OpenAI Codex config reference: https://developers.openai.com/codex/config-reference#configtoml
- OpenAI Codex app-server: https://developers.openai.com/codex/app-server
- OpenAI Codex CLI reference: https://developers.openai.com/codex/cli/reference
