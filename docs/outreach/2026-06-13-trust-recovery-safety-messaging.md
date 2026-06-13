# Trust, Recovery, And Safety Messaging

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.8`
**Status:** public-language templates and claim gates, not public copy
**Inputs:**
[positioning wedges](2026-06-13-positioning-wedges.md),
[milestone messaging](2026-06-13-milestone-proof-messaging.md),
[incident-to-control bulletin](../incident-to-control-bulletin.md),
[public roadmap](../public-roadmap.md)

## Goal

Hazmat needs public language that holds under stress: a regression, a failed
smoke, a sandbox-escape report, a misleading external claim, or an unsupported
platform request. The tone should be direct, narrow, and evidence-backed.

Use the primary wedge:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

Then immediately bind it to the proof boundary:

> Hazmat reduces the agent's host authority with a dedicated macOS user,
> session contract, Seatbelt policy, typed credential delivery, network/Docker
> boundaries, and rollback. It does not make arbitrary agent autonomy safe.

## Standing Principles

- Name the boundary before the benefit.
- Say "reduces blast radius" instead of "makes safe" unless a narrower proof
  supports the stronger phrase.
- Treat user reports as control evidence, not public-relations incidents.
- Separate quick diagnostics from sudo-adjacent full validation.
- Publish caveats next to claims, not in a separate limitations page.
- Keep unsupported configurations boring and explicit.
- Do not hide behind formal methods. Say which model, test, or trace backs the
  specific claim.

## Claim Gates

Public copy must pass one of these gates before it ships:

| Claim type | Evidence required | Example safe wording |
| --- | --- | --- |
| UX/command flow | Passing automated test or manually captured transcript | "`hazmat check` is read-only in quick mode and reports a repair plan." |
| Read/write boundary | Session contract plus unit/e2e coverage or sanitized smoke transcript | "The session contract shows project writes and credential-deny paths before launch." |
| Credential boundary | Credential lifecycle test, registry design doc, or TLA-backed lifecycle claim | "Provider keys are stored host-side and materialized only for matching sessions." |
| Setup/rollback ordering | TLA model plus implementation tests | "Setup and rollback order is model-governed; privilege is granted last and revoked first." |
| Native launch isolation | TLA launch/fd model plus implementation tests or approved smoke | "The native helper launch boundary is model-backed; runtime behavior still depends on macOS Seatbelt." |
| Live harness behavior | Exact approved command, date, machine context, sanitized output | "This prepared-host smoke passed on this machine with this command." |
| Integration support | Manifest, docs, tests, and caveats in `docs/STACKS.md` | "The `python-uv` integration opens uv caches; virtualenv policy remains project-specific." |
| Docker/database workflow | Docker Sandbox docs plus approved smoke for demo claims | "Native mode refuses host Docker socket shortcuts; Docker Sandbox is a separate boundary." |
| Platform support | Supported-platform docs and working release/install path | "macOS native containment is supported; Linux native launch remains readiness-gated." |
| Formal verification | Specific promoted spec and scoped invariant | "This invariant is covered by `MC_SetupRollback`; adjacent runtime internals are not." |

If a claim cannot name its evidence, it should not appear in public copy.

## Regression Warning Template

Use this when a release, commit, smoke, or user report shows behavior that may
weaken a promised boundary.

```text
We found a regression in <specific area>.

Impact:
- <who may be affected>
- <what boundary may be weaker, unavailable, or misleading>

What still holds:
- <controls that are not affected>

What to do now:
- <upgrade, downgrade, avoid command, run dry-run diagnostic, or wait>

Evidence:
- <issue, bead, test, transcript, or commit>

Next update:
- <expected follow-up trigger, not a vague promise>
```

Rules:

- Do not write "no evidence of exploitation" unless there is an actual
  investigation basis.
- Do not say "contained" if the affected path may have run as the primary user
  or reached host credentials.
- Prefer "do not use `<command>` for now" over ambiguous caution language.

## Hotfix Template

Use this when a fix has shipped.

```text
Hazmat <version> fixes <specific regression>.

Changed:
- <code path or command behavior>

Verified:
- <tests, TLA run, smoke, or transcript>

Remaining caveats:
- <unsupported path or evidence not yet collected>

Recommended action:
- <upgrade command or release link>
```

Rules:

- Pair every "fixed" statement with the evidence that caught or verifies it.
- If the fix touches setup, rollback, launch isolation, session permissions, or
  credential delivery, mention the model/test path used before implementation.
- If a live smoke remains approval-gated, say so directly.

## Security Report Acknowledgement

Use private channels for private reports. Use this public shape only after a
report is already safe to discuss.

```text
We received a report about <narrow subsystem>.

Status:
- <triaging, reproduced, fixed, not affected, unsupported configuration, or
  needs more evidence>

