# Codex App Host-State Path Classification

**Date**: 2026-05-28
**Scope**: Classify Codex App host-state paths from the UI sandboxing research before any future integration grants broad `Library`, cache, temp, socket, or `.codex` access.
**Method**: Static classification from installed-app surface, public Codex app-server docs, and Hazmat policy structure. This pass did not read host Codex state or launch the desktop app.

## Summary

Do not blindly add every Codex App path to `credentialDenySubs`.

`credentialDenySubs` emits runtime SBPL denies under the agent home and rejects integration grants that overlap those paths. That is correct for generic credential stores such as `.ssh`, `.aws`, and keychains. It is too blunt for several Codex paths because a future contained Codex desktop/app-server may legitimately need its own agent-owned `.codex` or `Library/Application Support/Codex` state while still needing to block reads of the host user's state.

The safer split is:

- Host Codex App state should be blocked by an explicit host-state import/grant deny list before it ever becomes part of a session.
- Temp sockets are capability endpoints and need path-policy treatment outside `credentialDenySubs`.
- Existing harness auth remains governed by Hazmat's Codex credential lifecycle, not by copying host `.codex` wholesale.

## Classification

| Path family | Data class | Decision | Notes |
|---|---|---|---|
| `~/.codex/auth.json` | Credential | Existing Codex credential lifecycle only | Hazmat stores this as `~/.hazmat/secrets/codex/auth.json`, materializes it to `/Users/agent/.codex/auth.json` during Codex sessions, harvests it on exit, and removes it. Do not grant host `.codex` as a substitute. |
| `~/.codex/sqlite/` | Private app/session state | Host-state deny, not immediate `credentialDenySubs` | Can contain conversation metadata, task state, local app state, and privacy-sensitive records. A blanket runtime deny under agent `.codex` could break contained Codex app-server state, so enforce this as a host-source grant/import denial unless a narrower tested exception exists. |
| `~/.codex/session_index.jsonl` and `~/.codex/sessions/` | Private transcripts and resumable session state | Existing narrow resume sync only | Hazmat already has targeted resume sync for Codex sessions. Do not grant or import the whole host `.codex` tree. |
| `~/Library/Application Support/Codex/` | Private app state; may include auth-adjacent data, crashpad, local DBs, and plugin/runtime state | Host-state deny, not immediate `credentialDenySubs` | This is high-risk host state. A future full GUI run as `agent` may need its own app support tree, so block host grants/copies rather than denying all agent-owned app support state globally. |
| `~/Library/HTTPStorages/com.openai.codex/` | Credential-bearing web storage | Host-state deny; candidate for stronger deny if a Library grant mechanism appears | Treat as equivalent to cookies/session storage. It should never be granted from the host user to a contained agent. |
| `~/Library/Caches/com.openai.codex/` | Private cache and possible response/body cache | Host-state deny by default | Not necessarily credential-bearing, but can hold private prompts, responses, metadata, and URLs. Safe cache recreation is preferable to host sharing. |
| `~/Library/Preferences/com.openai.codex.plist` | Host app preferences and feature/config state | Host-state deny by default | Not a credential store, but can carry account, feature, remote-connection, or local-path choices. Future opt-in desktop probes should use reversible launcher/env state rather than mutating this file autonomously. |
| `~/Library/Logs/com.openai.codex/` | Private logs and diagnostics | Host-state deny by default | Logs can contain prompts, file paths, errors, request IDs, and stack traces. |
| `/tmp/codex-browser-use/*.sock` | Local capability socket | Temp/socket deny, not `credentialDenySubs` | A socket grants control over a browser-use runtime. This is outside home-relative credential denies and should be handled by temp/socket policy. |
| `/tmp/codex-ipc/*.sock` and `/var/folders/.../T/codex-ipc/*.sock` | Local capability/control socket | Temp/socket deny, not `credentialDenySubs` | App-server control sockets are capabilities. The policy question belongs with `/private/tmp` and per-session temp narrowing. |

## Recommended Follow-Up

1. Add a host-state grant/import deny mechanism for Codex App state paths. This should reject future integration manifests, asset sync rules, or app-specific setup that tries to grant host Codex App `Library`, HTTP storage, cache, preferences, logs, or broad `.codex` state.
2. Keep Codex auth on the existing credential lifecycle path. Do not copy host `.codex` wholesale to solve desktop/app-server auth.
3. Treat Codex App temp sockets as part of the contained app-server `/private/tmp` exposure review. These paths are capability endpoints and do not fit the home-relative `credentialDenySubs` model.

Implementation note: `sandboxing-zz6k.7` added `hostStateDenySubs` as a preflight host-source deny list for session roots, integration `read_dirs`, and harness asset sync. It intentionally does not emit runtime SBPL denies.

## Related Beads

- `sandboxing-wsd1`: this classification.
- `sandboxing-8tj4`: `/private/tmp` exposure for contained Codex app-server; should include Codex App temp/control sockets.
- `sandboxing-zz6k.6`: explicit opt-in live desktop attach smoke, where host-state observation or mutation must be listed before running.
