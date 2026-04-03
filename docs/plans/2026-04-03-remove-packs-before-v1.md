# Remove Packs Before v1

Status: Accepted
Date: 2026-04-03
Related issues: `sandboxing-4o0`, `sandboxing-28u`

## Decision

Hazmat should not ship its first release with both `packs` and
`integrations` as live user-facing concepts.

For v1, the product model should be:

- `integrations` for stack-specific runtime and toolchain ergonomics
- explicit read-only and read-write extensions for extra filesystem scope
- separate first-class capabilities for credentials or external authority

`packs` should be removed as a user-facing concept and retired from the core
implementation model before release. Pre-release breakage is acceptable. The
goal is a simpler product, cleaner docs, and less migration debt carried into
the first public version.

## Why Hard-Cut Instead of Deprecate

The current codebase already shows the downside of trying to straddle both
models:

- `hazmat integration` is the intended UX, but `hazmat pack`, `--pack`, and
  `packs.pin`/`packs.unpin` still exist
- docs still point users to `stack-packs.md` and `.hazmat/packs.yaml`
- repo warnings like `unknown pack "gradle"` leak the legacy term into active
  QA
- core implementation types are still `Pack`, `PackMeta`, `PackSession`, and
  `mergePacks`, even when the behavior is now integration-driven

Because Hazmat is still pre-release, a compatibility layer is more costly than
useful. A hard cut now means:

- fewer nouns in the product
- fewer hidden aliases in the CLI
- fewer code paths to test
- a cleaner threat model and audit story
- less permanent debt in config, docs, and repo metadata

## Release Goal

By the time v1 ships:

- there is no `hazmat pack` command
- there is no `--pack` flag
- there are no `packs.*` config keys
- there is no `.hazmat/packs.yaml`
- there is no `~/.hazmat/packs/`
- core code no longer uses `Pack*` naming for the integration model
- docs and threat-model language talk only about integrations, capabilities,
  and path extensions

Migration help may exist as:

- release notes
- a one-off rewrite script
- a targeted validation error with an actionable rename message

Migration help must not exist as a permanent compatibility alias or dual
runtime abstraction.

## Current Legacy Surface

### User-facing legacy

- `hazmat pack` command alias in `hazmat/pack.go`
- hidden deprecated `--pack` flags in `hazmat/session.go` and
  `hazmat/explain.go`
- forwarded `--pack` parsing in session option handling in `hazmat/session.go`
- `packs.pin` and `packs.unpin` config aliases in `hazmat/config.go`
- repo recommendation file `.hazmat/packs.yaml`
- user manifest directory `~/.hazmat/packs/`
- docs that still describe the feature as packs:
  - `README.md`
  - `docs/usage.md`
  - `docs/overview.md`
  - `docs/stack-packs.md`
  - `docs/design-assumptions.md`
  - `docs/threat-matrix.md`

### Internal legacy

- `HazmatConfig.Packs`, `PacksConfig`, and `PackPin` in `hazmat/config.go`
- manifest and merge logic centered on `Pack`, `PackMeta`, `PackSession`, and
  `mergePacks` in `hazmat/pack.go`
- embedded built-ins under `hazmat/packs/*.yaml`
- repo recommendation approval records in `~/.hazmat/approvals.yaml`
- tests still centered on `pack_test.go` and pack-named helpers

## Recommended Implementation Strategy

### 1. Replace the nouns at the boundaries first

The release boundary should stop saying `pack` anywhere visible to users.

Target names:

- CLI concept: `integration`
- built-in manifest concept: `integration spec`
- repo recommendation file: `.hazmat/integrations.yaml`
- user-installed manifest directory: `~/.hazmat/integrations/`
- approval file: `~/.hazmat/integration-approvals.yaml`
- config storage: `integrations.pinned`

The first slice should remove the aliases and rename the files the user sees.
If an old repo file or config key is encountered, Hazmat should fail with a
direct migration message rather than silently supporting both forever.

### 2. Rename the internal model to match the product model

After the boundary cut, the code should stop encoding the old abstraction.

Recommended refactor:

- `pack.go` becomes `integration_manifest.go` or similar
- `Pack` becomes `IntegrationSpec`
- `PackMeta` becomes `IntegrationMeta`
- `PackSession` becomes `IntegrationSession`
- `PackBackup` becomes `IntegrationBackup`
- `resolveActivePacks` becomes `resolveActiveIntegrations`
- `mergePacks` becomes `mergeIntegrations`
- built-ins move from `hazmat/packs/*.yaml` to `hazmat/integrations/*.yaml`

