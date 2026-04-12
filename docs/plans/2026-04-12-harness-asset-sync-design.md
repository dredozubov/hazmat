# Managed Harness Asset Sync for User-Global Prompt Assets

Status: Proposed
Date: 2026-04-12
Related issue: `sandboxing-3gg6`
Related gate: `sandboxing-eyqk`

## Position

Hazmat should add a **managed launch-time sync** for non-secret, user-global
harness prompt assets.

This should ship as a narrow Mode A slice:

- copy on launch, not live host mounts
- hardcoded asset roots, not repo-defined manifests
- prompt/text assets only in v1
- managed ownership and atomic refresh, not destructive re-use of the existing
  `hazmat config import` apply path

The feature exists to solve one real compatibility gap:

- project-local prompt assets mostly already work when they live under the
  mounted project directory
- user-global prompt assets do not automatically appear in the agent home

This is therefore a **harness compatibility feature**, not a broad new session
capability class.

## Why The Existing Import Path Is Not Enough

Current curated imports are explicit, one-shot commands:

- `hazmat config import claude`
- `hazmat config import opencode`

That path is intentionally manual and destructive:

- it scans once
- it may remove existing destination paths before replacing them
- it records import metadata, not continuous sync ownership

That is acceptable for a user-invoked migration command. It is the wrong shape
for an always-on launch hook.

If Hazmat simply re-runs the current apply path every session, it will:

- clobber unmanaged files already present under `/Users/agent`
- make concurrent-session races worse
- make `hazmat explain` impure if wired in the wrong place
- preserve the current top-level symlink behavior, which is too permissive for
  a default-on automatic feature

The new feature should therefore be a separate subsystem with separate state.

## Decision Summary

1. Ship **Mode A** first: managed copy-on-launch sync.
2. Support `claude`, `codex`, and `opencode` in v1.
3. Do **not** add `gemini` until Hazmat has an actual `hazmat gemini` harness.
4. Limit v1 to **prompt/text assets** only.
5. Keep `hazmat config import ...` as a separate manual snapshot workflow.
6. Keep monorepo `<git-root>/.<tool>` pickup as a separate issue.
7. Treat plugins, MCP config, executable bundles, provider/runtime config, and
   durable auth state as out of scope for this slice.

## V1 Scope

### Included

Claude:

- `~/.claude/CLAUDE.md`
- `~/.claude/commands/`
- `~/.claude/skills/`
- `~/.claude/agents/`

Codex:

- `~/.codex/AGENTS.md`
- `~/.codex/prompts/`
- `~/.codex/rules/`
- `~/.agents/skills/`

OpenCode:

- `~/.config/opencode/commands/`
- `~/.config/opencode/agents/`
- `~/.config/opencode/skills/`

### Explicitly Deferred

Claude:

- `~/.claude/.credentials.json`
- `~/.claude/sessions/`
- `~/.claude/projects/`
- `~/.claude/plugins/`
- MCP config and hooks

Codex:

- `~/.codex/auth.json`
- `~/.codex/history.jsonl`
- `~/.codex/sessions/`
- `~/.codex/.codex-global-state.json`
- `~/.codex/themes/`
- `~/.codex/config.toml`

OpenCode:

- `~/.config/opencode/opencode.json`
- `~/.config/opencode/plugins/`
- `~/.config/opencode/tools/`
- `~/.config/opencode/themes/`
- `~/.config/opencode/modes/`
- `~/.config/opencode/auth.json`
- `~/.local/share/opencode/auth.json`

Gemini:

- all user-global Gemini paths until a built-in Gemini harness exists

## Non-Goals

- Live host-path mounts or symlink farms
- Repo-controlled declarations of host asset roots
- Per-project disabling that implies true per-session absence of already-synced
  assets
- Expanding the current session-wide seatbelt deny list for harness auth files
  before `sandboxing-gg16` migrates those secrets out of `/Users/agent`
- Migration of auth or other durable secrets into the agent home
- Replacing the explicit `config import` commands
- Monorepo `<git-root>/.claude` or `<git-root>/.gemini` discovery

## Asset Root Specification

The allowed asset roots should be hardcoded in Go.

The spec needs to distinguish:

- the harness
- the host-side source root
- the agent-home destination root
- whether the source is a file root or directory root

```go
type harnessAssetKind string

const (
    harnessAssetFileRoot harnessAssetKind = "file-root"
    harnessAssetDirRoot  harnessAssetKind = "dir-root"
)

type harnessAssetSpec struct {
    Harness   HarnessID
    Key       string
    Kind      harnessAssetKind
    HostPath  string
    AgentPath string
}
```

Example:

```go
var harnessAssetSpecs = map[HarnessID][]harnessAssetSpec{
    HarnessClaude: {
        {Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: "~/.claude/CLAUDE.md", AgentPath: agentHome + "/.claude/CLAUDE.md"},
        {Harness: HarnessClaude, Key: "commands",  Kind: harnessAssetDirRoot,  HostPath: "~/.claude/commands",  AgentPath: agentHome + "/.claude/commands"},
        {Harness: HarnessClaude, Key: "skills",    Kind: harnessAssetDirRoot,  HostPath: "~/.claude/skills",    AgentPath: agentHome + "/.claude/skills"},
        {Harness: HarnessClaude, Key: "agents",    Kind: harnessAssetDirRoot,  HostPath: "~/.claude/agents",    AgentPath: agentHome + "/.claude/agents"},
    },
}
```

