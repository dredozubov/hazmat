# Security Policy

## Reporting a Vulnerability

If you find a containment bypass, credential leak, sandbox escape, or other security issue in Hazmat, please report it privately.

**Email:** hazmat@codeofchange.io

Include:
- Hazmat version (`hazmat --version`)
- macOS version
- Steps to reproduce
- Impact assessment (what an attacker gains)

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for confirmed issues.

Do **not** open a public GitHub issue for:

- sandbox escapes
- credential leaks
- firewall bypasses
- privilege-escalation paths
- unsafe setup / rollback ordering bugs

Public GitHub issues are still the right place for non-sensitive bugs, docs problems, UX feedback, compatibility reports, and feature requests.

## What Counts as a Security Issue

- Sandbox escape (agent accessing paths outside the seatbelt policy)
- Credential exfiltration (agent reading denied credential paths)
- Firewall bypass (agent reaching blocked protocols)
- Privilege escalation (agent gaining host user or root capabilities)
- Init/rollback ordering bugs that leave the system in an unsafe state

## What Does Not

- HTTPS exfiltration to novel domains (documented limitation)
- Shared `/tmp` access (documented limitation)
- Issues requiring root access on the host machine

## Disclosure

We follow coordinated disclosure. We will work with you on timing and credit.