This should be done even if the YAML schema is still materially similar. The
point is to make the code express the real model, not the historical one.

### 3. Keep only three durable abstractions

The refactor should leave Hazmat with exactly these top-level concepts:

1. integrations
   runtime/toolchain ergonomics and safe path/env resolution
2. extensions
   explicit user read-only or read-write path additions
3. capabilities
   authority-granting features such as GitHub or other future credentials

Anything that does not fit one of those buckets should not be smuggled back in
under a renamed `pack`.

### 4. Treat repo recommendations as integration hints, not profiles

The repo recommendation model should survive, but under the new name and with a
narrower story:

- `.hazmat/integrations.yaml` contains only integration names
- host approval remains required
- approval storage is renamed accordingly
- no repo file can request capabilities or arbitrary path expansions

This preserves the useful “repo hints, host trust” behavior without carrying
the pack abstraction into v1.

## Concrete Work Slices

### Slice A: remove legacy CLI and config compatibility

Scope:

- remove `hazmat pack`
- remove `--pack`
- remove `packs.pin` and `packs.unpin`
- remove any help text that suggests legacy usage
- add explicit migration errors for old flags/keys if needed

Acceptance:

- `hazmat --help` exposes only integrations wording
- old pack flags/commands are not silently accepted

### Slice B: rename manifest/storage/approval artifacts

Scope:

- rename built-in manifest directory to `hazmat/integrations/`
- rename user manifest directory to `~/.hazmat/integrations/`
- rename repo recommendation file to `.hazmat/integrations.yaml`
- rename approval file and approval record types

Acceptance:

- no code path loads `.hazmat/packs.yaml`
- no code path reads `~/.hazmat/packs/`
- approval UX refers only to integrations

### Slice C: refactor core types and merge pipeline

Scope:

- replace `Pack*` types and helpers with integration-named equivalents
- update tests to match the new model
- keep behavior identical where semantics are unchanged

Acceptance:

- no core type or main resolver helper uses the `Pack` prefix
- session preparation and explain output still match current behavior

### Slice D: docs, threat model, and repo cleanup

Scope:

- rename `docs/stack-packs.md` to an integration-focused doc
- update README and usage docs
- update threat-model and design-assumption language
- replace repo-local `.hazmat/packs.yaml` references in examples and fixtures

Acceptance:

- user documentation does not teach packs
- threat-model language uses integrations/capabilities/extensions consistently

## Migration Policy

Recommended v1 policy:

- no runtime deprecation period
- no hidden aliases left in shipping code
- no auto-import of old config keys or repo files
- one-time migration help only

Acceptable migration aids:

- a standalone script in `scripts/`
- release notes with exact search/replace guidance
- explicit error text when encountering `.hazmat/packs.yaml` or `packs.pin`

Unacceptable migration aids:

- continuing to accept `--pack`
- continuing to ship `hazmat pack`
- dual config keys in stable code
- permanent support for both `.hazmat/packs.yaml` and
  `.hazmat/integrations.yaml`

## Acceptance Criteria for v1 Cut

- all tested stacks still behave as they do now under the integration model:
  - `node` positive case
  - `rust` positive case
  - `tla-java` positive case
  - `go` honest negative case when host permissions block the toolchain
- pack terminology is gone from normal CLI, docs, and session UX
- repo recommendation approvals still work under the new integration file name
- config pinning still works under integration-only naming
- no security regression from the rename/refactor

## Risks

- repo recommendation file rename may break internal repos until they are
  updated
- config/storage rename may strand pre-release local state if there is no
  migration note
- broad renaming across code and tests can cause churn if done together with
  behavioral changes

Mitigation:

- keep the behavior stable while renaming the model
- split the work into boundary, storage, core refactor, and docs slices
- do not combine the removal with new integration families in the same PR

## Recommendation

Proceed with a hard v1 cut:

- remove packs as a user-facing abstraction
- rename the remaining internal model to integrations
- preserve only one-time migration guidance, not runtime compatibility

This is the cleanest release story and matches the product direction already
validated by the current QA work.

## Tracked Work

- `sandboxing-idj` — remove legacy pack CLI and config surfaces before v1
- `sandboxing-55j` — rename pack manifests, recommendation files, and approval
  storage to integrations
- `sandboxing-a8d` — refactor core `Pack*` types and merge pipeline to
  integration terminology
- `sandboxing-1u8` — rewrite docs and threat model to remove pack terminology
  for v1
