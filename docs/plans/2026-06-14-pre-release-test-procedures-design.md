# Pre-Release Test Procedures and Product Coverage Design

Date: 2026-06-14

## Purpose

Hazmat's pre-release suite must prove two different things:

1. Package-owned invariants are enforced by the package that owns the contract.
2. Product-facing behavior works through the same flows a user depends on.

The suite should be auditable: a reviewer should be able to answer which command
covers a user-facing promise, which package owns the underlying invariant, which
OS runs the lane, and what evidence is expected before release.

This design turns the test suite into named lanes with explicit procedures. It
does not require every test to move immediately. It gives future refactors a
map for moving contract tests out of the root `hazmat` package while preserving
product-level coverage.

## Design Choice

Use a two-axis test model:

- **Package-ownership axis:** package tests live next to the package that owns the
  invariant. Examples: `containment` owns authority contracts,
  `containment/darwin` owns SBPL compilation, `sessionbackend` owns backend
  plan and prepared-launch DTO invariants, and `sessionrequest` owns request
  normalization.
- **Product axis:** scenario tests and pre-release flows cover user-facing
  promises even when the implementation spans many packages. Examples: refusal
  copy, approval gates, `hazmat explain`, session routing, harness launch
  generation, release packaging, and install refusal behavior.

Rejected alternatives:

- **Root-package catchall:** easy to add tests, but it hides ownership and makes
  Linux/macOS portability failures look accidental.
- **Package tests only:** clean ownership, but it misses integrated user-facing
  regressions such as wrong command guidance or broken release flow wiring.
- **One monolithic pre-release script:** simple invocation, but it is hard to
  audit, hard to shard by OS, and unsafe when live prepared-host checks are
  mixed with hermetic checks.

## Lane Taxonomy

Every automated test entrypoint should map to one or more lanes. Lane names are
stable audit labels; scripts and CI jobs may be reorganized as long as the lane
contract remains clear. The checked-in registry is
[docs/test-lanes.tsv](../test-lanes.tsv), and
`TestEveryEntrypointMapsToALane` fails when a top-level `scripts/*.sh` entrypoint
or `.github/workflows/ci.yml` job lacks a primary lane. The local non-live lane
aggregator is `scripts/test-lane.sh`; it refuses approval-gated lanes and
points to their exact wrappers instead.

| Lane | Procedure | Primary owner | Runs where | Release status |
| --- | --- | --- | --- | --- |
| `source-safety` | Secret scans, credential-regression scans, gitleaks, staged diff sanity, shell syntax. | Repository scripts and hooks | Linux or macOS | Required |
| `package-boundaries` | Import-boundary, dependency-graph, package-split, and VERIFIED.md governed-code checks. | Package architecture guard | Linux CI and local pre-release | Required |
| `package-contracts` | Package-local `go test` for invariant and DTO tests. Golden fixtures live with the producing package where possible. | Go packages | Linux and macOS | Required |
| `os-linux` | Linux `go test ./...`, Linux compile checks, Linux-specific runtime/package tests. | Linux-capable packages | Ubuntu CI, Apple Container locally | Required before claiming Linux compatibility |
| `os-macos` | macOS `go test ./...`, Darwin SBPL, launch-helper shape, Keychain/Seatbelt non-live tests. | Darwin-capable packages and root orchestration | macOS CI/local | Required before macOS release |
| `cli-ux` | CLI help, refusal messages, JSON output compatibility, approval-gate copy, diagnostic guidance. | CLI/root product surface | Linux or macOS unless OS-specific | Required |
| `product-workflows` | Hermetic scenario flows: session planning, explain, harness command generation, fake harness lifecycle, repo-matrix contract. | Root orchestration plus scenario fixtures | Linux or macOS depending on flow | Required |
| `release-artifacts` | Build, file-type checks, package layout, install/refusal policy, checksums, Homebrew formula assumptions. | Release scripts and CI | macOS for current artifacts | Required |
| `tla-proof-hygiene` | Proof ownership, trace artifact policy, and promoted-spec inventory drift checks. | TLA proof ledger | Ubuntu CI | Required |
| `tla-model-check` | Deep TLC run over promoted specs with proof audit artifacts. | TLA specs | Ubuntu CI | Required |
| `privileged-install-ownership` | Real-host uid/gid and writeability checks for agent-owned setup paths after init and rollback. | Setup/runtime ownership | Disposable prepared host | Required for release candidates and setup-path changes |
| `live-approved` | Prepared-host native/harness smokes that may use helper-backed launch, `sudo -n`, Apple Container, or local agent state. | Smoke wrapper owners | Prepared hosts only, exact approval required | Required only when the changed surface depends on it |
| `destructive-lifecycle` | Full setup, containment, backup/restore, rollback, and reinstall lifecycle. | Lifecycle/e2e scripts | Disposable VM or disposable host only | Required for release candidates when feasible |
| `drift` | Scheduled external drift checks for installers, stack matrix upstream heads, and ecosystem assumptions. | Scheduled CI workflows | CI only | Non-blocking signal unless promoted |

