# Compatibility Program

Hazmat should not promise "supports everything."

The compatibility program exists to turn vague support claims into concrete rows: harness, stack, macOS version, containment mode, status, evidence, and caveats.

## Status Meanings

Use these compatibility statuses consistently:

| Status | Meaning |
|--------|---------|
| **works** | confirmed working for the reported host / harness / stack combination |
| **works with caveats** | usable, but with important setup notes, sharp edges, or known limitations |
| **native only** | works in native containment, but not as a Docker-capable session |
| **needs Tier 4** | the workflow depends on capabilities that Hazmat should not punch through Tier 2 / Tier 3 for |
| **unsupported** | not a supported path today |

If a combination has no row yet, that means "no public report yet," not "it works."

## Evidence Rules

Add or update a compatibility row only when there is evidence:

- a real user report
- a reproducible repo test
- a recipe backed by documented repo behavior
- CI or stack-matrix evidence

Link the evidence in the row or in the associated issue.

## Starter Matrix

These are documented starting points, not exhaustive certification.

| Harness | Stack / recipe | Reported macOS | Mode | Status | Evidence | Notes |
|---------|----------------|----------------|------|--------|----------|-------|
| Claude Code | Next.js app | 15.x (Sequoia-era docs) | native containment | **native only** | [docs/harnesses.md](harnesses.md), [docs/integrations.md](integrations.md), [docs/shared-daemon-projects.md](shared-daemon-projects.md), [recipe](recipes/claude-nextjs.md) | Good default for code-only work. If the repo needs host Docker control, move to Tier 4 or keep Docker outside Hazmat. |
| Codex | Python + uv | 15.x (Sequoia-era docs) | native containment | **works with caveats** | [docs/harnesses.md](harnesses.md), [docs/integrations.md](integrations.md), [recipe](recipes/codex-uv.md) | Import path is the smoothest first-run option because Codex has a known startup-picker arrow-key issue under Hazmat. |
| OpenCode | Go | 15.x (Sequoia-era docs) | native containment | **works with caveats** | [docs/harnesses.md](harnesses.md), [docs/integrations.md](integrations.md), [recipe](recipes/opencode-go.md) | Good starting point for community confirmation; report toolchain and module-cache caveats explicitly. |
| Gemini | TLA+ / TLC | 15.x (Sequoia-era docs) | native containment | **works with caveats** | [docs/harnesses.md](harnesses.md), [docs/integrations.md](integrations.md), [recipe](recipes/gemini-tla.md) | Requires the Java/TLA runtime path to be available and usually needs a more manual auth/runtime setup than Claude. |

## Reporting Flow

1. File a GitHub compatibility report using the dedicated template.
2. Include the exact harness, stack, macOS version, containment mode, and caveats.
3. Link any supporting recipe or repro repo.
4. If the report is reusable, add or update a row in this document.

Compatibility reports are one of the best community contribution surfaces in Hazmat because they add signal without weakening the containment boundary.
