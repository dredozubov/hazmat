# First-Month Release And Announcement Cadence

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.4`
**Status:** launch cadence plan, not public copy
**Inputs:**
[positioning wedges](2026-06-13-positioning-wedges.md),
[demoable proof stack](2026-06-13-demoable-proof-stack.md)

## Strategy

Hazmat should not spend the first month on one broad launch post. The stronger
path is a sequence of small public moments that repeat the same wedge while
adding new proof:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

Use owned channels first: README, changelog, docs, release notes, and GitHub
Discussions after the repository setting is enabled. Use borrowed/rented
channels only when the proof artifact behind that moment is ready.

## Cadence Summary

| Order | Moment | Audience | Proof point | Release-note angle | Prerequisite beads |
| --- | --- | --- | --- | --- | --- |
| 1 | Proof-stack README refresh | First-time macOS agent users | Short demo path, session contract, denied-secret/recovery snippets | "Hazmat now has a first-run proof path for full-autonomy local agents" | `sandboxing-gs46`, `sandboxing-1cux`, `sandboxing-g39g`, `sandboxing-9q2n` |
| 2 | Harness breadth update | Claude/Codex/OpenCode/Gemini/Hermes/Qwen/Cursor Agent users | Seven harnesses in one containment story with auth/import caveats | "Supported harness matrix and lifecycle flow are now the evaluator path" | `sandboxing-cu4m.3`, current `docs/harnesses.md`; no live smoke claims unless approved |
| 3 | Community contribution opening | Contributors, compatibility testers, recipe authors | Recipes, compatibility matrix, Discussions categories, public roadmap | "Hazmat has low-risk community surfaces that do not widen the trust boundary" | `sandboxing-dtri` manual GitHub setting, existing community/roadmap docs |
| 4 | Incident-to-control bulletin | Security researchers, appsec teams, newsletter editors | A real agent-runtime incident mapped to concrete Hazmat controls and gaps | "Agent approval prompts are not a boundary; here is the host control map" | `sandboxing-n249` first bulletin, ongoing monthly bulletin lane |
| 5 | Docker/private-daemon boundary note | Docker-heavy agent users, platform/security engineers | Native containment refuses host Docker socket; Docker Sandbox route handles private-daemon workflows | "Hazmat separates native local-agent containment from private-daemon Docker workflows" | Current Docker docs; additional proof only after Docker Sandbox smokes are approved |

## Moment 1: Proof-Stack README Refresh

**Target timing:** Week 1, before outreach.

**Audience:** Developers who arrive from README, GitHub lists, social posts, or
security newsletters and want to know whether Hazmat is real in minutes.

**Proof point:** A compact path from install/setup preview to contained session
contract, denied credential boundary, and recovery snippet.

**Release-note angle:** "First-run proof stack: install, inspect the session
contract, run a contained agent, verify denied credential access, inspect
recovery."

**Prerequisites:**

- `sandboxing-gs46` denied-secret transcript;
- `sandboxing-1cux` recovery snippet;
- `sandboxing-g39g` refreshed visuals;
- `sandboxing-9q2n` README proof-stack block.

**Hold back if:** the transcript requires live Hazmat behavior that is not
stable, contains secret-looking output, or cannot be captured without
sudo-adjacent commands the user has not approved.

## Moment 2: Harness Breadth Update

**Target timing:** Week 1 or 2, after the README proof stack.

**Audience:** Users who already have a preferred coding agent and need to know
whether Hazmat supports their path.

**Proof point:** The harness matrix shows seven supported or documented
foreground containment paths, with auth/import paths and caveats.

**Release-note angle:** "Hazmat is not only a Claude wrapper. It provides one
containment story across Claude, Codex, OpenCode, Gemini, Hermes, Qwen, Cursor
Agent, `hazmat exec`, and `hazmat shell`."

**Prerequisites:**

- current `docs/harnesses.md`;
- compatibility rows for the strongest starter recipes;
- no new "works" claims unless backed by tests, recipes, or approved live
  smokes.

**Hold back if:** a harness has stale tested-version claims, unsupported auth
state, or live-smoke evidence is missing. Use "works with caveats" instead of a
stronger claim.

## Moment 3: Community Contribution Opening

**Target timing:** Week 2.

**Audience:** People who like the project but are not ready to touch seatbelt,
`pf`, setup, rollback, or credential delivery.

**Proof point:** The project has a public contribution map: recipes,
compatibility reports, integration proposals, incident repros, and docs/UX
polish.

**Release-note angle:** "Community-scalable surfaces are open without opening
the containment boundary."

**Prerequisites:**

- `docs/community.md`;
- `docs/compatibility.md`;
- `docs/recipes/README.md`;
- `docs/public-roadmap.md`;
- GitHub Discussions enabled manually for Recipes, Compatibility reports, RFCs,
  and Security research (`sandboxing-dtri`).

**Hold back if:** Discussions are not enabled yet. In that case, announce the
docs and issue templates, but do not tell users to start Discussions threads.

## Moment 4: Incident-To-Control Bulletin

**Target timing:** Week 3.

**Audience:** Security researchers, appsec teams, newsletter editors, and
skeptical developers who need evidence rather than a product slogan.

**Proof point:** A real incident pattern maps to Hazmat controls, limitations,
and follow-up work. The first bulletin already frames "Prompt Approval Is Not A
Boundary."

**Release-note angle:** "Hazmat publishes incident-to-control notes so claims
stay tied to controls, gaps, and regression tests."

**Prerequisites:**

- `docs/incident-to-control-bulletin.md`;
- one short social/newsletter-safe summary;
- follow-up beads for any gap named in the bulletin.

**Hold back if:** the bulletin names a control that is not implemented or
overstates coverage. If the lesson is mostly a gap, publish it as a gap note,
not as a win.

## Moment 5: Docker/Private-Daemon Boundary Note

**Target timing:** Week 4 or later.

**Audience:** Users whose agent workflows involve Docker, Compose,
devcontainers, databases, or local service stacks.

**Proof point:** Hazmat gives a clear answer instead of a dangerous shortcut:
native containment does not expose the host Docker socket; private-daemon
Docker workflows use Docker Sandbox mode; shared-daemon workflows need Tier 4
or a code-only native session.

**Release-note angle:** "Docker is a different boundary, and Hazmat says so."

**Prerequisites:**

- `docs/overview.md`;
- `docs/tier3-docker-sandboxes.md`;
- `docs/shared-daemon-projects.md`;
- approved Docker Sandbox smoke evidence before stronger demo claims.

**Hold back if:** Docker Sandbox smokes are not approved or not passing on the
current machine. In that case, keep the note educational and caveated.

## Channel Plan

Owned:

- README and changelog for every moment;
- docs pages for proof details;
- GitHub Releases when a release is cut;
- GitHub Discussions after manual enablement.

Borrowed:

- awesome-list PRs only when the README proof stack is live;
- security newsletters after the incident-to-control bulletin is ready;
- MacAdmin or developer communities after contribution surfaces are clear.

Rented:

- short X/LinkedIn/Mastodon posts for each moment;
- Hacker News or Reddit only after the README proof path can stand on its own.

Every rented or borrowed post should link back to the owned proof path, not to a
long thread as the primary artifact.

## Holdback Rules

- Do not announce a proof artifact before it exists in the repo.
- Do not claim a live smoke passed unless the exact command was approved and
  the output was captured safely.
- Do not position Linux native launch as supported; it is plan-only until its
  readiness gates are satisfied.
- Do not describe Docker Sandbox as a transparent fallback from native
  containment.
- Do not lead with formal verification when the announced feature is outside
  the proof boundary.
- Do not invite community changes to trust-boundary code as a first
  contribution.

## Release Note Template

For each moment, keep the release note tight:

```text
### Added
- <artifact or feature> so <target adopter> can <promised outcome>.

### Proof
- <link to demo/screenshot/log/doc>

### Caveats
- <what this does not prove yet>
```

This makes each public moment repeat the wedge while keeping proof and caveats
visible.
