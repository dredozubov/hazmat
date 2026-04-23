# Public Roadmap

_Generated from bd issues via `scripts/export-public-roadmap.sh`._

This is a curated subset of Hazmat work. It is meant to make real contribution opportunities visible without dumping the entire local issue database into the repo.

## Community contribution surfaces

These are the best places for outside contributors to start. They grow the ecosystem without widening Hazmat's trust boundary.

### Publish community support tiers and ownership model (`sandboxing-u57l`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Document Hazmat's contribution model in repo docs using explicit support tiers: Verified Core, Supported, Community, Experimental. Spell out which areas stay maintainer-owned (trust boundary, TLA-governed behavior, rollback, secret delivery, seatbelt/pf policy) and which areas are good community surfaces (integrations, recipes, compatibility reports, docs, incident repros, benchmark data). Keep the language precise enough that contributors understand where patches are welcome and where security review is required.

### Unify security reporting docs and issue templates (`sandboxing-jtby`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Remove the current trust inconsistency where CONTRIBUTING.md tells people to open public issues for security problems while SECURITY.md and GitHub issue-template config say to email privately. Update repo docs and issue templates so the private reporting path is consistent everywhere, and public templates steer security reports away from public issues.

### Ship integration author kit and manifest contract checks (`sandboxing-qj42`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Turn integrations into Hazmat's first community wedge. Add author-facing docs covering manifest schema, validation rules, accepted fields, path/env safety constraints, required tests, review checklist, and maintainer expectations. Add repo-level checks or tests that keep built-in and future community manifests inside the documented contract.

### Add compatibility program and reporting template (`sandboxing-fms6`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Create a contributor-facing compatibility program for Hazmat. Define status meanings such as works, works with caveats, native only, needs Tier 4, unsupported; publish an initial public matrix shape; and add an issue template for compatibility reports that captures harness, stack, macOS version, containment mode, caveats, and repro details.

### Launch Hazmat recipes library (`sandboxing-4g49`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Create a lightweight recipes library for common contained workflows such as Claude + Next.js, Codex + uv, OpenCode + Go, and Gemini + TLA+. Recipes should be practical docs that show what integrations to enable, what extra read/write scope is typical, what mode to use, and what caveats are known. This is intended as a low-risk, community-expandable contribution surface.

### Enable GitHub Discussions and document categories (`sandboxing-dtri`)

- Status: `open`
- Priority: `P2`
- Type: `task`
- Summary: Turn on GitHub Discussions for the Hazmat repo and organize initial categories for Recipes, Compatibility reports, RFCs, and Security research. Add repo docs that explain what belongs in each category. This likely requires a manual GitHub repo setting change in addition to git-tracked docs.

### Define sponsor lanes and maintainer program (`sandboxing-6s13`)

- Status: `closed`
- Priority: `P2`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Map project funding asks to explicit ownership lanes such as Linux backend, harness maintainers, compatibility stewards, formal verification work, and researcher bounties. Add repo-visible documentation so potential sponsors understand what support buys and what remains maintainer-controlled.

## Maintainer-owned trust boundary work

These items matter, but they touch containment, rollback, secret handling, or the proof boundary and need deeper maintainer review.

### Narrow agent-home seatbelt rules without moving HOME (`sandboxing-93r8`)

- Status: `open`
- Priority: `P1`
- Type: `feature`
- Summary: Implement the low-risk first half of the session-home hardening plan from docs/plans/2026-04-10-brokered-capability-hardening-design.md. Replace the blanket seatbelt allow over /Users/agent with explicit allow rules for the persistent agent-home paths Hazmat intentionally supports, while keeping HOME=/Users/agent and preserving the current durable transcript/export, shell RC, and XDG path layout. This issue is specifically the safe seatbelt-tightening slice and does not move HOME or XDG roots into /private/tmp.

### Move harness and Git credentials to host-owned secret storage (`sandboxing-gg16`)

- Status: `open`
- Priority: `P1`
- Type: `feature`
- Summary: Implement the host-owned secret store and credential delivery model from docs/plans/2026-04-10-brokered-capability-hardening-design.md without creating an upgrade cliff for existing users. Stop storing long-lived Claude/OpenCode auth material, Anthropic API keys, and Git HTTPS credentials as readable files under /Users/agent. Add a host-owned Hazmat secret store, a brokered Git credential helper, harness adapter hooks for per-session auth delivery, and automatic first-launch migration or compatibility reads for legacy agent-home auth state. Update import/config commands and docs so non-secret portable assets still import while durable secrets move out of the persistent agent home.

### Replace managed Git SSH runtime with brokered transport capability (`sandboxing-n1xy`)

- Status: `open`
- Priority: `P1`
- Type: `feature`
- Summary: Implement the brokered Git SSH design from docs/plans/2026-04-10-brokered-capability-hardening-design.md without regressing the current managed Git SSH UX. Replace the current session-local ssh-agent socket and generated git-ssh wrapper with an immutable helper plus host-side per-session broker that enforces host/port/Git verb restrictions and performs the actual SSH connection outside the contained session. Preserve normal git clone/pull/push UX, keep arbitrary SSH shells unsupported, and preserve working ProxyJump-based Git flows that native managed Git SSH already supports today. If a broker rollout ever cannot preserve that behavior, Hazmat must ship a separate blocking deprecation path before removal rather than silently dropping it. Broker startup and policy failures must be concrete and actionable rather than generic launch failures.

### Model and implement Linux setup/rollback resources before publishing artifacts (`sandboxing-pk5x`)

- Status: `open`
- Priority: `P1`
- Type: `task`
- Summary: Linux install and release artifacts are intentionally disabled by the platformed installer/release refactor. Before enabling them, extend MC_SetupRollback or add an explicitly scoped Linux setup/rollback model for the Linux resource actions, implement the Linux native backend resources behind the platform interfaces, and only then enable Linux install/release artifacts.

### Define harness adapter RFC without opening arbitrary plugins (`sandboxing-lksm`)

- Status: `open`
- Priority: `P2`
- Type: `task`
- Summary: Write a design note for a future harness adapter contract that keeps Hazmat's trust boundary maintainer-owned. The RFC should define what a harness adapter may configure, what stays built-in, and why Hazmat is not opening an arbitrary plugin model yet. This is documentation/design work, not an implementation change.

## Evidence and category-building

Hazmat's moat is evidence-backed safety. These items turn incidents and open questions into durable public material.

### Export curated public roadmap from beads (`sandboxing-owyu`)

- Status: `closed`
- Priority: `P1`
- Type: `task`
- Assignee: `Denis Redozubov`
- Summary: Make Hazmat's near-term roadmap visible outside local bd usage. Add a repo-visible public roadmap page generated or curated from beads issues, with a bounded process for keeping it current. The public output should avoid leaking internal-only work while making contribution opportunities legible to outside contributors.

### Publish monthly incident-to-control bulletin (`sandboxing-n249`)

- Status: `open`
- Priority: `P2`
- Type: `task`
- Summary: Create a repeatable content format that ties real incidents and CVEs to Hazmat's controls and current gaps. The bulletin should answer what happened, why ordinary approval prompts failed, what Hazmat contains, and what remains unsolved. This is meant to feed the evidence-based content flywheel without turning into generic marketing.

