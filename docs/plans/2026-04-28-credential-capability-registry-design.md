# Credential Capability Registry Design

Hazmat credential handling should be registry-driven. A credential surface is not
just a path; it is a typed capability with an owner, storage backend, delivery
mode, legacy residue, and redaction policy.

## Registry Shape

The source of truth is `credentialDescriptor` in
`hazmat/credential_registry.go`. Each descriptor has:

- a stable typed `credentialID`
- a `credentialKind` such as provider API key, harness auth, Git HTTPS, Git SSH,
  cloud backup, GitHub token, integration env, or external auth
- a storage backend such as host secret store, broker, Keychain, or external file
- an explicit delivery mode: none, env, materialized file, brokered helper, or
  external reference
- optional harness scoping, env var name, agent materialization path, legacy
  paths, and conflict-archive behavior
- a redaction flag for diagnostics and future check output

Host-owned file material uses a relative `StoreRelPath` under
`~/.hazmat/secrets`. Registry validation rejects absolute paths, empty
components, and `..` components before joining with the host secret root.

## Delivery Invariant

Callers should not construct durable credential paths directly. They should ask
the descriptor for the operation they are performing:

- env delivery calls `EnvDeliveryVar()`
- session file delivery calls `AgentMaterializationPath()`
- host storage calls `StorePathForHome(home)`

Those accessors reject the wrong delivery mode. This gives later refactors,
checks, and formal models a single place to define which operations are legal for
each credential kind.

Materialized file delivery is session-scoped. The existing harness auth runtime
still copies data into `/Users/agent` only for the matching harness session,
harvests it back into the host store, removes the session copy, and archives
conflicts when host and session state diverge.

## Current Coverage

The initial registry covers the credential surfaces already supported by the
secret-store branch:

- `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and `GEMINI_API_KEY`, stored under
  `~/.hazmat/secrets/providers/*` and injected as env only into the matching
  harness session
- Claude credential and state auth files
- Codex auth file
- OpenCode auth file
- Gemini OAuth and account index files

Provider path lookup and harness auth materialization now use registry
descriptors rather than duplicating path constants.

## Remaining Backend Decisions

Some existing features should not be modeled as simple imported files:

- Git HTTPS credentials should become a host-owned entry exposed through a
  brokered git credential helper.
- Git SSH provisioned private keys should become secret capabilities; external
  private-key paths should remain explicit external references after validation.
- Cloud backup secret material should move out of mixed config and legacy
  `~/.hazmat/cloud-credentials` into typed host-store entries.
- GitHub tokens should share the same capability shape once the GitHub
  capability work lands.
- Integration-provided credential env vars should become explicit credential
  grants, not inherited ambient environment.
- Keychain-backed auth is represented as a non-file backend. Gemini Keychain
  OAuth is currently registered as `adapter-required`, so Hazmat does not claim
  to import, harvest, or materialize it.

## Verification Path

The registry is intended to drive three follow-on controls:

- a generalized TLA+ model with registry membership, delivery modes, session
  scope, crash/restart, and conflict recovery
- `hazmat check` inventory and migration diagnostics that print only redacted
  IDs and backend status
- local and CI gates that reject new ad hoc durable writes to credential-shaped
  `/Users/agent` paths or host secret-store paths outside the registry API
