# Public Copy Execution Plan

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.9`
**Status:** ordered implementation plan
**Inputs:**
[positioning wedges](2026-06-13-positioning-wedges.md),
[public surface audit](2026-06-13-public-surface-conversion-audit.md),
[demoable proof stack](2026-06-13-demoable-proof-stack.md),
[first-month cadence](2026-06-13-first-month-launch-cadence.md),
[contributor loop](2026-06-13-contributor-participation-loop.md),
[integration channels](2026-06-13-integration-distribution-channels.md),
[milestone messaging](2026-06-13-milestone-proof-messaging.md),
[trust messaging](2026-06-13-trust-recovery-safety-messaging.md)

## Objective

Turn the positioning work into public surfaces in the order a first evaluator
will encounter them:

1. README first screen.
2. Proof artifacts.
3. Demo/tutorial paths.
4. Release-note and changelog conventions.
5. Community and distribution posts.
6. Claim-audit guardrails.

The plan keeps one memory dominant:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

Every public surface should then show the visible session contract, proof link,
and caveat that make the statement inspectable.

## Ordered Work Plan

| Order | Target surface | Change | Owner-ready acceptance criteria | Bead |
| --- | --- | --- | --- | --- |
| 1 | `README.md` first screen | Rewrite the hero/intro around the account-boundary wedge, session-contract backup line, why-now sentence, preview-before-mutation path, proof-today bullets, and alternate harness link. | First screen says the wedge once, distinguishes `init --dry-run` from `explain`, links to harnesses, and keeps proof/caveat language above feature sprawl. | `sandboxing-c0bd` |
| 2 | README proof stack | Add compact proof block using denied-secret transcript, recovery snippet, refreshed session-contract visual, and harness matrix link. | Reader can inspect one contained run, one denied credential boundary, one recovery path, and one deeper verification link without reading the full docs tree. | `sandboxing-9q2n`, blocked on `sandboxing-gs46`, `sandboxing-1cux`, `sandboxing-g39g` |
| 3 | Proof visuals and transcripts | Refresh quickstart/session-contract visuals and capture sanitized snippets. | Assets match current CLI output; snippets contain no secret bytes; live commands are not claimed unless explicitly approved and recorded. | `sandboxing-g39g`, `sandboxing-gs46`, `sandboxing-1cux` |
| 4 | `docs/harnesses.md` link path | Keep harness docs as the alternate-harness onboarding target from README and proof posts. | README and distribution drafts link here instead of duplicating seven setup flows; caveats for Hermes, Qwen, and Cursor Agent remain visible. | Existing docs; no new bead unless stale copy is found. |
| 5 | Demo/tutorial surfaces | Publish stack-specific proof stories for the top three distribution channels. | Each story has a recipe link, compatibility or fixture evidence, session-contract angle, and caveats for network/cache/Docker authority. | `sandboxing-a8bg`, `sandboxing-q97k`, `sandboxing-u0zt` |
| 6 | `CHANGELOG.md` and release notes | Add a proof-and-caveat convention for safety-facing bullets. | Release notes with containment, credential, setup, rollback, Docker, or platform claims include proof, caveat, and follow-up when evidence is incomplete. | `sandboxing-gg0x`, related `sandboxing-pa5g` for community evidence |
| 7 | GitHub Discussions starter surfaces | Prepare the pinned starter prompt and label/category map. | Discussions prompt asks for concrete workflows and safe evidence; labels separate recipes, compatibility, integration proposals, design-first work, and private security routing. | `sandboxing-8ylw`, `sandboxing-j5pf`, manual `sandboxing-dtri` |
| 8 | Community recognition loop | Add release-note convention for community evidence. | Community contributions are recognized for recipes, compatibility reports, incident evidence, and docs/UX findings without implying trust-boundary ownership. | `sandboxing-pa5g` |
| 9 | Public claim audit | Add maintainer checklist for README/docs/release/social copy. | Checklist rejects blanket secure/safe/formally verified claims, requires exact evidence for live smokes, and forces Docker/Linux/service-agent caveats. | `sandboxing-dgzs` |
| 10 | Social/distribution posts | Draft posts only after owned proof surfaces exist. | Each post links to an owned artifact, names one proof, names one caveat, and avoids current stars/adoption metrics unless verified at post time. | Use `sandboxing-a8bg`, `sandboxing-q97k`, `sandboxing-u0zt`; create post-specific beads only when a channel/date is chosen. |

## README Copy Requirements

The first README pass should not try to tell the whole story. It should make a
skeptical evaluator continue reading.

Required first-screen pieces:

- headline: "Run full-autonomy coding agents on your Mac without giving them
  your real account."
- backup line: "Hazmat shows the session contract first, then runs the agent
  as a dedicated macOS user inside OS-level containment."
- why-now line: "Approval prompts slow agents down, but they do not change what
  a running process can reach."
- fast path: install, `hazmat init --bootstrap-agent claude`, `hazmat claude`.
- preview path: `hazmat init --dry-run` for setup preview and `hazmat explain`
  for session-contract preview.
- proof today: dedicated `agent` user, Seatbelt policy, credential deny floor,
  `pf`/DNS controls, snapshots/rollback, seven harnesses, Docker Sandbox route.
- caveat link: limitations/testing/proof-boundary docs.

Avoid on the first screen:

- TLA+ as the headline.
- A long harness table.
- Docker details beyond the separate-boundary note.
- Blanket "secure" or "safe" claims.

## Demo And Tutorial Requirements

Each tutorial or distribution proof story should use the same frame:

```text
Workflow:
- <harness + stack + useful task>

Boundary:
- <what stays outside the agent's authority>

Proof:
- <recipe, compatibility row, transcript, session contract, test, or smoke>

Caveat:
- <network/cache/Docker/cloud/platform limit>
```

The first three should be:

- Next.js / Node: broad developer reach and easy project-write proof.
- Python / uv: strong Codex/Python fit and modern reproducible tooling story.
- Docker / database boundary: strongest security differentiation, with a
  strict note that native containment refuses host Docker socket shortcuts.

## Release And Changelog Requirements

Safety-facing release bullets should use this shape:

```text
### Proof
- <artifact> demonstrates <specific boundary>.

### Caveats
- <non-goal or unproven adjacent behavior>.

### Next
- <instrumentation, smoke, recipe, or compatibility follow-up>.
```

This is required for claims about:

- setup/rollback ordering;
- `hazmat check` / `hazmat doctor` behavior;
- credential delivery or cleanup;
- native launch isolation;
- Docker, Linux, service harnesses, or desktop GUI containment;
- formal verification.

## Distribution Sequence

Do not start with a broad launch post. Use the owned surfaces first:

1. README first-screen rewrite.
2. Proof-stack block and visuals.
3. Changelog/release-note convention.
4. Pinned community workflow prompt.
5. Next.js/Node proof story.
6. Python/uv proof story.
7. Docker/database boundary story.
8. Incident-to-control bulletin or security-proof post.

Borrowed/rented channels should wait until the target owned artifact exists.
Every post should link back to README, a recipe, compatibility evidence, a
testing/proof page, or an incident-to-control bulletin.

## Done Criteria For The Epic

The positioning epic is ready to close after this plan lands because it now has:

- chosen primary and backup wedges;
- public surface audit;
- proof-stack definition;
- first-month cadence;
- contributor/community loop;
- integration distribution priorities;
- milestone and proof messaging;
- trust/recovery/safety templates;
- an ordered implementation plan with follow-up beads.

Implementation beads remain open by design. They are the application of this
playbook, not prerequisites for the playbook itself.
