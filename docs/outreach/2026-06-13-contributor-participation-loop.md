# Contributor And Community Participation Loop

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.5`
**Status:** community growth design, not GitHub-side setup
**Inputs:**
[first-month launch cadence](2026-06-13-first-month-launch-cadence.md),
[community model](../community.md),
[GitHub Discussions plan](../discussions.md)

## Goal

Hazmat should make useful participation visible and low-friction without
creating busywork or inviting unsafe changes to the trust boundary.

The loop is:

```text
real workflow or evidence
  -> recipe / compatibility / integration / research post
  -> maintainer triage
  -> small docs, matrix, recipe, or bounded integration PR
  -> public recognition and next prompt
```

That keeps community energy pointed at artifacts that improve adoption and
evidence while leaving containment enforcement under stricter review.

## Contributor Entry Points

| Entry point | Best first action | Output | Review owner |
| --- | --- | --- | --- |
| Compatibility report | File the issue template or Discussion with macOS, harness, stack, mode, evidence, and caveats | Row or note in `docs/compatibility.md` | Maintainer or compatibility steward |
| Recipe draft | Start a Discussion or PR with one harness + one stack + commands + caveats | `docs/recipes/*.md` | Maintainer or recipe steward |
| Integration proposal | Use `docs/integration-author-kit.md` and keep fields inside the manifest contract | Built-in or proposed manifest docs/test update | Maintainer |
| Docs/UX friction | File the docs/UX template with the confusing command, output, or page | Small docs or CLI-copy issue | Maintainer |
| Incident evidence | Start a safe public research Discussion or private report depending on sensitivity | Incident-to-control bulletin or private security triage | Maintainer |
| Harness bring-up notes | Document install/auth/profile shape without changing code | Candidate evaluation note or harness request | Maintainer |

## Good-First Contribution Categories

Good first contributions should have a visible user benefit and low chance of
widening authority:

- add a real compatibility report for a harness + stack on a specific macOS
  version;
- improve or add a recipe with exact commands and caveats;
- add missing integration detection docs or snapshot-exclude notes;
- clarify a confusing check/doctor/explain message with before/after output;
- add a redaction-safe screenshot or transcript for the proof stack;
- turn a public incident into a control/gap mapping;
- tighten a troubleshooting entry after reproducing it.

Avoid tagging trust-boundary code as good first work. Seatbelt, `pf`, setup,
rollback, launch helpers, credential delivery, broker protocols, and TLA+
governed behavior are valid contributions, but they need design-first review.

## Review And Safety Boundaries

Every community path should answer two questions before review:

1. Does this change add evidence or usability without increasing session
   authority?
2. If it changes authority, which model, test, or maintainer-owned design gate
   owns that boundary?

Default review routing:

- **Fast path:** docs typo, recipe caveat, compatibility row with evidence,
  non-authority integration docs.
- **Normal path:** new recipe, new compatibility status, new integration
  manifest, harness candidate evaluation, docs that change support claims.
- **Design-first path:** harness adapter behavior, credential delivery, Docker
  routing, setup/rollback, session-home activation, service harnesses, network
  policy, Linux launch.
- **Private path:** sandbox escape, credential leak, firewall bypass,
  privilege escalation, unsafe setup/rollback ordering.

## Contributor Recognition Surfaces

Recognition should reward evidence and maintenance, not only code volume:

- changelog "Community" notes for recipes, compatibility reports, and incident
  evidence that influenced a release;
- README or docs acknowledgements only for durable, public contributions;
- compatibility matrix links back to reports or recipe PRs;
- Discussions accepted-answer or maintainer summary after converting a thread
  into a bead/PR;
- periodic "new recipes / new compatibility evidence" update in release notes;
- public roadmap references for community-open work.

Do not publish names for private security reports without explicit coordinated
disclosure agreement.

## Public Discussion Prompt

Use this as the first pinned prompt after Discussions are enabled:

```md
What should Hazmat support well enough to prove on your machine?

If you are using local coding agents on macOS, share one concrete workflow:

- agent or harness: Claude, Codex, OpenCode, Gemini, Hermes, Qwen, Cursor Agent, or other
- stack: language/framework/tooling
- repo shape: Docker/no Docker, database needs, local services
- current blocker: setup, auth, integration paths, network, credentials, rollback, docs
- evidence you can share: `hazmat explain`, recipe commands, compatibility report, or safe transcript

Please do not post private vulnerabilities, secret values, or containment bypass
details here. Use SECURITY.md for private security reports.
```

The prompt asks for workflows, not opinions. It should feed recipes,
compatibility rows, integration proposals, and harness evaluation beads.

## Label And Template Needs

Existing issue templates cover the main inputs. The remaining implementation
needs are:

- label map for `good-first-recipe`, `compatibility-report`,
  `integration-proposal`, `needs-evidence`, `design-first`, and
  `security-private-route`;
- pinned Discussions starter prompt after GitHub Discussions are enabled;
- a small release-note convention for community evidence.

## Anti-Patterns

Do not create participation loops around:

- broad "add support for everything" requests;
- plugin or manifest systems that load executable policy from repos;
- asking users to paste private security findings in public;
- encouraging host Docker socket passthrough because it makes a recipe easier;
- "works for me" compatibility claims without host/harness/version evidence;
- community PRs that silently promote experimental paths to supported status.

## Success Measures

The loop is working when:

- compatibility rows cite real evidence;
- recipes name caveats rather than hiding them;
- integration proposals stay inside the manifest contract;
- more public questions convert into beads or small PRs;
- maintainer review time is spent on clear artifacts, not untangling broad
  requests;
- security-sensitive reports route privately without public disclosure churn.
