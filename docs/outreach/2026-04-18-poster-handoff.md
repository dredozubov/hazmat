# Hazmat poster handoff

**Date:** 2026-04-18
**Owner:** operator
**Audience:** poster / outbound executor
**Status:** review-ready internal handoff — no external writes executed

## 1) What this handoff is

This is the **tightened Hazmat outbound handoff** for the person actually posting / pitching.

It intentionally removes:
- already-used repo PR targets
- language-specific and generic devtool stretches
- broad discovery surfaces that no longer feel like clean fits

It keeps only the surfaces that still feel clearly aligned with Hazmat’s real positioning:
- **macOS-native containment**
- **secure local execution**
- **sandboxing / runtime hardening**
- **blast-radius reduction for risky local execution and agent-heavy workflows**

This handoff should be treated as the current poster-facing shortlist.

---

## 2) Core positioning to preserve

### One-line positioning
**Hazmat is an open-source macOS-native containment layer for risky local execution and agent-heavy workflows.**

### Best language to use
Use some combination of these phrases early:
- secure local execution
- runtime containment
- sandboxed local execution
- blast-radius reduction
- macOS-native
- isolated users
- Seatbelt sandboxing
- PF firewall controls
- DNS restrictions / blocklists
- backup / rollback

### What Hazmat is *not*
Do **not** pitch Hazmat as:
- a generic AI safety platform
- an agent framework
- a prompt-injection scanner
- a language package or library
- a PHP / Python / Node / Ruby / Swift devtool
- a hosted sandbox
- a Linux container runtime
- a generic endpoint suite

---

## 3) Already used — do not re-open or re-pitch as fresh repo work

These repo PR targets have already been used and should be treated as **done / not fresh poster work**:

### Wave 1 — security / agent lists
- `bureado/awesome-agent-runtime-security#3`
- `ucsb-mlsec/Awesome-Agent-Security#3`
- `AgenticHardening/awesome-agentic-hardening#1`

### Wave 2 — macOS / shell dev
- `jaywcjlove/awesome-mac#1999`
- `alebcay/awesome-shell#695`

### Wave 3 — MacAdmins
- `smashism/awesome-macadmin-tools#55`
- `maccy10/definitivemacadmins#8`

**Poster rule:** do not spend time preparing copy for these again unless explicitly asked to follow up on status.

---

## 4) Active shortlist for the poster

## Primary target 1 — `restyler/awesome-sandbox`

- **Repo:** https://github.com/restyler/awesome-sandbox
- **Route:** PR or issue
- **Why it stays:** this is the cleanest remaining repo fit after the already-used waves
- **Fit:** very high

### Poster goal
Add Hazmat as a **macOS-native secure-local-execution / sandbox option** for risky local execution and agent-heavy workflows.

### Suggested PR title
```text
Add Hazmat as a macOS-native secure local execution option
```

### Suggested listing copy
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - macOS-native containment for risky local execution and agent-heavy workflows.
```

### Suggested PR note
```md
Possible addition for local secure execution on macOS:

