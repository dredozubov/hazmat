# Session-Home Typed Adapter Architecture

Status: Implemented initial adapter registry; live validation pending
Date: 2026-06-13
Parent issue: `sandboxing-ywqd`
Blocking issue: `sandboxing-gabh`

## Purpose

The first activation smoke for session-local `HOME` reached `hazmat exec` and
then failed closed with adapter-required durable paths. That is the correct
failure mode. It is not a setup drift problem, and `hazmat init` or a generic
doctor repair must not try to clear it by copying or symlinking broad agent-home
state.

This design defines the adapter layer that can turn those blockers into
implemented behavior while preserving the durable-mirror security properties.
The core rule is that a manifest class may become activation-ready only when a
typed adapter owns its materialization, writeback, cleanup, and diagnostics.

## Non-Goals

- Do not add a blanket copy of `/Users/agent` into the session home.
- Do not bridge broad toolchain or harness directories back into persistent
  agent home.
- Do not copy session-mutated executable shims back to persistent state without
  a typed installer/update path.
- Do not make `hazmat check` or plan-only explain output probe private
  agent-owned paths.
- Do not make live activation smokes part of the default test suite.

## Adapter Contract

A session-home adapter is a small policy module selected by manifest class and,
where necessary, by relative path. It must declare:

- supported manifest classes and path prefixes,
- copy-in behavior,
- writeback behavior,
- executable handling,
- symlink handling,
- persistent-path conflict handling,
- cleanup behavior,
- public blocker wording and JSON metadata,
- hermetic tests and any approval-gated live smoke.

Adapters return one of four outcomes for each assembly entry:

| Outcome | Meaning |
| --- | --- |
| `implemented` | The adapter can materialize the path for activation. |
| `ignored-ephemeral` | Existing persistent state is intentionally not imported; the session gets an empty local path. |
| `manual-only` | Hazmat can explain the issue but has no mutating repair. |
| `unsupported` | Activation remains blocked for this path. |

Only `implemented` and `ignored-ephemeral` remove an activation blocker.
`manual-only` and `unsupported` must keep activation fail-closed.

## Initial Adapter Set

### Toolchain Cache Adapter

Targets: `.cargo`, `.npm`, `.node-gyp`, `.gem`, `.gradle`, `.ivy2`, `.m2`,
`.pub-cache`, `.rustup`, `.sbt`, `.swiftpm`, `.terraform.d`, `.bun`, `.deno`,
`.local/lib`, and similar `toolchain-state` roots.

Default behavior: `ignored-ephemeral`.

Rationale: package manager caches and build metadata are useful for speed, but
they are not credentials and should not block the security boundary. Existing
persistent cache content can remain outside the session. Integrations may still
grant explicit read-only host cache roots when a project asks for them. Any
session-created cache content is disposable unless a future package-manager
adapter owns a narrower checked-writeback contract.

Tests:

- planner maps known toolchain cache roots to `ignored-ephemeral`;
- persistent cache existence does not create an activation blocker;
- no writeback receipt is produced;
- executable child paths under these roots are not copied back.

### Executable Tooling Adapter

Targets: `.local/bin`, `.opencode/bin`, `.claude/hooks`, package-manager `bin`
children, and other `executable-tooling` entries.

Default behavior: `implemented` as seed-only executable import with no
writeback.

Rationale: persistent executable shims are authority-bearing. If a session can
rewrite them and Hazmat copies them back, future sessions may run attacker-chosen
code before any prompt or package-manager command. The first activation
therefore imports existing executable paths into the session home, preserves
their executable bits, rejects symlinks, and never writes session mutations back
to persistent agent state. A future package-manager adapter may add a narrower
installer/update receipt model.

- seed-only executable import for trusted existing files, with no writeback;
- future typed installer/update receipts for a specific package manager.

Tests:

- executable persistent paths do not block once the executable-tooling adapter
  is selected;
- materialization copies existing executable paths into the session home;
- symlinked executable paths are rejected;
- activation materialization does not produce executable writeback receipts.

### Harness State Adapter

Targets: `.agents`, `.claude`, `.codex`, `.cursor`, `.gemini`, `.opencode`,
`.qwen`, `.config/opencode`, `.config/mcp`, and broad `.hazmat` harness state
outside explicit transcript bridges.

Default behavior: supported broad parent roots are `ignored-ephemeral`; narrow
state surfaces stay `manual-only` or `unsupported` per harness. Current adapter
labels are explicit (`claude-state`, `codex-state`, `opencode-state`,
`gemini-state`, `qwen-state`, `mcp-state`, and related roots). This means the
mere existence of `.claude`, `.codex`, `.opencode`, `.hazmat`, or similar
supported parents no longer blocks activation: the session gets an empty local
parent and only explicit child paths are imported, bridged, or materialized.
Narrow surfaces such as `.config/mcp` and `.config/opencode` still block until
their owning adapter defines portable config and credential behavior. Managed
prompt assets are handled separately: launch-time asset sync remaps explicit
asset destinations into the active session-local `HOME` and does not write the
persistent harness-assets manifest for ephemeral session homes.

