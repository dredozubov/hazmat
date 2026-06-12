# Path Deny Overlay Evaluation

**Status:** decided
**Date:** 2026-06-12
**Related bead:** `sandboxing-48m0`

Hazmat should not implement live read-only project mounts with per-session
repo-internal deny overlays as the default way to hide sensitive files. For
workflows that need repo-internal secrets to be unavailable to a reviewer or
harness, the default architecture should materialize a workspace where those
bytes are absent before launch.

This decision does not change ordinary `hazmat` session semantics: the selected
project directory is still intentionally writable and readable. If `.env`,
Ansible inventories, auth profiles, or other secrets live inside that project
directory, they are exposed by design. This evaluation is about future
review-packet and sandbox-workspace modes that want a narrower project view
than the real directory.

## Decision

Prefer materialized project views over live path-deny overlays.

Practical ranking:

1. Manifest-only materialization for high-sensitivity review packets. Only
   explicitly included files, diffs, and issue metadata exist.
2. APFS clone plus delete or mask denied paths for same-volume macOS workspaces.
   This preserves repo shape cheaply while making excluded bytes absent before
   launch.
3. Filtered copy as the portable fallback. It is slower, but its security story
   is simple.
4. Sparse worktree for tracked-file scopes only. It is not sufficient for
   untracked repo-local secrets.
5. Live read-only mount plus deny overlay remains a research item, not a
   supported mode.

The core invariant should be:

> Sensitive bytes that are outside the approved review packet are not present in
> the session workspace.

That invariant is easier to explain, test, and preserve across backends than
"sensitive bytes are present behind a negative policy exception inside an
otherwise broad mount."

## Rationale

Hazmat's current path authority is positive-grant based. `pathpolicy` resolves
canonical paths and rejects project, read-only, and read-write grants that
overlap credential, host-state, or host-authority deny zones. `containment`
then carries typed path grants and a structural credential floor to backend
compilers. The TLA+ containment models prove that credential-deny paths and
their broad parents are not mounted.

A repo-internal deny overlay changes that shape. It would mount a broad project
tree and then depend on per-session negative exceptions for paths below that
tree. That is a different authority model, with different failure modes:

- Path aliases: symlinks, hardlinks, case folding, and realpath differences can
  make a denied file reachable through a name the overlay did not cover.
- Glob drift: deny patterns can be too narrow, too broad, or evaluated by
  different engines with different semantics.
- Live mutation: a host-side change after launch can create, move, replace, or
  link sensitive files under a mount that was previously safe.
- Backend mismatch: SBPL, Docker Sandbox, Apple Container, and future Linux or
  remote launchers do not share one deny-overlay primitive.
- Proof complexity: "deny overrides allow" must dominate every read path, every
  backend compiler, and every path normalization path. That is a larger proof
  surface than proving excluded bytes never entered the workspace.

The current design already treats host credential and host-state roots as
outside the project grant. Extending that invariant to review packets by
materializing a narrower workspace is consistent with the existing architecture.

## Required Model Work If Revisited

If Hazmat later accepts live path-deny overlays, implementation must start from
the model and design notes, not from backend code. The model must specify at
least:

- Deny-overrides-allow precedence for broad read-only project mounts.
- Canonical path normalization before policy admission.
- Symlink, hardlink, case-sensitivity, and parent-directory traversal behavior.
- Glob grammar, expansion time, unresolved pattern failures, and race handling.
- Whether metadata reads on denied paths are also denied.
- How concurrent sessions get independent deny sets.
- How host-side mutations after launch are handled or rejected.
- Machine-readable metadata for the effective project view.
- Teardown obligations and failure behavior if a backend cannot enforce the
  overlay exactly.

The existing TLA+ containment specs are not enough for that mode because they
prove exclusion at mount-planning time, not a broad grant with repo-internal
deny exceptions.

## Testing Architecture

For materialized project views, default tests should be host-local and
unprivileged:

- Manifest materialization includes only approved paths and rejects unresolved
  entries.
- APFS clone and filtered-copy planners remove or mask denied paths before any
  launch artifact is prepared.
- Symlink and hardlink cases prove excluded content is absent, not merely hidden
  by one path spelling.
- Concurrent materializations produce independent directories and metadata.
- Backend contract tests reject launches that point at the real project when a
  narrowed materialized view was requested.

Live validations, if added later, must be opt-in and named separately from the
default unit suite because they are sudo-adjacent in this repo's workflow.

## Consequence For Redteam Review Packets

OPS or redteam workflows must not depend on live path-deny overlays. The MVP
should use manifest-only materialization. A broader sandbox workspace can later
use APFS clone plus deletion or filtered copy fallback, but the reviewer should
still receive a materialized directory whose excluded sensitive bytes are not
present.
