# Community Label Map

Use these labels for community-visible issues, PRs, and Discussions converted
into tracked work. They are meant to route evidence and contribution surfaces
without making trust-boundary changes look like casual first issues.

## Primary Labels

| Label | Use for | Default route |
| --- | --- | --- |
| `good-first-recipe` | Recipe additions or improvements with clear harness + stack scope | Community/docs review |
| `compatibility-report` | Host, harness, stack, macOS, mode, evidence, and caveat reports | Compatibility triage |
| `integration-proposal` | Built-in integration ideas or manifest improvements | Integration author kit review |
| `needs-evidence` | Claims or requests missing transcript, fixture, recipe, compatibility, or test evidence | Ask for evidence before implementation |
| `design-first` | Work that affects authority, lifecycle, setup/rollback, credentials, Docker routing, service harnesses, or platform support | Design note and maintainer review before code |
| `security-private-route` | Public issue/discussion that appears to involve a private vulnerability, bypass, credential leak, or privilege issue | Redirect to `SECURITY.md`; do not triage details publicly |

## Supporting Labels

| Label | Use for | Notes |
| --- | --- | --- |
| `docs-ux` | Copy, onboarding, screenshots, examples, troubleshooting, and CLI text | Good community surface when claims stay evidence-backed |
| `recipe` | Any recipe lifecycle work after it is no longer a first issue | Pair with stack or harness label when useful |
| `harness-usability` | Auth/import/bootstrap friction or harness docs that do not change containment | Escalate to `design-first` if profile/state authority changes |
| `proof-artifact` | Sanitized transcript, visual, fixture, smoke output, or demo asset work | Requires secret-scan awareness |
| `incident-control` | Public incident or CVE mapped to Hazmat controls and gaps | Use private route for undisclosed findings |
| `release-evidence` | Changelog/release-note proof, caveat, or community evidence convention work | Pair with `needs-evidence` when claims are too broad |

## Label Rules

- `good-first-recipe` must not be used on seatbelt, `pf`, setup/rollback,
  credential delivery, launch-helper, broker, Docker-routing, or TLA-governed
  behavior.
- `compatibility-report` needs host/version/mode evidence before it changes the
  public matrix.
- `integration-proposal` does not imply a manifest will be accepted. The
  proposal still needs schema, path, env, and snapshot-exclude review.
- `needs-evidence` should block public copy claims until a proof source exists.
- `design-first` means code should wait for a design note, model review when
  applicable, and a test plan.
- `security-private-route` is for routing, not public investigation. Do not ask
  the reporter to paste sensitive details into the public thread.

## Discussion Conversion

When converting a Discussion into an issue or bead:

1. Add one primary label.
2. Add one support label only if it clarifies ownership.
3. Copy the evidence link, caveat, and unresolved boundary question into the
   issue body.
4. If the thread is broad, split it into one evidence artifact and one
   implementation task.

Examples:

- "Claude + Next.js recipe needs pnpm caveat" -> `good-first-recipe`, `docs-ux`
- "Codex + uv works on macOS 15.5 with imported auth" -> `compatibility-report`
- "Add Bazel integration" -> `integration-proposal`, `needs-evidence`
- "Let native sessions use host Docker socket" -> `design-first` or close as
  unsupported with shared-daemon docs
- "I found a way to read host SSH keys" -> `security-private-route`

## GitHub Setup

After Discussions are enabled, create these labels in GitHub with short
descriptions copied from this page. Color is cosmetic; avoid using color as the
only signal for priority or risk.
