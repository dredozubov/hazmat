# Public Copy Checklist

Use this before merging safety-facing README, docs, changelog, release-note,
blog, social, or discussion-prompt changes.

The checklist is intentionally stricter than ordinary copy review because
Hazmat's public language is part of its trust boundary. A claim that sounds
broader than the evidence can mislead users into running agents with the wrong
expectation.

## Required Checks

- The copy has one primary claim, not a stack of slogans.
- The claim names the boundary: user, filesystem, credential, network, Docker,
  setup/rollback, launch, harness, platform, or service-agent lifecycle.
- The claim links to repo-owned evidence: test, TLA+ spec/design note, docs
  page, compatibility row, recipe, or sanitized transcript.
- Any live-smoke claim includes exact command, approval context, date or
  release context, machine scope, and sanitized output.
- The caveat appears near the claim, not only in a separate limitations page.
- `hazmat check` quick-mode language says read-only and non-prompting.
- `hazmat doctor --fix`, `hazmat check --full`, `hazmat status --full`,
  prepared-host smokes, and desktop attach probes are described as
  sudo-adjacent when relevant.
- Docker language distinguishes native containment, Docker Sandbox
  private-daemon workflows, and unsupported shared-host-daemon authority.
- Linux language says readiness-gated or compile-only unless support has been
  implemented, tested, and released.
- Formal-methods language names the specific promoted spec or invariant.
- Unsupported paths say unsupported because unproven, not secretly enabled.

## Reject These Phrases Unless Narrowed

- "Hazmat is secure."
- "Hazmat makes agents safe."
- "Hazmat is formally verified."
- "Works with Docker" without the native/shared-daemon caveat.
- "Supports Linux" while native Linux launch is still readiness-gated.
- "Doctor fixes everything."
- "Check validates the whole host."
- "Live smokes passed" without the exact approved evidence.
- "Drop-in sandbox for every agent."

## Safer Replacements

| Instead of | Use |
| --- | --- |
| "secure agents" | "reduced host authority for contained agent sessions" |
| "safe autonomy" | "full-autonomy flow with a smaller blast radius" |
| "formally verified Hazmat" | "specific setup, rollback, launch, credential, and policy invariants are model-backed" |
| "Docker support" | "Docker Sandbox for private-daemon workflows; native containment refuses host Docker socket shortcuts" |
| "Linux support" | "Linux native launch remains readiness-gated" |
| "doctor fixes it" | "`doctor --dry-run` explains the repair plan; `doctor --fix` applies typed executable repairs" |

## Evidence Gate

Before publishing a claim, answer:

1. What can a user inspect or run now?
2. What exact boundary does that prove?
3. What does it not prove yet?

If any answer is missing, convert the sentence into a plan, caveat, or
follow-up bead instead of public copy.

## Claim-Specific Requirements

### Diagnostics And Repair

Allowed:

- "`hazmat check` quick mode is read-only and non-prompting."
- "`hazmat doctor --dry-run` previews the repair plan."
- "`hazmat doctor --fix` applies typed executable repairs."

Requires explicit sudo-adjacent caveat:

- `hazmat check --full`
- `hazmat status --full`
- prepared-host smoke wrappers
- native helper-backed live probes
- Codex desktop attach probes

### Formal Verification

Allowed:

- "`MC_SetupRollback` governs setup/rollback ordering."
- "`MC_LaunchFDIsolation` models the native helper fd table before sandboxing."
- "The proof boundary is documented in `tla/VERIFIED.md`."

Not allowed:

- "Hazmat is formally verified."
- "The sandbox is proved secure."

### Docker And Databases

Allowed:

- "Native containment refuses host Docker socket shortcuts."
- "Private-daemon Docker workflows use Docker Sandbox."
- "Shared host-daemon workflows need Tier 4 or a code-only native session."

Not allowed:

- "Hazmat supports Docker" as a standalone claim.
- "Docker Sandbox is a transparent fallback from native mode."

### Platform And Desktop Claims

Allowed:

- "macOS native containment is the supported path today."
- "Linux native launch is a modeled, readiness-gated future provider."
- "Codex desktop attach is an explicit opt-in live probe, not autonomous
  backend coverage."

Not allowed:

- "Linux support" without release artifacts and setup/rollback implementation.
- "Codex desktop is contained" without the approved desktop attach evidence and
  residual host-state caveats.
