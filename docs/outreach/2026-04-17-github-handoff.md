# Hazmat GitHub handoff

**Date:** 2026-04-17
**Owner:** operator
**Status:** review-ready internal handoff — no external writes executed
**Audience:** Hazmat team
**Scope:** GitHub-native execution only

## 1) What this handoff is

This is the cleanest GitHub-first execution packet for Hazmat based on already-validated research.

It intentionally focuses on **GitHub-native actions** the team can execute directly:
- pull requests to relevant awesome lists and curated repos
- issue-first submissions where a repo prefers issues over direct PRs
- short, maintainable listing copy that preserves Hazmat’s positioning

It intentionally excludes non-GitHub surfaces for now:
- newsletters / editorial pitches
- HN / Slashdot / Dev Hunt
- non-GitHub directories and marketplaces

Those can be run as a separate distribution wave.

---

## 2) Hazmat positioning to keep consistent everywhere

### Core one-line positioning
**Hazmat is an open-source macOS-native containment layer for AI agents and coding-agent workflows.**

### Lead ideas
Use some combination of these phrases early:
- runtime containment
- secure local execution
- bounded local execution
- blast-radius reduction
- sandboxing / isolation / capability confinement
- macOS-native

### Concrete controls to mention
- isolated macOS users
- Seatbelt sandboxing / kernel-enforced isolation
- PF firewall controls
- DNS restrictions / blocklists
- backup / rollback
- formal-hardening / TLA+-oriented work

### Do not pitch Hazmat as
- a generic AI safety platform
- an agent framework
- a prompt-injection scanner
- a hosted SaaS
- a generic endpoint-security suite
- an MDM / Jamf / RMM replacement

---

## 3) Recommended execution order

### Wave 1 — canonical high-fit GitHub PRs
Open these first. This is still the strongest GitHub-native Hazmat stack.

1. `bureado/awesome-agent-runtime-security`
2. `ucsb-mlsec/Awesome-Agent-Security`
3. `AgenticHardening/awesome-agentic-hardening`
4. `restyler/awesome-sandbox`

### Wave 2 — developer/macOS discovery GitHub lists
Open after Wave 1 is live.

5. `jaywcjlove/awesome-mac`
6. `alebcay/awesome-shell`

### Wave 3 — MacAdmins / Apple fleet GitHub lists
Open after Wave 2 or in parallel if the team wants a Mac-admin-specific push.

7. `smashism/awesome-macadmin-tools`
8. `petarov/awesome-mdm-dev`
9. `maccy10/definitivemacadmins`

---

## 4) Exact GitHub targets and copy

## Wave 1 — canonical high-fit GitHub PRs

### Target 1 — bureado / awesome-agent-runtime-security
- **Repo:** https://github.com/bureado/awesome-agent-runtime-security
- **Route:** pull request
- **Suggested section:** `Sandboxing & Isolation`
- **Why first:** cleanest direct category fit in the whole GitHub inventory

**Proposed listing copy**
```md
| [Hazmat](https://github.com/dredozubov/hazmat) | macOS, Seatbelt, PF firewall, isolated users, rollback | macOS-native runtime containment for AI agents and coding-agent workflows using isolated users, Seatbelt sandboxing, PF firewall controls, DNS blocklists, backup/rollback, and TLA+-oriented hardening to reduce host and network blast radius. |
```

**Suggested PR note**
```md
Adding Hazmat under Sandboxing & Isolation. It gives macOS agent operators a host-side containment layer for local coding-agent execution, with isolated users, kernel-enforced Seatbelt policies, PF/DNS controls, and rollback-oriented recovery.
```

**Confidence:** very high

---

