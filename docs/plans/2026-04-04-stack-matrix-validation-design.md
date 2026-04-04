# Repo-Matrix Stack Validation

Status: Proposed
Date: 2026-04-04
Related issue: `sandboxing-e7p`
Related epic: `sandboxing-8ax`
Depends on:
- `sandboxing-9gz` for self-hosting bootstrap coverage
- `sandboxing-c3w` for `hazmat explain --json`
- `sandboxing-ckt` for the checked-in repo corpus

## Position

Hazmat now has two different validation needs:

1. **Self-hosting**: can Hazmat develop Hazmat under containment?
2. **User-repo compatibility**: when users bring real repos, do detection,
   routing, integration resolution, permission planning, and smoke workflows
   behave correctly?

`scripts/e2e-bootstrap.sh` should continue to answer the first question. The
second question needs a separate repo-driven matrix that is built around the
same prepared-session model as real launches.

The new validation system should therefore:

- consume a checked-in corpus of pinned open-source repos from
  `testdata/stack-matrix/repos.yaml`
- treat `hazmat explain --json` as the stable automation surface for previewing
  session behavior
- run layered checks instead of a single build-or-fail script
- distinguish required supported stacks from informational expansion targets

This keeps the validation model aligned with Hazmat's contract-first design.
We are not testing "does a shell script parse the right stderr". We are testing
whether Hazmat computes and executes the right session plan for representative
repos.

## Goals

- Catch regressions in stack detection before they reach users
- Catch contract drift between preview and execution
- Measure whether supported stacks are practically usable under containment
- Expand coverage by adding future stacks without redesigning the system
- Keep fast PR feedback cheap while reserving heavy workflow checks for macOS
  agents that actually resemble user machines

## Non-Goals

- Replacing `scripts/e2e-bootstrap.sh`
- Running full upstream test suites for every external repo
- Tracking live upstream heads on blocking CI by default
- Treating Homebrew formulas or repo metadata as Hazmat's security boundary

## Validation Layers

Each repo case in the corpus should support up to four layers:

### 1. Detect

Run `hazmat explain --json` without activating integrations explicitly and
assert:

- `suggested_integrations` matches the expected suggestions for the repo
- routing is plausible for the repo shape
- no unsupported extra suggestions appear

This is the cheapest and most important regression check.

### 2. Contract

Still using `hazmat explain --json`, assert that the previewed session contract
is honest and machine-readable:

- `active_integrations`, `integration_sources`, `integration_details`, and
  `planned_host_mutations` are populated as expected when integrations are
  activated
- auto read-only dirs, snapshot excludes, warnings, and notes are stable enough
  for automation
- manual-activation repos such as `opentofu-plan` remain explicit instead of
  being silently auto-detected

Detect answers "what did Hazmat suggest?" Contract answers "what would Hazmat
actually do if we activate the intended integrations?"

### 3. Containment

Run a narrow contained command with `hazmat exec` and activated integrations to
verify that the session boundary looks like a real Hazmat session:

- process runs as the agent user
- credential deny zones are not readable
- expected read-only toolchain paths are reachable

This layer checks that preview-time contract data corresponds to a workable
contained environment.

### 4. Workflow

Run one smoke workflow command from the corpus manifest under `hazmat exec`.
This is intentionally smaller than "run the upstream CI". The point is to
verify a representative developer path:

- `pnpm lint` for Next.js
- `uv run pytest ...` for Python/uv repos
- `cargo test -p ...` for Rust repos

Workflow checks are where usability failures show up: inaccessible toolchains,
bad permissions, missing read-only paths, or containment behavior that is
technically secure but practically broken.

## Corpus Model

The checked-in source of truth is `testdata/stack-matrix/repos.yaml`.

Each entry records:

- pinned Git commit
- stack wave
- `track`: `required` or `informational`
- `default_check`: `smoke` or `detect`
- expected auto-detected suggestions
- integrations to activate for contract and workflow checks
- required Homebrew formulas
- smoke commands
- short notes

The first implementation should keep the corpus in these waves:

