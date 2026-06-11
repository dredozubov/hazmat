# Hazmat Diagnostics Repair UX

## Decision

`hazmat check` is read-only. It reports health, typed findings, repairability, and the next command to use, but it does not accept `--fix` and does not apply repairs.

`hazmat doctor` is also plan-only by default. It runs the same diagnostics, builds the typed repair plan, and explains repairability, authority, preconditions, rollback boundaries, and verification targets.

Mutation requires `hazmat doctor --fix`. In an interactive terminal, `--fix` may ask for per-plan consent before any executor runs. In automation, mutation requires both `--fix` and `--yes`; `hazmat doctor --fix` without a TTY and without `--yes` is blocked by policy.

## Command Contract

```bash
hazmat check
hazmat check --json
hazmat doctor
hazmat doctor --json
hazmat doctor --fix
hazmat doctor --fix --yes
hazmat doctor --fix --yes --json
```

## Safety Rationale

Repair plans are generated from Hazmat-owned typed metadata, not from finding text or repository files. Repo-controlled inputs such as `.hazmat/integrations.yaml` can provide evidence for toolchain findings, but cannot approve host mutations, widen credential access, or select arbitrary privileged repairs.

Every executable repair item must name a typed repair action, authority class, receipt ID, verification ID, rollback boundary, preconditions, and test obligations. Untyped findings and unsupported/manual/optional classes remain non-executable plan items until explicitly modeled.