### Target 2 — ucsb-mlsec / Awesome-Agent-Security
- **Repo:** https://github.com/ucsb-mlsec/Awesome-Agent-Security
- **Route:** pull request
- **Suggested section:** `Blue-teaming → System-level Runtime Defense`
- **Why second:** strongest research/security-credibility placement in the current stack

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - open-source macOS-native containment layer for AI agents and coding-agent workflows, focused on isolated users, Seatbelt sandboxing, PF firewall controls, DNS blocklists, backup/rollback, and reduced host-side blast radius.
```

**Suggested PR note**
```md
Adding Hazmat as a practical system-level runtime defense for local AI-agent execution on macOS. It complements model-centric defenses by constraining host and network actions once the agent can already execute tools or code.
```

**Confidence:** very high

---

### Target 3 — AgenticHardening / awesome-agentic-hardening
- **Repo:** https://github.com/AgenticHardening/awesome-agentic-hardening
- **Route:** pull request
- **Suggested section:** `Hardening Techniques → Runtime Sandboxing & Capability Confinement`
- **Why third:** exact thematic fit around practical hardening and runtime confinement

**Proposed listing copy**
```md
| [Hazmat](https://github.com/dredozubov/hazmat) | 🔧 Tool | macOS-native runtime containment for AI agents and coding-agent workflows using isolated users, Seatbelt sandboxing, PF firewall controls, DNS blocklists, backup/rollback, and TLA+-oriented hardening to reduce host and network blast radius. |
```

**Suggested PR note**
```md
Adding Hazmat under Runtime Sandboxing & Capability Confinement. It is an open-source macOS-native containment layer for local AI-agent and coding-agent execution, focused on isolated users, Seatbelt policies, PF/DNS controls, and rollback-oriented blast-radius reduction.
```

**Confidence:** very high

---

### Target 4 — restyler / awesome-sandbox
- **Repo:** https://github.com/restyler/awesome-sandbox
- **Route:** pull request
- **Suggested section:** secure code execution / platform-level isolation section that best matches the live README
- **Why fourth:** strongest sandboxing-focused add-on outside the core first three lists

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) — macOS-native runtime containment for AI agents and coding-agent workflows using isolated users, Seatbelt sandboxing, PF firewall controls, DNS blocklists, backup/rollback, and TLA+-oriented hardening to reduce host and network blast radius.
```

**Suggested PR note**
```md
Adding Hazmat as a macOS-native secure-execution / containment option for local AI-agent and coding-agent workflows. It focuses on host-side blast-radius reduction via isolated users, Seatbelt policies, PF/DNS controls, and rollback-oriented recovery.
```

**Confidence:** very high

**Reviewer note:** confirm the current section headings immediately before opening the PR.

---

## Wave 2 — developer/macOS discovery GitHub lists

### Target 5 — jaywcjlove / awesome-mac
- **Repo:** https://github.com/jaywcjlove/awesome-mac
- **Route:** pull request or issue
- **Suggested section:** `Security Tools`
- **Fallback section:** `Developer Utilities` if the maintainer prefers workflow framing over security framing
- **Why here:** strong fit for a Mac-native developer-side control around local agents and shell-heavy workflows

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - macOS-native runtime containment for AI-agent and coding-agent workflows, using isolated users, Seatbelt sandboxing, PF firewall controls, DNS restrictions, and rollback to reduce local blast radius.
```

**Suggested PR note**
```md
Adding Hazmat as a macOS-native secure-local-execution tool for developers using AI agents and coding agents.

Hazmat focuses on reducing local blast radius on macOS via isolated users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work. It fits especially well for local shell-heavy or agent-driven workflows where generated code and commands would otherwise run with normal user authority.
```

**Confidence:** high

---

### Target 6 — alebcay / awesome-shell
- **Repo:** https://github.com/alebcay/awesome-shell
- **Route:** pull request or issue
- **Suggested section:** `System Utilities` or `For Developers`
- **Why here:** strong CLI-native fit for generated shell tasks, risky local automation, and agent-driven command execution

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - macOS-native secure-local-execution tooling for AI-agent and coding workflows, helping contain generated shell commands, local automation, and risky command-line tasks.
```

**Suggested PR note**
```md
Useful command-line collection.

One tool that may fit under System Utilities or For Developers is Hazmat:
https://github.com/dredozubov/hazmat

Hazmat is an open-source macOS-native containment layer for AI-agent and coding-agent workflows. It is especially relevant for command-line-heavy development because generated scripts, local automation, and agent-issued shell commands otherwise run with normal user and network authority on a trusted Mac.

Hazmat focuses on isolated users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work to give local execution a bounded runtime on macOS.
```

**Confidence:** high

---

## Wave 3 — MacAdmins / Apple fleet GitHub lists

### Target 7 — smashism / awesome-macadmin-tools
- **Repo:** https://github.com/smashism/awesome-macadmin-tools
- **Route:** pull request
- **Suggested section:** relevant command-line, utility, deployment, or security-adjacent section based on live README
- **Why here:** cleanest direct Mac-admin GitHub fit in the current inventory

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - macOS-native containment for risky local admin automation, scripts, installers, and AI-assisted workstation workflows on trusted Macs.
```

**Suggested maintainer note**
```md
Possible addition for Mac admins working with local automation and risky tooling:

[Hazmat](https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution, admin scripts, installers, browser-assisted workflows, and AI-assisted workstation tasks. It helps reduce blast radius when Mac admin tools, generated shell commands, downloaded utilities, or remediation workflows would otherwise run with ordinary user and network authority.

Hazmat uses isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

**Confidence:** very high

---

### Target 8 — petarov / awesome-mdm-dev
- **Repo:** https://github.com/petarov/awesome-mdm-dev
- **Route:** pull request
- **Suggested section:** Apple tools / Apple-management tooling section in the live README
- **Why here:** strong open-source Apple-management fit without pretending Hazmat is an MDM server

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - open-source macOS-native containment for risky local admin tooling, scripts, and AI-assisted workstation execution in Apple fleet workflows.
```

**Suggested maintainer note**
```md
Potential Apple/macOS tooling addition:

[Hazmat](https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution on managed Macs. It is useful when Mac admins, packaging/build workflows, remediation scripts, browser-assisted admin sessions, or AI-assisted scripting run on a trusted workstation and would otherwise inherit normal user and network authority.

Hazmat combines isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

**Confidence:** high

---

### Target 9 — maccy10 / definitivemacadmins
- **Repo:** https://github.com/maccy10/definitivemacadmins
- **Route:** pull request
- **Suggested section:** tools, security, or automation-relevant section in the live README
- **Why here:** broad MacAdmins directory with community-friendly maintenance model

**Proposed listing copy**
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - macOS-native secure-local-execution boundary for Apple admin scripts, installers, browser-assisted workflows, and other risky workstation tasks.
```

**Suggested maintainer note**
```md
Suggested tool for Apple systems administrators:

[Hazmat](https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution. It helps Apple systems administrators reduce blast radius when scripts, installers, browser-assisted admin tasks, downloaded utilities, or AI-assisted workstation workflows would otherwise run with ordinary workstation authority.

Hazmat uses isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

**Confidence:** high

---

## 5) Execution guidance for the team

### Open order
- Open **Wave 1** first.
- Once those are live, open **Wave 2**.
- Run **Wave 3** as the Mac-admin-specific expansion.

### Per-PR hygiene
- Re-read the live README before opening the PR.
- Land Hazmat in the narrowest accurate section.
- Keep the PR body short and factual.
- Do not over-explain the whole product if a one-line listing plus a tight note is enough.
- Match the formatting style already used by the target repo.

### Messaging discipline
- On agent-security lists, lead with **runtime defense / containment / blast-radius reduction**.
- On developer/macOS lists, lead with **secure local execution for local agents, shell tasks, and automation on macOS**.
- On MacAdmins lists, lead with **safer execution for scripts, installers, browser-assisted admin work, and AI-assisted workstation tasks**.

### Practical batching
- Recommended burst size: **4 PRs in Wave 1**, then pause for maintainer feedback.
- If maintainers are receptive, open **Wave 2 and Wave 3** next.
- If a maintainer asks for wording changes, keep the same positioning but adapt to the repo’s taxonomy.

---

## 6) Suggested tracking checklist

- [ ] `bureado/awesome-agent-runtime-security`
- [ ] `ucsb-mlsec/Awesome-Agent-Security`
- [ ] `AgenticHardening/awesome-agentic-hardening`
- [ ] `restyler/awesome-sandbox`
- [ ] `jaywcjlove/awesome-mac`
- [ ] `alebcay/awesome-shell`
- [ ] `smashism/awesome-macadmin-tools`
- [ ] `petarov/awesome-mdm-dev`
- [ ] `maccy10/definitivemacadmins`

For each target, capture:
- PR URL
- date opened
- exact section used
- maintainer response
- merge / reject / pending status

---

## 7) What is deliberately not in this GitHub handoff

These are valid Hazmat opportunities, but they are **not GitHub-native** and should stay in a separate distribution packet:
- `Dev Hunt`
- `Hacker News`
- `Slashdot`
- `console.dev`
- `tl;dr sec`
- `DeepNLP AI Agent Marketplace`
- `MacAdmins.news`
- `Scripting OS X`

That separation keeps this handoff clean for a team that just wants the GitHub execution queue.

---

## 8) Source packets

Primary sources used for this handoff:
- `state/hazmat/batches/2026-04-11/hazmat-distribution-batch-25.md`
- `state/hazmat/batches/2026-04-17/hazmat-distribution-batch-110.md`
- `state/hazmat/batches/2026-04-17/hazmat-distribution-batch-116.md`

## Outcome

Created one consolidated GitHub-native Hazmat handoff with:
- 9 prioritized GitHub targets
- exact suggested listing copy
- exact suggested PR/maintainer notes
- sequencing guidance by wave
- positioning guardrails so the Hazmat story stays consistent across repos
