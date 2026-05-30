# Shared Provider Credential Registry Design

Status: Proposed
Date: 2026-05-30
Related:
- `docs/plans/2026-04-28-credential-capability-registry-design.md`
- `docs/plans/2026-05-30-hermes-harness-design.md`
- `tla/MC_CredentialCapabilityLifecycle.tla`
- `hazmat/credential_registry.go`
- `hazmat/config_agent.go`
- `hazmat/secret_store.go`

## Position

Hazmat should make provider API-key credentials **provider-owned and
multi-consumer**, while keeping harness auth files **single-harness**.

The motivating Hermes case is simple: a user who already configured
`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, or
`OPENROUTER_API_KEY` in Hazmat should be able to launch Hermes and have Hermes
receive the same env var transparently when Hermes is an allowed consumer. The
user should not need to learn Hazmat's internal credential registry collision or
choose a different provider key only because it is easier for Hazmat to model.

The current registry cannot express that cleanly. `credentialDescriptor.Harness`
is a single `HarnessID`; `providerCredentialDescriptorForEnvVar` looks up by env
var only; provider secret-store paths are derived through that env lookup; and
`sessionCredentialEnvGrant` attribution carries credential ID and env var, not
the consuming harness. Adding a second Hermes-scoped `OPENAI_API_KEY`
descriptor would therefore be ambiguous, while forcing Hermes to use a
non-colliding key would leak Hazmat implementation details into user workflow.

The fix belongs in Hazmat's credential architecture. Hermes does not need a
special env var for Hazmat. Hazmat needs a credential model where one provider
credential can be delivered to multiple explicitly allowed harnesses without
becoming globally available.

## Goals

- Keep the user model transparent: provider keys are configured once in Hazmat
  and delivered to every explicitly allowed harness that can use them.
- Preserve the security invariant: a credential is never exposed to an
  unlisted harness.
- Keep provider key storage stable under `~/.hazmat/secrets/providers/*`.
- Avoid duplicate stored files for the same env var.
- Keep materialized harness auth files single-harness.
- Keep env delivery session-scoped: provider keys appear only in the launched
  harness process environment.
- Make explain/metadata attribution precise enough to show which harness
  consumed which provider credential.
- Update TLA+ before Go changes.

## Non-Goals

- No bulk import of provider keys from `~/.hermes`, `~/.codex`, `~/.claude`, or
  other harness config trees.
- No ambient passthrough of invoking-shell provider keys.
- No global "all harnesses get all provider keys" policy.
- No materialization of provider keys into `/Users/agent`.
- No attempt to model Hermes OAuth, Nous Portal device-code state, MCP OAuth,
  messaging tokens, SSH keys, cloud keys, or tool-home secrets in this slice.
- No change to the GitHub token or Git SSH/Git HTTPS capability designs except
  where tests need to prove they remain separate.

## Current State

Provider API keys are modeled as harness-scoped descriptors:

- Anthropic key: `ANTHROPIC_API_KEY`, `HarnessClaude`
- OpenAI key: `OPENAI_API_KEY`, `HarnessCodex`
- Gemini key: `GEMINI_API_KEY`, `HarnessGemini`

That shape worked when each provider key had one first-class consumer. Hermes
breaks the assumption because it is a provider-flexible assistant that can use
several existing provider env vars. The single-harness field also creates a
bad UX incentive: pick the one env var that does not collide with existing
Hazmat descriptors, even when that is not the user's preferred provider.

The TLA+ model mirrors the one-scope assumption. `MC_CredentialCapabilityLifecycle`
partitions credentials into `ClaudeScopedCreds`, `CodexScopedCreds`,
`GeminiScopedCreds`, and `GlobalCreds`; `CredentialHarness(c)` returns one
harness or `NoHarness`; and `NoCrossHarnessExposure` checks exposed credentials
against that one value.

## Recommended Model

Replace single harness ownership with an explicit consumer set for env-delivered
provider credentials.

Conceptually:

```go
type credentialDescriptor struct {
    ID              credentialID
    DisplayName     string
    Kind            credentialKind
    Backend         credentialStorageBackend
    Delivery        credentialDeliveryMode
    Support         credentialSupportStatus
    StoreRelPath    string

    // For legacy single-consumer file auth and external auth.
    Harness HarnessID

    // For shared provider env credentials.
    ConsumerHarnesses []HarnessID

    EnvVar          string
    AgentPath       string
    ExternalRef     string
    LegacyPaths     []string
    Redacted        bool
    ConflictArchive bool
}
```

The implementation can either keep `Harness` for single-consumer credentials
and add `ConsumerHarnesses` for shared env credentials, or replace both with one
`AllowedHarnesses []HarnessID` field. The important invariant is semantic, not
field name:

- file-backed harness auth credentials have exactly one consuming harness
- provider env credentials have one or more consuming harnesses
- global brokered credentials remain global only when the descriptor says so
- adapter-required credentials are never deliverable

Recommended v1 provider consumers:

| Credential | Env var | Store path | Allowed consumers |
|---|---|---|---|
| Anthropic API key | `ANTHROPIC_API_KEY` | `providers/anthropic-api-key` | Claude, Hermes |
| OpenAI API key | `OPENAI_API_KEY` | `providers/openai-api-key` | Codex, Hermes |
| Gemini API key | `GEMINI_API_KEY` | `providers/gemini-api-key` | Gemini, Hermes |
| OpenRouter API key | `OPENROUTER_API_KEY` | `providers/openrouter-api-key` | Hermes |

OpenRouter starts with Hermes because no existing Hazmat harness currently
needs it. Future harnesses can be added as explicit consumers.

## Registry API

The registry should stop answering provider-key questions by env var alone.

Replace:

```go
providerCredentialDescriptorForEnvVar(envVar string)
providerSecretStorePathForHome(home, envVar string)
```

with harness-aware or descriptor-aware calls:

```go
providerCredentialDescriptorForEnvVarAndHarness(envVar string, h HarnessID)
providerCredentialDescriptorsForHarness(h HarnessID) []credentialDescriptor
providerSecretStorePathForDescriptor(home string, id credentialID)
```

The env-var-only helper can remain only for code paths that prove uniqueness, or
it can be removed to avoid accidental ambiguity. Tests should reject duplicate
provider env vars unless the registry uses descriptor ID or harness-aware lookup
for every call site.

Descriptor accessors should grow one predicate:

```go
func (d credentialDescriptor) CanDeliverTo(h HarnessID) bool
```

For provider env credentials, `CanDeliverTo` checks `ConsumerHarnesses`. For
single-harness credentials, it checks `Harness`. For global brokered
credentials, it returns true for any active harness only if the credential kind
and delivery mode are explicitly global.

## Config UX

`hazmat config agent` should become provider-centric for API keys.

Current wording says it prompts for API keys "for installed harnesses." That
creates duplicates once one provider key can feed more than one harness. New
wording should say:

> Configure provider API keys and agent Git identity.

Prompt examples:

- `OpenAI API key (used by Codex and Hermes when installed)`
- `Anthropic API key (used by Claude Code and Hermes when installed)`
- `Gemini API key (used by Gemini and Hermes when installed)`
- `OpenRouter API key (used by Hermes when installed)`

Prompt eligibility should come from installed harnesses plus descriptor
consumers:

1. collect installed managed harnesses
2. find provider env descriptors whose consumer set intersects installed
   harnesses
3. prompt once per descriptor
4. store at the descriptor's host-store path

If no harness is installed, the existing "show a discovery prompt anyway" UX can
remain, but it should use provider language rather than "Claude prompt." A
reasonable first prompt is Anthropic or OpenAI, but that is a UX choice and
should not affect registry invariants.

## Session Delivery

`applyHarnessAPIKeyEnvForSession` should become multi-key and consumer-aware.

Recommended shape:

```go
func applyProviderAPIKeyEnvForSession(cfg *sessionConfig, planOnly bool) error {
    for _, descriptor := range providerCredentialDescriptorsForHarness(cfg.HarnessID) {
        value, source, err := lookupConfiguredProviderAPIKey(descriptor)
        ...
        cfg.HarnessEnv[descriptor.EnvVar] = value
        cfg.CredentialEnvGrants = append(...{
            EnvVar: descriptor.EnvVar,
            CredentialID: descriptor.ID,
            Harness: cfg.HarnessID,
            Source: source,
        })
    }
}
```

Hermes may receive multiple provider env vars when the user has configured
multiple keys. That is acceptable because Hermes already resolves providers from
its own config and environment. Hazmat's job is not to pick the provider; it is
to deliver the configured provider credentials that the active harness is
allowed to see.

The function should not pass every known provider key to every harness. It
should pass only descriptors where `CanDeliverTo(cfg.HarnessID)` is true.

## Storage And Migration

Existing provider store paths should stay stable:

- `providers/anthropic-api-key`
- `providers/openai-api-key`
- `providers/gemini-api-key`

Add:

- `providers/openrouter-api-key`

No migration is needed for existing keys if descriptor IDs and store paths are
preserved. The migration work is semantic: existing OpenAI/Anthropic/Gemini
host-store entries gain additional allowed consumers.

Legacy agent `.zshrc` migration should become descriptor-driven. Today it uses
env var lookup, which becomes ambiguous for duplicated env vars. The new path
should look up the descriptor selected by the prompt or by the active harness,
then migrate only that env var to that descriptor's store path. Because shared
provider descriptors have one store path per env var, no duplicate migration is
needed for shared consumers.

## TLA+ Design

`MC_CredentialCapabilityLifecycle` should change from a single harness function
to an allowed-consumer relation.

Replace the modeled shape:

```tla
CredentialHarness(c) \in Harnesses \cup {NoHarness}
EligibleCreds(h) == {c : CredentialHarness(c) \in {h, NoHarness}}
```

with:

```tla
CredentialConsumers(c) \subseteq Harnesses
GlobalCreds \subseteq Credentials
EligibleCreds(h) ==
    {c \in Credentials :
        /\ CredentialSupport(c) # AdapterRequiredSupport
        /\ (c \in GlobalCreds \/ h \in CredentialConsumers(c))}
```

The cardinality assumption should change from "each credential is in exactly
one harness-scope set or global" to "each non-global credential has a non-empty
consumer set." This preserves precision while allowing shared provider env
credentials.

The key invariant becomes:

```tla
NoCrossHarnessExposure ==
    phase \in ActivePhases =>
        \A c \in ExposedCreds :
            c \in GlobalCreds \/ activeHarness \in CredentialConsumers(c)
```

The model config should include at least:

- `OPENAI_API_KEY` consumed by Codex and Hermes
- `ANTHROPIC_API_KEY` consumed by Claude and Hermes
- `GEMINI_API_KEY` consumed by Gemini and Hermes
- `OPENROUTER_API_KEY` consumed by Hermes only
- one file-backed credential consumed by one harness
- one brokered global credential
- one adapter-required credential that is never deliverable

This model change should land before Go code. It should also reconcile current
model drift: `MC_CredentialCapabilityLifecycle` should include the harnesses it
needs to reason about, including Hermes once added, and should not accidentally
drop existing harnesses with credential surfaces.

## Tests

Registry tests:

- provider descriptors can share an env var only when lookup is descriptor-ID
  or harness-aware
- `OPENAI_API_KEY` has one store path and multiple allowed consumers
- `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, and `OPENROUTER_API_KEY` have the
  expected consumer sets
- file-backed harness auth credentials still have exactly one consumer
- adapter-required credentials cannot be env-delivered

Config-agent tests:

- prompt once for OpenAI when Codex and Hermes are both installed
- prompt labels list all installed consumers
- storing a shared key writes one host-store file
- legacy `.zshrc` migration remains one env var to one provider store path

Session tests:

- Codex receives `OPENAI_API_KEY`
- Hermes receives `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, and
  `OPENROUTER_API_KEY` when configured and allowed
- Claude does not receive `OPENAI_API_KEY` unless explicitly allowed
- OpenCode does not receive provider env keys unless explicitly allowed
- explain JSON and session metadata attribute the grant to both credential ID
  and consuming harness

TLA+ tests:

- `tla/check_suite.sh` passes
- `NoCrossHarnessExposure` fails if a provider key is exposed to an unlisted
  harness
- `DeliveryMatchesRegistry` still rejects env/file/broker delivery mismatches

## Rollout

1. Land this design and beads.
2. Update `MC_CredentialCapabilityLifecycle` and run TLC.
3. Refactor Go registry schema and tests without changing runtime behavior.
4. Refactor config-agent provider prompts.
5. Refactor session env delivery.
6. Add OpenRouter provider descriptor.
7. Wire Hermes consumers after `HarnessHermes` exists.
8. Update explain/session metadata and docs.

The registry work can land before Hermes itself. That is preferable: it removes
the credential collision from the Hermes harness implementation and improves the
architecture for any future provider-flexible harness.
