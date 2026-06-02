# Remote Launch Envelope Schema And Worker Admission Plan

**Date:** 2026-06-02
**Status:** Plan only. No remote runner is implemented or approved by this document.
**Owns bead:** `sandboxing-nmqn`
**Related architecture:** [2026-06-02-modular-architecture-direction.md](2026-06-02-modular-architecture-direction.md)
**Implementation placeholder:** `hazmat/sessionbackend.RemoteEnvelope`

This document defines the first remote/orchestrated launch envelope shape and
the worker admission checks that must exist before Hazmat can execute sessions
on a remote worker. It is deliberately a plan, not executable behavior.

The current Hazmat trust model is local: one controlling user, one local
`agent` macOS user, local Seatbelt/pf/user isolation, and host-owned credential
storage. Remote execution adds a new machine boundary, a control-plane boundary,
and replay/integrity risks. A remote runner therefore requires new TLA+ model
work and a threat-model update before any launch code is accepted.

## Non-Goals

- Do not implement a remote runner.
- Do not make `sessionbackend.RemoteEnvelope` executable authority.
- Do not serialize raw secrets, credential file contents, broker sockets, or
  unredacted host credential paths.
- Do not treat control-plane JSON, API flags, or saved records as authority
  until they pass `ParseAndValidate`.

## Dominant Rule

Wire DTOs are not authority.

```text
RemoteEnvelopeDTO bytes
  -> strict schema decode
  -> canonical serialization check
  -> integrity and replay verification
  -> ParseAndValidate(...)
  -> ValidatedRemoteEnvelope
  -> WorkerAdmission
  -> backend compiler
  -> backend runner
```

Every field that can affect authority must be re-validated into a type with
constructor-enforced invariants. DTO fields are public for serialization only.
They must not be passed to backend compilers, credential delivery code, cleanup
code, or telemetry emitters directly.

## Envelope V1 Shape

The envelope should be a UTF-8 JSON object with a single top-level schema
version. All signature/MAC inputs use a pinned canonical JSON form:

- object keys sorted lexicographically
- no insignificant whitespace
- UTF-8 strings only
- integers for versions, sequence numbers, and clock-skew seconds
- RFC3339 timestamps in UTC
- no floats
- no duplicate object keys
- absent optional fields are omitted, not serialized as `null`

The signed payload is the envelope without `integrity.signature` or
`integrity.mac`. The canonicalization identifier is itself part of the signed
payload, so a producer cannot change serialization rules without invalidating
the envelope.

```json
{
  "schema_version": 1,
  "canonicalization": "hazmat-json-v1",
  "envelope_id": "01HZ...",
  "session_id": "01HZ...",
  "nonce": "base64url-random...",
  "produced_at": "2026-06-02T00:00:00Z",
  "not_before": "2026-06-02T00:00:00Z",
  "expires_at": "2026-06-02T00:05:00Z",
  "producer": {
    "id": "control-plane:example",
    "hazmat_version": "0.0.0",
    "key_id": "signing-key-1"
  },
  "worker_target": {
    "worker_id": "worker:example",
    "backend": "linux-native",
    "workspace_id": "workspace:example",
    "attestation_epoch": "manual-v1"
  },
  "plan": {
    "format_version": 1,
    "digest": "sha256:...",
    "backend": "linux-native",
    "harness": "codex",
    "network_policy": "default"
  },
  "paths": {
    "project": {
      "workspace_relative": ".",
      "mode": "read-write"
    },
    "read_only": [],
    "read_write": []
  },
  "credentials": [],
  "capability_gaps": [],
  "cleanup": {
    "policy": "best-effort-with-proof",
    "artifacts": []
  },
  "telemetry": {
    "record_version": 1,
    "default_classification": "control-plane-private"
  },
  "integrity": {
    "algorithm": "mac-or-signature-v1",
    "key_id": "signing-key-1",
    "payload_digest": "sha256:...",
    "signature": "base64url..."
  }
}
```

This JSON is illustrative. The final field names may change when the remote
model is written, but the validation obligations below are not optional.

## DTO To Validated Type Rules

