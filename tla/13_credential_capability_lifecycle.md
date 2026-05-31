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

1. only file-backed credentials may be materialized under `/Users/agent`
2. env credentials must only appear in explicit session env grants
3. brokered credentials must only appear as broker grants
4. external-reference credentials must not be silently copied into the file
   store
5. adapter-required credentials, such as Gemini Keychain OAuth today, must not
   be delivered at all until a backend adapter exists
6. crash/restart must clear session-only grants while preserving and recovering
   file-backed residue
7. logged-out or empty baseline runtime auth files must not overwrite
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
| `NoCrossHarnessExposure` | During an active session, exposed credentials must list the active harness as a consumer. Global credentials model that by listing all harnesses. |
| `NoSessionExposureOutsideActivePhase` | Env, broker, and external grants are cleared outside active session phases, including after crash. |
| `LaunchOnlyAfterRecovery` | Sessions cannot deliver credentials until file-backed residue recovery is complete. |
| `CleanRecoveredStateHasNoAgentResidue` | A recovered idle state has no modeled credential file left under `/Users/agent`. |
| `LatestValueNeverSilentlyLost` | Managed host-store values known as latest remain in host storage, agent residue, or conflict archive. |
| `CleanRecoveredStateKeepsLatestHostOwned` | After recovery, latest managed values are host-owned: primary store or conflict archive. |
| `IdleClearsSessionState` | Idle state has no active harness, no active grants, and no stale harvest baseline. |

`RegistryWellFormed` is a constant-level assumption over the model bounds: file
delivery implies managed host-secret-store support, and adapter-required support
implies non-host external-reference delivery.

## Model Bounds

Default config:

- `Harnesses = {claude, codex, opencode, gemini, hermes}`
- `Credentials = {claude_file, opencode_file, anthropic_api, openai_api, gemini_api, openrouter_api, git_https, gemini_keychain}`
- `Values = {v1, v2}`

The credential set intentionally includes one representative for each important
delivery/backend class:

- materialized file: Claude and OpenCode auth files as representative
  single-consumer harness auth surfaces
- env: Anthropic, OpenAI, Gemini, and OpenRouter provider API keys
- broker: Git HTTPS credential helper
- adapter-required external backend: Gemini Keychain OAuth

Provider env credentials are intentionally multi-consumer where the registry
allows transparent key reuse:

- `anthropic_api` is consumed by Claude and Hermes
- `openai_api` is consumed by Codex and Hermes
- `gemini_api` is consumed by Gemini and Hermes
- `openrouter_api` is consumed by Hermes only

File-backed harness auth remains single-consumer in the model. Codex and
file-backed Gemini auth use the same implementation class as the two modeled
file credentials; they are not enumerated separately so the maintained TLC
suite stays tractable.

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

Observed TLC result for the promoted model:

- `Model checking completed. No error has been found.`
- `25,623,297 states generated`
- `6,963,327 distinct states found`
- `depth 32`
- runtime about 85 minutes on the standalone local 10-worker run

## Scope Boundary

This proof is registry-level. It does **not** model:

- exact concrete file paths or filesystem permissions
- exact JSON/file payload merge semantics
- real Keychain APIs or authorization prompts
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
4. Any future path that creates durable `/Users/agent` credential material must
   be modeled as file delivery and preserve the recovery invariants.
5. Git HTTPS, cloud backup, SSH identity, and integration/env credential work
   should use this model as the lifecycle contract before adding concrete
   backend-specific behavior.
6. Adding a harness that consumes existing provider API keys requires updating
   the consumer sets here before implementation.
