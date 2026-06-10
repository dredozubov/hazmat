# Beadpost Attestation Boundary — Three-Part TLA+ Spec Plan

Date: 2026-06-09

Status: draft spec plan (the `.2` deliverable for epic `sandboxing-x74u`). No Go
changes. This is the spec-first artifact: it scopes three coordinated TLA+ updates
that must each pass TLC before any broker implementation begins.

Model under verification: **contained-agent submitter + dr-owned host broker**.
A contained agent submits *closed request payloads* (request content only, never
authority fields) to a per-session, dr-owned broker socket. The broker is created
only after confirmed `sandbox_init()`, derives project/session/tier from launch
facts, never trusts agent-supplied authority fields, holds the Beadpost HMAC
key/registry/ledger host-side, and invokes Beadpost delivery/review itself. No
agent-readable attestation token and no attestation key path exists in the agent
context.

The update is split into **three specs, not one overloaded model**, because each
governs a distinct boundary with its own state space:

| Part | Spec | Boundary it proves |
|------|------|--------------------|
| 1 | `MC_SeatbeltPolicy` (extend) | the signing key is unreadable/unwritable by the contained agent |
| 2 | `MC_LaunchFDIsolation` (extend, minimal) | broker/mint happen only *after* confirmed `sandbox_init`; the key never rides an agent fd |
| 3 | `MC_BeadpostBrokerBoundary` (new) | the request membrane: authority derived from launch facts, never from the agent |

---

## Part 1 — `MC_SeatbeltPolicy`: attestation-key non-readability

**Goal.** Model the dr-owned signing key as a host-authority deny target proved
unreadable AND unwritable by the contained agent, with **no carve-out** (unlike the
narrowly-scoped Claude OAuth keychain exception).

**Additions** (real symbols, approx. line refs from current spec):

- New path constants alongside the existing identifiers (~line 76):
  `attestationKeyDir`, `attestationKeyFile`.
- New set constants `HostAuthorityPaths` and `HostAuthorityTargets` (~line 51),
  with `ASSUME HostAuthorityPaths \subseteq Paths` and
  `ASSUME HostAuthorityTargets \subseteq Paths`. Keep them separate from
  `CredPaths` / `CredentialTargets` so keychain-exception semantics can never
  apply to broker authority material by accident.
- Extend `Contains(child, parent)` (~line 92–112) with
  `\/ (child = attestationKeyFile /\ parent = attestationKeyDir)`.
- `EmitCredDenies` (section 8, ~line 276) should deny read+write for
  `CredPaths \cup HostAuthorityPaths`. **Do not add any section-9 exception** for
  the key (contrast `AgentKeychainExceptionScoped`, ~line 381).

**New invariants:**

```
AttestationKeyReadDenied ==
    section = 10 =>
        \A target \in HostAuthorityTargets : EffectiveRead(target) = "deny_read"

AttestationKeyWriteDenied ==
    section = 10 =>
        \A target \in HostAuthorityTargets : EffectiveWrite(target) = "deny_write"
```

**.cfg additions:** bind `attestationKeyDir`/`attestationKeyFile`; extend
`Paths`, set `HostAuthorityPaths = {attestationKeyDir}` and
`HostAuthorityTargets = {attestationKeyDir, attestationKeyFile}`; add the two
invariants. Do not add either key path to `CredPaths`, `CredentialTargets`, or
`AgentKeychainExceptionPaths`.

> **CORRECTION to the grounded sketch (load-bearing).** The sketch added the key to
> the deny sets but left `ProjectChoices`/`ReadChoices` unchanged. Then
> `AttestationKeyReadDenied` holds *vacuously* — the model never explores the case
> the requirement is about. **You must add `attestationKeyDir` to both
> `ProjectChoices` and `ReadChoices`** so TLC actually exercises "key dir chosen as
> ProjectDir / included in ReadDirs" and proves section-8 deny still dominates the
> section-2 project/read grant (last-match-wins). Without this, the invariant proves
> nothing.

**Registration:** add both invariants to VERIFIED.md §2 key-invariants and two
`proof_ownership.tsv` rows under `tla/02_seatbelt_policy_structure.md`.

**Risk:** modest state growth (two paths + one set). Confirm via TLC.

---

## Part 2 — `MC_LaunchFDIsolation`: confirmed-ordering only (keep it minimal)

**Goal.** Add *only* the launch-order fact that the broker/mint happen after
containment is confirmed, and that the key never reaches the agent fd table. Do
**not** model request routing here — that is Part 3.

Order to enforce: `sandbox_init` succeeds → helper emits confirmed-containment
metadata → host may open broker / mint authority → agent execs.

**Additions:** new booleans `metadataEmitted`, `brokerActive`, `tokenMinted`, plus
an `authority` fd-target class. New actions:

