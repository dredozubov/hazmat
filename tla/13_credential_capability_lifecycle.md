# Problem 13 — Credential Capability Lifecycle

**Status:** proved registry-level lifecycle model for `sandboxing-03wd`.
This spec generalizes the file-backed secret-store recovery model to cover the
credential registry shape used by provider API keys, harness auth files,
brokered credentials, and non-file external backends.

## Problem Statement

Hazmat now treats credentials as typed capabilities rather than ad hoc paths.
Each credential has a registry entry with a storage backend, support status,
delivery mode, and explicit consumer harness set. That creates a broader correctness
problem than file recovery alone:

1. only file-backed credentials may be materialized under the persistent agent
   home or an explicit session-local HOME credential target
2. env credentials must only appear in explicit session env grants
3. brokered credentials must only appear as broker grants
4. external-reference credentials must not be silently copied into the file
   store
5. syncable Keychain credentials may use the host-owned store as the neutral
   exchange point, but host-user and agent-user Keychain caches must reconcile
   before launch and after harvest
6. no-delivery credentials model contained-only profile state and must never be
   exposed by the credential sync machinery
7. adapter-required credentials, such as Gemini Keychain OAuth today, must not
   be delivered at all until a backend adapter exists
8. crash/restart must clear session-only grants while preserving and recovering
   file-backed residue
9. logged-out or empty baseline runtime auth files must not overwrite
   host-owned file-backed credentials during harvest

The model checks those registry-level constraints independently of any one
credential implementation.

## Governed Boundary

This model governs the intended lifecycle rules for the credential registry in:

- `hazmat/credential_registry.go`
- `hazmat/harness_auth_runtime.go`
- future Git HTTPS broker, cloud credential, SSH identity, and integration/env
  credential work that consumes registry descriptors

The older `MC_SecretStoreRecovery` model remains the more detailed proof for
file-backed harness auth conflict preservation. This model is the broader
capability-safety contract: which delivery modes may expose which credential
types, and when.

The one-shot `hazmat migrate credentials` command is intentionally not part of
`MC_Migration`, which models init-version transitions. Credential repair is
user-data reconciliation: it must preserve managed host-store values, remove
agent-home residue where possible, and keep external or adapter-required
boundaries explicit. Those properties are governed here and by
`MC_SecretStoreRecovery`; concrete parsing and path coverage live in Go tests.

## What the TLA+ Model Checks

| Invariant | Meaning |
|-----------|---------|
| `NonHostBackendsHaveNoHostStore` | Keychain, broker, and external-file backends do not acquire host secret-store values or agent file residue in this model. |
| `DeliveryMatchesRegistry` | Session exposure must match the registry delivery mode: file, env, broker, or external reference. |
| `AdapterRequiredNeverExposed` | Adapter-required credentials are never active, delivered, materialized, env-granted, broker-granted, or externally granted. |
| `NoDeliveryNeverExposed` | Contained-only/no-delivery profile state cannot be selected as an active credential or exposed through file/env/broker/external grants. |
| `NoCrossHarnessExposure` | During an active session, exposed credentials must list the active harness as a consumer. Global credentials model that by listing all harnesses. |
| `NoSessionExposureOutsideActivePhase` | Env, broker, and external grants are cleared outside active session phases, including after crash. |
| `LaunchOnlyAfterRecovery` | Sessions cannot deliver credentials until file-backed/keychain residue recovery is complete and host-user Keychain caches are reconciled. |
| `CleanRecoveredStateHasNoCredentialResidue` | A recovered idle state has no modeled credential file left under either the persistent agent home or a session-local HOME target, and no agent-keychain residue. |
| `LatestValueNeverSilentlyLost` | Managed store values known as latest remain in Hazmat storage, a host-user Keychain cache, agent residue, or conflict archive. |
| `CleanRecoveredStateKeepsLatestHostOwned` | After recovery, latest managed values are host-owned: Hazmat primary store, host-user Keychain cache, or conflict archive. |
| `IdleClearsSessionState` | Idle state has no active harness, no active grants, and no stale harvest baseline. |

`RegistryWellFormed` is a constant-level assumption over the model bounds: file
delivery implies managed host-secret-store support, and adapter-required support
implies non-host external-reference delivery.

## Model Bounds

Default config:

- `Harnesses = {claude, codex, opencode, gemini, hermes}`
- `Credentials = {claude_file, opencode_file, claude_keychain, anthropic_api, git_https, gemini_keychain, hermes_profile}`
- `Values = {v1, v2}`