## Ownership Procedure

When adding or moving a test, first classify the assertion:

1. **Pure invariant:** put it in the package that owns the type or compiler.
   Examples: path grant normalization belongs in `containment`; backend gap
   acceptance belongs in `sessionbackend`; launch spec compilation belongs in
   `containment/linux`, `containment/darwin`, `containment/docker`, or
   `containment/applecontainer`.
2. **Product scenario:** keep it in a product scenario package or root-facing
   test when the assertion is about the user surface. Examples: exact CLI
   refusal text, `hazmat explain --json` shape, default routing behavior, and
   release-script gating.
3. **OS runtime behavior:** put non-live runtime admission tests in the relevant
   runtime package, such as `internal/runtime/darwin`, `internal/runtime/docker`,
   or `internal/runtime/applecontainer`.
4. **Live prepared-host behavior:** keep it behind a script wrapper that defaults
   to disclosure-only and requires exact approval for live modes.

Golden fixtures should follow the producer. Backend plan goldens belong with
`sessionbackend`; containment compiler goldens belong with `containment/*`;
hostbroker wire-contract fixtures belong with `hostbroker`; root package
goldens should be reserved for user-facing CLI or cross-package scenario output.

If a test is hard to place, that is design feedback. Either the production code
boundary is unclear, or the test is actually a product scenario and should be
named as such.

`scripts/check-import-boundaries.sh` is the first-class entrypoint for the
`package-boundaries` lane. It must stay blocking in CI and in local pre-release
before any package split or golden migration deletes old root-package coverage.

## Relationship to Prior Work

This document complements, but does not replace, the package-split and
core-session extraction plans:

- [2026-06-03 package split implementation roadmap](2026-06-03-package-split-implementation-roadmap.md)
- [2026-06-12 core session extraction design](2026-06-12-core-session-extraction-design.md)

Those plans remain authoritative for semantic code movement. This document only
defines test ownership, product-flow coverage, and pre-release audit procedure.
It does not authorize over-exporting package-main internals to move tests. When
a moved function is governed by `tla/VERIFIED.md`, update the governed-code
reference in the same commit and keep
`TestVerifiedLedgerGovernedFunctionsExist` green.

## Privileged Install Ownership Outcomes

Issue-17-class failures are real filesystem ownership outcomes, not command
shape failures. The required property is: every setup-created parent directory
that an agent process must write is owned by the expected agent uid/gid, and the
agent can create a probe child there after setup. Rollback must not leave
root-owned residue under the agent home.

Required checks:

- A hermetic package-contract invariant derives setup chown targets from the
  actual agent-written path set instead of mirroring a production literal.
- The invariant is OS-parameterized for the Darwin `/Users/agent` and future
  Linux `/home/agent` path sets.
- The `privileged-install-ownership` lane runs on a disposable prepared host
  for changes touching setup, bootstrap, tooling, or platform path ownership.
- The real-host lane stats every required directory and verifies an agent-owned
  mkdir probe succeeds.
- The rollback half asserts no residual root-owned setup path remains under the
  agent home.

The approval-gated wrapper for the real-host check is
`scripts/check-privileged-install-ownership.sh`. Use `--run` after setup and
`--after-rollback` after rollback.

Apple Container Linux tests do not prove this property: they run as the invoking
UID/GID and do not exercise sudo-backed install ownership.

## Product-Facing Flow Procedures

Product-facing coverage is organized as flows. Each flow should have at least
one hermetic test path and, when appropriate, one live-approved path.

### Flow: CLI Refusal and Guidance

User promise: dangerous or environment-dependent commands refuse clearly and
tell the user the exact safe next step.

Required checks:

- Help text names approval-gated commands and prerequisites.
- Default mode is disclosure-only for live wrappers.
- Refusal copy names the exact acknowledgement variable or flag.
- Diagnostic guidance points to the command authority, not competing advice.
- JSON guidance remains stable where users or scripts consume it.

Evidence:

- `scripts/test-entrypoint-guards.sh`
- CLI help/smoke checks
- Diagnostic and frontend CLI package tests

### Flow: Session Plan and Explain

User promise: before launch, Hazmat can explain the backend, grants, network
mode, credential handling, mutations, and unsupported capability gaps without
performing host mutations.

Required checks:

