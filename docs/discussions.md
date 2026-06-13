# GitHub Discussions

GitHub Discussions are for community knowledge that benefits from iteration
before it becomes a bug report, pull request, recipe, or compatibility row.

They are not a replacement for the private security reporting path in
[SECURITY.md](../SECURITY.md), and they are not a way to bypass the review
rules for containment, credential, setup, rollback, or network policy changes.

## Categories

Configure these categories when Discussions are enabled for the repo:

| Category | Use it for | Good first post shape | Move out when |
|----------|------------|-----------------------|---------------|
| **Recipes** | Practical harness + stack workflows that may become `docs/recipes/*.md` | Harness, stack, containment mode, needed integrations, extra scope, caveats | The recipe is stable enough for a docs PR |
| **Compatibility reports** | Host, harness, stack, and macOS reports that may update [docs/compatibility.md](compatibility.md) | macOS version, Hazmat version, harness, project shape, mode, exact evidence, caveats | There is a reproducible bug, missing integration, or confirmed matrix row |
| **RFCs** | Design proposals, integration ideas, testing architecture changes, and UX changes before implementation | Problem, proposed behavior, trust boundary, alternatives, tests, rollout plan | The proposal is ready for a tracked bead or PR |
| **Security research** | Public research, incident analysis, CVE mapping, attack-surface notes, and non-sensitive repros | Source links, affected surface, what Hazmat contains, what remains unsolved | The post describes a private vulnerability or concrete bypass |

Keep category names short and exact. If GitHub requires singular names, use
`Recipe`, `Compatibility report`, `RFC`, and `Security research`, but keep the
meaning and routing rules from this document.

## Routing Rules

Use **Issues** for:

- reproducible bugs
- docs defects
- integration requests that already have a concrete target
- harness requests with an identified CLI or protocol
- compatibility reports that already contain enough evidence for triage

Use **Discussions** for:

- recipe drafts that need feedback
- compatibility reports that need other users to confirm a host or harness shape
- RFCs that need design review before implementation
- public security research that does not disclose a live private vulnerability

Use **private security email** for:

- sandbox escapes
- credential leaks
- firewall bypasses
- privilege escalation
- unsafe setup or rollback ordering bugs

Do not ask reporters to copy a private vulnerability into Discussions before it
has been triaged privately.

## Expected Evidence

Posts are more useful when they include concrete evidence instead of general
claims.

For recipes:

- exact command used, such as `hazmat claude` or `hazmat exec -- ...`
- active integrations
- extra read-only or read-write scope
- known setup friction
- whether the workflow needs native containment, Docker Sandbox mode, or Tier 4

For compatibility reports:

- `sw_vers` output
- `hazmat --version`
- harness and harness version when available
- containment mode
- project markers, such as `go.mod`, `uv.lock`, or `package.json`
- whether evidence came from a real session, `hazmat explain`, a recipe, or a
  stack-matrix run

For RFCs:

- the user problem
- the authority boundary affected
- alternatives considered
- non-live tests
- any live or sudo-adjacent validation that must stay opt-in
- whether the change touches a TLA-governed area

For security research:

- source links and dates
- affected agent, harness, package manager, protocol, or OS surface
- what Hazmat contains today
- what Hazmat does not contain yet
- whether the finding is safe to discuss publicly

## Moderation And Conversion

Maintainers should convert a Discussion into a bead or PR only when the next
action is concrete. Good conversion examples:

- a recipe draft becomes a docs PR
- a compatibility report updates [docs/compatibility.md](compatibility.md)
- an RFC becomes a design note under `docs/plans/`
- a security research post becomes an incident-to-control writeup

Close or redirect Discussions that ask Hazmat to:

- import broad host profiles as a shortcut
- expose credential env vars through integrations
- grant Docker socket access in native containment
- make live or sudo-adjacent probes part of default checks
- treat an unverified recipe as a supported harness adapter

## Repo Setup Checklist

A repo maintainer still needs to do the GitHub-side setup:

1. Enable Discussions in repository settings.
2. Create the four categories above.
3. Pin the starter post in
   [docs/discussions-starter-prompt.md](discussions-starter-prompt.md).
4. Keep issue templates enabled for concrete bugs, compatibility reports,
   integration requests, harness requests, and docs / UX problems.
5. Keep private security reporting as the only route for vulnerabilities.

Until that repository setting is enabled, this document is the source of truth
for the intended category model.