The credential set intentionally includes one representative for each important
delivery/backend class:

- materialized file: Claude and OpenCode auth files as representative
  single-consumer harness auth surfaces
- env: Anthropic provider API key as the representative static env credential
- broker: Git HTTPS credential helper
- syncable Keychain reference: Claude Keychain OAuth, with Hazmat store as the
  neutral exchange point between host-user and agent-user keychains
- adapter-required external backend: Gemini Keychain OAuth
- contained-only/no-delivery profile state: Hermes project profile state as the
  representative for Qwen/Cursor/Pi/Hermes-style broad profile homes that must
  not be synced as credentials

The provider env representative is intentionally multi-consumer where the
registry allows transparent key reuse:

- `anthropic_api` is consumed by Claude and Hermes
- `claude_keychain` is consumed by Claude only

File-backed harness auth remains single-consumer in the model. Syncable
Keychain auth is also single-consumer here, but unlike file auth it has two
external caches: the host-user Keychain and the agent-user Keychain. The host
Keychain cache may change while the user runs the plain host CLI; Hazmat launch
is blocked until that value is synchronized into the neutral store or the store
is published back to the host Keychain when the store is known latest. The
runtime target is a model constant rather than session state. The maintained config sets
`RuntimeCredentialTarget = session_home` to prove the new validation-activation
target class; the pre-existing persistent-agent-home behavior is the same
transition relation with `RuntimeCredentialTarget = persistent_agent_home`, and
was the previously promoted proof shape. Codex and file-backed Gemini auth use
the same implementation class as the two modeled file credentials; OpenAI,
Gemini, and OpenRouter provider env keys use the same implementation class as
the Anthropic provider representative. They are not enumerated separately so the
maintained TLC suite stays tractable.

Two file-backed credentials are enough to check cross-harness exposure. Two
secret values are enough to witness stale residue, refresh, conflict archive,
host-store update cases, and non-harvestable logged-out baseline runtime auth.

## How to Run

```bash
cd tla
bash run_tlc.sh -workers auto -config MC_CredentialCapabilityLifecycle.cfg MC_CredentialCapabilityLifecycle.tla
```

This spec is also part of the maintained local suite:

```bash
cd tla
bash check_suite.sh
```

Observed TLC result for the promoted seamless-sync model:

- `Model checking completed. No error has been found.`
- `21,747,980 states generated`
- `5,182,905 distinct states found`
- `depth 40`
- runtime 26m56s on the standalone local 10-worker run

### Antigravity keychain isolation correction

Antigravity's OAuth credential is adapter-required rather than an external
reference. Seatbelt can authorize a keychain database, but cannot restrict a
session to one item inside the shared agent login keychain. Treating the shared
database as Antigravity's external credential would therefore expose credentials
owned by other harnesses. The credential must remain inert until Hazmat provides
a per-harness keychain or an item-scoped broker.

## Scope Boundary

This proof is registry-level. It does **not** model:

- exact concrete file paths or filesystem permissions
- exact JSON/file payload merge semantics
- real Keychain APIs or authorization prompts
- OAuth provider refresh timing or token validity checks
- concrete git credential-helper protocol bytes
- cloud provider API behavior
- SSH agent socket behavior
- integration manifest parsing or project approval UX
- concurrent writes to the same host secret while a session is active

Those details remain governed by implementation tests, docs, and narrower
future specs where the state machine warrants it.

## Change Rules

1. Adding a new credential delivery mode or support status requires updating
   this model before implementation.
2. Adding a new credential backend that can expose secret material must be
   represented in the model as host-store, broker, external-reference, or a new
   explicit class.
3. Adapter-required credentials must remain undeliverable until an adapter is
   modeled and proved.
4. Adding a syncable Keychain adapter requires modeling the host-user cache,
   agent-user cache, neutral store, conflict policy, and cleanup/recovery path.
5. No-delivery contained profile state must not become eligible for sync without
   a narrower registered credential surface and updated proof.
6. Any future path that creates durable `/Users/agent` credential material must
   be modeled as file delivery and preserve the recovery invariants.
7. Changing whether a managed file credential materializes to the persistent
   agent home, a session-local HOME, or another runtime target requires updating
   the target abstraction here before implementation.
8. Git HTTPS, cloud backup, SSH identity, and integration/env credential work
   should use this model as the lifecycle contract before adding concrete
   backend-specific behavior.
9. Adding a harness that consumes existing provider API keys requires updating
   the consumer sets here before implementation.
