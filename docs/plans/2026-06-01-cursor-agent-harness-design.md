# Cursor Agent Managed Harness Design

Status: Implemented
Date: 2026-06-01

## Decision

Hazmat supports Cursor Agent as a managed foreground/headless harness named
`cursor-agent`.

The first slice is intentionally conservative:

- add `HarnessCursorAgent` to the built-in harness registry
- add `hazmat cursor-agent` with native containment and Docker Sandbox routing
- add `hazmat bootstrap cursor-agent` as a detection-only verifier for
  `/Users/agent/.local/bin/cursor-agent`
- forward Cursor Agent argv exactly as supplied
- keep Cursor Agent out of `hazmat config import`
- do not import host Cursor IDE state, host `~/.cursor`, auth settings,
  workspace trust, MCP/extensions, memories, or profile data
- do not add a Hazmat-managed `CURSOR_API_KEY` credential grant in v1
- cover the launch path with unit tests and fake-binary harness smoke policy

## Compatibility Intake

Cursor Agent fits Hazmat's harness lane because it has a foreground/headless CLI
entrypoint that can run as `/Users/agent` under the same session contract as the
other managed harnesses. Open Design's candidate runtime uses:

```text
cursor-agent --print --output-format stream-json --stream-partial-output --force --trust [--workspace cwd]
```

with prompt input supplied on stdin. Hazmat does not synthesize that argv. The
operator or upstream integration must pass Cursor Agent's own headless flags
explicitly.

Service, daemon, browser-control, or IDE-control behavior is outside this v1
harness. Those modes would need a service lifecycle, trust-state design, and
new test coverage.

## State and Credential Boundary

Cursor Agent has broad host-profile surfaces in a normal workstation setup:
Cursor IDE state, auth/profile state, workspace trust, and possible API-key
configuration. Hazmat's v1 boundary is:

- Cursor Agent runs with `HOME=/Users/agent`
- agent-side Cursor state may persist under `/Users/agent`
- host Cursor IDE/profile state is never copied into `/Users/agent`
- no host Cursor state is stored under `~/.hazmat/secrets`
- `hazmat config agent` does not grant `CURSOR_API_KEY`

Users who need Cursor auth should run `hazmat cursor-agent -- login` or
configure Cursor Agent inside the contained profile. A future `CURSOR_API_KEY`
capability should start with the credential capability model and an explicit
registry descriptor.

## TLA+ Boundary

Adding Cursor Agent updates `MC_HarnessLifecycle.tla` before implementation:

- `cursor-agent` is in `Harnesses`
- `cursor-agent` is not in `ImportableHarnesses`
- bootstrap records the current harness state version
- dry-run behavior stays read-only
- rollback removes host-owned metadata but preserves agent-home artifacts unless
  `--delete-user` is used

TLC result after the model change:

- `Model checking completed. No error has been found.`
- `18,899,708 states generated`
- `943,528 distinct states found`
- `depth 15`

## Launch Contract

`hazmat cursor-agent` uses the common harness session path:

1. parse Hazmat flags and forward all Cursor Agent flags/args
2. resolve project/read/write access and integrations
3. route to native containment or Docker Sandbox
4. apply credential/materialization hooks, which are no-ops for Cursor Agent in
   v1 except for generic capabilities such as `--github`
5. skip harness asset sync because Cursor Agent has no v1 asset spec
6. launch `/Users/agent/.local/bin/cursor-agent` from the writable project root

Hazmat does not prepend `--force`, `--trust`, or any other Cursor Agent approval
flag. That keeps the wrapper transparent and avoids coupling Hazmat's
`session.skip_permissions` setting to Cursor-specific trust semantics before a
separate permission model exists.

## Follow-Ups

- real Cursor Agent install/update bootstrap with a pinned or otherwise audited
  supply-chain design
- typed `CURSOR_API_KEY` credential delivery, if Cursor Agent needs it outside
  contained profile setup
- curated host import only after enumerating exact Cursor auth/profile files and
  workspace-trust semantics
- service/browser-control mode only after Hazmat has a generic service lifecycle
