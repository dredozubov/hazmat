# Problem 15 — Beadpost Attestation-Boundary Broker

**Status:** design-proved membrane model for the Hazmat↔Beadpost containment
attestation boundary (epic `sandboxing-x74u`, Part 3 of 3). Companion specs:
`MC_SeatbeltPolicy` (attestation-key non-readability, Part 1) and
`MC_LaunchFDIsolation` (confirmed-containment mint ordering + authority-fd
hygiene, Part 2). Plan: `docs/plans/2026-06-09-beadpost-attestation-spec-plan.md`.

## Problem Statement

The chosen integration model is **contained-agent submitter + dr-owned host
broker**. A contained agent submits *closed request payloads* — request content
only, never authority fields — to a per-session, dr-owned broker socket. The
broker (host side) holds the Beadpost HMAC key / registry / ledger, derives
project/session/tier authority from host launch facts, and invokes Beadpost
delivery/review itself. The agent never holds the signing key, a key path, or a
reusable bearer token.

This spec proves the **membrane** between the untrusted agent submitter and the
host-owned broker. Beadpost internals (registry rows, ledger hash-chain, bead
mutation, real delivery side effects) are deliberately abstract; they are not
modeled here.

The correctness obligations:

1. a broker socket exists only for a session whose containment was confirmed;
2. a request is accepted only for a confirmed session;
3. authority attached to a request is always exactly the host launch facts —
   the agent cannot supply or influence it (there is no agent authority field);
4. an accepted request's authority equals the host launch facts;
5. no two sessions share a broker socket (deterministic per-session binding);
6. a closed session retains no socket, content, authority, or acceptance;
7. host launch facts are write-once and never mutated by any action;
8. delivery occurs only for an accepted request.

## Why "derive", not "validate"

An earlier sketch modeled the agent as supplying `(project, tier)` and the broker
*validating* it against launch facts, rejecting mismatches. That contradicts the
design: the agent submits **no** authority fields, so there is nothing to
validate. The broker stamps `deliveredAuthority := launchFacts[s]`
unconditionally. `AgentCannotSupplyAuthorityFields` is therefore a structural
property (authority is never a function of agent content), not a comparison
outcome an agent could drive.

## Model

Per-session lifecycle: `prepared → confirmed → accepted → closed`. Confirmation
allocates a unique per-session broker socket. The agent submits content; the
broker derives authority and accepts; delivery transitions to `accepted`; close
releases the socket and clears residual state. `launchFacts` is chosen at `Init`
and never primed; `genesisFacts` is its immutable witness.

The launch-time confirmation *ordering* itself (sandbox_init then
confirmed-containment metadata before broker activation) is proved separately in
`MC_LaunchFDIsolation`; this spec treats "confirmed" as the abstract entry gate.

## Invariants

`BrokerSocketOnlyAfterConfirmedSession`, `AcceptedRequestHasConfirmedSession`,
`AgentCannotSupplyAuthorityFields`, `AcceptedAuthorityEqualsLaunchFacts`,
`NoCrossSessionRequest`, `NoRequestAfterSessionClose`,
`HostAuthorityNeverAgentReadable`, `DeliveryOnlyFromAcceptedRequest`.

TLC: "No error has been found" across 1,088 reachable states (3,104 generated,
depth 9, <1s) with 2 sessions, 2 projects, 2 tiers, 2 sockets.

## Scope boundary / non-fits

- `HostAuthorityNeverAgentReadable` is a *design-separation* property
  (`launchFacts` is host-set and write-once); OS-level memory/key isolation is
  proved by `MC_SeatbeltPolicy` (key deny) and `MC_LaunchFDIsolation` (fd
  hygiene), not here.
- Replay defense (single-use nonce), strong-tier requirement, and cross-host
  token theft are Beadpost-side obligations (tracked under `bp-fyg`), not
  modeled here.
- Real delivery, registry, and ledger semantics remain implementation/test
  obligations.

## Change rules

- Any change letting agent content influence delivered authority must update
  `AgentCannotSupplyAuthorityFields` first and re-run TLC.
- Removing the confirmed-session gate before socket allocation or acceptance
  requires re-proving `BrokerSocketOnlyAfterConfirmedSession` and
  `AcceptedRequestHasConfirmedSession` (they will fail — this is the firewall).
- Adding authority fields beyond `(project, tier)` requires extending
  `AcceptedAuthorityEqualsLaunchFacts`.
- Allowing socket reuse across concurrent sessions requires re-proving
  `NoCrossSessionRequest`.