[Hazmat](https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution. It is useful when AI agents, copied shell snippets, downloaded helper tools, browser-assisted setup flows, and one-off terminal commands would otherwise run with ordinary workstation and network authority on a trusted Mac.

Hazmat uses isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

### Poster notes
- Re-read the live README before opening the PR.
- Put Hazmat in the narrowest sandbox / secure-execution section available.
- Keep the PR body short and factual.
- Do not drift into language-specific framing.

---

## Primary target 2 — `tl;dr sec`

- **Surface:** https://tldrsec.com/
- **Route:** newsletter / editorial pitch
- **Why it stays:** strongest remaining editorial fit for sandboxing + agent-runtime hardening
- **Fit:** high

### Poster goal
Pitch Hazmat as a **macOS-native answer to the trusted-workstation problem** in agent-heavy local execution.

### Suggested subject line
```text
Possible tl;dr sec item: macOS-native containment for risky local agent execution
```

### Suggested pitch
```text
Hazmat (https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution.

It feels relevant to the current agent-security discussion because many teams are now running AI agents, copied shell commands, downloaded helper tools, browser-assisted setup flows, and other risky local workflows on the same trusted Mac that also holds source code, browser sessions, SSH keys, API tokens, and cloud credentials.

Hazmat creates a tighter execution boundary using isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

### Poster notes
- Keep it short.
- Lead with the real problem: risky local execution on trusted Macs.
- Do not over-explain the whole product.
- Do not position Hazmat as “another AI security scanner.”

---

## Reserve target 3 — `petarov/awesome-mdm-dev`

- **Repo:** https://github.com/petarov/awesome-mdm-dev
- **Route:** PR
- **Why it remains only reserve:** defensible Mac-admin fit, but weaker than pure sandbox / agent-runtime surfaces
- **Fit:** medium-high

### Poster goal
Position Hazmat as a **macOS-native containment layer for risky admin-side local execution**, not as an MDM product.

### Suggested listing copy
```md
- [Hazmat](https://github.com/dredozubov/hazmat) - open-source macOS-native containment for risky local admin tooling, scripts, and AI-assisted workstation execution in Apple fleet workflows.
```

### Suggested maintainer note
```md
Potential Apple/macOS tooling addition:

[Hazmat](https://github.com/dredozubov/hazmat) is an open-source macOS-native containment layer for risky local execution on managed Macs. It is useful when Mac admins, packaging/build workflows, remediation scripts, browser-assisted admin sessions, or AI-assisted scripting run on a trusted workstation and would otherwise inherit normal user and network authority.

Hazmat combines isolated macOS users, Seatbelt sandboxing, PF firewall controls, DNS restrictions/blocklists, backup/rollback, and formal-hardening work.
```

### Poster rule
Only use this if the poster wants a third action after the two primary targets above.

---

## 5) Explicitly out of scope for this poster pass

Do **not** spend time on these in this pass:

### Already-used repo lanes
- `bureado/awesome-agent-runtime-security`
- `ucsb-mlsec/Awesome-Agent-Security`
- `AgenticHardening/awesome-agentic-hardening`
- `jaywcjlove/awesome-mac`
- `alebcay/awesome-shell`
- `smashism/awesome-macadmin-tools`
- `maccy10/definitivemacadmins`

### Cut for fit reasons
- language-specific lists/newsletters: Python, PHP/Laravel, Node/TypeScript, Ruby/Rails, Swift/iOS
- generic devtool lists
- broader maker / CAD / 3D-printing lanes
- generic launch surfaces unless explicitly requested

### Not for this poster pass unless asked later
- GitHub topic metadata updates (`sandbox`, `agent-security`, `macos-security`)
- long-form editorial development like `InfoQ`
- broad directory submissions like `DeepNLP`

---

## 6) Recommended posting order

If the poster has limited time:

1. `restyler/awesome-sandbox`
2. `tl;dr sec`
3. `petarov/awesome-mdm-dev` only if there is still capacity

If there is time for only **one** action, do:
- **`restyler/awesome-sandbox`**

If there is time for only **two** actions, do:
- **`restyler/awesome-sandbox`**
- **`tl;dr sec`**

---

## 7) Tracking checklist

- [ ] `restyler/awesome-sandbox` opened
- [ ] `tl;dr sec` pitched
- [ ] `petarov/awesome-mdm-dev` opened (reserve)

For each action, capture:
- date sent/opened
- exact URL
- exact wording used
- response / status
- merged / pending / declined

---

## 8) Source context

Main source packets behind this handoff:
- `state/hazmat/batches/2026-04-11/hazmat-distribution-batch-25.md`
- `state/hazmat/review/2026-04-17/hazmat-github-handoff.md`
- `state/hazmat/batches/2026-04-18/hazmat-distribution-batch-133.md`
- memory correction logged on `2026-04-18` for already-used PR targets

## Outcome

Created one tightened poster-facing Hazmat handoff with:
- already-used repos removed from the active queue
- 2 primary actions
- 1 reserve action
- exact copy for the poster
- explicit do-not-touch / out-of-scope guidance
