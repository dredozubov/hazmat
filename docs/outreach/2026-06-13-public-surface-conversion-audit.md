# Public Surface Conversion Audit

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.2`
**Status:** audit and copy plan, not a README rewrite
**Input:** [Hazmat Positioning Wedges](2026-06-13-positioning-wedges.md)

## Evaluator Frame

Assume a first-time evaluator just heard:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

The public surfaces should answer four questions quickly:

1. What is it?
2. Why now?
3. Can I try it fast?
4. Is it alive and credible?

## Surface Inventory

| Surface | Current role | Conversion job |
| --- | --- | --- |
| `README.md` | Main landing page, quickstart, proof, limitations, community map | Carry the wedge, show the shortest trial path, prove the claim, and route deeper readers |
| `docs/overview.md` | Boundary/tier explainer | Help evaluators decide whether Hazmat is the right boundary for their repo |
| `docs/usage.md` | Full user guide | Support users after first conviction, not carry first-screen conversion |
| `docs/harnesses.md` | Harness setup matrix and auth paths | Prove breadth and give users a harness-specific first session path |
| `docs/compatibility.md` | Evidence-backed matrix | Show what is known to work, with caveats instead of broad promises |
| `docs/recipes/README.md` | Practical workflow examples | Give community-expandable, stack-specific trial paths |
| `docs/community.md` and `CONTRIBUTING.md` | Contribution model | Show the project can scale without opening the trust boundary |
| `docs/public-roadmap.md` | Curated work queue | Show visible direction and credible contribution opportunities |
| `CHANGELOG.md` | Release history | Prove release cadence and recent feature work |
| `SECURITY.md` | Private security reporting path | Show containment failures are treated as security issues |
| `docs/testing.md` | Verification map | Show what is automatically tested, manually smoked, live-gated, and proof-governed |

## Current Strengths

- The README already leads with the chosen primary wedge.
- The first screen names the trust boundary: dedicated macOS user plus
  OS-level containment.
- The quickstart is short and concrete.
- The session contract screenshot reinforces the backup wedge.
- "What Works Today" is unusually honest for an early-stage security tool.
- Limitations are visible, which improves trust.
- Changelog entries show recent release activity and breadth expansion.
- Community, compatibility, recipes, and public roadmap docs already make the
  project look contributor-shaped rather than private-research-only.

## Friction By Evaluator Question

### What Is It?

The README answers this, but it still competes with several category labels:
"containment layer," "runtime containment," "sandboxing," "secure local
execution," "agent workflows," "Tier 2," "Docker Sandbox," and "formal
verification." Those are accurate, but a first-time reader needs one dominant
memory first.

Recommendation:

- Keep the account-boundary wedge as the headline.
- Make the next sentence the backup wedge: every run starts with a visible
  session contract.
- Move feature-list language below proof, not above it.

### Why Now?

The README has a strong "Why This Exists" section, but it appears after
quickstart and session contract. That is acceptable for users who arrive with
the problem already in mind, but weaker for people arriving from a security
newsletter or awesome list.

Recommendation:

- Add a short above-the-fold "why now" line before quickstart:
  "Approval prompts slow agents down, but they do not change what the process
  can reach."
- Keep the CVE/incident evidence lower on the page so the first screen stays
  focused.

### Can I Try It Fast?

The README quickstart is short, but it assumes Claude as the first path and
does not tell the evaluator what to do if they are a Codex/OpenCode/Gemini user
until after the command block. It also does not distinguish side-effect-free
preview from setup that changes the machine strongly enough.

Recommendation:

- Keep the Claude quickstart as the default.
- Add a one-line "not ready to mutate this Mac?" preview path immediately under
  quickstart: `hazmat explain` for project/session preview and
  `hazmat init --dry-run` for setup preview.
- Add a compact "Using another harness?" link line before the detailed proof
  section, not after it.
- Route failed setup/check experiences directly to `hazmat doctor --fix`, with
  `hazmat doctor --dry-run` framed only as the preview path.

### Is It Alive And Credible?

Activity proof exists, but it is distributed across `CHANGELOG.md`, "What Works
Today," compatibility docs, testing docs, and the public roadmap. A first-time
reader can find it, but the README does not yet summarize the release cadence
or evidence stack in a compact block.

Recommendation:

- Add a short "Proof today" block near the top with:
  macOS native support, seven harnesses, Docker Sandbox route, session contract,
  rollback, and explicit verification boundaries.
- Add a "recently shipped" line that links to `CHANGELOG.md`.
- Keep public roadmap and compatibility links in the deeper "Read Next" table.

## Proposed Above-The-Fold README Structure

Use this order before the first major long-form section:

1. Logo and project name.
2. Primary wedge:
   "Run full-autonomy coding agents on your Mac without giving them your real
   account."
3. Backup wedge:
   "Hazmat shows the session contract first, then runs the agent as a dedicated
   macOS user inside OS-level containment."
4. One-sentence why now:
   "Approval prompts slow agents down, but they do not change what a running
   process can reach."
5. Fast path:

   ```bash
   brew install dredozubov/tap/hazmat
   hazmat init --bootstrap-agent claude
   cd your-project
   hazmat claude
   ```

6. Preview path:
   "Preview first: `hazmat init --dry-run` for setup, `hazmat explain` for the
   session contract."
7. Proof chips or short bullets:
   dedicated `agent` user, Seatbelt policy, credential deny, `pf`/DNS controls,
   snapshot/restore, seven harnesses, Docker Sandbox for private-daemon Docker.
8. Link line:
   "Codex/OpenCode/Gemini/Hermes/Qwen/Cursor Agent: see `docs/harnesses.md`."

This keeps the first screen conversion-oriented and lets the existing deeper
sections carry evidence, limitations, architecture, and contribution paths.

## Minimum Copy Blocks To Support The Wedge

### Hero Block

Must say:

- full-autonomy coding agents;
- on your Mac;
- without giving them your real account.

Should not say first:

- TLA+;
- seven harnesses;
- Docker;
- generic AI safety.

### Session Contract Block

Must show:

- project read-write scope;
- read-only extensions;
- integrations;
- network mode;
- snapshot/rollback status.

This block converts the backup wedge into a concrete product object.

### Fast Trial Block

Must include:

- install command;
- setup command;
- first contained session command;
- preview path before mutation;
- alternate harness link.

### Proof Today Block

Must include only currently true claims:

- macOS native containment today;
- seven supported harnesses;
- Docker Sandbox support for private-daemon workflows;
- typed credential stores and brokered Git capability language;
- tests/proof boundary link;
- known limitations link.

### Alive Project Block

Must include:

- latest release/changelog link;
- public roadmap link;
- compatibility/recipes/community links;
- private security reporting link.

## Public Surface Follow-Ups

- README rewrite should be a separate slice after the proof-stack bead so it
  can select the exact demo artifacts.
- `CHANGELOG.md` should be linked earlier from README when release cadence is a
  conversion goal.
- `docs/harnesses.md` should stay the alternate-harness onboarding target, not
  be duplicated in the README.
- `docs/overview.md` should stay the "which boundary" explainer for skeptical
  users and Docker-heavy workflows.
- `docs/testing.md` should be linked from proof claims, especially where live
  smokes require explicit approval.

## Acceptance Notes

The current public surfaces can support the chosen wedge with a README reorder
and a few small copy blocks. They do not need a new landing page. The conversion
path should make one promise first, prove it with the session contract and
current harness support, then route users to deeper docs only after they know
what Hazmat is for.
