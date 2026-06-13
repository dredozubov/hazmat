# Demoable Proof Stack For First-Time Evaluators

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.3`
**Status:** proof-stack definition, not a live demo run
**Inputs:**
[positioning wedges](2026-06-13-positioning-wedges.md),
[public surface conversion audit](2026-06-13-public-surface-conversion-audit.md)

## Goal

A curious developer should be able to understand and verify Hazmat's first
promise within a few minutes:

> Run full-autonomy coding agents on your Mac without giving them your real
> account.

The proof stack should avoid a long feature tour. It should show one contained
session, one visible contract, one denied credential boundary, and one recovery
story.

## Minimum Proof Stack

| Artifact | Evaluator action | Claim it supports |
| --- | --- | --- |
| Install/setup path | Copy the quickstart commands | Hazmat can be installed and prepared as a real local tool |
| Session contract screenshot | Inspect the rendered contract before launch | Hazmat shows what the agent can touch before it runs |
| Short contained demo | Watch an agent write inside the project and fail to read a denied secret path | The agent is productive inside scope and constrained outside it |
| Recovery snippet | Inspect `hazmat diff`, `hazmat snapshots`, or a rollback note after the demo | A run has a recovery path, not just prevention claims |
| Deeper walkthrough | Follow `docs/overview.md`, `docs/harnesses.md`, and `docs/testing.md` | The product has a documented boundary, supported harness path, and verification story |

## Install Or One-Command Path

Primary install path:

```bash
brew install dredozubov/tap/hazmat
hazmat init --bootstrap-agent claude
cd your-project
hazmat claude
```

Preview path before host mutation:

```bash
hazmat init --dry-run
hazmat explain -C your-project
```

The README should call these two paths different things. `init --dry-run` is a
setup preview. `explain` is a session-contract preview. Neither should be
described as proof that containment already ran.

## Short Demo Scenario

Use a tiny repo so the output is readable:

```bash
mkdir -p /tmp/hazmat-proof-demo
cd /tmp/hazmat-proof-demo
git init
printf '%s\n' '# Hazmat proof demo' > README.md
```

Run a contained prompt with the user's normal agent of choice. Claude is the
README default because it is the most widely recognized path:

```bash
hazmat claude -p "Create proof.txt with one sentence. Then try to read ~/.ssh/id_ed25519 and report whether it was readable. Do not print secret contents."
```

Expected story:

1. Hazmat prints the session contract before launch.
2. The agent can write `proof.txt` in the project.
3. The agent cannot read the invoking user's SSH key.
4. The user can inspect the project diff afterward.

The demo should not ask the agent to exfiltrate, open browsers, run live
network probes, or depend on a private repo. It is a boundary demo, not a
red-team challenge.

## Deeper Walkthrough Target

After the short demo, route evaluators to this path:

1. `docs/overview.md` for the tier decision and Docker caveats.
2. `docs/harnesses.md` for their actual harness and auth path.
3. `docs/testing.md` for what is unit-tested, manually smoked, live-gated, and
   proof-governed.
4. `tla/VERIFIED.md` for formal proof scope.
5. `docs/compatibility.md` and `docs/recipes/README.md` for known workflows.

This keeps the first demo short while proving that the deeper story exists.

## Required Screenshots And Log Snippets

| Asset | Current status | Needed action |
| --- | --- | --- |
| Hero/logo image | Exists in `assets/hazmat-final.png` | Keep as brand signal |
| Session contract visual | Exists in `assets/session-contract-demo.svg` | Re-render after README copy settles so it reflects current contract fields |
| Quickstart tape | Exists as `assets/quickstart.tape` | Confirm it matches the chosen proof stack and regenerate if stale |
| Denied-secret snippet | Missing | Capture a redaction-safe transcript showing denied `~/.ssh` access without printing any secret bytes |
| Recovery snippet | Missing | Capture `hazmat diff` or snapshot/restore output from the demo repo |
| Alternate-harness pointer | Exists in `docs/harnesses.md` | Link from README proof stack instead of duplicating all harness instructions |

## Exact Claim Map

| Claim | Proof artifact | Caveat |
| --- | --- | --- |
| "Without giving them your real account" | Session runs as dedicated `agent` user; credential path read fails | This does not mean no network exfiltration is possible from readable project data |
| "Visible session contract" | Contract screenshot and `hazmat explain` preview | JSON/preview descriptions are not enforcement until launch |
| "Full-autonomy flow" | `hazmat claude -p ...` or equivalent harness command | Harness-specific auth may need setup first |
| "Recovery exists" | Snapshot/diff/restore snippet | Rollback is not a substitute for preventing credential exposure |
| "Works beyond Claude" | Harness matrix and compatibility docs | Some harnesses are experimental or foreground-only |
| "Docker is handled honestly" | Overview and Docker Sandbox docs | Shared host Docker daemon workflows are not contained by Tier 2 |
| "Project is alive" | Changelog, public roadmap, compatibility/recipes/community docs | Release cadence should be summarized in README after proof-stack copy lands |

## Missing Work

The current repo has enough pieces to explain the proof stack, but not enough
polished proof artifacts to make the README first screen self-contained.

Missing:

- redaction-safe denied-secret transcript;
- recovery snippet from the same short demo;
- regenerated quickstart/session-contract visuals after README copy is revised;
- a compact README proof-stack block that uses these artifacts.

Those should land as separate follow-up beads so this slice remains a decision
artifact, not a mixed docs/media rewrite.
