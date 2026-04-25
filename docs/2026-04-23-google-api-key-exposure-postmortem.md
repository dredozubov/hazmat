# Google API Key Exposure Post-Mortem

## Summary

On 2026-04-23, GitGuardian reported a Google API key pattern exposed in
`dredozubov/hazmat`. The value was not production code; it was a test fixture
added in `hazmat/config_agent_test.go` to exercise generic key masking for
Gemini credentials. The immediate issue was still real: the fixture matched the
shape of a live Google API key, was pushed to GitHub, and triggered external
secret scanning.

## Timeline

- `2026-04-23 16:04:26 UTC`: GitGuardian reported a Google API key exposure in
  the repository.
- `2026-04-23 18:04:22 +02:00`: commit `48fce7e` introduced the provider-shaped
  fixture while adding `hazmat config agent` coverage for installed harnesses.
- Same session: the offending literal was replaced with an obviously fake test
  value and the branch history was rewritten so published refs no longer point
  at the exposed blob.
- Same session: local developer hooks and CI gained a Google API key pattern
  check so the same class of mistake fails before commit/push/merge.

## Impact

- Public repository history briefly contained a provider-shaped Google API key.
- External scanners correctly flagged the repository, creating incident-response
  work and potential uncertainty about whether the value was real.
- The incident consumed security response time even though the underlying
  product behavior was not compromised.

## Root Cause

The test used a realistic credential-shaped string instead of an obviously fake
fixture. The repo had no automated guard in `pre-commit`, `pre-push`, or CI to
reject provider-shaped Google API keys in tracked content, so the mistake moved
from local development into published history.

## Contributing Factors

- Test authors optimized for realism of the masking input rather than safety of
  the fixture shape.
- Secret scanning was external and after-the-fact instead of part of the local
  and CI quality gates.
- The repo did not have a written rule that tests and docs must never contain
  provider-issued credential formats, even when the values are fake.

## What Changed Immediately

- Replaced the provider-shaped fixture with a clearly fake long value that still
  exercises the masking fallback path.
- Rewrote the affected commit so current published branches no longer reference
  the exposed value.
- Added `scripts/check-secret-patterns.sh`.
- Wired the secret-pattern check into:
  - `scripts/pre-commit` for staged content
  - `scripts/check-fast.sh` and therefore `scripts/pre-push`
  - `.github/workflows/ci.yml`
- Updated `docs/testing.md` so the new gate is part of the documented workflow.

## Process Changes That Should Be Permanent

1. Fake fixtures must be obviously fake. Never use strings that match
   provider-issued credential formats in tests, docs, examples, or screenshots.
2. Secret-pattern checks belong in the default path. If a developer can commit
   or push without the scan, the control is too weak.
3. Published-history cleanup is not enough by itself. Credential revocation or
   confirmation of invalidity must happen in parallel with repo cleanup.
4. Security-relevant test fixtures deserve review attention. A reviewer should
   treat realistic secrets in tests as a blocking issue, not harmless sample
   data.
5. Post-incident follow-up must become tracked work, not just lessons learned
   in chat.

## Follow-Up Work

- Broaden the secret-pattern gate beyond Google API keys with a reviewed
  allowlist strategy for safe test fixtures.
- Add a short contributor guideline for synthetic credentials so future tests
  reuse known-safe placeholder formats instead of inventing new ones ad hoc.

## Subsequent Follow-Up

- The fast repo-local secret gate now covers a reviewed set of additional
  provider-shaped credentials beyond Google keys.
- Safe fixture guidance now lives in
  [docs/synthetic-credentials.md](synthetic-credentials.md).
- The allowlist strategy remains intentionally narrow:
  - prefer obviously fake `example-*` placeholder shapes that do not resemble
    provider-issued secrets
  - allowlist only scanner-definition files, lockfiles, or specific obviously
    fake stopwords when generic detectors still misfire
  - never allowlist docs/tests/examples simply because they contain
    credential-shaped values

## Operator Checklist For This Incident

- Revoke or verify invalidity of the reported Google API key in Google Cloud.
- Confirm the rewritten branches are force-pushed and the old commit is no
  longer referenced by published refs.
- Resolve or close the GitGuardian alert after rotation and history cleanup are
  complete.