| DTO value | Validation rule | Validated authority |
| --- | --- | --- |
| `schema_version` | Must be exactly one supported version. Unknown versions fail closed unless a tested migration adapter exists. | `EnvelopeVersion` |
| `canonicalization` | Must be one supported canonical form. The worker recomputes canonical bytes before integrity verification. | `Canonicalization` |
| `envelope_id` | Must be globally unique enough for replay tracking and accepted only once per worker identity within the replay window. | `EnvelopeID` |
| `session_id` | Must bind all worker runtime state, cleanup records, telemetry, and credential handles for one launch. | `SessionID` |
| `nonce` | Must be high-entropy, bound to the worker identity and session ID, and accepted only once inside the replay window. | `ReplayNonce` |
| `produced_at`, `not_before`, `expires_at` | Must be valid UTC timestamps. Admission rejects expired, not-yet-valid, or excessive lifetime envelopes under a modeled clock-skew rule. | `AdmissionWindow` |
| `producer` | Must identify the control-plane principal and verification key. Key ID must map to a trusted producer for the targeted worker. | `ProducerIdentity` |
| `worker_target` | Must match the receiving worker identity, backend kind, workspace identity, and attestation/readiness evidence. | `WorkerTarget` |
| `plan.digest` | Must match the canonical digest of the validated planner output used to produce the envelope. | `PlanDigest` |
| `plan.backend` | Must match the target worker backend and the prepared artifact kind. | `BackendKind` |
| `plan.harness` | Must name a supported harness registry entry for that backend and worker. | `HarnessRef` |
| `plan.network_policy` | Must be one of the modeled network policies and must be enforceable by the selected backend. Unsupported enforcement is a capability gap, not a silent downgrade. | `NetworkPolicy` |
| `paths.project` | Must resolve under the worker workspace root, stay out of credential and host-state deny zones, reject symlink escape, and be writable only when the planner granted project write scope. | `RemoteProjectRoot` |
| `paths.read_only` | Each grant must resolve under an allowed worker root, stay out of deny zones, and not expand authority beyond the validated plan. | `RemoteReadOnlyGrant` |
| `paths.read_write` | Same as read-only, plus write scope must be explicitly present in the validated plan. | `RemoteReadWriteGrant` |
| `credentials` | Must be empty until a new remote credential model is proved. If later enabled, every entry must be a scoped handle, never secret bytes, with session binding, delivery mode, expiry, cleanup, and revocation semantics. | `RemoteCredentialHandle` |
| `capability_gaps` | Must name exact planner gaps, the accepting principal, reason, expiry, and backend impact. Unaccepted gaps fail admission. | `AcceptedCapabilityGap` |
| `cleanup` | Must enumerate expected worker runtime artifacts, cleanup strategy, retention limits, and cleanup proof shape. Unknown cleanup classes fail closed. | `CleanupPolicy` |
| `telemetry` | Must classify each emitted field. Secret and secret-adjacent values are omitted, redacted, or hashed before crossing the worker boundary. | `TelemetryPolicy` |
| `integrity` | Must cover the canonical signed payload, bind producer key, worker target, session ID, nonce, expiry, and schema version. | `EnvelopeIntegrity` |

## Worker Admission Checklist

Admission runs before a backend compiler or runner sees the envelope.

1. Decode the envelope as a DTO with duplicate-key rejection and size limits.
2. Reject unsupported schema versions and canonicalization rules.
3. Recompute canonical payload bytes and `payload_digest`.
4. Verify signature or MAC using the producer key allowed for the receiving
   worker.
5. Confirm the `worker_target.worker_id` matches the receiving worker identity.
6. Enforce `not_before`, `expires_at`, maximum lifetime, and clock-skew rules.
7. Check replay storage for `(worker_id, envelope_id)` and `(worker_id,
   session_id, nonce)` before launch; record admission atomically.
8. Verify the worker backend is installed, healthy, and able to enforce every
   requested policy.
9. Recompute the planner digest from the validated plan artifact or reject if
   the referenced plan is unavailable.
10. Map path grants into worker-local paths through validated constructors.
11. Reject path grants that escape the workspace, enter credential deny zones,
    or depend on unresolved symlinks.
