# Keychain Credential Boundary

Hazmat does not currently import, harvest, or broker arbitrary macOS Keychain
items as durable credential material. The credential registry now represents
that explicitly instead of implying that every Gemini auth path is covered by
`~/.hazmat/secrets`.

## Decision

Keychain-backed Gemini OAuth remains an adapter-required backend:

- registry ID: `harness.gemini.keychain-oauth`
- backend: `keychain`
- delivery: `external-reference`
- support status: `adapter-required`
- external authority: macOS Keychain item owned by Gemini CLI

The descriptor intentionally has no `StoreRelPath` and no `AgentPath`. Calling
the host secret-store or agent materialization accessors for it fails, so
Keychain material cannot be accidentally copied into `/Users/agent` through the
file-backed harness auth runtime.

## Supported Today

Hazmat supports these Gemini credential paths today:

- `GEMINI_API_KEY`, stored under
  `~/.hazmat/secrets/providers/gemini-api-key` and injected only into Gemini
  sessions
- file-backed Gemini OAuth fallback:
  `~/.hazmat/secrets/gemini/oauth_creds.json`
- file-backed Gemini account index:
  `~/.hazmat/secrets/gemini/google_accounts.json`

Those file-backed entries are session-scoped: they are materialized into
`/Users/agent/.gemini/` for a Gemini session, harvested on cleanup, and removed
from the agent home.

## Not Supported Yet

Hazmat cannot yet move a host Gemini Keychain login into the host-owned secret
store or broker it into a session. A future Keychain adapter would need to:

- query only registry-declared Keychain items
- preserve host-user ownership and authorization prompts
- expose credentials as a brokered/session-scoped capability, not a durable
  `/Users/agent` file
- model crash/recovery behavior separately from file-backed harvest

Until that exists, `hazmat config import gemini` skips host Keychain OAuth and
imports only file-backed Gemini OAuth when `~/.gemini/oauth_creds.json` exists.
`hazmat check` reports the Keychain boundary as an adapter-required credential
backend.