1. Wave 1, required: Python and JavaScript/TypeScript
2. Wave 2, required: Rust
3. Wave 3, informational: Go and Java
4. Wave 4, informational: Ruby and OpenTofu/Terraform
5. Wave 5, informational: Elixir, TLA+, and Haskell

That matches the current priority order for the repos Hazmat is most likely to
meet first, while still keeping later stacks visible in the same manifest.

## Result Schema

The runner should emit a machine-readable result file plus human summaries.

Recommended result fields per repo:

- repo id, URL, pinned ref, track, wave
- requested validation layer
- actual `hazmat explain --json` payload
- activated integrations
- resolved runtime sources
- planned host mutations
- command exit codes and durations
- failure classification

Failure classes should be explicit:

- `detect_false_negative`
- `detect_false_positive`
- `contract_mismatch`
- `containment_failure`
- `workflow_failure`
- `toolchain_missing`
- `repo_setup_failure`

The explain JSON payload itself should be embedded or preserved in artifacts so
the runner does not become a second lossy summary layer.

## Pass / Fail Policy

The system should not treat every repo equally.

### Required repos

These are supported stacks Hazmat intends to keep working for users now.

- PR lane: fail on detect or contract regressions
- macOS contained lane: fail on containment or workflow regressions
- scheduled drift lane: report regressions, but do not block merges

### Informational repos

These are coverage-expansion targets or later-wave stacks.

- PR lane: optionally run detect checks, but report-only by default
- macOS contained lane: report-only until the stack is promoted
- scheduled drift lane: always non-blocking

Promotion from informational to required should be a data change in the corpus,
not a redesign of the runner.

## CI Split

The validation system should run in three lanes.

### 1. Fast PR lane

Purpose: catch regression-level breakage quickly.

- run unit tests
- run self-hosting bootstrap coverage
- run repo-matrix `detect` and `contract` checks for required repos only
- skip heavyweight smoke commands

This lane should be deterministic and use pinned refs only.

### 2. macOS contained lane

Purpose: prove usability under a realistic local environment.

- self-hosted macOS or the existing VM-backed strategy
- preinstall required Homebrew formulas for the selected waves
- run containment and workflow checks for required repos
- start with Waves 1 and 2, then promote later waves when stable

This is the right home for failures involving Homebrew path resolution, ACL
repair, agent execution, and filesystem traversal.

### 3. Scheduled drift lane

Purpose: watch ecosystem change without destabilizing PR feedback.

- refresh repos against newer upstream heads or refreshed pins
- run non-blocking detect and selected workflow checks
- publish artifacts showing what changed and which failures are probably Hazmat
  regressions versus upstream churn

This lane turns the matrix into an ongoing usability signal rather than a one-
time bring-up exercise.

## Runner Shape

The implementation should prefer a small Go runner over more shell.

Reasons:

- it can parse `hazmat explain --json` directly without brittle text handling
- it can emit JSON, Markdown, and JUnit artifacts from the same in-memory model
- it can classify failures instead of only returning shell exit codes
- it is easier to shard or filter by wave, track, or validation layer

Recommended interfaces:

- `stackcheck detect`
- `stackcheck contract`
- `stackcheck smoke`

These can remain internal commands or scripts initially. They do not need to
be public `hazmat` subcommands in the first slice.

## Operating Rules

- Pins are refreshed intentionally, not on every run
- Required formulas are installed outside Hazmat sessions, not by the runner
- `hazmat explain --json` is the only stable preview API for automation
- Human-oriented stderr output is non-authoritative for matrix tooling
- Self-hosting bootstrap remains an always-on gate even if the repo matrix grows

## Initial Rollout

1. Land `hazmat explain --json` as the stable automation surface
2. Check in the pinned repo corpus
3. Implement the first runner slice for `detect` and `contract`
4. Add PR coverage for required Waves 1 and 2
5. Add macOS contained smoke checks for required Waves 1 and 2
6. Promote later waves when the signal is stable

This order keeps the fast path honest while still moving toward broader
tech-stack validation.
