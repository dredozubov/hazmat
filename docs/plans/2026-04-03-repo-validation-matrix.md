# Repository Validation Matrix

Status: Current as of 2026-04-03
Scope: Public validation corpus for session integrations, self-hosting, and
Homebrew-backed toolchain resolution

This note records the real repositories used during the current Hazmat
integration and self-hosting work. It is not a design document. It is a
validation ledger: which repos were exercised, what was verified, and what the
observed outcome was.

For documentation and repeatable QA, the primary corpus should stay focused on
open-source repositories. The current public corpus is:

- `/Users/dr/workspace/mcp-serp-clustering`
- `/Users/dr/workspace/poly`
- `/Users/dr/workspace/hazmat`
- `/Users/dr/workspace/openclaw`
- `/Users/dr/workspace/beads`

These five repos are enough to cover the most important current integration
stories:

- Node runtime resolution from Homebrew
- repo-pinned `pnpm` bootstrap for Node sessions
- Rust sysroot resolution from Homebrew
- Go positive and negative cases
- TLA+/Java support through Hazmat self-hosting
- session honesty when a toolchain exists on the host but is not executable by
  the `agent` user

## Open-Source Validation Targets

### `/Users/dr/workspace/mcp-serp-clustering`

- Stack: Node.js / TypeScript
- Integration: `node`
- Mode: native containment
- Verified:
  - `hazmat explain -C ... --integration node --no-backup`
  - `hazmat shell -C ... --integration node`
  - inside session: `command -v node`, `node -v`
- Outcome:
  - Hazmat resolved the active Node runtime
  - session contract showed the Homebrew Node root in `Auto read-only`
  - `node -v` succeeded inside containment

### `/Users/dr/workspace/poly`

- Stack: Rust
- Integration: `rust`
- Mode: native containment
- Verified:
  - `hazmat explain -C ... --integration rust --no-backup`
  - `hazmat shell -C ... --integration rust`
  - inside session: `command -v rustc`, `rustc --print sysroot`,
    `cargo --version`
- Outcome:
  - Hazmat resolved the same Homebrew Rust toolchain that the contained session
    actually used
  - `rustc --print sysroot` and `cargo --version` both succeeded

### `/Users/dr/workspace/hazmat`

- Stack: Hazmat self-hosting (Go + TLA+ + beads tooling)
- Integrations: `go`, `tla-java`
- Mode: native containment
- Verified from the self-hosting branch:
  - contained `go test ./...`
  - contained `bd bootstrap --dry-run`
  - contained `cd tla && /bin/bash ./run_tlc.sh -help`
  - contained TLC invocation from `tla/`
- Outcome:
  - Hazmat can now produce a session usable for meaningful Hazmat development
  - Go tests pass in-session
  - TLC is reachable through the repo wrapper
  - `bd` is reachable inside the session via staged tool bootstrap
  - the repo also serves as the public `tla-java` validation target, because it
    contains the actual TLA runner workflow used by Hazmat

### `/Users/dr/workspace/openclaw`

- Stack: Node.js / TypeScript, repo-pinned `pnpm`, Python-side test helpers
- Integration: `node`
- Mode: native containment with `--docker=none`
- Verified from the self-hosting branch:
  - `hazmat explain -C ... --docker=none --integration node --no-backup`
  - contained `command -v pnpm`, `command -v corepack`, `pnpm --version`,
    `pnpm help`, `python3 --version`
- Outcome:
  - Hazmat resolved the active Node runtime
  - Hazmat bootstrapped `pnpm@10.32.1` into the agent home from
    `package.json#packageManager`
  - `corepack` was staged into the session when the host install was not
    directly agent-reachable
  - Python was already reachable in-session

### `/Users/dr/workspace/beads`

- Stack: Go
- Integration: `go`
- Mode: native containment with `--docker=none`
- Verified:
  - `hazmat explain -C ... --docker=none --integration go --no-backup`
  - `hazmat shell -C ... --docker=none --integration go`
  - inside session: `echo "$GOROOT"`, `command -v go`, `go version`
- Outcome:
  - Hazmat correctly refused to advertise a usable Go toolchain when the active
    Homebrew Go cellar was not executable by the `agent` user
  - session contract showed `Auto read-only: none`
  - `GOROOT` remained empty in-session
  - the remaining shell-level `permission denied: go` behavior is a PATH/shim
    UX detail, not a contract dishonesty issue

## Non-Public Supplemental Targets

The following repos were useful during exploration, but they should not be the
primary documentation/testing corpus because they are not part of the current
open-source set:

- `/Users/dr/workspace/mypadelway`
  - useful for Docker shared-daemon routing checks
- `/Users/dr/workspace/LuxTTS`
  - useful for exploratory Python detection work
- `/Users/dr/workspace/V3SP3R`
  - was used earlier as a Java/TLA confirmation target before the corpus was
    narrowed around open-source repos

## Notes

- `mcp-serp-clustering`, `poly`, `hazmat`, `openclaw`, and `beads` should be
  the default examples in future testing docs and audit notes.
- The Node and Rust positive cases validated the runtime-resolution model.
- The Beads negative case validated contract honesty when a toolchain exists on
  the host but is not safely reachable by the `agent` user.
- The Hazmat and OpenClaw cases validated the bootstrap story: Hazmat must
  produce a session where a coding agent can actually perform meaningful repo
  development, not merely see toolchain directories in the contract.
- OpenClaw still has one optional follow-up for Bun-oriented workflows:
  `sandboxing-5nk`.
- Python detection still needs refinement before a public Python repo becomes a
  strong default corpus target. Follow-up: `sandboxing-89z`.
