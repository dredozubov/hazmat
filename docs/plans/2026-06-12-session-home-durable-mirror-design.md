# Session-Home Durable Mirror Semantics

Status: Proposed
Date: 2026-06-12
Parent issue: `sandboxing-eyqk`
Design issue: `sandboxing-ecfm`

## Purpose

Session-local `HOME` cannot become executable until Hazmat has precise behavior
for state that is currently durable under `/Users/agent`. A generic "copy the
agent home into `/private/tmp`" approach is too slow, leaks too much authority
into the session, and creates unclear post-session writeback behavior. A generic
symlink approach is worse: it reintroduces persistent agent-home authority
through the side door.

The durable mirror design is therefore adapter-based. Every manifest entry must
have one of a small set of explicit runtime policies before `activation_ready`
can become true.

## Runtime Policies

`ephemeral-cache`: The session receives an empty session-local path. No copy-in,
no copy-back, and no activation blocker. This is for cache state that is safe to
lose, such as `.cache`.

`durable-external`: The session receives a narrow bridge to a durable root
outside the session home. This is only valid when the path must survive crashes
and post-session export, and the bridge contract is explicit. Current examples:
Claude `.claude/projects` and Hermes `.hazmat/hermes/projects`.

`seed-only`: Hazmat copies small durable inputs into the session home before
launch, but never writes session changes back automatically. This is the default
for shell RC files, `.gitconfig`, `.config/git` configuration, prompt assets,
and harness configuration that should be available at startup but should not be
mutated by untrusted session code.

`checked-writeback`: Hazmat copies a bounded file or directory into the session
home and may copy it back after launch, but only if the persistent source
fingerprint is unchanged since copy-in. If both persistent and session copies
changed, Hazmat must keep a recovery artifact and report a doctor finding rather
than silently choosing a winner.

`adapter-required`: No generic materialization is allowed. The owning harness or
toolchain must provide a typed import/export adapter first. This is the default
for broad harness state, executable tool state, and large toolchain directories
such as `.cargo`, `.rustup`, `.npm`, `.m2`, and `.gradle`.

## Initial Manifest Mapping

Shell config files (`.zshrc`, `.zprofile`, `.bashrc`, `.bash_profile`,
`.profile`, `.zshenv`) are `seed-only`. Hazmat setup remains responsible for
persistent edits outside the session.

Git config (`.gitconfig`, `.config/git`) is `seed-only`. Git credentials are not
part of this policy; they must remain in host-owned credential storage or
session-local broker/runtime delivery.

Transcript roots are `durable-external`. They must never be hidden inside the
ephemeral home because crash recovery and post-session export depend on durable
state.

General harness state is `adapter-required` unless a path is explicitly listed
as `seed-only` or `durable-external`. This avoids accidentally persisting login
tokens, sockets, or unstable database files.

Toolchain state and executable state are `adapter-required` by default. Large
package caches should be supplied by integrations as explicit read-only host
inputs or left ephemeral; executable shims must not be copied back from an
untrusted session without a typed installer/update path.

XDG cache is `ephemeral-cache`. XDG config/data are `adapter-required` unless a
specific covered path is classified more narrowly.

## Ownership And Cleanup

The session metadata root should be host-owned so Hazmat can discover stale
sessions without entering the sandbox. The assembled `home` and XDG
subdirectories should be agent-owned `0700` so the launched process can use them
as a normal home. Bridge creation must validate both sides with cleaned absolute
paths and must not follow symlink escapes.

Cleanup must not follow durable bridges. The safe shape is:

1. remove agent-owned session-home contents through the agent helper,
2. remove host-owned session metadata through the host process,
3. leave durable-external roots intact,
4. report cleanup failures without enabling activation.

Crash cleanup may remove only marked session directories older than the bounded
age. It must preserve durable-external roots and any checked-writeback recovery
artifacts.

## Writeback Rules

Checked writeback is opt-in per adapter. It requires:

- source fingerprint at copy-in,
- session fingerprint at copy-out,
- atomic write to a temporary persistent path,
- no writeback when the persistent source changed concurrently,
- no writeback of symlinks that escape the declared root,
- a receipt describing copied paths, conflicts, and preserved recovery files.

The first activation can ship with zero checked-writeback adapters if all
currently durable state is covered by `seed-only`, `durable-external`,
`ephemeral-cache`, or `adapter-required` blockers.

The follow-up adapter architecture is tracked in
`docs/plans/2026-06-13-session-home-typed-adapters-design.md`. That design keeps
activation fail-closed for executable, broad harness, and broad XDG state while
allowing harmless toolchain cache roots to become explicit ephemeral session
state rather than setup drift.

## Testing Architecture

Pure planner tests must fail when a manifest entry has no runtime policy.

Hermetic filesystem tests must cover copy-in, bridge creation, symlink escape
rejection, cleanup without following bridges, checked-writeback conflict
handling, and stale-home cleanup.

Session contract tests must expose `activation_ready=false` and blockers until
every manifest entry is mapped to an implemented policy.

Live activation tests are separate and sudo-adjacent. They must ask before
running and must cover go, npm, pip, cargo, git, and harness startup flows.
