# Runtime Authority Support Gate

Hazmat owns the local macOS runtime-authority surfaces consumed by Planescape
and ops. The support gate is intentionally test-only: it previews authority,
verifies capability declarations, checks conformance and revocation state,
derives hostbroker route facts, and validates replayable trace records without
launching jobs or reading credentials.

Run:

```bash
scripts/check-runtime-authority-support
```

The gate covers:

- `runtimeauthority`: neutral `runtime.authority.v1` preview, unsupported-field
  reporting, malformed input rejection, path/network/session-home projection,
  and credential-broker-required mode.
- `runtimecapability`: TLV capability fingerprints, signed declarations,
  conformance coverage, signed verifier results, revocation feed lifecycle, and
  stale or tampered artifact rejection.
- `hostbroker` with `beadpost_hostbroker`: route facts derived from launch and
  peer credentials, rejecting requester-authored project/session/principal
  overrides.
- `runtimeauthoritytrace`: hash-addressed JSONL trace records and the exported
  fixture at
  `hazmat/runtimeauthoritytrace/testdata/runtime_authority_trace.jsonl`.

The check also runs full `go test ./...`, `go vet ./...`, and a build-tagged
hostbroker vet pass so the Planescape integration does not depend on hidden
package-local assumptions.
