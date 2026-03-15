# Sandboxing Claude Code Auto Mode on macOS

Research and practical guides for securely running Claude Code with `--dangerously-skip-permissions` on macOS.

## Setup Guide

| File | Description |
|------|-------------|
| **[setup-option-a.md](setup-option-a.md)** | **Complete setup: dedicated `agent` user + soft pf blocklist + DNS blocklist + LuLu** |

## Research: Attack Surfaces & Threat Analysis

| File | Description |
|------|-------------|
| [attack-surface-deep-dive.md](attack-surface-deep-dive.md) | Prompt injection, supply chain, exfiltration vectors, macOS isolation gaps |
| [soft-pf-blocklist.md](soft-pf-blocklist.md) | Blocklist philosophy, pf rules, DNS domain blocklist, what's blockable vs not |
| [incidents-and-cves.md](incidents-and-cves.md) | Documented incidents and CVEs (Claude Code + OpenClaw) |
| [threat-matrix.md](threat-matrix.md) | What each tier protects against, mapped to real attack vectors |

## Research: Isolation Tiers

| File | Description |
|------|-------------|
| [overview.md](overview.md) | Executive summary — threat model, tier overview, decision flowchart |
| [tier0-builtin-sandbox.md](tier0-builtin-sandbox.md) | Claude Code's built-in `/sandbox` command (Seatbelt-based) |
| [tier1-seatbelt-wrappers.md](tier1-seatbelt-wrappers.md) | Open-source Seatbelt wrappers: nono, claude-sandbox, ai-jail |
| [tier2-user-pf-isolation.md](tier2-user-pf-isolation.md) | Dedicated macOS user + pf firewall + encrypted APFS volume |
| [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) | Docker Sandboxes, devcontainers, Docker Compose + Squid proxy |
| [tier4-vm-isolation.md](tier4-vm-isolation.md) | Full VM isolation: Lima, Lume, Tart |

## Research: macOS Internals & Tools

| File | Description |
|------|-------------|
| [macos-sandboxing-internals.md](macos-sandboxing-internals.md) | Seatbelt/SBPL, TCC, SIP, Hardened Runtime, Launch Constraints |
| [vm-tools-comparison.md](vm-tools-comparison.md) | Lima vs Lume vs Tart vs UTM vs OrbStack vs Apple Containers |
| [network-monitoring.md](network-monitoring.md) | LuLu, Little Snitch, pf rules, Anthropic API IPs |
| [seatbelt-profile-reference.md](seatbelt-profile-reference.md) | SBPL syntax reference + 3 ready-to-use sandbox profiles |
| [sources.md](sources.md) | 100+ references organized by topic |
