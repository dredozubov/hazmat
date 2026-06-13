# Milestone And Proof-Point Messaging

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.7`
**Status:** message bank and metric policy, not public copy
**Inputs:**
[positioning wedges](2026-06-13-positioning-wedges.md),
[demoable proof stack](2026-06-13-demoable-proof-stack.md),
[first-month cadence](2026-06-13-first-month-launch-cadence.md),
[integration channels](2026-06-13-integration-distribution-channels.md)

## Goal

Hazmat should turn milestones into evidence, not vanity. A milestone post or
release note should answer three questions:

1. What can a user inspect or run now?
2. What exact boundary does that prove?
3. What does it not prove yet?

The default wedge stays:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

Milestones should make that wedge more concrete through release artifacts,
harness coverage, verified models, recipes, diagnostics, and captured proof
snippets. They should not imply broad security guarantees or adoption numbers
that the project does not measure.

## Metric Policy

### Publishable Now

These are legitimate because they are repo-owned, inspectable, and can be
counted from tracked files:

| Proof point | Current source | Safe claim shape | Caveat |
| --- | --- | --- | --- |
| Releases and changelog entries | `CHANGELOG.md`, Git tags, GitHub Releases when cut | "This release adds..." | Do not imply adoption from release count. |
| Harness coverage | `docs/harnesses.md` | "Hazmat documents seven contained harness paths." | Narrow v1 harnesses have import/auth limits; mention them. |
| Built-in integrations | `docs/STACKS.md` | "Hazmat ships 27 built-in integration manifests." | Some are unit-tested or limitation-bearing, not all live-smoked. |
| Recipe coverage | `docs/recipes/` | "The repo includes 12 worked recipes." | Recipe presence is not a live pass result. |
| Promoted TLA+ specs | `tla/promoted_specs.tsv`, `tla/VERIFIED.md` | "Seventeen promoted specs govern specific containment, rollback, credential, and launch boundaries." | Never say all of Hazmat is formally verified. |
| Read-only diagnostics | `docs/testing.md`, `hazmat check`, `hazmat doctor --dry-run` | "Quick diagnostics are non-prompting and produce a repair plan." | Full validation is approval-gated and sudo-adjacent. |
| Approval-gated live smokes | `docs/testing.md`, captured transcripts | "This exact approved smoke passed on this machine." | Only claim a pass with the exact command, date, and sanitized output. |
| Session contract and proof snippets | README block after `sandboxing-9q2n`, denied-secret and recovery beads | "The proof path shows allowed project work plus denied credential access." | Keep transcripts synthetic or sanitized. |

### Needs Instrumentation

Do not publish these as metrics until the project has a durable measurement
source and an owner for keeping it current:

| Metric | Why it needs instrumentation | Possible source |
| --- | --- | --- |
| Active users, installs, or repeat usage | Hazmat does not collect telemetry by default. | Opt-in package/download analytics, if the project chooses to add them. |
| Star growth or current star count | External and time-sensitive. | GitHub API snapshot captured at post time. |
| Smoke pass rate over time | Current smokes are local and approval-gated. | CI artifact history plus local transcript ledger. |
| Compatibility report volume | Reports need a consistent intake channel. | GitHub Discussions/issues with labels and release-note rollups. |
| Time-to-fix or issue-to-PR conversion | Requires issue lifecycle hygiene. | Beads/GitHub mapping and release audit. |
| Real denied-secret incidents prevented | Counterfactual and high-risk to overstate. | Incident-to-control bulletins can discuss controls, not prevented harm. |

### Avoid

These claims create risk or outrun the proof boundary:

- "Secure", "safe", "airtight", "guaranteed", or "unbreakable" without a
  narrow object and proof source.
- "Hazmat is formally verified" as a blanket statement. Say which promoted
  specs govern which boundary.
- "Works with Docker" without stating native mode refuses host Docker socket
  shortcuts and Docker Sandbox is a separate boundary.
- "Linux native support" as shipped. The current Linux-native path is a
  readiness-gated plan, not a supported user-facing launch path.
- "Supports every agent" or "drop-in for all CLIs." Harness support is adapter
  owned and documented in `docs/harnesses.md`.
- "No sudo" for all flows. Quick diagnostics should be non-prompting, but init,
  rollback, full validation, and native helper-backed smokes are
  sudo-adjacent.
- "Live smoke passed" without exact-command approval, date, machine context,
  and sanitized output.

## Message Pattern

Use this structure for every milestone:

```text
<milestone in one sentence>

Proof:
- <repo-owned link, command transcript, spec, recipe, or release artifact>

Boundary:
- <what authority is constrained or made visible>

Caveat:
- <what this does not prove yet>
```

The proof and caveat should be near the top. If a post cannot include both, the
artifact is not ready for public amplification.

## Message Bank

### Star Milestone

Use only after verifying the number at post time.

```text
Hazmat crossed <N> GitHub stars.

