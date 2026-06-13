# Hazmat Diagnostics Repair UX

## Decision

`hazmat check` is read-only. It reports health, typed findings, repairability, and the next command to use, but it does not accept `--fix` and does not apply repairs.

`hazmat doctor --fix` is the primary actionable repair path when the typed plan
contains executable repairs. `hazmat doctor --dry-run` is the explicit plan-only
repair preview: it runs the same diagnostics, builds the typed repair plan, and
explains repairability, authority, preconditions, rollback boundaries, and
verification targets. Plain `hazmat doctor` remains compatible and non-mutating,
but user-facing recommendations should lead with `hazmat doctor --fix` and name
`hazmat doctor --dry-run` only as the optional preview path.

Mutation requires `hazmat doctor --fix`. In an interactive terminal, `--fix` may ask for per-plan consent before any executor runs. In automation, mutation requires both `--fix` and `--yes`; `hazmat doctor --fix` without a TTY and without `--yes` is blocked by policy.

Global `--dry-run` overrides `--fix`: `hazmat doctor --fix --dry-run` is still
a non-mutating preview and must not dispatch repair backends.

## Command Contract

```bash
hazmat check
hazmat check --json
hazmat doctor --fix
hazmat doctor --fix --yes
hazmat doctor --fix --yes --json
hazmat doctor --dry-run
hazmat doctor --dry-run --json
hazmat doctor --fix --dry-run
```

## Safety Rationale

Repair plans are generated from Hazmat-owned typed metadata, not from finding text or repository files. Repo-controlled inputs such as `.hazmat/integrations.yaml` can provide evidence for toolchain findings, but cannot approve host mutations, widen credential access, or select arbitrary privileged repairs.

Every executable repair item must name a typed repair action, authority class, receipt ID, verification ID, rollback boundary, preconditions, and test obligations. Untyped findings and unsupported/manual/optional classes remain non-executable plan items until explicitly modeled.
