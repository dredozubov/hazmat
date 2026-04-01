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

Hazmat canonicalizes the project path (`Abs` + `EvalSymlinks`) before storing
the pin. At session start, the session's project path is resolved the same way
and compared for exact equality. This means `~/workspace/my-app` and
`/Users/dr/workspace/my-app` both resolve to the same canonical pin. Re-running
`packs.pin` for the same project replaces the existing pin set.

## Built-In Packs

| Pack | Detects | Read dirs | Env passthrough | Snapshot excludes |
|------|---------|-----------|-----------------|-------------------|
| `go` | `go.mod` | — | `GOPATH`, `GOPROXY`, `GOPRIVATE`, `CGO_ENABLED` | `vendor/` |
| `node` | `package.json` | `/opt/homebrew/lib/node_modules` | `NODE_ENV` | `node_modules/`, `.next/`, `.turbo/`, `.nuxt/`, `out/`, `.vercel/` |
| `python-poetry` | `pyproject.toml`, `poetry.lock` | `~/.local/share/pypoetry` | `VIRTUAL_ENV` | `.venv/`, `__pycache__/`, `.pytest_cache/`, `.mypy_cache/`, `.ruff_cache/`, `*.pyc`, `dist/`, `*.egg-info/` |
| `rust` | `Cargo.toml` | `~/.cargo/registry`, `~/.rustup/toolchains` | `RUSTUP_HOME`, `CARGO_HOME`, `CARGO_TARGET_DIR` | `target/` |
| `terraform-plan` | `main.tf`, `terraform.tf` | — | — | `.terraform/`, `*.tfstate`, `*.tfstate.backup` |
| `tla-java` | `MC_*.cfg` files | `/opt/homebrew/opt/openjdk`, `/Library/Java` | `JAVA_HOME` | `tla/states/`, `*.dot` |

Packs influence three parts of session setup:

1. **Read-only access** — toolchain and cache directories
2. **Pre-session snapshot excludes** — reproducible build artifacts
3. **Safe environment passthrough** — passive selectors from the invoker's environment

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

## Repo-Recommended Packs

A repo can declare which packs it needs in `.hazmat/packs.yaml`:

```yaml
packs:
  - go
  - tla-java
```

This file is pure data: a list of existing pack names. No inline definitions,
no custom paths, no env keys, no executable hooks.

**Repo owns intent; host owns trust.** Hazmat reads the file as a hint, not
authority. On first encounter, it prompts:

```
hazmat: this repo recommends packs: go, tla-java
hazmat: source: /Users/dr/workspace/hazmat/.hazmat/packs.yaml
hazmat: approve these packs for this repo? [y/N]
```

Approval is stored outside the repo in `~/.hazmat/approvals.yaml`, keyed by
canonical project path + SHA-256 of the file contents:

- Same repo + same file = no prompt (approved)
- File changes (pack added or removed) = re-approve
- Repo cloned to a different path = re-approve

If the user declines, packs are not activated. They can still use `--pack`
manually.

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

## Self-Hosting: Developing Hazmat Under Hazmat

Hazmat's own repo includes `.hazmat/packs.yaml` recommending `go` and
`tla-java`. On first `hazmat claude` in this repo, approve the recommended
packs and the session gets Go toolchain support plus Java paths for TLC model
checking.

Prerequisites:
- Go installed locally
- Java 17+ installed locally (Homebrew: `brew install openjdk`)
- `~/workspace/tla2tools.jar` downloaded (see `tla/VERIFIED.md`)
- `~/workspace` as the sole entry in `session.read_dirs`
