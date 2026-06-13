# Hazmat Positioning Wedges

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.1`
**Status:** positioning decision for follow-up public-copy work

## Decision

Primary wedge:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

Backup wedge:

> Every agent run starts with a visible session contract: what it can read,
> write, reach, and recover from.

The primary wedge wins because a developer can repeat it after one read and it
contrasts directly with the status quo: same-user agent execution plus manual
approval prompts. It is broad enough for Claude, Codex, OpenCode, Gemini,
Hermes, Qwen, Cursor Agent, shell scripts, and future harnesses, while still
anchored in Hazmat's current macOS-native proof.

## Target Adopter

The first adopter is a macOS developer or security-minded team lead who already
uses coding agents locally and wants the productivity of full-autonomy modes
without treating a trusted workstation as disposable.

They do not need to be a sandboxing expert. They need a memorable answer to:

- "Can I let this agent run longer without watching every command?"
- "What happens if a prompt injection or dependency script goes sideways?"
- "Why is this better than just clicking approve or running in my normal shell?"

## Incumbent Contrast

The incumbent is not one tool. It is the operating habit of running local agents
as the primary user:

- manual approval mode that interrupts flow but is not a security boundary;
- bypass/yolo modes that unlock productivity by inheriting the user's whole
  home, shell, network, and credential surface;
- ad hoc Docker or VM setups that work for some repos but do not cover native
  macOS harnesses cleanly;
- same-user wrappers that describe intent without changing the host authority
  boundary.

Hazmat should contrast against that habit, not against a single vendor.

## Candidate Wedges

| Candidate | Type | One-line version | Audience that can repeat it | Strength | Weakness |
| --- | --- | --- | --- | --- | --- |
| Full autonomy without your real account | Broad aspiration | Run full-autonomy coding agents on your Mac without giving them your real account. | Agent-heavy macOS developers | Memorable, direct, product-level | Needs proof nearby so it does not sound like a slogan |
| Session contract first | Capability-specific | Every agent run starts with a visible contract for files, network, credentials, and rollback. | Security reviewers, teams evaluating controls | Differentiates the actual UX | Less emotional than the account-boundary line |
| Approval prompts are not a sandbox | Incumbent/status quo | Stop choosing between broken flow and fake safety prompts. Put the boundary below the agent. | Claude/Codex users who know permission friction | Sharp contrast with current pain | Can sound adversarial if used as the only headline |
| macOS-native containment for local agents | Category wedge | Open-source macOS-native containment for local AI agents and shell-heavy workflows. | Awesome-list maintainers, security researchers | Accurate, easy to categorize | More descriptive than compelling |
| Incident-to-control safety case | Evidence wedge | Turn agent-runtime incidents into concrete host controls you can inspect. | Security teams, researchers, maintainers | Builds trust and supports content strategy | Too abstract for first-time users |

## Primary Wedge Detail

**Audience:** macOS developers and small teams already using local coding
agents in Claude Code, Codex, OpenCode, Gemini, Hermes, Qwen, Cursor Agent, or
custom shell loops.

**Status quo:** The agent runs as the same user that owns SSH keys, cloud
credentials, browser state, source code, shell config, and local services.
Approval prompts add friction, but a compromised or over-trusted agent still
inherits the user's authority once a command runs.

**Promised outcome:** Users keep high-autonomy agent flow while moving the
execution boundary to a dedicated macOS user, kernel sandbox, network controls,
credential-deny rules, and pre-session rollback.

**Proof Hazmat can show today:**

- macOS native containment is executable today for `darwin/arm64` and
  `darwin/amd64`.
- The agent runs as a dedicated `agent` macOS user, not the invoker.
- Each session renders an inspectable contract before launch.
- Native sessions compile a per-session Seatbelt policy.
- Credential deny rules cover common secret paths, including agent-home secret
  surfaces.
- `pf` and DNS controls reduce common exfiltration/tunnel paths.
- Kopia snapshots support diff and restore after a run.
- Seven harnesses have documented containment support.
- Docker-heavy private-daemon workflows have a separate Docker Sandbox route
  instead of punching through to the host Docker socket.
- Proof boundaries, TLA-governed areas, and known limitations are explicit.

## Backup Wedge Detail

The session-contract wedge is the best backup because it turns a security
claim into a product interaction. It should appear immediately after the
primary line in README, demos, screenshots, and launch copy:

> Before the agent runs, Hazmat shows the contract: project writes, read-only
> extensions, integrations, network mode, credentials, and rollback.

Use this wedge when speaking to teams that need reviewability, policy
traceability, or predictable onboarding. It is also the strongest bridge to
future UI, doctor/check, and provider-alignment work because it makes the
contract the repeated product object.

## Repeatability Test

A first-time reader should be able to repeat this after one read:

> Hazmat lets me run autonomous coding agents on my Mac without giving them my
> real account. It shows the session contract first, then runs the agent as a
> contained `agent` user with rollback.

That is the target memory. Follow-up public-copy work should remove anything
that competes with it on the first screen.

## Copy Guardrails

Lead with:

- full-autonomy local agents;
- not giving the agent your real account;
- visible session contracts;
- macOS-native containment;
- blast-radius reduction and rollback.

Avoid leading with:

- a long feature list;
- formal methods as the headline;
- generic AI safety;
- prompt-injection detection;
- Docker or Linux claims beyond current support;
- "secure" without naming the boundary.

## Follow-Up Use

Use this decision as input to:

- README and first-screen audit (`sandboxing-cu4m.2`);
- demoable proof stack (`sandboxing-cu4m.3`);
- launch/release cadence (`sandboxing-cu4m.4`);
- contributor/community messaging (`sandboxing-cu4m.5`);
- integration-distribution targeting (`sandboxing-cu4m.6`);
- milestone and trust language (`sandboxing-cu4m.7`, `sandboxing-cu4m.8`);
- final executable public-copy plan (`sandboxing-cu4m.9`).