Boundary under review:
- <filesystem, credential, network, Docker, setup/rollback, launch, harness,
  or service-agent lifecycle>

User guidance:
- <safe current action>

Disclosure:
- <private report status, CVE status, or public issue link when appropriate>
```

Rules:

- Do not ask reporters to post secrets, transcripts, or host paths publicly.
- Do not reclassify a security report as a support issue until the boundary
  question is answered.
- If the report is about an unsupported mode, say whether unsupported means
  "refused by Hazmat" or "possible but not protected."

## Unsupported Configuration Template

Use this for Linux native launch, host Docker socket access, arbitrary harness
plugins, service-style agents, or unmodeled platform paths.

```text
Hazmat does not support <configuration> as a protected path today.

Why:
- <boundary Hazmat cannot currently prove or enforce>

Supported alternatives:
- <native supported path, Docker Sandbox path, recipe-only path, dry-run
  diagnostic, or docs link>

What would need to change:
- <model, adapter, smoke, compatibility evidence, or design work>
```

Examples:

- Linux native launch: readiness-gated until setup/rollback resources, launch
  provider behavior, and release artifacts are implemented and tested.
- Host Docker socket: refused in native containment; use Docker Sandbox for
  private-daemon workflows or Tier 4 for shared-daemon authority.
- Arbitrary plugins: unsupported because project-defined policy extensions
  would move trust-boundary code outside maintainer review.
- OpenHands first-class support: recipe-only until service-harness lifecycle,
  local attach, credential, readiness, and cleanup controls are implemented.

## Claim Boundary Correction

Use this when public copy, a post, or a third-party summary overstates Hazmat.

```text
Correction: <overstated claim> is too broad.

The narrower claim is:
- <exact supported statement>

Evidence:
- <docs/spec/test/transcript>

Boundary:
- <what remains unproven or unsupported>
```

Common corrections:

- Not "Hazmat is formally verified." Use "specific setup/rollback, launch,
  credential, policy, and service-harness lifecycle invariants are covered by
  promoted TLA+ specs."
- Not "Hazmat makes agents safe." Use "Hazmat reduces host authority and makes
  the session contract visible before launch."
- Not "Hazmat supports Docker." Use "native containment refuses host Docker
  socket shortcuts; Docker Sandbox is a separate private-daemon boundary."
- Not "Hazmat supports Linux." Use "Linux native launch is modeled as a future
  provider and remains readiness-gated."

## Incident-To-Control Post Template

Use after a public agent-security incident, CVE, or research result.

```text
Incident:
- <what happened, with source>

Why ordinary approval was not enough:
- <prompt, trust dialog, same-user execution, project config, network, Docker,
  or credential authority failure>

Hazmat control mapping:
- <existing controls that reduce blast radius>

Gaps:
- <what Hazmat still does not contain>

Follow-up:
- <beads, docs, tests, models, or recipes>
```

Rules:

- Do not imply Hazmat would have prevented the incident unless the exact attack
  path is modeled, tested, or reproduced.
- Do not turn another project's incident into victory copy.
- Always include at least one current gap.

## Recovery And Rollback Language

Use recovery language when discussing snapshots, rollback, doctor/check, or
session cleanup.

Safe wording:

- "Rollback and snapshots are recovery tools, not permission boundaries."
- "`hazmat check` should be read-only and non-prompting in quick mode."
- "`hazmat doctor --fix` applies approved typed executable repairs;
  `doctor --dry-run` previews the plan."
- "Full validation and prepared-host smokes are sudo-adjacent and require
  explicit approval."
- "If a session crashes with materialized file-backed credentials, startup
  recovery must clear residue before the next session."

Avoid:

- "Rollback makes risky runs safe."
- "Doctor fixes everything."
- "Check validates the whole host."
- "Live smokes are just tests."

## Public Release Checklist

Before publishing a safety-facing release note, README change, post, or
discussion pin, require:

- One primary claim.
- One proof link or exact command transcript.
- One caveat in the same section.
- One unsupported-path note if Docker, Linux, service agents, arbitrary
  plugins, or full desktop GUI containment are mentioned.
- No blanket "secure/safe/formally verified" language.
- No live-smoke pass claim without exact approved command and sanitized output.
- No suggestion that quick diagnostics may prompt for sudo.

## Short Reusable Phrases

- "Approval prompts are useful friction, not the containment boundary."
- "Hazmat moves the boundary below the agent process."
- "The session contract is the thing to inspect before autonomy."
- "Recovery is part of the workflow, not a substitute for least authority."
- "Docker is a separate authority boundary, not a binary to pass through."
- "Specific invariants are model-backed; adjacent runtime behavior still needs
  tests and traces."
- "Unsupported means unproven, not secretly enabled."
