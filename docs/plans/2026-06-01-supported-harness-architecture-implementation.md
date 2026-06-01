# Supported Harness Architecture and Implementation Plan

## Purpose

Hazmat supports five agent code harnesses today: Claude Code, Codex, OpenCode,
Gemini, and Hermes. The immediate lifecycle CLI work added user-facing
`hazmat harness status`, `hazmat harness update`, and
`hazmat harness uninstall`, but the next step is to make the architecture
explicit enough that every harness can evolve without copying policy decisions
across bootstrap, credential import, launch, status, uninstall, and docs.

The target is a single harness ownership model:

- every supported harness has a registry entry with its state version, launch
  command, update path, status probe, owned code artifacts, and preserved data
  boundary
- lifecycle status reads from the registry and reports the same categories for
  every harness
- install/update code stays harness-specific where supply-chain behavior differs
- uninstall is plan/apply and only removes declared Hazmat-owned code artifacts
  plus the selected host metadata record by default
- auth, imported basics, profile roots, session history, and provider data are
  preserved unless a future purge design updates the TLA+ lifecycle model first

## Current State

The current branch already has the first version of the lifecycle surface.
`hazmat/harness.go` owns `ManagedHarness` registry entries. Each entry names the
display spec, launch command, bootstrap command, install probe, version probe,
managed code artifact function, preserved-data list, and bootstrap function.
`hazmat/harness_lifecycle.go` implements the CLI, status formatting, update
delegation, uninstall planning, artifact drift detection, and metadata removal.

The current design note and model are `tla/08_harness_lifecycle.md` and
`tla/MC_HarnessLifecycle.tla`. They prove that bootstrap/import recording uses
known harness versions, dry-run bootstrap/import/uninstall leaves state
untouched, explicit uninstall removes only selected harness code plus metadata,
core state saves preserve harness metadata, and rollback still has the old
agent-user boundary.

Remaining architectural work is mostly consolidation and hardening, not a new
user-facing command family. The lifecycle CLI should become the place where
users manage harness ownership, while `hazmat bootstrap <harness>` remains a
compatibility spelling for `hazmat harness update <harness>`.

## Recommended Architecture

Use a registry-driven adapter model. The registry should remain static and
closed over built-in harnesses; Hazmat should not accept arbitrary plugin
harnesses until there is a separate trust and policy design. Each harness entry
should provide four categories of behavior.

First, identity and state: `HarnessSpec`, state version, launch command, and
whether curated import is supported. This feeds status, state recording, init
bootstrap selection, explain-mode coverage, and TLA+ model membership.

Second, code ownership: probe, update, owned artifact plan, and preserved data
description. Probe is read-only and can be used in status. Update is
side-effecting and must go through `Runner`. Owned artifacts are exact paths
under `/Users/agent` that Hazmat is allowed to remove. Preserved data is
documentation and guardrail text, not executable behavior.

Third, credential and profile boundaries: durable host secret-store descriptors,
session delivery rules, curated import behavior, asset-sync behavior, and any
managed profile root. These already exist across the credential registry,
config-import files, harness asset sync, and Hermes state root code; the plan is
to make lifecycle status consume their summaries consistently.

Fourth, launch integration: static session env, auth artifact application,
credential injection, network mode support, Docker Sandbox routing, resume
sync, trace metadata, and status-bar behavior. These should keep flowing
through the shared session preparation pipeline, with the harness ID selecting
only the minimum harness-specific branches.

## Approaches Considered

The recommended approach is the static adapter registry described above. It
matches the existing code, is easy to test, and keeps security-sensitive
ownership decisions in one reviewable place.

An alternative is to add larger per-harness objects that own bootstrap, import,
asset sync, session env, and uninstall end to end. That would localize each
harness, but it would also fragment cross-cutting guarantees such as dry-run
behavior, credential delivery, and uninstall preservation.

Another alternative is a generic manifest format for harnesses. That is too
early. Installation scripts, auth harvesting, launch semantics, and sandbox
policy changes are not declarative enough to accept from arbitrary manifests
without a separate plugin trust model.

## Shared Lifecycle Data Flow

The lifecycle command should have a stable internal flow:

1. Resolve harness ID through `managedHarnessByID`.
2. Load `~/.hazmat/state.json` once.
3. Run the harness probe through agent-user read helpers.
4. Query credential inventory for descriptors relevant to the harness.
5. Build a status object with install, version, state, import, credential, next
   action, owned artifact, and preserved-data fields.
6. Render list or detail output.

For update:

1. Resolve harness ID.
2. Build `UI` and `Runner`.
3. Call the harness adapter update function.
4. Record harness state only after the update/verification path succeeds.
5. Do not write state in dry-run.

For uninstall:

1. Resolve harness ID.
2. Require the agent user.
3. Load state and build an uninstall plan.
4. Inspect each exact owned artifact path as the agent user.
5. Refuse paths outside `/Users/agent` and refuse type drift unless an explicit
   force path is selected.
6. Print remove and preserve sections.
7. Ask unless dry-run, `--yes`, or a future non-interactive mode is selected.
8. Remove only planned owned artifacts and then remove the selected harness
   metadata record.

## Per-Harness Boundaries

