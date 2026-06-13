# Python / uv Proof Story

**Date:** 2026-06-13
**Bead:** `sandboxing-q97k`
**Status:** owned distribution copy, fixture-backed but not a live transcript
**Recipe:** [Codex + uv](../recipes/codex-uv.md)
**Compatibility row:** [Codex / Python + uv](../compatibility.md)

## Short Version

Hazmat can run Codex against a Python project that uses `uv` while keeping the
agent out of the developer's real home account. The `python-uv` integration
detects `uv.lock`, resolves the Python runtime, opens uv cache/tooling paths,
and keeps project work visible in the session contract.

The proof claim is deliberately narrow: this is a documented native containment
path with fixture/test evidence and a recipe. It is not a claim that every
Python project, virtualenv layout, or package install is safe.

## Audience

Use this story for:

- Python developers using Codex or Claude on macOS;
- `uv` users who want fast test/refactor loops under a smaller host authority;
- teams evaluating whether Hazmat supports modern Python tooling without broad
  home-directory access.

Do not use it for:

- production database or cloud-credential demos;
- broad "Python is supported everywhere" claims;
- workflows that require arbitrary writable host virtualenv or cache roots;
- live pass claims without an approved transcript.

## Proof Path

Owned artifacts:

- [README](../../README.md) for the account-boundary wedge and preview path.
- [Codex + uv recipe](../recipes/codex-uv.md) for setup and typical commands.
- [Compatibility row](../compatibility.md) for status, evidence, and Codex auth
  caveat.
- [Stack coverage](../STACKS.md) for `python-uv` status and cache paths.
- [Integrations docs](../integrations.md) for manifest behavior and snapshot
  excludes.
- Fixture evidence in `hazmat/integration_manifest_test.go` and
  `hazmat/integration_resolver_test.go` for `uv.lock` suggestion and resolver
  behavior.

Safe command shapes:

```bash
hazmat config import codex
hazmat codex --integration python-uv
hazmat exec --integration python-uv -- uv run pytest -q
hazmat exec --integration python-uv -- uv run ruff check .
```

Session-contract angle:

- project directory is read-write;
- `python-uv` opens `~/.local/share/uv` and `~/.cache/uv` for the agent;
- Python runtime resolution is integration-owned, not ambient home access;
- project-local `.venv/`, `.pytest_cache/`, `.ruff_cache/`, and similar output
  are snapshot-excluded as integration behavior;
- host credential paths stay denied;
- extra writable virtualenv or cache roots require explicit `-W`.

## Fixture Evidence

This story can cite fixture-backed evidence without running a live smoke:

- `docs/STACKS.md` marks `python-uv` as end-to-end verified inside
  `hazmat exec`.
- `docs/compatibility.md` marks Codex + Python/uv as "works with caveats."
- `hazmat/integration_manifest_test.go` covers `uv.lock` suggestion,
  arbitration against `python-pip`, and project pin behavior.
- `hazmat/integration_resolver_test.go` covers `python-uv` manifest loading and
  resolver output.

Do not upgrade this to "passed on this machine" without an approved command
transcript.

## Caveats To Keep In The Copy

- Codex startup auth still has a known first-run picker rough edge under
  Hazmat; importing Codex auth is the smoothest first path.
- `uv` caches are useful inputs, not a grant to every Python-related host path.
- If a workflow depends on a host virtualenv outside the project, expose it
  explicitly with `-W` and understand that it becomes writable by the session.
- Project-local secrets such as `.env` remain readable because the project is
  the agent's work area.
- Package installation can execute dependency code. Hazmat reduces host
  authority; it does not prove arbitrary Python packages are safe.

## Post Template

```text
Codex + uv is a good first Python proof path for Hazmat.

In a `uv.lock` project, Hazmat can launch a contained Codex session or one-off
test command with the `python-uv` integration:

    hazmat config import codex
    hazmat codex --integration python-uv
    hazmat exec --integration python-uv -- uv run pytest -q

What the contract makes visible:
- project writes
- uv cache/tooling access
- explicit writable extras, if any
- credential-deny boundaries
- snapshot/recovery state

Proof:
- README: <link>
- Recipe: <link>
- Compatibility row: <link>
- Fixture evidence: <link>

Caveat:
- This is a native containment path with Codex auth caveats and fixture-backed
  integration evidence. Do not treat it as a live-smoke pass unless an approved
  transcript is linked.
```

## README Link Path

The public path should be:

1. README first screen for the wedge and preview path.
2. `docs/harnesses.md` for Codex auth/import details.
3. `docs/recipes/codex-uv.md` for the exact Python/uv commands.
4. `docs/compatibility.md` for status and caveats.
5. `docs/integrations.md` and `docs/STACKS.md` for integration boundaries.

## Acceptance Checklist For Reuse

Before using this externally:

- State that the story is fixture-backed unless a live transcript is linked.
- Include the Codex import/auth caveat.
- Include virtualenv/write-scope caveats.
- Include package-install/supply-chain caveat.
- Avoid "safe Python agents" language.