Rationale: broad harness state may contain auth tokens, sockets, remembered tool
permissions, plugins, MCP descriptors, or remote-control state. The safe parent
directory behavior is therefore to start with an empty session-local root and
let the owning harness adapter select portable config, credential delivery,
transcript roots, and volatile caches explicitly.

Tests:

- supported broad harness roots do not block and are not copied from
  persistent state;
- unsupported narrow harness roots keep activation blocked;
- blocker metadata identifies the owning harness/state surface instead of a
  generic adapter-required bucket;
- known transcript roots stay `durable-external`;
- known prompt/config assets that are already classified as seed-only remain
  seed-only;
- managed prompt-asset sync targets the active session-local home and leaves
  persistent agent state untouched;
- diagnostics do not claim `doctor --fix` can repair broad harness state.

### XDG Data And Config Adapter

Targets: `.local/share`, broad `.config`, and other `xdg-data` or `xdg-config`
entries not covered by a narrower path rule.

Default behavior: `ignored-ephemeral` for broad `.config`, `.local`, and
`.local/share` roots; `manual-only` for future XDG paths that are not covered by
the initial manifest.

Rationale: broad XDG data/config trees mix harmless preferences with auth,
plugin, cache, and runtime state. Hazmat should not import the whole tree. Each
covered path needs a narrower adapter or should remain unavailable in the
session-local home. Because activation already points `XDG_CONFIG_HOME` and
`XDG_DATA_HOME` at session-local directories, ignoring existing broad XDG roots
is safer than blocking on them or copying them.

Tests:

- broad XDG paths do not block activation and are not copied from persistent
  state;
- covered Git config stays seed-only;
- covered harness config follows its harness adapter;
- plan-only output does not host-read private XDG contents.

## UX Contract

When blockers remain, Hazmat should tell the user what kind of work is required
without sending them through an init loop:

- `hazmat check` remains read-only and never prompts for sudo.
- `hazmat check` should not recommend `hazmat init` for adapter-required
  session-home blockers.
- `hazmat doctor --dry-run` may show the adapter plan, but it must not imply a
  mutating repair exists when all remaining blockers are manual-only or
  unsupported.
- `hazmat doctor --fix` may only execute adapters that declare an implemented
  materializer and post-fix verifier.
- Live validation stays behind explicit smoke commands and user approval.

Blocker JSON should continue to include relative path, reason, manifest class,
runtime policy, and future adapter outcome/name. Human output should group by
reason first, then include class/policy/adapter labels for each path.

## Implementation Order

1. Add adapter outcome/name fields to the internal blocker and public
   `session_home.activation_blockers` contract.
2. Add a pure adapter registry that maps assembly entries to adapter outcomes
   without touching the filesystem.
3. Change activation blocker computation so existing toolchain cache roots with
   `ignored-ephemeral` no longer block activation.
4. Keep broad harness state and any unsupported narrow paths blocked until
   their adapters become implemented.
5. Add hermetic tests for every manifest class and for the user-observed live
   failure classes.
6. Only after non-live tests prove the plan, ask for approval to run the live
   activation smoke.

## Security Invariants

- Activation readiness is computed from typed adapter outcomes, not from a
  best-effort filesystem copy.
- Missing persistent paths never block activation.
- Existing private agent-home paths are inspected only through approved
  activation-time agent helpers, never by read-only `check`.
- No adapter follows symlinks from persistent agent state into arbitrary host
  paths.
- No adapter writes persistent executable state unless a package-manager or
  installer-specific contract owns that write.
- Cleanup removes session-local materialization without following durable
  bridges.

## Testing Architecture

Default tests are hermetic and non-live:

- pure registry tests cover every `AgentHomeStateClass`;
- launch-plan tests cover blocker calculation by adapter outcome;
- materializer tests cover symlink rejection and no writeback for ignored
  ephemeral paths;
- session contract tests cover defensive copies and stable JSON fields;
- diagnostic tests cover no `hazmat init` retry advice for adapter blockers and
  no `doctor --fix` recommendation when no executable adapter exists.

Approval-gated tests are separate:

- session-home activation smoke for go, npm, pip, cargo, git, and harness
  startup;
- harness-specific live smoke only after the corresponding harness adapter has
  non-live coverage;
- push hooks must not run these live paths without explicit user approval.
