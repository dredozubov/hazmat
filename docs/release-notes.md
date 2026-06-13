# Release Note Convention

Hazmat release notes are part of the safety surface. Any changelog entry that
claims a containment, credential, setup, rollback, Docker, platform, or formal
verification improvement should include the evidence and caveat close to the
claim.

## Required Shape

Use this block when a release item changes a safety-facing boundary:

```md
### Proof
- <artifact> demonstrates <specific boundary>.

### Caveats
- <non-goal or unproven adjacent behavior>.

### Next
- <instrumentation, smoke, recipe, compatibility, or model follow-up>.
```

The block can live directly under a release heading when several bullets share
the same proof, or inside one bullet when the claim is narrow.

## Claims That Need Proof And Caveat

Require this convention for claims about:

- `hazmat check`, `hazmat doctor`, init, rollback, or repair behavior;
- credential storage, materialization, harvest, brokered Git, SSH, or cleanup;
- native launch isolation, Seatbelt policy, `pf`, DNS, or helper behavior;
- Docker Sandbox, shared Docker daemon limits, or database/service workflows;
- Linux, Apple Container, Codex desktop, OpenHands, or other platform/service
  boundaries;
- formal verification, model-governed behavior, or TLA+ scope.

## Evidence Sources

Prefer repo-owned evidence:

- test name or CI job;
- exact approved smoke command plus sanitized transcript;
- TLA+ spec/design note and successful TLC run when the claim is model-backed;
- docs page that records the boundary and limitation;
- compatibility row, recipe, or fixture transcript for integration claims.

Do not claim a live smoke passed unless the exact command was approved and the
output was captured safely. Prepared-host and desktop attach smokes are not
generic tests; they are machine-context evidence.

## Community Evidence

Community contributions should be visible in release notes when they add
durable evidence or user value:

- recipes with exact commands and caveats;
- compatibility reports with host, harness, stack, mode, evidence, and
  limitations;
- integration proposals that stayed inside the manifest contract;
- docs/UX fixes backed by confusing output, screenshots, or setup traces;
- public incident-to-control evidence that changed docs, tests, or beads.

Use a dedicated section when a release includes community evidence:

```md
### Community Evidence
- <contributor or report link> added <recipe, compatibility row, caveat,
  incident mapping, or docs fix>. Evidence: <link>. Caveat: <scope limit>.
```

Rules:

- Do not publish names for private security reports without coordinated
  disclosure agreement.
- Do not imply community ownership of seatbelt, `pf`, setup/rollback,
  credential delivery, launch helpers, broker protocols, Docker routing, or
  TLA-governed behavior.
- Do not promote a recipe, compatibility report, or integration proposal to
  "supported" without maintainer review and evidence.
- Pair broad community claims with `docs/community.md` and
  `docs/community-labels.md` so readers understand the ownership model.

## Wording Rules

Use:

- "reduces blast radius";
- "read-only quick diagnostic";
- "approval-gated live smoke";
- "model-backed invariant";
- "native containment refuses host Docker socket shortcuts";
- "unsupported because unproven."

Avoid:

- "secure" or "safe" without a narrow object;
- "formally verified Hazmat";
- "Docker support" without the native/shared-daemon caveat;
- "Linux support" while native Linux launch remains readiness-gated;
- "doctor fixes everything";
- "check validates the whole host."

## Example

```md
### Fixed
- `hazmat check` quick mode no longer runs sudo or helper-backed probes; it
  reports a typed repair plan and leaves executable repairs to
  `hazmat doctor --fix`.

### Proof
- `TestCheckQuickModeSkipsSudoAdjacentValidation`
- `docs/testing.md`

### Caveats
- `hazmat check --full`, `hazmat status --full`, and prepared-host smoke
  scripts remain sudo-adjacent and require explicit approval in agent workflows.
```
