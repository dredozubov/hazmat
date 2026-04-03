# Repository Validation Matrix

Status: Current as of 2026-04-03
Scope: Session integrations, self-hosting, and repo-targeted bootstrap checks

This note records the real repositories used during the current Hazmat
integration and self-hosting work. It is not a design document. It is a
validation ledger: which repos were exercised, what was verified, and what the
observed outcome was.

## Positive Integration Targets

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

### `/Users/dr/workspace/V3SP3R`

- Stack: TLA+ / Java
- Integration: `tla-java`
- Mode: native containment
- Verified:
  - `hazmat explain -C ... --integration tla-java --no-backup`
  - `hazmat shell -C ... --integration tla-java`
  - inside session: `echo "$JAVA_HOME"`, `"$JAVA_HOME/bin/java" -version`
- Outcome:
  - Hazmat resolved a real Homebrew OpenJDK home, not the `/usr` launcher stub
  - session contract, `JAVA_HOME`, and executable runtime matched
  - `java -version` succeeded inside containment

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

## Routing / Policy Targets

### `/Users/dr/workspace/mypadelway`

- Stack: Node.js repo with shared-daemon Docker signals
- Integration: `node`
- Verified:
  - `hazmat explain -C ... --integration node --no-backup`
  - `hazmat explain -C ... --docker=none --integration node --no-backup`
- Outcome:
  - automatic routing correctly blocked shared-daemon Docker usage
  - explicit `--docker=none` produced a code-only native session with the right
    explanatory notes

## Negative / Honesty Targets

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

## Exploratory Python Target

### `/Users/dr/workspace/LuxTTS`

- Stack: Python project with Poetry-style markers
- Integration path explored: `python-poetry`
- Outcome:
  - useful as a detection target, but not a strong positive validation target
    for the current branch
  - current detection is too broad for generic `pyproject.toml` repos
  - follow-up tracked in `sandboxing-89z`

## Notes

- The Node and Rust positive cases validated the runtime-resolution model.
- The Beads negative case validated contract honesty when a toolchain exists on
  the host but is not safely reachable by the `agent` user.
- The Hazmat and OpenClaw cases validated the bootstrap story: Hazmat must
  produce a session where a coding agent can actually perform meaningful repo
  development, not merely see toolchain directories in the contract.
- OpenClaw still has one optional follow-up for Bun-oriented workflows:
  `sandboxing-5nk`.
