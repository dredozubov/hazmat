<p align="center">
  <img src="assets/hazmat-final.png" alt="Hazmat" width="200">
</p>

<h1 align="center">Hazmat</h1>

<p align="center">
  <strong>Full autonomy. Controlled environment.</strong><br>
  macOS containment for AI agents running with <code>--dangerously-skip-permissions</code>
</p>

---

Hazmat runs AI agents with full permissions — inside containment.

No guardrails. No sandboxed APIs. Just isolation.

```bash
hazmat claude                     # Claude Code in containment
hazmat exec ./my-agent-loop.sh    # any agent, any script
hazmat shell                      # interactive contained shell
```

The agent gets its own macOS user, a kernel-enforced filesystem sandbox, a firewall blocking exfiltration protocols, and snapshot-based rollback. It can do anything it needs to inside the boundary. It can't get out.

## Why This Exists

If you're running Claude Code, Codex, or any agent loop with full system access, the agent can:

- Delete your files
- Read your SSH keys and AWS credentials
- Exfiltrate code over SMTP, IRC, or tunnel services
- Modify your shell config, git hooks, or LaunchAgents
- `curl` your secrets to any server

Permission prompts don't help when you're running `--dangerously-skip-permissions`. And if you're running agent loops (Ralph, Gastown, custom scripts), there are no prompts at all.

Hazmat doesn't try to make the agent behave. It isolates it.

## What It Does

| Layer | Protection |
|-------|------------|
| **User isolation** | Dedicated `agent` macOS user — separate home, no access to your files |
| **Filesystem sandbox** | Per-session [seatbelt](https://developer.apple.com/documentation/security) policy. Project gets read-write, everything else is denied |
| **Credential deny** | SSH keys, AWS creds, GPG keys, Keychain, GitHub tokens — all blocked at the kernel level |
| **Network firewall** | `pf` rules block SMTP, IRC, FTP, Tor, VPN, and other exfiltration protocols for the agent user |
| **DNS blocklist** | Known tunnel/paste services (ngrok, pastebin, etc.) resolve to localhost |
| **Backup/restore** | Kopia snapshots of the workspace — roll back if the agent breaks something |

## Quick Start

```bash
# Build
cd hazmat
make

# One-time setup (~10 min, needs sudo)
hazmat init

# Launch Claude Code in containment
cd ~/workspace/my-project
hazmat claude
```

`hazmat init` creates the agent user, configures the firewall and DNS blocklist, installs Claude Code for the agent, and asks for your API key. It's interactive — every step is explained and confirmed. Preview without changes:

```bash
hazmat init --dry-run
```

## Usage

### Run Claude Code

```bash
hazmat claude                              # current directory as project
hazmat claude -C ~/workspace/other-proj    # specify project
hazmat claude -R ~/workspace/shared-lib    # expose a read-only directory
```

### Run Any Command

```bash
hazmat exec npm test
hazmat exec python train.py
hazmat exec ./run-agent-loop.sh
hazmat exec -C ~/workspace/proj make build
```

### Interactive Shell

```bash
hazmat shell    # zsh as the agent user, inside containment
```

### Read-Only Directories

The agent can only write to the project directory. Expose additional read-only paths with `-R`:

```bash
hazmat claude -R ~/workspace -R ~/reference-docs
```

This is enforced by the seatbelt — not advisory. The agent physically cannot write outside the project.

## Verify Setup

```bash
hazmat status              # quick health checklist
hazmat init check          # full verification suite
hazmat init check --full   # include live network probes
```

## Backup and Restore

```bash
# Local backup (rsync)
hazmat backup /Volumes/BACKUP/workspace

# Cloud backup (encrypted, incremental via Kopia)
hazmat init cloud                # one-time: configure S3 credentials
hazmat backup --cloud            # snapshot
hazmat restore --cloud           # restore latest
```

## What the Agent Can and Can't Do

**Can:**
- Read and write files in the project directory
- Read directories exposed with `-R`
- Make HTTPS/HTTP requests to any host
- Run any command available to the agent user
- Use git, npm, python, make — normal dev tools

**Can't:**
- Read your SSH keys, AWS credentials, GPG keys, or Keychain
- Send email (SMTP), use IRC, FTP, Tor, VPN, or SOCKS proxies
- Access Docker (socket locked to your user)
- Write files outside the project directory
- Use `sudo`
- Read your shell history, browser data, or credential stores

## Architecture

```
  You (dr)                          Agent (agent)
  ────────                          ─────────────
  ~/                                /Users/agent/
  ~/.ssh, ~/.aws  ← denied →       ~/.claude/ (API key)
  ~/workspace/    ← shared →        ~/workspace/ (symlink)

  hazmat claude
       │
       ├── generates per-session seatbelt policy
       ├── sudo -u agent hazmat-launch <policy>
       ├── sandbox-exec -f <policy> ...
       └── claude --dangerously-skip-permissions
```

Three enforcement layers, all OS-level:
1. **Unix user** — the agent process runs as a different user. Your home directory is simply not accessible.
2. **Seatbelt** — kernel-level filesystem policy denies reads/writes outside approved paths. Credential directories are explicitly denied.
3. **pf firewall** — packet filter rules scoped to `user agent` block dangerous protocols. The agent can't send email, connect to IRC, or tunnel out.

## Undo Everything

```bash
hazmat init rollback                               # remove system config
hazmat init rollback --delete-user --delete-group   # also delete agent account
```

Your project files are not touched.

## Requirements

- macOS (Ventura or later recommended)
- Go 1.21+ (to build)
- Admin access (for one-time `hazmat init`)
- An Anthropic API key (for Claude Code)

## Honest Limitations

Hazmat is OS-level containment, not a VM. Here's what that means:

- **Seatbelt is defense-in-depth.** Apple's SBPL is undocumented and has known mach service escape paths. It prevents accidents and blocks obvious credential access. It is not a security jail against a determined adversary.
- **Network blocking is port/domain-based.** HTTPS exfiltration to novel domains is not blocked. The agent can `curl` any URL on port 443.
- **DNS blocklist is exact-match.** `ngrok.io` is blocked, `*.ngrok.io` subdomains are not (use dnsmasq or NextDNS for wildcard blocking).
- **Shared `/tmp`.** The agent can read temp files from other processes.
- **macOS only.** No Linux, no WSL. The containment primitives (`sandbox-exec`, `dscl`, `pfctl`) are macOS-specific.

For the full threat model, see [threat-matrix.md](threat-matrix.md). For the design assumptions and tradeoffs, see [design-assumptions.md](design-assumptions.md).

If you need stronger isolation, see [tier4-vm-isolation.md](tier4-vm-isolation.md) for the full VM path.

## Documentation

| Doc | What it covers |
|-----|---------------|
| [usage.md](usage.md) | Complete user guide |
| [overview.md](overview.md) | Tier selection and design choices |
| [threat-matrix.md](threat-matrix.md) | Risk-by-risk coverage analysis |
| [design-assumptions.md](design-assumptions.md) | Every non-obvious design decision |
| [attack-surface-deep-dive.md](attack-surface-deep-dive.md) | Escape and exfiltration paths |
| [security-evidence.md](security-evidence.md) | Incidents, CVEs, and academic sources |
| [tla/VERIFIED.md](tla/VERIFIED.md) | TLA+ formal verification of setup/rollback ordering |

## License

MIT
