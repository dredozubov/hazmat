# Stack Packs

Stack packs are optional ergonomics overlays for common technology stacks.
They let Hazmat carry a small amount of stack-specific convenience into a
session without weakening the containment model.

## What Packs Can Do

- Add read-only directories that are useful for a stack, such as toolchains or caches
- Add snapshot exclude patterns for reproducible build artifacts
- Pass through a small safe set of environment selectors and path pointers from the invoker environment
- Show warnings or suggested commands for the stack

## What Packs Cannot Do

- Widen project write scope
- Bypass the seatbelt credential deny list
- Change network policy
- Inject arbitrary flags or preload-style environment variables
- Reconfigure Claude/OpenCode runtime settings

This is the core design rule: packs may reduce friction, but they may not
weaken Hazmat's trust boundary.

## Inspecting Packs

```bash
hazmat pack list
hazmat pack show node
```

`hazmat pack list` shows built-in packs, user-installed packs under
`~/.hazmat/packs/`, and any project pinning currently configured.

`hazmat pack show <name>` shows the pack's detect files, read-only paths,
env passthrough keys, snapshot excludes, warnings, and command hints.

## Activating Packs

Activate a pack for a single session:

```bash
hazmat claude --pack node
hazmat opencode --pack go
hazmat shell --pack rust
hazmat exec --pack python-poetry poetry run pytest
```

If no packs are active, Hazmat may suggest built-in packs based on files in the
project directory, such as `go.mod`, `package.json`, or `Cargo.toml`.

## Project Pinning

Pin packs so they auto-activate for a specific project:

```bash
hazmat config set packs.pin "~/workspace/my-app:node,go"
hazmat config set packs.unpin ~/workspace/my-app
```

Hazmat stores the raw project path in `~/.hazmat/config.yaml`, then matches it
as a canonical resolved path in `~/.hazmat/config.yaml`, then matches against
the canonical project path at session start. Re-running `packs.pin` for the
same project replaces the existing pin set instead of creating duplicate
entries for different spellings of the same path.

## Built-In Behavior

Today, packs can influence three parts of session setup:

1. Read-only access
2. Pre-session snapshot excludes
3. Safe environment passthrough

For example:

- `node` excludes `node_modules/`, `.next/`, `.turbo/`, and related build output from automatic snapshots
- `python-poetry` adds `~/.local/share/pypoetry` read-only when present
- `go` can pass through `GOPATH`, `GOPROXY`, and `GOPRIVATE`
- `rust` can pass through `RUSTUP_HOME`, `CARGO_HOME`, and `CARGO_TARGET_DIR`

Hazmat prints pack-derived read-only paths, snapshot excludes, registry redirect
keys, and warnings at session start so the behavior stays visible.

## Safe Environment Passthrough

Packs may only request env keys from Hazmat's allowlist. The intent is to allow
passive selectors and path pointers, not code-injection knobs.

Examples of allowed keys:

- `GOPATH`
- `GOPROXY`
- `RUSTUP_HOME`
- `CARGO_HOME`
- `VIRTUAL_ENV`
- `JAVA_HOME`

Examples of intentionally forbidden keys:

- `NODE_OPTIONS`
- `PYTHONPATH`
- `GOFLAGS`
- `LD_PRELOAD`
- `DYLD_INSERT_LIBRARIES`
- credential variables such as `AWS_ACCESS_KEY_ID` or `GITHUB_TOKEN`

Registry redirect keys like `GOPROXY` and `NPM_CONFIG_REGISTRY` are allowed but
surfaced explicitly at session start because they change where downloads come
from.

## User Packs

User-installed packs live in:

```text
~/.hazmat/packs/<name>.yaml
```

Hazmat loads built-in packs first, then falls back to user packs with the same
schema. User packs are validated before use:

- pack name format is restricted
- manifest size is bounded
- read-only paths are canonicalized and checked against Hazmat's credential deny zones
- env passthrough keys must be in the safe allowlist

If a pack is invalid, Hazmat rejects it instead of partially applying it.
