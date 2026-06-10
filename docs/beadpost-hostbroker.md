# Hazmat ↔ Beadpost Host-Broker Attestation Contract

This is the operator contract for Hazmat's optional Beadpost host-broker support:
how a contained agent's cross-project disclosure request is attested and routed,
what the security boundary guarantees, and what is **not** yet guaranteed.

> **Public Hazmat does not include Beadpost host-broker support.** The default
> build compiles dependency-free, fail-closed stubs and never links the Beadpost
> contract module, the Beadpost root module, or Dolt. Support is an explicit
> operator opt-in (see [Build modes](#build-modes)).

## Model B: contained-agent submitter + dr-owned Beadpost broker

Hazmat is the **containment substrate and forwarder**; Beadpost is the
**disclosure broker**. They meet at typed contracts, never by Hazmat importing
Beadpost.

```
contained agent ──content──▶ Hazmat per-session socket (.6)
                              │  host derives authority from launch facts,
                              │  computes the request fingerprint,
                              │  mints a v2 attestation (.4)
                              ▼
                     dr-owned beadpost-broker daemon ──▶ verify v2 + policy/ledger/delivery
                       (typed beadpost.hostbroker.v1 IPC, Unix socket)
```

- **Hazmat** proves the agent was launched in a constrained project context,
  derives authority from launch facts, mints the attestation, and forwards the
  request. It owns no broker policy, registry, ledger, or review state.
- **Beadpost** decides whether a contained request may cross into another project.
  It owns the broker policy, registry, ledger, review, and delivery semantics, and
  it runs the dr-owned broker daemon (`cmd/beadpost-broker`).
- **`local/beadpost-contracts`** is a small, stdlib-only module that owns the
  canonical wire formats both sides share (one implementation, no duplication).

## Build modes

| | Default / public | Operator / private |
| --- | --- | --- |
| Build | `go build ./...`, `go test ./...` | add `-tags beadpost_hostbroker` |
| `local/beadpost-contracts` | not imported | imported by the tagged files |
| Beadpost root / Dolt | never imported | never imported |
| `hostbroker` package | dependency-free stubs returning `ErrDisabled` | real signer/client/session |
| Cross-project disclosure | **disabled** (fails closed) | enabled |

The real implementation lives behind `//go:build beadpost_hostbroker`; the
default build uses the stub files (`!beadpost_hostbroker`). In the default build
`hostbroker.Mint`/`Verify`/`Open`/`Client.*` all return `hostbroker.ErrDisabled`,
no agent socket is opened, and no broker is dialed.

Operator builds resolve `local/beadpost-contracts` through a **local `go.work`**
that `use`s the contract module's directory (no `go.mod` require is added). That
`go.work` is intentionally **untracked / gitignored**: committing it would make a
public checkout depend on a private module path, which the default posture
forbids. `TestImportBoundaries` enforces both halves — the default build must not
depend on `local/beadpost-contracts`, `beadpost`, or `github.com/dolthub`; the
tagged build may depend only on `local/beadpost-contracts` (still never the
Beadpost root or Dolt).

## Security boundary

1. **Confirmed containment gates the socket.** The per-session agent socket is
   opened only after `sandbox_init` is confirmed and the launch facts are
   complete (`confirmSandboxBoundary`). No confirmation ⇒ no socket.
2. **The agent submits content only.** Payloads are bounded and strict-decoded;
   any agent-supplied authority field — `project`, `uid`, `tier`, `registry`,
   `ledger`, key path, `token`, `attestation`, `fingerprint`, `signature` — is
   rejected, as are malformed, oversized, and unknown-op payloads.
3. **The host derives authority from launch facts.** Origin project, agent uid,
   and tier come from the session's launch facts, never from the agent
   (`deriveAuthorityFromLaunchFacts`). The effective tier is the honest,
   over-claim-guarded `.5` derivation.
4. **The host computes the request fingerprint** with the shared contract
   (`local/beadpost-contracts/request.Fingerprint`) over the submitted canonical
   content.
5. **The host mints a v2 attestation** (`beadpost.containment.attestation.v2`)
   binding project/uid/tier/fingerprint, with a short TTL.
6. **Beadpost verifies and triple-binds.** The broker requires a v2 token and
   checks `token fingerprint == request fingerprint == origin-envelope
   fingerprint`; a mismatch (or a v1 token) fails closed. A token minted for one
   request cannot authorize another.
7. **Responses never carry secrets.** The agent-facing response carries only the
   broker's outcome message or a non-secret error — never the key, token, or
   attestation.

The host-authority HMAC key is held by the two dr-owned processes (Hazmat signs,
beadpost-broker verifies) and is denied to the contained agent by the `.3`
pathpolicy host-authority deny and the seatbelt credential floor.

## Non-claims and open gaps

These are explicitly **not** guaranteed by the current implementation:

- **Replay prevention is not complete.** A valid v2 token re-verifies within its
  TTL because the signed nonce is not yet consumed. Single-use enforcement lands
  with the nonce-consumption ledger (**bp-fyg.1**, open). Do not describe this
  path as replay-proof until that ships; `TestReplayNotYetPreventedAtVerify` pins
  the current behavior.
- **Strong-tier enforcement on the Beadpost policy path is pending.** Hazmat
  stamps the honest tier, but the broker does not yet *require* a minimum tier to
  authorize disclosure (**bp-fyg.2**, open).
- **Host binding is optional/open.** Cross-host token theft is not bounded by a
  host-binding field yet (**bp-fyg.4**, open).

Already in place: **request/provenance binding** — the v2 signed fingerprint binds
a token to exactly one request (**bp-fyg.3**, closed).

## Beadpost contract references

- **`local/beadpost-contracts`** owns the canonical formats: `attestation`
  (`beadpost.containment.attestation.v1`/`.v2`), `request`
  (`beadpost.request.v1` fingerprint), and `hostbrokerwire`
  (`beadpost.hostbroker.v1` IPC). It is stdlib-only and is the single source of
  the canonicalization; it never imports Beadpost or Dolt. `contractfixture`
  pins the canonical bytes that both repos' tests consume.
- **Beadpost** owns broker policy, ledger, review, and delivery semantics and the
  dr-owned broker daemon.
- **Hazmat must not import the Beadpost root module or Dolt.** It speaks the IPC
  and attestation contracts as the shared module, enforced by
  `TestImportBoundaries`.

## Verification references

| Concern | Bead | Artifact |
| --- | --- | --- |
| Attestation key non-readability | `sandboxing-x74u.2` | `MC_SeatbeltPolicy` — [VERIFIED.md](../tla/VERIFIED.md) §2 |
| Confirmed mint/broker ordering, fd hygiene | `sandboxing-x74u.9` | `MC_LaunchFDIsolation` — VERIFIED.md §9 |
| Broker membrane (confirm-gate, authority-from-facts, no cross-session, clean teardown) | `sandboxing-x74u.10` | `MC_BeadpostBrokerBoundary` — VERIFIED.md §15 |
| Host-authority key custody + pathpolicy deny | `sandboxing-x74u.3` | `hazmat/attestationkey`, `hazmat/pathpolicy` |
| Honest effective-tier derivation | `sandboxing-x74u.5` | `hazmat/attestationtier` |
| v2 signer + Dolt-free IPC client | `sandboxing-x74u.4` | `hazmat/hostbroker/attestation.go`, `client.go` |
| Per-session agent socket + lifecycle | `sandboxing-x74u.6` | `hazmat/hostbroker/session.go` |
| Forgery/non-interference + shared wire fixtures | `sandboxing-x74u.7` | `hazmat/hostbroker/*_test.go`, `local/beadpost-contracts/contractfixture` |

The verified spec scope is authoritative in [`tla/VERIFIED.md`](../tla/VERIFIED.md);
this document is the operator-facing companion, not a verification record.