- Request normalization is package-owned by `sessionrequest`.
- Contract preview DTOs are package-owned by `sessioncontract`.
- Backend plan and prepared-launch DTOs are package-owned by `sessionbackend`.
- Product-level explain output is covered by scenario/golden tests.
- Linux, Docker, Darwin, and Apple Container backend gaps remain explicit.

Evidence:

- `sessionrequest`, `sessioncontract`, `sessionbackend`, and `sessionplanner`
  tests
- Explain JSON/golden scenario tests
- Linux Apple Container `go test ./...` for Linux portability

### Flow: Containment Compilation

User promise: the validated containment contract compiles into the correct
backend artifact without widening authority.

Required checks:

- Backend-neutral authority invariants live in `containment`.
- SBPL behavior lives in `containment/darwin`.
- Linux plan-only launch specs live in `containment/linux`.
- Docker Sandbox specs live in `containment/docker`.
- Apple Container launch specs live in `containment/applecontainer`.
- Root orchestration tests assert that the selected artifact matches the chosen
  backend, not the compiler's internal details.

Evidence:

- Package-local compiler tests and goldens
- `sessionbackend.NewPreparedLaunch` tests
- macOS and Linux OS lanes

### Flow: Harness and Credential Workflow

User promise: managed harnesses launch with the intended command, scoped
credentials, session-local state rules, and no accidental host credential
writeback.

Required checks:

- Managed harness registry changes require hermetic smoke coverage.
- Credential descriptors and registries stay package-owned.
- Runtime materialization/harvest tests use fake credentials and fake harnesses.
- Live harness smokes remain approval-gated and prepared-host only.

Evidence:

- `scripts/e2e-harness-smoke.sh`
- Harness registry coverage tests
- Credential package/runtime tests
- Optional live wrappers for Codex, Claude workflow export, cache integration,
  OpenHands recipe, and session-local HOME activation

### Flow: Release and Install

User promise: release artifacts are the expected binaries for the supported OS,
install paths are correct, unsupported platforms refuse clearly, and published
metadata is internally consistent.

Required checks:

- Build commands target `./cmd/hazmat` and `./cmd/hazmat-launch`, never the root
  library package.
- Current release artifacts are Darwin-only until Linux setup/rollback and
  lifecycle lanes are implemented.
- Linux install/release refusal is tested.
- Homebrew formula checksum/update assumptions are verified in release CI.

Evidence:

- CI build jobs
- `scripts/release.sh`
- `scripts/pre-release-local.sh`
- install/refusal tests
- release workflow artifact checks

`.github/workflows/release.yml` has a release preflight job before artifact
builds. It runs macOS vet/tests, package-boundary checks, entrypoint guards, and
CLI smoke tests on the tagged commit. Tags should still be cut only from commits
that already passed blocking CI and the local pre-release gate, because release
preflight is a final artifact guard, not a replacement for the full CI matrix.

## OS Mapping

### Linux

Linux lanes must not depend on Darwin host state. Required automated coverage:

1. `source-safety`
2. `package-contracts`
3. `os-linux`
4. Linux-compatible `cli-ux`
5. Linux-compatible `product-workflows`

Current local command:

```bash
make linux-apple-container-test APPLE_CONTAINER_ACK=1
```

Current CI baseline:

```bash
cd hazmat && go test ./...
bash scripts/check-linux-compile.sh
```

Local Apple Container baseline:

```bash
make linux-apple-container-test APPLE_CONTAINER_ACK=1
```

Linux setup, rollback, install artifacts, native launch, firewall, account, and
service behavior remain blocked until their model and disposable lifecycle lanes
exist.

### macOS

macOS lanes cover the currently supported native product surface. Required
automated coverage:

1. `source-safety`
2. `package-contracts`
3. `os-macos`
4. `cli-ux`
5. `product-workflows`
6. `release-artifacts`

Prepared-host checks are separate:

```bash
bash scripts/check-codex-app-server-smoke.sh --run --i-understand-this-runs-hazmat-codex-app-server
bash scripts/e2e-harness-smoke-native.sh --run --i-understand-this-runs-native-harness-smoke
bash scripts/e2e-vm.sh --quick
```

Those commands are examples of lane coverage, not blanket approval. Agents must
still ask for exact-command approval before running approval-gated paths.

## Pre-Release Procedure

The release owner should collect evidence in this order:

1. **Source safety:** run the fast repository gate and confirm staged/working
   tree scans pass.
2. **Package boundaries:** run the import-boundary and package-split guard.
3. **Package contracts:** run full package tests on the supported host and the
   Linux lane where available.
4. **OS lanes:** run macOS `go test ./...`; run Linux `go test ./...` through
   CI or Apple Container.
5. **Product workflows:** run hermetic harness smoke, repo-matrix contract, CLI
   smoke, and entrypoint guards.
