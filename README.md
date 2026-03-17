# Sandboxing Claude Code Auto Mode on macOS

Research and practical guides for running Claude Code with `--dangerously-skip-permissions` on macOS without treating the built-in permission model as a security boundary.

## Start Here

If you only read three files, read these in order:

1. [overview.md](overview.md) for the design choices and tier-selection rules
2. [threat-matrix.md](threat-matrix.md) to verify whether a tier blocks your main risk
3. One implementation doc:
   - [setup-option-a.md](setup-option-a.md) for the dedicated-user Tier 2 path
   - [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) for Docker-required projects

## Fast Path by Need

| If you need to answer... | Start with | Then read |
|--------------------------|------------|-----------|
| Which tier should I use at all? | [overview.md](overview.md) | [threat-matrix.md](threat-matrix.md) |
| I want the main no-VM setup | [setup-option-a.md](setup-option-a.md) | [tier2-user-pf-isolation.md](tier2-user-pf-isolation.md) |
| My repo needs `docker build` or `docker compose` | [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) | [attack-surface-deep-dive.md](attack-surface-deep-dive.md) |
| I want the rationale behind the controls | [incidents-and-cves.md](incidents-and-cves.md) | [attack-surface-deep-dive.md](attack-surface-deep-dive.md) |
| I want the full incident/CVE/research evidence base | [security-evidence.md](security-evidence.md) | [threat-matrix.md](threat-matrix.md) |
| I need the lowest-friction option for trusted repos | [tier0-builtin-sandbox.md](tier0-builtin-sandbox.md) | [tier1-seatbelt-wrappers.md](tier1-seatbelt-wrappers.md) |
| I need the strongest boundary for hostile or unattended work | [tier4-vm-isolation.md](tier4-vm-isolation.md) | [vm-tools-comparison.md](vm-tools-comparison.md) |

## Design Choices This Repo Makes

- Tier 2 is the default "strong boundary without a VM" path when the project does not need Docker.
- Tier 3 is the default when the project needs Docker or Docker Compose. The key design choice is a dedicated Docker daemon or microVM, not host daemon sharing.
- Docker socket protection in Tier 2 comes from host filesystem permissions (`0700` on the socket), not from Seatbelt policy.
- `pf` is useful in the dedicated-user model, but container traffic is a separate surface and must be controlled differently.
- Incidents and CVEs are treated as design inputs, not as a historical appendix.

## Documentation Map

### 1. Choose a Boundary

| File | What it answers |
|------|-----------------|
| [overview.md](overview.md) | Which tier fits the use case, and why |
| [tier0-builtin-sandbox.md](tier0-builtin-sandbox.md) | What Claude's built-in sandbox does and does not do |
| [tier1-seatbelt-wrappers.md](tier1-seatbelt-wrappers.md) | What extra Seatbelt wrappers add |
| [tier2-user-pf-isolation.md](tier2-user-pf-isolation.md) | How the dedicated-user model works |
| [tier3-docker-sandboxes.md](tier3-docker-sandboxes.md) | How to handle Docker-required projects safely |
| [tier4-vm-isolation.md](tier4-vm-isolation.md) | When to move to a full VM |

### 2. Understand the Risk Surface

| File | What it answers |
|------|-----------------|
| [threat-matrix.md](threat-matrix.md) | Which tier blocks which attack classes |
| [attack-surface-deep-dive.md](attack-surface-deep-dive.md) | Where the real escape and exfiltration paths are |
| [incidents-and-cves.md](incidents-and-cves.md) | Which real incidents and CVEs justify the design |
| [security-evidence.md](security-evidence.md) | Full incident catalog, CVE table, academic papers, and per-attack sandbox coverage analysis |
| [soft-pf-blocklist.md](soft-pf-blocklist.md) | What `pf` can and cannot realistically block |

### 3. Implement and Validate

| File | What it answers |
|------|-----------------|
| [setup-option-a.md](setup-option-a.md) | How to set up the dedicated-user workflow end to end |
| [network-monitoring.md](network-monitoring.md) | How to observe or audit egress |
| [seatbelt-profile-reference.md](seatbelt-profile-reference.md) | How SBPL rules work |
| [macos-sandboxing-internals.md](macos-sandboxing-internals.md) | How Seatbelt, SIP, TCC, and related internals fit together |
| [sources.md](sources.md) | Primary sources and supporting references |
