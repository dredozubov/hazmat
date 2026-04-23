# Git Hook Approval UX Design

**Status:** design draft for `sandboxing-kpde`  
**Date:** 2026-04-23

## Problem

Hazmat already has the right trust shape for session integrations:

- repo declares intent in tracked files
- host records approval in host-owned state
- future activation is gated on that approval record

Git hooks need the same shape. A contained agent can author hook code in the
repo, but the host must not execute that code implicitly on future `git commit`
or `git push` runs. Manual install patterns such as copying scripts into
`.git/hooks/` are too ad hoc for Hazmat, and package-manager lifecycle install
patterns (`prepare`, `postinstall`, Husky auto-init, Lefthook auto-install)
cross the trust boundary in the wrong direction.

The main hazard is not just post-approval hook drift. A contained agent with
write access to `.git/config` can rewrite `core.hooksPath` and route future
host-side Git invocations around any approved dispatcher unless Hazmat treats
hook activation as a host capability and validates that routing decision before
Git runs.

## Recommended v1 Shape

Repo-owned intent:

- tracked hook bundle under `.hazmat/hooks/`
- tracked manifest at `.hazmat/hooks/hooks.yaml`
- manifest declares hook type, script path, purpose, allowed interpreter, and
  required binaries

Host-owned activation:

- approval persisted outside the repo, keyed by canonical repo path plus bundle
  hash
- approved snapshot copied to host-owned state outside the repo
- Hazmat installs a host-managed Git wrapper plus per-hook dispatchers
- wrapper validates `core.hooksPath`, hook layout, and approved snapshot before
  invoking real Git
- managed dispatcher executes only approved snapshot content
- fallback dispatcher in `.git/hooks/` exists only as drift detection and
  refusal, not as an alternate approved execution path

This keeps the contract simple: the repo may propose hooks, but only the host
may activate them, and execution happens from immutable approved bytes rather
than from live repo contents.

## Trust Boundary

### In scope

- repo-local hooks only
- `pre-commit`, `pre-push`, and `commit-msg`
- explicit install / uninstall owned by Hazmat
- repo drift detection and re-approval
- host-side cleanup on uninstall / rollback

### Out of scope for v1

- global hooks
- `init.templateDir`
- global `core.hooksPath`
- package-manager lifecycle auto-install
- `post-*`, server-side hooks, and `pre-receive`

Hazmat should refuse automatic installation when it detects pre-existing
non-default `core.hooksPath` ownership from another tool. Replacing another
tool's hook setup silently is worse than failing loudly.

## Proposed UX

1. Agent edits files in `.hazmat/hooks/` like normal source.
2. On the next `hazmat` launch, or via `hazmat hooks status`, Hazmat detects the
   repo manifest and computes a bundle hash.
3. Hazmat prompts with manifest-derived intent, not a raw file diff:

   - hook types declared
   - human-readable purpose
   - interpreter
   - required binaries
   - whether this is first install or drift from prior approval

4. Approval writes a host-owned record and copies the bundle into a host-owned
   immutable snapshot.
5. Hazmat installs:

   - a host-side Git wrapper
   - managed dispatchers for approved hooks
   - fallback `.git/hooks/*` dispatchers that only detect reroute / drift and
     refuse

6. Future drift reuses the same prompt shape with a readable diff summary and a
   single re-approval path.
7. `hazmat hooks uninstall` and `hazmat rollback` remove the approval record,
   approved snapshot, wrapper, and dispatchers atomically.

## Execution Contract

### Wrapper

The wrapper is the primary defense against `core.hooksPath` bypass. Before
dispatching to real Git, it validates:

- the effective `core.hooksPath` is the Hazmat-managed path
- the managed hook directory contains only the expected dispatcher files
- `.git/hooks/` contains only the expected fallback dispatchers
- the repo manifest hash matches the approved record
- the approved snapshot still matches the approved record

If any check fails, the wrapper refuses execution and prints an actionable
repair path.

### Dispatchers

Managed dispatchers execute only the approved snapshot, never live repo bytes.
They re-check the bundle hash and managed hook layout on every run.

Fallback `.git/hooks/*` dispatchers exist only to catch cases where Git reaches
the default hook path outside the wrapper. Their job is to refuse and explain
that `core.hooksPath` drifted away from the Hazmat-managed value.

## TLA+ Scope

The first TLA+ model should cover the host-owned approval and activation state
machine, not every shell detail of the wrapper implementation.

Target invariants:

- `ApprovedContentOnly`
- `HooksPathPinned`
- `DriftRefusesExecution`
- `RollbackClearsHookInstall`
- `NoImplicitWidening`

The model should also include the `core.hooksPath` reroute hazard explicitly:
repo drift alone is not the interesting attack; rerouting Git around the
approved dispatcher is.

## Draft Modeling Boundary

`MC_GitHookApproval.tla` should model:

- repo-declared hook set and bundle hash
- host approval record
- host-owned immutable snapshot
- managed `core.hooksPath`
- wrapper-mediated host Git invocation
- managed dispatcher execution
- fallback dispatcher refusal on default-path drift
- uninstall / rollback cleanup

It should **not** claim to prove behavior for arbitrary direct invocations of a
foreign `git` binary outside the Hazmat-managed wrapper path. That remains a
documented scope boundary unless Hazmat later takes ownership of every host Git
entrypoint it expects users to exercise.

