# Next.js / Node Proof Story

**Date:** 2026-06-13
**Bead:** `sandboxing-a8bg`
**Status:** owned distribution copy, safe to adapt for posts after README proof stack lands
**Recipe:** [Claude + Next.js](../recipes/claude-nextjs.md)
**Compatibility row:** [Claude Code / Next.js app](../compatibility.md)

## Short Version

Hazmat's easiest web-agent story is Claude Code in a Next.js repo: the agent
can edit and test the project under native macOS containment without inheriting
the developer's real home account.

The point is not "Node is safe." The point is that a normal web-agent workflow
can keep project writes, Node toolchain reads, cache access, credential denies,
network posture, and rollback visible in the session contract before the agent
runs.

## Audience

Use this story for:

- Next.js, React, and frontend/full-stack developers using local coding agents;
- Claude Code users who want full-autonomy flow without same-user execution;
- teams evaluating whether Hazmat is practical for ordinary web projects before
  they inspect deeper security docs.

Do not use it for:

- Docker-heavy devcontainer workflows;
- repos that require host Docker socket control;
- production credential or cloud-deploy demos;
- claims that Node package installation is fully safe.

## Proof Path

Owned artifacts:

- [README](../../README.md) for the account-boundary wedge and first contained
  session path.
- [Claude + Next.js recipe](../recipes/claude-nextjs.md) for the exact command.
- [Compatibility row](../compatibility.md) for status, mode, evidence, and
  caveats.
- [Stack coverage](../STACKS.md) for the `node`, `pnpm`, `yarn`, `bun`, and
  `deno` integration boundaries.
- [Testing matrix](../testing.md) for the difference between automated checks
  and approval-gated live smokes.

Safe command shape:

```bash
hazmat claude --integration node
```

Sticky project setup:

```bash
hazmat config set integrations.pin "~/workspace/my-next-app:node"
```

Session-contract angle:

- project directory is read-write;
- Node runtime/toolchain and caches are opened through the integration;
- extra docs/design references should be passed with `-R`;
- host credential paths stay denied;
- snapshots/diff/restore provide recovery workflow;
- host Docker socket is not granted by this recipe.

## Caveats To Keep In The Copy

- This is a native-only proof story. If the repo needs Docker or Compose, do
  not punch host Docker control through native containment.
- Package-manager network fetches and install scripts are still supply-chain
  risk. Hazmat reduces host authority, but it does not prove arbitrary npm
  dependencies are safe.
- Project-local secrets such as `.env` files remain readable because the
  project is the agent's work area.
- Live "works on this machine" claims require an approved command transcript.
  This story is based on tracked docs, recipe, compatibility row, and stack
  integration evidence.

## Post Template

```text
Run a full-autonomy web agent in a Next.js repo without giving it your real Mac
account.

Hazmat launches Claude Code as a dedicated macOS `agent` user, shows the
session contract first, and opens the Node integration for project-local web
work:

    hazmat claude --integration node

What the contract makes visible:
- project writes
- Node toolchain/cache access
- credential-deny boundaries
- network mode
- snapshot/recovery state

Proof:
- README: <link>
- Recipe: <link>
- Compatibility row: <link>

Caveat:
- This is the native, code-only web workflow. If your Next.js repo needs the
  host Docker daemon or devcontainer authority, use a different boundary
  instead of punching that socket through native containment.
```

## README Link Path

The public path should be:

1. README first screen for the wedge and preview path.
2. `docs/harnesses.md` for Claude auth/import details.
3. `docs/recipes/claude-nextjs.md` for the exact Next.js command.
4. `docs/compatibility.md` for status and caveats.
5. `docs/overview.md` for Docker-heavy workflows.

## Acceptance Checklist For Reuse

Before using this externally:

- README proof stack exists or the post links only to current owned evidence.
- No live-smoke pass is implied unless the exact approved transcript exists.
- Docker caveat appears near the main claim.
- Package-manager/supply-chain caveat is present.
- The post says "reduces host authority" or equivalent, not "safe Node agents."