6. **TLA gates:** run proof hygiene and deep TLC model checking for every
   promoted spec, and rerun affected specs before implementation when a verified
   area changes.
7. **Release artifacts:** build supported artifacts and verify file type,
   package contents, checksums, and unsupported-platform refusal.
8. **Privileged install ownership:** for setup-path changes and release
   candidates, run the real-host ownership lane or block the release.
9. **Live-approved checks:** run only the prepared-host smokes relevant to the
   changed surface, after exact approval.
10. **Destructive lifecycle:** for release candidates, run the lifecycle suite in
   a disposable VM or document why it was intentionally skipped.
11. **Audit record:** record commands, OS/arch, pass/fail result, skipped lanes,
   and the reason for every skip.

Minimum local release gate today:

```bash
bash scripts/pre-release-local.sh
make linux-apple-container-test APPLE_CONTAINER_ACK=1
```

The first command runs the VM destructive lifecycle by default; set
`HAZMAT_PRERELEASE_SKIP_E2E_VM=1` only with an audit-recorded skip reason. The
second command is Apple Container live execution and remains approval-gated.

## CI Flow

CI should be organized by lane, not by historical script names.

Recommended blocking jobs:

- `source-safety` on Ubuntu.
- `package-boundaries` on Ubuntu with `bash scripts/check-import-boundaries.sh`.
- `go-test-linux` on Ubuntu with `cd hazmat && go test ./...`.
- `go-test-macos` on macOS with `cd hazmat && go test ./...`.
- `lint` on macOS or Ubuntu, matching the supported local toolchain.
- `entrypoint-guards` on macOS.
- `cli-ux` on macOS.
- `repo-matrix-contract` on macOS.
- `release-build-darwin` on macOS for Darwin artifacts.
- `tla-proof-hygiene` on Ubuntu.
- `tla-model-check` on Ubuntu, after proof hygiene.

Recommended optional jobs:

- `linux-apple-container` on self-hosted macOS 26 Apple silicon.
- `destructive-lifecycle-vm` on disposable macOS VM infrastructure.
- Scheduled drift jobs for upstream installers and stack matrix heads.

CI job names should include the lane where possible. If a script covers multiple
lanes, the job summary should list the covered lanes.

## Audit Checklist

Use this checklist during review:

- Every changed production package has package-local tests for its invariants.
- Every changed user-facing promise maps to a product flow test or documented
  manual/live-approved procedure.
- The package-boundary guard is green before and after test relocation.
- Every live wrapper defaults to disclosure-only.
- Every sudo-adjacent or host-mutating path requires exact approval.
- Setup-path changes include issue-17-class uid/gid and writeability evidence.
- Linux lanes do not rely on Darwin-only paths, users, Homebrew state, or
  Seatbelt behavior.
- macOS lanes do not claim Linux setup/rollback/install support.
- Golden fixtures live with the package or product surface that produces them.
- Governed-code references in `tla/VERIFIED.md` move in the same commit as
  governed functions.
- Deep TLC model checking remains blocking for promoted specs.
- Release artifacts are tested for the currently supported OS set only.
- Skipped lanes have explicit reasons in the release audit record.

## Migration Plan

Move tests in small, reviewable chunks:

0. SBPL, planner, and Docker-launch goldens now exercise exported package
   producers directly: `containment/darwin.Compile`, `sessionplanner.Build`,
   and `containment/docker.Compile` plus Docker argv derivation helpers.
1. Add lane labels to `docs/test-lanes.tsv` and keep
   `TestEveryEntrypointMapsToALane` green.
2. Keep `package-boundaries` green in CI and local pre-release.
3. Backend plan goldens now live under `sessionbackend/testdata/golden`.
4. Linux and Apple Container launch goldens now live with their exported
   compilers under `containment/linux/testdata/golden` and
   `containment/applecontainer/testdata/golden`.
5. Docker launch and SBPL compiler goldens now live under
   `containment/docker/testdata/golden` and
   `containment/darwin/testdata/golden`.
6. Planner fixtures now live under `sessionplanner/testdata/golden`;
   request/contract fixtures should move to `sessionrequest` and
   `sessioncontract` when they gain standalone product-shape baselines.
7. Keep root package tests only for CLI orchestration, product scenarios, and
   compatibility shims that cannot move yet.
8. Add CI jobs for Linux `go test ./...` and lane-named macOS tests.
9. Add release audit output once lane commands are stable.

Each migration commit should keep both macOS `go test ./...` and Linux
Apple Container `go test ./...` green before deleting old root-package coverage.
Green tests are not enough by themselves: deletion also requires
assertion-equivalence, either by identical golden bytes or by keeping both tests
for one commit and comparing the moved fixture output.