- `HelperEmitsConfirmedContainmentMetadata` — enabled only at `stage = "sandboxed"`;
  sets `metadataEmitted' = TRUE`.
- `HostBrokerActivates` — requires `metadataEmitted`; sets `brokerActive' = TRUE`.
- `HostMintsToken` — requires `brokerActive`; sets `tokenMinted' = TRUE`.

**Invariants:**

```
BrokerStartsOnlyAfterSandboxConfirmed ==
    brokerActive => (stage \in {"sandboxed","agent"} /\ metadataEmitted)

TokenMintedOnlyAfterSandboxConfirmed ==
    tokenMinted => (brokerActive /\ metadataEmitted)

AgentFDTableDoesNotCarryAuthority ==
    stage = "agent" => \A fd \in agentFds : fdTarget[fd] # "authority"
```

> **CORRECTIONS to the grounded sketch (two real defects):**
>
> 1. The sketch wrote `BrokerStartsOnlyAfterSandboxConfirmed` and
>    `TokenMintedOnlyAfterSandboxConfirmed` **identically** (both `brokerActive => …`).
>    They must be distinct: introduce a separate `tokenMinted` variable so the second
>    invariant has real content (mint strictly follows broker activation).
> 2. `AgentFDTableDoesNotCarryAuthority` was vacuous — the sketch set
>    `agentFDsCarryAuthority = FALSE` in Init and never flipped it. Instead, model
>    `authority` as an **fd-target class** and add an *adversarial* action that tries
>    to place an `authority`-class fd into the helper fd table before sandbox. Prove
>    it is gone by `stage = "agent"`, exactly mirroring the existing
>    `CredentialFDsGoneBeforeSandbox` / `AgentFDTableAllowlisted` (~line 186–194).
>    A non-adversarial model proves nothing.

**Registration:** VERIFIED.md §9 status + change-rules; three `proof_ownership.tsv`
rows under `tla/09_launch_fd_isolation.md`. Keep the spec's scope note: this spec
governs launch/fd boundaries, not request routing.

**Risk:** three booleans + one target class; small growth. The new `metadataEmitted`/
`brokerActive`/`tokenMinted` are monotonic (only flip to true) — matches "once
confirmed, stays confirmed"; revisit only if broker restart must be modeled.

---

## Part 3 — `MC_BeadpostBrokerBoundary` (new spec)

**Goal.** Prove the membrane: authority is derived from host launch facts, the agent
can never supply authority, requests bind to their own confirmed session, and
delivery happens only for an accepted request. Beadpost internals (registry/ledger/
bead mutation) stay abstract. Style copied from `MC_GitSSHRouting`: finite domains,
validation-before-ready, deterministic binding, no cross-session confusion.

**Lifecycle:** session prepared → confirmed sandboxed → host opens per-session broker
socket → agent submits closed request payload (content only) → broker derives
project/session/tier from launch facts → broker invokes delivery only for an accepted
request → session closes (socket released, payload cleared).

> **CORRECTION to the grounded sketch (design-level — this is the most important
> one).** The sketch modeled `agentPayload` as carrying `(project, tier, session)` and
> had the broker *validate that the agent's claimed project/tier match launchFacts,
> rejecting mismatches*. That is "validate," and it contradicts the chosen design:
> the agent submits **no authority fields**, and the broker **derives** authority
> from launch facts and **ignores** anything authority-shaped from the agent. Model
> it that way instead:
>
> - `agentPayload[s]` = request **content only** (e.g., a target route id + opaque
>   body), with **no** project/tier/session authority fields.
> - The authority attached to any delivery is `launchFacts[s]` **by construction** —
>   there is no comparison step that the agent can influence, so there is no
>   "mismatch → reject" path the agent can drive.
> - This makes `AgentCannotSupplyAuthorityFields` a structural property (authority is
>   never a function of `agentPayload`) rather than a comparison outcome.

**Variables (corrected):** `launchFacts` (`Sessions -> Projects \X Tiers`, set once at
Init/host setup), `sessionState` (`prepared|confirmed|accepted|closed`),
`confirmedSessions`, `activeSessions`, `agentContent` (`Sessions -> RequestContent`,
**no authority fields**), `brokerSocket` (`Sessions -> BrokerSockets \cup {NoSocket}`),
`deliveredAuthority` (`Sessions -> (Projects \X Tiers) \cup {NoAuthority}` — what the
broker actually stamped), `requestAccepted` (`Sessions -> BOOLEAN`).

**Invariants (corrected wording):**

