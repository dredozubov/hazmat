# Incident-to-Control Bulletin

This page is the repeatable format for turning public agent-security incidents,
CVEs, and research into Hazmat control work. It is not marketing copy and it is
not a vulnerability disclosure channel.

Use it when an incident says something concrete about:

- why approval prompts or trust dialogs were not enough
- which authority boundary failed
- which Hazmat control would have reduced the blast radius
- what Hazmat still does not contain
- which bead, doc, smoke, or model should change next

Use [SECURITY.md](../SECURITY.md) for private vulnerabilities. Use this page
only for public, already-discussable evidence.

## Bulletin Format

Each bulletin should stay short enough to read during triage.

| Section | Required content |
|---------|------------------|
| What happened | One concrete incident, CVE, research result, or attack chain |
| Why prompts failed | The permission, trust, or approval layer that was bypassed or mis-scoped |
| Hazmat controls | Existing controls that would reduce impact |
| Current gaps | Honest limitations and unsupported cases |
| Control work | Specific docs, tests, models, integrations, or beads to update |
| Evidence | Local source docs or external public links |

Do not use a bulletin to claim a control prevents more than it really does. If
the agent still has project write access, network egress, Docker authority, or
tool credentials, say that explicitly.

## Publishing Cadence

Publish one bulletin when there is enough evidence to teach a control lesson.
Monthly is a useful default, not a quota. A thin bulletin is worse than waiting
for a concrete incident-to-control mapping.

Good triggers:

- a new public CVE in an agent, MCP, Docker, IDE, or sandbox component
- a repeat user incident that exposes a recurring approval failure
- a new harness class that needs recipe-only, service-harness, or first-class
  adapter boundaries
- a live smoke or compatibility report that reveals a confusing failure mode

Bad triggers:

- generic "AI is risky" summaries
- incident lists without a Hazmat control implication
- private reports that have not completed disclosure triage
- claims that require unverified live behavior

## Bulletin 2026-06: Prompt Approval Is Not A Boundary

### What happened

The local evidence base tracks several public agent-security incidents where
approval prompts, trust dialogs, or app-level allowlists failed to be the real
boundary:

- Claude Code destructive-command incidents, including the Wolak `rm -rf /`
  case and the Reddit home-directory deletion case.
- PromptArmor's hidden `.docx` exfiltration chain.
- Claude Code CVEs where project files, settings, shell parsing, or URL/domain
  checks reached execution or exfiltration before the user-facing safety layer
  could be relied on.
- OpenClaw cases where localhost WebSockets, malicious skills, exposed
  instances, and credential directories became the attack surface.

The shared lesson is narrow: prompts are useful friction, but they are not the
security boundary for an autonomous coding agent.

Evidence:

- [docs/research/incidents-and-cves.md](research/incidents-and-cves.md)
- [docs/research/security-evidence.md](research/security-evidence.md)
- [docs/cve-audit.md](cve-audit.md)

### Why Prompts Failed

Approval UX tends to fail in three ways:

- The dangerous action happens before the prompt, such as project-file or
  settings-triggered initialization behavior.
- The prompt describes the wrong authority, such as approving a command without
  showing the home-directory, credential, network, or Docker boundary it can
  reach.
- The user intentionally bypasses prompts for speed, then the agent acts with
  the full authority of the primary user.

This is why Hazmat should not describe itself as "better prompts." The control
is structural authority reduction.

### Hazmat Controls

Existing controls that map to these incidents:

- Separate `agent` user so `~/` in the agent context is not the primary user's
  home.
- Native Seatbelt policy to keep filesystem writes limited to the project,
  declared agent-home paths, and session temp.
- Credential deny floor for high-value agent-home and host credential paths.
- Host-owned credential store and typed materialization instead of durable
  plaintext provider credentials in the agent home.
- `pf` network posture for native sessions, with Docker treated as a separate
  boundary rather than a native-session permission.
- Docker Sandbox routing for private-daemon Docker workflows and explicit
  refusal for shared host-daemon shortcuts.
- `hazmat check` / `hazmat doctor` repair plans that distinguish executable
  repairs from manual or unsupported findings.

These controls reduce blast radius. They do not make arbitrary agent autonomy
safe.

### Current Gaps

The remaining gaps matter for how Hazmat should talk about itself:

- Workspace files remain in scope. If a project contains secrets, the agent can
  still read them unless a future materialized-project view removes them before
  launch.
- Network controls are not a universal data-loss prevention system. Allowed
  destinations can still be abused.
- Docker Desktop and Compose are their own attack surface. Native containment
  must not grant the host Docker socket.
- Broad harness state is still tricky. Session-local HOME activation remains
  fail-closed for adapter-required state until typed adapters or documented
  exclusions cover the live machine.
- Service-style agents such as OpenHands need service-harness lifecycle,
  readiness, local attach, credential, and cleanup controls before first-class
  support.

### Control Work

Concrete work this bulletin reinforces:

- Keep `hazmat check` read-only and direct users to `hazmat doctor --fix` only
  for executable typed repairs.
- Keep live smokes opt-in and disclosure-first, including fixture checks that
  inspect local tool setup.
- Keep recipe-only agents from silently becoming first-class harnesses.
- Continue session-local HOME work only through typed adapter/materializer
  contracts, not broad copies or symlinks.
- Keep incident evidence mapped to docs and beads so public messaging stays
  grounded in controls.

### Reusable Summary

For public posts or release notes:

Hazmat treats approval prompts as an inner layer, not the trust boundary. The
outer layer is structural: separate user, default-deny filesystem policy,
credential isolation, explicit network and Docker boundaries, typed repair
plans, and live smokes that require deliberate approval. The open gaps are also
structural: workspace-secret exposure, allowed-network exfiltration,
shared-Docker authority, broad harness state, and service-agent lifecycle
support.