These specs must not come from project config, repo files, or integration
manifests.

## Managed Ownership Manifest

The sync subsystem needs a host-owned ownership manifest separate from:

- `~/.hazmat/state.json`
- manual import metadata

Recommended file:

- `~/.hazmat/harness-assets.json`

Recommended shape:

```json
{
  "version": 1,
  "harnesses": {
    "claude": {
      "entries": {
        "/Users/agent/.claude/commands/create-prd.md": {
          "spec_key": "commands",
          "dest_path": "/Users/agent/.claude/commands/create-prd.md",
          "source_path": "/Users/dr/.claude/commands/create-prd.md",
          "kind": "file",
          "fingerprint": "sha256:...",
          "managed_at": "2026-04-12T09:00:00Z"
        }
      }
    }
  }
}
```

This manifest is the authority for what Hazmat may later update or delete.

It must not be inferred from:

- destination path prefixes alone
- current file contents alone
- whether a file "looks imported"

## Ownership Rules

The sync engine should use these rules:

1. If a destination path is already manifest-owned by this subsystem, Hazmat
   may replace or delete it.
2. If a destination path is not manifest-owned, Hazmat must not overwrite it
   just because it lives under a managed directory.
3. If an unmanaged destination path already exists and is byte-for-byte equal
   to the source, Hazmat may **adopt** it into the manifest.
4. If an unmanaged destination path exists and differs from the source, Hazmat
   should skip it and emit a warning rather than clobbering it.

This is the main behavioral difference from `hazmat config import`, which is
allowed to be more forceful because it is explicitly user-invoked.

## Symlink And Escape Rules

The existing import helpers are too permissive for default-on launch-time use.

V1 rules should be:

1. The declared root itself may be absent. That is not an error.
2. A top-level file or directory entry may be a symlink.
3. After `EvalSymlinks`, the resolved top-level target must still stay within
   the declared allowed root.
4. Nested symlinks inside a copied directory tree are rejected in v1.
5. Broken symlinks, unreadable entries, sockets, fifos, device files, and
   other non-regular/non-directory entries are skipped with a warning.

The key property is: auto-sync must never turn `~/.claude/commands/foo.md` into
an ambient gateway to arbitrary host paths outside the hardcoded allowlist.

This validation should live inside the harness-asset subsystem itself. It
should not reuse `credentialDenySubs` as a shortcut, because that list also
drives the generated SBPL deny rules for `/Users/agent`, and this feature is
not allowed to break the current auth runtime before the secret-store migration
exists.

## Sync Algorithm

The sync engine should work in four phases.

### 1. Build Desired State

For a given harness:

- enumerate every configured hardcoded asset root
- ignore missing roots
- for file roots:
  - include the file if it exists and validates
- for directory roots:
  - enumerate only direct children
  - resolve and validate each child
  - classify each desired destination path

Each desired entry should carry:

- spec key
- source path
- destination path
- entry kind
- fingerprint

Fingerprints should be content-based:

- files: SHA-256 of content
- directories: deterministic tree hash over relative paths, file modes, and
  file contents

### 2. Diff Against Manifest And Filesystem

For each desired entry:

- if manifest-owned and fingerprint unchanged: no-op
- if manifest-owned and changed: replace via staged copy
- if destination absent: create via staged copy
- if destination exists, unmanaged, and equal: adopt
- if destination exists, unmanaged, and differs: skip with warning

For each manifest-owned entry no longer present in desired state:

- delete it if the destination still exists
- remove it from the manifest

### 3. Apply Atomically

Every write should use a temp sibling plus rename:

- files: write `dest.tmp-<pid>-<rand>`, `fsync`, rename
- directories: copy into temp dir, then rename into place

The sync path must never `RemoveAll` an entire managed root before rebuilding
it.

### 4. Persist Manifest

Write the updated manifest only after successful entry application.

If a specific entry fails:

- keep prior manifest ownership for unaffected entries
- leave the failed entry unchanged if possible
- emit a warning and continue when the failure is local to one asset

The launch should fail only for subsystem-level failures such as:

- cannot acquire the lock
- cannot read or write the ownership manifest
- destination path escapes the expected agent-home prefix

## Locking

The sync path needs a per-harness lock because Hazmat already documents shared
agent-home harness state as racy.

Recommended lock files:

- `~/.hazmat/locks/harness-assets-claude.lock`
- `~/.hazmat/locks/harness-assets-codex.lock`
- `~/.hazmat/locks/harness-assets-opencode.lock`

Use an OS-level advisory lock around the entire sync transaction:

- load manifest
- compute desired state
- apply changes
- rewrite manifest

This is enough for v1. Cross-host coordination is not needed because the repo
is macOS-local by design.

## Session Wiring

The new subsystem should be planned in `resolvePreparedSession` and executed in
`beginPreparedSession`.

