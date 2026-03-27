# TLA+ Formal Verification — Hazmat

Hazmat is a macOS CLI tool that creates a contained environment for running AI
agents: dedicated system user, seatbelt sandboxing, pf firewall, DNS blocklist.
Setup creates ~14 system resources in sequence; rollback removes them.

The setup/rollback process is sequential (no concurrency) but has a large state
space of possible intermediate states: setup can be interrupted at any step,
rollback can be partial or full, and re-running setup must recover from any
prior state. These ordering and state-machine properties are exactly what TLA+
and TLC's exhaustive state exploration are designed to verify.

## Problems

| # | File | Hazard | Priority |
|---|------|--------|----------|
| 1 | `01_setup_rollback_state_machine.md` | Security window: agent launchable without firewall after partial setup | **Critical** |

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