```
BrokerSocketOnlyAfterConfirmedSession ==
    \A s \in Sessions : brokerSocket[s] # NoSocket => s \in confirmedSessions

AcceptedRequestHasConfirmedSession ==
    \A s \in Sessions : requestAccepted[s] => s \in confirmedSessions

\* authority is NEVER a function of agent input: the stamped authority is always
\* exactly the host launch facts for that session.
AcceptedAuthorityEqualsLaunchFacts ==
    \A s \in Sessions :
        requestAccepted[s] => deliveredAuthority[s] = launchFacts[s]

\* no Next action ever lets agent content flow into launchFacts or deliveredAuthority
\* other than via launchFacts[s] itself (write-once host authority).
AgentCannotSupplyAuthorityFields ==
    \A s \in Sessions :
        deliveredAuthority[s] # NoAuthority => deliveredAuthority[s] = launchFacts[s]

\* an accepted request for s binds only s's own socket + launch facts;
\* no other session contributes authority or socket.
NoCrossSessionRequest ==
    \A s \in Sessions :
        requestAccepted[s] =>
            /\ brokerSocket[s] # NoSocket
            /\ deliveredAuthority[s] = launchFacts[s]

NoRequestAfterSessionClose ==
    \A s \in Sessions :
        sessionState[s] = "closed" =>
            /\ brokerSocket[s] = NoSocket
            /\ agentContent[s]  = NoContent
            /\ requestAccepted[s] = FALSE

\* launchFacts is write-once at host setup; modeled by proving every Next action
\* leaves launchFacts UNCHANGED (host authority is not agent-mutable/readable in-model).
HostAuthorityNeverAgentReadable ==
    launchFacts = launchFacts0   \* launchFacts0 captured at Init; invariant via action constraint

DeliveryOnlyFromAcceptedRequest ==
    \A s \in Sessions :
        sessionState[s] \in {"accepted","closed"} => requestAccepted[s] \/ sessionState[s] = "closed"
```

> Notes on the corrected invariants:
> - `HostAuthorityNeverAgentReadable` cannot be an OS memory-isolation proof here —
>   that is Parts 1 & 2's job (key deny + fd hygiene). In *this* spec it means
>   "`launchFacts` is host-set and never mutated by any agent action"; enforce it by
>   making every action keep `launchFacts` UNCHANGED and asserting it as an invariant
>   (or as a refinement check). State the OS-isolation caveat in the §15 writeup so
>   no one over-reads it.
> - `DeliveryOnlyFromAcceptedRequest`: closed sessions clear `requestAccepted`, so the
>   accepted state must be checked at the `accepted` transition; verify the exact
>   form against your `SessionClose` action ordering.

**.cfg (small finite domains):** `Sessions = {s1, s2}`, `Projects = {pa, pb}`,
`Tiers = {tNative, tDocker}`, `BrokerSockets = {k1, k2}`, `NoSocket`, `NoAuthority`,
`NoContent`, plus a tiny `RequestContent` set. All eight invariants + `TypeOK`.

**Registration (new spec — full wiring):**
- `tla/MC_BeadpostBrokerBoundary.tla` + `.cfg`
- `tla/15_beadpost_broker_boundary.md` (design note)
- VERIFIED.md new §15 (Design Proved; now Implemented — governed code is
  `hazmat/hostbroker/session.go`, behind `//go:build beadpost_hostbroker`)
- `proof_ownership.tsv`: 9 rows (8 invariants + `TypeOK`) under
  `tla/15_beadpost_broker_boundary.md`
- `check_suite.sh`: `run_spec MC_BeadpostBrokerBoundary no`
- `promoted_specs.tsv`: `MC_BeadpostBrokerBoundary	no`
- `proof_ownership_check.sh`: bump `EXPECTED_PROMOTED_SPEC_COUNT` 14 → 15

**Risk:** keep `Sessions`/`Projects`/`Tiers`/`BrokerSockets` at 2 each; socket
allocation is nondeterministic (pool exhaustion explored) — fine at this size.

---

## Implementor checklist

Update: `MC_SeatbeltPolicy.tla/.cfg`, `MC_LaunchFDIsolation.tla/.cfg`, new
`MC_BeadpostBrokerBoundary.tla/.cfg`, `tla/VERIFIED.md`, `tla/proof_ownership.tsv`,
`tla/promoted_specs.tsv`, `tla/check_suite.sh`, `tla/proof_ownership_check.sh`, and the
three `tla/NN_*.md` design notes.

Run:

```bash
cd /Users/dr/workspace/hazmat/tla
bash proof_ownership_check.sh
bash check_suite.sh   # must exit 0 ("No error has been found") for all three
```

Only after all three pass TLC do the implementation beads (`.3` key custody, `.4`
host broker mint/verify/call-Beadpost, `.5` tier derivation, `.6` per-session socket)
begin — per the spec-first rule in CLAUDE.md.