The useful part is not the number by itself. The project now has an inspectable
proof path for running local coding agents under a dedicated macOS account:
seven documented harness paths, 27 built-in integration manifests, 12 worked
recipes, and a README proof stack that shows project work without broad home
authority.

Proof:
- <README proof-stack link>
- <harness matrix link>
- <integration stack link>

Caveat:
- Some live smokes are machine-specific and approval-gated; the post links only
  to captured or repo-owned evidence.
```

Avoid: "Thanks for trusting Hazmat to keep agents safe." The milestone proves
interest, not trustworthiness.

### Contributor Milestone

Use when a meaningful external contribution lands or a contribution surface is
opened.

```text
Hazmat now has a contribution path that does not start in the trust boundary.

Good first contributions are recipes, compatibility reports, integration
caveats, incident repros, docs, and UX traces. Setup/rollback, credential
delivery, launch isolation, and policy changes stay model-first and
maintainer-reviewed.

Proof:
- <community guide link>
- <recipe guide link>
- <compatibility guide link>

Caveat:
- Trust-boundary code is intentionally slower to accept because it needs model,
  test, and recovery evidence.
```

Avoid: inviting arbitrary plugins, unreviewed manifests, or project-owned
policy extensions.

### Release Milestone

Use for tagged releases and substantial changelog entries.

```text
Hazmat <version> is out.

This release improves <specific workflow> so <target user> can <specific
contained outcome>.

Proof:
- <changelog entry>
- <docs page or transcript>
- <test or spec reference>

Boundary:
- <the authority constrained, denied, or made explicit>

Caveat:
- <remaining unsupported path, approval-gated smoke, or platform limit>
```

Examples of strong release hooks:

- "Quick diagnostics stay read-only and non-prompting; full validation is an
  explicit sudo-adjacent path."
- "The harness lifecycle now preserves auth/profile data by default while
  removing only Hazmat-owned code artifacts."
- "Docker is handled as a separate boundary instead of exposing the host socket
  from native containment."

### Test Milestone

Use when a new automated gate, smoke, or transcript becomes a durable artifact.

```text
Hazmat added a new proof for <workflow>.

The test exercises <exact behavior> and fails closed when <boundary violation>
would occur.

Proof:
- Command: `<exact command>`
- Scope: <unit, hermetic e2e, prepared-host smoke, or live approved smoke>
- Output: <sanitized transcript link or CI artifact>

Caveat:
- <what the test cannot observe, such as OAuth browser state, host keychain
  contents, Docker internals, or Linux runtime behavior>
```

Never fold prepared-host or live smokes into generic "tests pass" language.
They require explicit approval and machine context.

### Security-Proof Milestone

Use when a specific control, model, or denied-access transcript is ready.

```text
Hazmat now has proof for <specific boundary>.

The claim is narrow: <one-sentence invariant or denied authority>.

Proof:
- <TLA+ design note or promoted spec>
- <implementation test>
- <sanitized runtime transcript, if available>

Caveat:
- This does not prove <adjacent subsystem or runtime internals>.
```

Good claim examples:

- "The native helper reaches `sandbox_init()` with only stdio plus
  helper-opened policy state in the model."
- "Quick diagnostics do not run sudo probes or helper-backed agent probes."
- "File-backed harness secrets are recovered before session launch if a prior
  session crashed with residue."

Bad claim examples:

- "Hazmat is formally verified."
- "Agents cannot escape."
- "Docker workflows are secured automatically."

### Integration Milestone

Use when a recipe, compatibility row, or integration proof story is ready.

```text
Hazmat now has a proof story for <stack>.

The workflow lets a contained agent <do useful project work> while <credential,
home, Docker, cluster, or cloud authority> stays outside the session.

Proof:
- <recipe link>
- <compatibility row or transcript>
- <session contract snippet>

Caveat:
- <network/cache/toolchain/Docker/cloud limit>
```

Best first targets:

- Next.js / Node: broad audience and easy project-write proof.
- Python / uv: strong Codex/Python fit and reproducible tooling story.
- Docker / database boundary: strongest differentiation, but only after
  approved Docker Sandbox evidence or as an explicitly educational caveat.

## Release-Note Block

Use this block inside release notes when a milestone is proof-oriented:

```text
### Proof
- <artifact> demonstrates <specific boundary>.

### Caveats
- <non-goal or unproven adjacent behavior>.

### Next
- <instrumentation, smoke, recipe, or compatibility follow-up>.
```

This keeps each release note from drifting into slogans and gives users a clear
path to reproduce or inspect the claim.

## Instrumentation Backlog

Create follow-up work before publishing growth metrics:

- Add a local smoke transcript ledger format with command, date, machine class,
  approval source, sanitized output path, and residual risks.
- Define a compatibility report intake label/category and monthly rollup.
- Decide whether GitHub star/install snapshots belong in release notes or only
  in outreach planning docs.
- Add a release-proof checklist that requires proof link plus caveat for every
  security-facing release bullet.
