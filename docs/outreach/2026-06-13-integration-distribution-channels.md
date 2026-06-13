# Integration Distribution Channels

**Date:** 2026-06-13
**Bead:** `sandboxing-cu4m.6`
**Status:** distribution prioritization, not public outreach copy
**Inputs:**
[contributor participation loop](2026-06-13-contributor-participation-loop.md),
[stack coverage](../STACKS.md),
[recipes](../recipes/README.md)

## Goal

Use integrations as distribution channels without turning them into policy
escapes. A good channel has a community that already cares about local agents,
coding CLIs, security review, or macOS development, and a proof story that
community can repeat.

## Candidate Ecosystems

| Ecosystem | Existing Hazmat surface | Distribution story | Main caveat |
| --- | --- | --- | --- |
| Next.js / Node / pnpm / yarn / bun | `node`, `pnpm`, `yarn`, `bun`, `deno`; Claude + Next.js recipe | "Run full-autonomy web agents without exposing your real home or tokens" | JS dependency installs can need network and postinstall scrutiny |
| Python / uv / pip / Poetry | `python-uv`, `python-pip`, `python-poetry`; Codex + uv recipe | "Let agents run tests and refactors against modern Python projects with cache/tooling ergonomics" | Virtualenv/write-scope choices vary by project |
| Go / Rust / systems | `go`, `rust`, `cmake`; OpenCode + Go recipe | "Fast native-agent workflows with explicit toolchain reads and project writes" | Less visually demoable than web/Python |
| TLA+ / formal methods | `tla-java`; Gemini + TLA+ recipe | "Hazmat uses formal methods and can run formal-methods workflows in containment" | Niche audience, high credibility but lower volume |
| Docker / databases / services | `docker`; database recipes; Docker Sandbox docs | "Hazmat refuses unsafe host Docker socket shortcuts and routes private-daemon work separately" | Strong proof may require approved Docker Sandbox smokes |
| Local AI/ML model workflows | `huggingface`, `ollama`, `pytorch-torch-hub` | "Agents can use local model caches without broad home access" | Live cache smokes are approval-gated and fixture-dependent |
| Kubernetes / Terraform / OpenTofu | `kubernetes-render`, `terraform-plan`, `opentofu-plan` | "Render and plan inside containment without cluster/cloud credentials" | Must avoid implying apply/cluster authority |
| Beads / agent project tooling | `beads`; public roadmap and issue workflow | "Hazmat itself dogfoods bounded project-agent workflows" | Distribution narrower, mostly agent-builder audience |
| ACP / MCP / service agents | ACP and MCP recipes; OpenHands recipe-only path | "Protocol/service agents can be contained as recipes before becoming supported harnesses" | Must avoid plugin/manifest overreach |

## Ranking Method

Score each candidate 1-5:

- **Adopter fit:** how directly the community matches the chosen wedge.
- **Implementation cost:** higher score means lower cost because docs/recipes
  already exist.
- **Credibility:** how strongly the proof supports a non-obvious Hazmat claim.
- **Distribution leverage:** how likely the story travels through communities,
  repos, newsletters, or compatibility reports.

## Top Three

| Rank | Channel | Adopter fit | Cost | Credibility | Leverage | Why |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| 1 | Next.js / Node agent workflows | 5 | 4 | 4 | 5 | Broadest developer audience, existing recipe, easy to understand: web agent writes project code but not user credentials |
| 2 | Python / uv agent workflows | 5 | 4 | 4 | 4 | Strong fit for Codex/Python users, existing recipe, modern `uv` community likes reproducible tooling |
| 3 | Docker/database boundary workflows | 4 | 3 | 5 | 5 | Strongest security differentiation: Hazmat refuses host Docker socket shortcuts and gives a private-daemon/Tier 4 decision path |

Close contenders:

- Local AI/ML cache workflows have high distribution potential but need approved
  fixture/live-smoke evidence before public proof claims.
- TLA+ has high credibility and good founder-market fit, but the audience is
  narrower. Use it as trust/proof content, not the first distribution wedge.
- Go/Rust systems workflows are credible and low cost, but less distinctive
  than Node/Python/Docker stories.

## Selected Channel Stories

### 1. Next.js / Node

**Target adopter:** frontend/full-stack developers using Claude or Codex on a
macOS workstation with npm/pnpm/yarn/bun projects.

**Story:** "Hazmat lets a full-autonomy agent work in a Next.js app while the
session contract exposes project writes, read-only toolchain/cache access,
snapshot excludes, and credential-deny boundaries."

**Proof artifact needed:**

- refreshed Claude + Next.js recipe;
- compatibility row with macOS version and caveats;
- proof-stack snippet showing project write plus denied credential read;
- README proof block link to the recipe.

**Distribution surfaces:**

- Next.js/React community posts after proof stack exists;
- awesome-shell/macOS follow-up comments if maintainers ask for examples;
- recipe PR invites for pnpm/yarn/bun variations.

### 2. Python / uv

**Target adopter:** Python developers using Codex or Claude on macOS, especially
projects that already adopted `uv`.

**Story:** "Hazmat can run coding-agent test/refactor loops in a Python + uv
project without exposing cloud credentials, SSH keys, or the whole home
directory."

**Proof artifact needed:**

- refreshed Codex + uv recipe;
- compatibility row with exact `uv`/Python caveats;
- command transcript for `hazmat exec --integration python-uv -- uv run pytest`
  using a safe fixture or example project;
- note on virtualenv/write-scope choices.

**Distribution surfaces:**

- Python/uv community posts;
- recipe and compatibility report call-for-feedback;
- docs/UX issues for missing Python stack caveats.

### 3. Docker / Database Boundary

**Target adopter:** developers whose agent work needs Docker, Compose,
PostgreSQL/Redis, or service stacks.

**Story:** "Docker is not just another binary. Hazmat native containment does
not expose the host Docker socket; private-daemon workflows use Docker Sandbox,
and shared-daemon workflows need Tier 4 or code-only containment."

**Proof artifact needed:**

- database Tier 3 recipe;
- shared-daemon limitation doc;
- approved Docker Sandbox smoke evidence before any "works" demo claim;
- comparison snippet that shows native mode refusing unsafe Docker shortcuts.

**Distribution surfaces:**

- security/devops communities;
- database recipe posts;
- Docker-heavy compatibility report prompts.

## Proof Requirements Before Outreach

- Do not publish a "works" claim unless the recipe, compatibility row, or
  approved smoke evidence exists.
- Do not ask communities to run live Hazmat or Docker smokes without explicit
  command disclosure.
- Keep integration stories about ergonomics plus visible authority. Do not
  describe integrations as permission grants.
- Prefer "render/plan only" language for Kubernetes/Terraform/OpenTofu.
- Treat local AI/ML cache stories as pending until the cache-integration smokes
  have approved evidence.

## Follow-Up Work

The three selected channels need small follow-up slices so distribution does
not outrun proof:

- publish a Next.js/Node proof story from the existing recipe and proof stack;
- publish a Python/uv proof story from the existing recipe and compatibility
  evidence;
- package the Docker/database boundary as a proof story after approved smoke
  evidence or keep it as an educational caveat if smoke evidence is absent.