It should **not** perform filesystem mutations directly in
`resolvePreparedSession`, because:

- `hazmat explain` calls `resolvePreparedSession`
- explain must remain a pure preview

Recommended structure:

1. add a helper that maps `commandName` to an optional harness asset plan
2. call it from `resolvePreparedSession(...)` after `applyIntegrations(...)`
3. merge the resulting plan into `prepared.HostMutationPlan`
4. surface the plan in the session contract and explain output through the
   existing host-mutation path

The helper should key off `commandName`, not a new `harnessID` field on
`harnessSessionOpts`. The session resolver already knows whether the command is
`claude`, `codex`, `opencode`, `shell`, or `exec`.

## UX And Config

### Default Behavior

Default on for:

- `hazmat claude`
- `hazmat codex`
- `hazmat opencode`
- `hazmat explain --for claude|codex|opencode` as preview only

Default off for:

- `hazmat shell`
- `hazmat exec`

### CLI Escape Hatch

V1 should expose:

- `--skip-harness-assets-sync`

That flag should mean:

- do not refresh managed assets for this launch

It should **not** claim to hide already-synced assets from the harness. Under
the current persistent `/Users/agent` home, Hazmat cannot guarantee true
per-session absence without the later session-local HOME design.

### Config

V1 should add only a global toggle:

- `session.harness_assets`

Semantics:

- default: `true`
- `false`: disable automatic sync for supported harness commands

Per-project or per-harness disable should be deferred until Hazmat has a
session-local assembled home or another mechanism that can make the semantics
honest.

## Relationship To `hazmat config import`

Do not deprecate the existing import commands in this slice.

Their role becomes:

- explicit one-time snapshot/migration
- manual adoption path for users who want agent-home files without automatic
  launch-time refresh

The new sync path is different:

- automatic
- narrow
- non-destructive to unmanaged destinations
- host-owned manifest backed

Sharing low-level copy helpers is fine. Sharing apply semantics is not.

## TLA+ Impact

### Required In V1

No TLA model change is required for the managed copy-on-launch slice.

Reason:

- no new live host mounts are added to the session contract
- no new seatbelt allow or deny rules are added
- no Docker Sandbox mount-planner behavior changes

### Important Constraint

Do **not** expand `credentialDenySubs` in this feature.

That helper currently feeds both:

- host-side deny-zone checks
- the generated SBPL deny list for `/Users/agent`

Adding `/.claude/.credentials.json` or similar paths there now would deny the
current agent-home auth files before `sandboxing-gg16` replaces them with a
host-owned secret-store adapter. That would couple this compatibility feature
to a different migration track and create an upgrade cliff.

The new sync path should instead use its own explicit allowlist validator based
on:

- hardcoded root specs
- top-level symlink containment checks
- nested symlink rejection
- destination-prefix checks under `/Users/agent`

### Documentation Note

The harness lifecycle docs should note that launch-time managed asset sync is
governed by tests/docs rather than the current harness metadata state machine
proof.

## Test Plan

### New Unit Coverage

Add focused tests for:

- hardcoded asset-spec resolution per harness
- absent roots are ignored
- file-root sync
- directory-root child enumeration
- manifest-owned update
- manifest-owned deletion of stale entries
- unmanaged-equal adoption
- unmanaged-different conflict skip
- nested symlink rejection
- top-level symlink escape rejection
- per-harness lock behavior

### Session / Explain Coverage

Add tests proving:

- `resolvePreparedSession("claude", ...)` includes a planned host mutation when
  managed assets exist
- `resolvePreparedSession("shell", ...)` does not
- `hazmat explain --for claude` previews the planned mutation without mutating
  the filesystem
- `beginPreparedSession(...)` applies the mutation before launch

### Existing Deny-List Coverage

No deny-list expansion tests are needed in this slice.

Instead, add focused tests for the new asset-root validator:

- allowed file root accepted
- allowed directory child accepted
- top-level symlink escaping the root rejected
- nested symlink in copied tree rejected
- destination outside the expected agent-home prefix rejected

## Implementation Outline

1. Add `hazmat/harness_assets.go` for spec definitions, manifest I/O, locking,
   diffing, and staged copy helpers.
2. Add a new host-owned manifest file under `~/.hazmat/`.
3. Add a planning helper that returns a `sessionMutationPlan` for supported
   harness commands.
4. Merge that plan in `resolvePreparedSession(...)`.
5. Add `session.harness_assets` config plumbing and the
   `--skip-harness-assets-sync` flag to harness commands plus explain.
6. Update docs:
   - `README.md`
   - `docs/usage.md`
   - `docs/design-assumptions.md`
   - `docs/claude-import.md`
   - `docs/opencode-import.md`

## Deferred Follow-Ups

1. Gemini harness support once `hazmat gemini` exists.
2. Monorepo `<git-root>/.<tool>` prompt-asset discovery.
3. Optional Codex/OpenCode theme sync if there is a concrete compatibility need.
4. Manual-import tightening so its symlink rules align with the new managed
   sync path.
5. Session-local assembled home integration after the persistent-state manifest
   track is ready.
