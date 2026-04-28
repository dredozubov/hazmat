# TLA+ Formal Verification — Hazmat

Hazmat is a macOS CLI tool that creates a contained environment for running AI
agents: dedicated system user, seatbelt sandboxing, pf firewall, DNS blocklist.
Setup creates ~14 system resources in sequence; rollback removes them.

The setup/rollback process is sequential (no concurrency) but has a large state
space of possible intermediate states: setup can be interrupted at any step,
rollback can be partial or full, and re-running setup must recover from any
prior state. These ordering and state-machine properties are exactly what TLA+
and TLC's exhaustive state exploration are designed to verify.

## Verified Specs

| # | File | What it covers | Priority |
|---|------|----------------|----------|
| 1 | `01_setup_rollback_state_machine.md` | Setup/rollback ordering, privilege-before-containment hazards, and reversibility | **Critical** |
| 2 | `02_seatbelt_policy_structure.md` | Seatbelt rule ordering, credential denies, project write re-assertion, and resume-path safety | **High** |
| 3 | `03_backup_restore_safety.md` | Snapshot/restore ordering and overwrite safety | **High** |
| 4 | `04_version_migration.md` | Version migration, rollback from any state, and migration recovery | **High** |
| 5 | `05_tier3_launch_containment.md` | Tier 3 host-side mount planning, gating, and policy-before-launch ordering | **High** |
| 6 | `06_tier2_tier3_effective_policy_equivalence.md` | Canonical Tier 2/Tier 3 core containment equivalence and intentional exact-identity breaks | **High** |
| 7 | `07_session_permission_repairs.md` | Session-time host permission repair planning, preview/launch semantics, and rollback persistence | **High** |
| 8 | `08_harness_lifecycle.md` | Built-in harness bootstrap/import state recording and rollback cleanup semantics | **High** |
| 9 | `09_launch_fd_isolation.md` | Native helper fd-table cleanup before `sandbox_init()` and stdio-only agent exec | **High** |
| 10 | `10_git_ssh_routing.md` | Multi-key Git SSH routing, profile resolution, and deterministic host-to-key selection | **High** |
| 11 | `11_git_hook_approval.md` | Repo-local Git hook approval, immutable approved snapshots, hooksPath pinning, and drift refusal | **High** |
| 12 | `12_secret_store_recovery.md` | Host-owned harness secret-store recovery across materialize, refresh, harvest, removal, and crash/restart | **High** |
| 13 | `13_credential_capability_lifecycle.md` | Registry-level credential delivery modes, session scoping, adapter-required backends, and crash/recovery exposure rules | **High** |

See `VERIFIED.md` for the authoritative current status, proof boundaries, and change rules for each spec.

## What TLA+ Adds Here

Hazmat's correctness hazards are **state-machine bugs**, not concurrency bugs.
The individual setup steps are correct in isolation. The failures emerge from
unexpected intermediate states — interrupted setup, partial rollback, re-entry
with stale artifacts. Manual reasoning about ~2^14 resource combinations is
unreliable; TLC checks them all exhaustively in seconds.

## System Boundaries for Modelling

**Model explicitly:**
- Resource existence (user, group, files, firewall rules, daemon)
- Setup step ordering and failure points
- Rollback scope (core vs. destructive)
- Dependencies between resources

**Abstract away:**
- macOS kernel behavior (dscl, pfctl, launchctl internals)
- File content correctness (seatbelt policy syntax, pf rule semantics)
- Network connectivity (whether pf rules actually block traffic)
- Interactive prompts (password input, confirmation dialogs)