| Harness | Update Owner | Owned Code Artifacts | Preserved By Default | Import | Asset Sync |
|---|---|---|---|---|---|
| Claude Code | Hazmat downloads official installer and verifies pinned checksum | `/Users/agent/.local/bin/claude` | `/Users/agent/.claude`, `/Users/agent/.claude.json`, provider secrets | Yes | Yes |
| Codex | Hazmat resolves latest GitHub release and verifies published digest | `/Users/agent/.local/bin/codex` | `/Users/agent/.codex`, `/Users/agent/.agents`, provider secrets | Yes | Yes |
| OpenCode | Hazmat runs official installer and maintains PATH shim | `/Users/agent/.opencode/bin/opencode`, `/Users/agent/.local/bin/opencode` | `/Users/agent/.config/opencode`, `/Users/agent/.local/share/opencode`, provider secrets | Yes | Yes |
| Gemini | Hazmat installs `@google/gemini-cli@latest` into agent local prefix | `/Users/agent/.local/bin/gemini`, npm package dir | `/Users/agent/.gemini`, provider secrets, Keychain boundary | Yes, file-backed only | Yes |
| Hermes | Hazmat verifies a manual executable only | none in v1 | `/Users/agent/.local/bin/hermes`, `/Users/agent/.hazmat/hermes`, provider secrets | No | No |

Hermes is the deliberate exception. It participates in launch, explain,
credential delivery, status, and metadata recording, but Hazmat does not fetch
or remove Hermes code in v1. Any future Hermes installer must start with a
supply-chain design and an update to the lifecycle model if uninstall semantics
change.

## Implementation Phases

Phase 1 is complete on the current branch: add the lifecycle CLI, registry
metadata, safe uninstall plan/apply, state metadata removal, TLA+ uninstall
modeling, docs, and tests.

Phase 2 should normalize status output. Add an internal status struct with
machine-readable fields that can later back `--json`. Keep the text table
stable, but avoid parsing text in tests. Expand status to distinguish
`not installed`, `installed but unrecorded`, `recorded but missing binary`,
`state version stale`, `probe failed`, and `credential repair needed`.

Phase 3 should harden code ownership. Artifact plans should carry ownership
kind, expected file type, optional symlink target policy, and whether the path is
created by update or only verified. For package-manager installs, record enough
metadata to avoid deleting a user-managed directory that only happens to match
the path. Gemini's npm package directory is the first place this matters.

Phase 4 should unify import and credential summaries. The lifecycle status
should not duplicate credential registry logic; it should ask the registry for a
per-harness summary and ask import modules whether curated import is supported.
This phase should also make Gemini Keychain-backed OAuth and Hermes no-import
boundaries visible without turning them into errors.

Phase 5 should add optional JSON output for automation. The JSON schema should
include harness ID, display name, install status, binary path, version, state
version, import timestamp, credential summary counts, owned artifacts, preserved
paths, and next action. It should redact anything credential-like and should not
include raw command stderr unless explicitly classified as safe.

Phase 6 should finish release validation. Add manual test rows for each harness
and a scripted non-destructive smoke that exercises `hazmat harness status`,
`hazmat harness status <harness>`, `hazmat harness update <harness> --dry-run`,
and `hazmat harness uninstall <harness> --dry-run`.

## Error Handling Rules

Status should degrade gracefully. A missing binary is a status row, not an
error. A version probe failure should show `version unavailable` with a short
reason and should still render state and credential information. An unreadable
state file should mark state as unreadable and avoid destructive actions.

Update should fail early when the agent user is missing or a supply-chain check
fails. State recording should happen after a successful update or successful
manual verification only. Failed state recording should remain a warning for
compatibility with existing bootstrap behavior.

Uninstall should be conservative. It should refuse paths outside the managed
agent home, refuse the agent home root itself, and refuse type drift by default.
It should remove metadata only when state is readable. It should not remove
credentials, imported basics, session history, profile roots, provider secrets,
Git credentials, or SSH identities by default.

## Testing Strategy

Model-level tests:

- update `MC_HarnessLifecycle` before any change that adds a harness, changes
  dry-run behavior, changes explicit uninstall scope, or changes rollback scope
- run TLC and record the result in `tla/08_harness_lifecycle.md`

Go unit tests:

- registry completeness for all five harnesses
- status object construction for installed, missing, stale, and probe-failure
  states
- update delegation from `hazmat harness update` and legacy bootstrap commands
- uninstall planning for present, missing, drifted, and out-of-scope artifacts
- metadata removal preserving other harness records and core init state
- Hermes manual ownership boundary
- credential summary selection by harness

CLI smoke:

- help for `harness`, `harness status`, `harness update`, and
  `harness uninstall`
- dry-run update and uninstall for at least one fixture harness if a hermetic
  agent-user fixture becomes available

Manual tests:

- one status/detail pass for each harness
- dry-run update for each harness
- dry-run uninstall for each harness
- destructive uninstall only on a disposable agent user
- one launch after reinstall/update to confirm preserved credentials still work

## Implementation Follow-Ups

The plan breaks naturally into four follow-up beads:

- `sandboxing-imqb`: normalize lifecycle status and add a future-proof JSON
  shape
- `sandboxing-4drn`: harden owned artifact metadata and drift handling
- `sandboxing-5a7c`: unify lifecycle credential/import summaries through
  registry-owned helpers
- `sandboxing-9yy1`: add non-destructive lifecycle smoke coverage across all
  supported harnesses

Each of those can land independently after the current lifecycle CLI branch.
Any follow-up that changes what uninstall deletes must update
`MC_HarnessLifecycle` first.