12. Reject raw secret material. Until the remote credential model exists,
    reject all non-empty credential handle lists.
13. Reject unaccepted capability gaps and accepted gaps that are stale,
    malformed, or not present in the planner output.
14. Build a cleanup plan before launch and ensure cleanup proof storage is
    writable by the worker supervisor.
15. Build a telemetry policy before launch and reject fields without a
    classification.
16. Only after all admission checks pass, compile the backend-specific artifact
    on the worker.

## Integrity And Replay Design

The integrity check must bind the envelope to one worker target. A valid
envelope for worker A must be invalid on worker B even if both workers share a
backend kind. The signed payload therefore includes:

- `schema_version`
- `canonicalization`
- `envelope_id`
- `session_id`
- `nonce`
- `producer`
- `worker_target`
- `plan.digest`
- `not_before`
- `expires_at`
- capability-gap acceptances
- credential handle IDs, when modeled

Replay defense is worker-owned. The control plane may also track envelope use,
but the worker must keep its own admitted-envelope record because network
partitions or scheduler bugs must not allow the same envelope to launch twice.
Replay records expire only after the admission window plus modeled clock skew
and cleanup grace period.

## Credential Handle Lifecycle

Remote v1 should be credential-free unless the TLA+ model for remote
credential handles is written and proved first. If credential handles later
cross the worker boundary, they must extend the guarantees from Specs 12 and 13:

- delivery mode remains authoritative
- handles are scoped to exactly one session and worker identity
- handles expire independently of worker cleanup success
- crash/restart cannot resurrect a session-only grant
- cleanup either revokes the handle or records a failed revocation proof
- telemetry may show redacted handle IDs, never handle payloads or secret paths
- adapter-required credentials remain inert unless their adapter is modeled

This means `credentials: []` is the only admitted remote envelope credential
state until a successor model says otherwise.

## Capability Gaps

Remote workers must treat unsupported policy as an explicit gap. A gap must
carry:

- feature name from planner output
- required semantic guarantee
- backend or worker limitation
- accepting principal
- acceptance reason
- expiration or one-session scope

Admission rejects a gap that was not produced by the planner, has no accepting
principal, outlives the session, or would affect credential-deny, path escape,
or raw-secret handling. Those are hard failures, not acceptable gaps.

## Cleanup And Telemetry

Remote cleanup records are versioned artifacts. A cleanup proof should include
the session ID, worker ID, artifact class, cleanup action, result, timestamp,
and redaction-safe diagnostics. It must not include raw paths to host
credential stores, token-like env values, broker socket paths, or secret file
contents.

Telemetry defaults to `control-plane-private`. Public diagnostics must be
explicitly selected field-by-field. A future implementation should use these
classes:

| Class | Allowed use |
| --- | --- |
| `public-diagnostic` | CLI summaries safe to show without host topology or credential context. |
| `operator-private` | Local operator logs and support bundles. |
| `control-plane-private` | Scheduler records and worker admission/cleanup state. |
| `secret-adjacent` | Values that must be omitted, redacted, or hashed before leaving the worker. |

## Required Model And Documentation Gates

Remote execution cannot ship until all of these are complete:

- Add a new TLA+ model for remote envelope admission covering schema version,
  integrity verification order, replay defense, worker identity binding, path
  mapping, capability-gap rejection, cleanup proof creation, and telemetry
  classification.
- Extend Specs 12 and 13 or add successor specs before credential handles cross
  a machine boundary.
- Update `tla/VERIFIED.md` with governed code paths before implementation.
- Add golden canonical-envelope fixtures and invalid-envelope fixtures.
- Add admission tests for bad signature/MAC, unknown schema, duplicate keys,
  expired envelope, replay, wrong worker, path escape, unsupported backend,
  non-empty credentials before credential modeling, unaccepted gaps, and
  unclassified telemetry fields.
- Update `docs/design-assumptions.md` when remote execution changes the local
  trust model from assumption to product behavior.
- Update `docs/cve-audit.md` and any threat-model docs before remote execution
  is claimed as a supported containment layer.

Until those gates are green, remote envelope artifacts are planning records and
must not be treated as launch authority.
