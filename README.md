<p align="center">
  <img src="assets/hazmat-final.png" alt="Hazmat" width="200">
</p>

<h1 align="center">Hazmat</h1>

<p align="center">
  <strong>Full autonomy. Controlled environment.</strong><br>
  The missing runtime for <code>--dangerously-skip-permissions</code>
</p>

---

Claude Code is most useful when you let it work autonomously. But `--dangerously-skip-permissions` means exactly what it says — the agent runs with your full privileges, your credentials, your files.

Hazmat makes that safe.

```bash
hazmat claude    # full autonomy, OS-level containment, automatic snapshots
```

One command. The agent gets its own macOS user, a kernel-enforced sandbox, a firewall, and automatic pre-session backups. You get full productivity without the risk.

## The Problem

`--dangerously-skip-permissions` is where the real productivity is. Permission prompts break flow, interrupt agent loops, and make multi-step tasks impractical. Every serious Claude Code user ends up here eventually.

But the built-in protections aren't enough:

- **Agents actively reason about escaping.** Ona's research showed Claude Code [bypassing its own denylist](https://ona.com/stories/how-claude-code-escapes-its-own-denylist-and-sandbox) via `/proc/self/root` path traversal, then attempting to disable bubblewrap when that was caught.
- **[16 Claude Code CVEs](docs/cve-audit.md) and counting.** [CVE-2025-59536](https://nvd.nist.gov/vuln/detail/CVE-2025-59536): RCE through project config files. [CVE-2026-25725](https://advisories.gitlab.com/pkg/npm/@anthropic-ai/claude-code/CVE-2026-25725/): sandbox escape via `settings.json` injection. [CVE-2026-21852](https://nvd.nist.gov/vuln/detail/CVE-2026-21852): API key exfiltration before the trust prompt appeared.
- **Supply chain attacks execute instantly.** The [axios npm compromise](https://github.com/axios/axios/issues/10604) (2026) delivered a RAT through a `postinstall` hook in 2 seconds — before `npm install` even finished. The [s1ngularity attack](https://www.wiz.io/blog/s1ngularity-supply-chain-attack) weaponized Claude Code itself to steal credentials.

No single layer is enough. A seatbelt profile can block file reads — but not HTTPS exfiltration. A firewall can block protocols — but not credential access. You need all of them working together.

## What Hazmat Does

```bash
hazmat claude                     # Claude Code with full autonomy
hazmat exec ./my-agent-loop.sh    # any agent, any script
hazmat shell                      # interactive contained shell
```

| Layer | What it protects |
|-------|-----------------|
| **User isolation** | Dedicated `agent` macOS user. Your `~/.ssh`, `~/.aws`, Keychain — structurally inaccessible |
| **Kernel sandbox** | Per-session [seatbelt](https://developer.apple.com/documentation/security) policy. Project gets read-write, everything else denied |
| **Credential deny** | SSH keys, AWS creds, GPG keys, GitHub tokens — blocked at the kernel level, even inside agent home |
| **Network firewall** | `pf` rules block SMTP, IRC, FTP, Tor, VPN, and other exfiltration protocols |
| **DNS blocklist** | Known tunnel/paste/C2 services (ngrok, pastebin, webhook.site) resolve to localhost |
| **Supply chain hardening** | npm `ignore-scripts=true` by default — blocks the entire class of postinstall attacks |
| **Automatic snapshots** | Kopia snapshots before every session — roll back if the agent breaks something |

### Comparison

| | [Built-in sandbox](https://github.com/anthropic-experimental/sandbox-runtime) | [Agent Safehouse](https://github.com/eugene1g/agent-safehouse) | [SandVault](https://github.com/webcoyote/sandvault) | [nono](https://github.com/always-further/nono) | [Docker](https://docs.docker.com/ai/sandboxes/) | **Hazmat** |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| Separate user account | — | — | ✓ | — | ✓ | ✓ |
| Seatbelt / kernel sandbox | ✓ | ✓ | ✓ | ✓ | n/a | ✓ |
| Credential path deny | — | partial | — | — | ✓ | ✓ |
| Network firewall (pf) | — | — | — | — | ✓ | ✓ |
| DNS blocklist | — | — | — | — | — | ✓ |
| Supply chain hardening | — | — | — | — | — | ✓ |
| Backup / rollback | — | — | — | ✓ | — | ✓ |
| Agent-agnostic | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| macOS native | ✓ | ✓ | ✓ | ✓ | — | ✓ |

## Quick Start

```bash
# Install via Homebrew
brew install dredozubov/tap/hazmat

# One-time setup (~10 min)
hazmat init

# Start working
cd your-project
hazmat claude
```

`hazmat init` creates the agent user, configures containment, installs Claude Code, and sets up automatic snapshots. Every step is explained and confirmed. Preview first with `hazmat init --dry-run`.

## Daily Workflow

```bash
# Claude Code — full autonomy, contained
hazmat claude
hazmat claude -p "refactor the auth module"
hazmat claude -C ~/other-project

# Any command in containment
hazmat exec npm test
hazmat exec python train.py
hazmat exec ./run-agent-loop.sh

# Interactive shell
hazmat shell

# See what the agent changed
hazmat diff
hazmat snapshots
hazmat restore          # undo last session
```

### Read-Only Directories

The agent can only write to the project directory. Expose additional read-only paths:

```bash
hazmat claude -R ~/workspace/shared-lib -R ~/reference-docs
```

Enforced by the kernel sandbox — not advisory.

## Configuration

```bash
hazmat config                                        # view everything
hazmat config edit                                   # open config in $EDITOR
hazmat config agent                                  # set API key + git identity
hazmat config cloud                                  # set up S3 backup
hazmat config set session.skip_permissions false      # re-enable Claude's permission prompts
hazmat config set backup.retention.keep_latest 30     # change snapshot retention
```

All settings live in `~/.hazmat/config.yaml`.

## Architecture

```
  You (dr)                          Agent (agent)
  ────────                          ─────────────
  ~/                                /Users/agent/
  ~/.ssh, ~/.aws  ← denied →       ~/.claude/
  ~/workspace/    ← shared →        ~/workspace/ (symlink)

  hazmat claude
       │
       ├── snapshot project (Kopia)
       ├── generate per-session seatbelt policy
       ├── sudo -u agent hazmat-launch <policy>
       ├── sandbox_init() (kernel sandbox)
       └── exec claude --dangerously-skip-permissions
```

Three OS-level enforcement layers:
1. **Unix user** — the agent runs as a different user. Your home directory is structurally inaccessible.
2. **Seatbelt** — kernel-level filesystem policy. Default deny, explicit allows for project and toolchain paths.
3. **pf firewall** — packet filter rules scoped to `user agent`. Blocks dangerous protocols.

Setup ordering, seatbelt policy structure, and backup safety are [formally verified with TLA+](tla/VERIFIED.md).

## Undo Everything

```bash
hazmat rollback                               # remove all system config
hazmat rollback --delete-user --delete-group   # also delete agent account
```

Your project files are not touched.

## Honest Limitations

- **Seatbelt is defense-in-depth.** Apple's SBPL is undocumented. It prevents accidents and blocks credential access, but is not a VM-level boundary.
- **HTTPS exfiltration is not blocked.** The agent can `curl` any URL on port 443. The DNS blocklist catches known-bad domains but not novel ones.
- **macOS only.** No Linux (yet). The containment primitives are macOS-specific.
- **Shared `/tmp`.** The agent can read temp files from other processes.

For the full threat model, see [threat-matrix.md](docs/threat-matrix.md). For stronger isolation, see [tier4-vm-isolation.md](docs/tier4-vm-isolation.md).

## Documentation

| Doc | What it covers |
|-----|---------------|
| [usage.md](docs/usage.md) | Complete user guide |
| [cve-audit.md](docs/cve-audit.md) | How hazmat defends against every known Claude Code CVE |
| [threat-matrix.md](docs/threat-matrix.md) | Risk-by-risk coverage analysis |
| [design-assumptions.md](docs/design-assumptions.md) | Every non-obvious design decision |
| [brief-supply-chain-hardening.md](docs/brief-supply-chain-hardening.md) | Supply chain attack analysis and mitigations |
| [tla/VERIFIED.md](tla/VERIFIED.md) | TLA+ formal verification specs |

## Contributing

Hazmat is early. The UX, security model, and documentation are all actively evolving. Feedback is the most valuable contribution right now.

**Ways to help:**

- **Try it and tell us what broke.** [Open an issue](https://github.com/dredozubov/hazmat/issues) with your macOS version and what happened. Rough bug reports are fine.
- **Tell us what's confusing.** If a prompt didn't make sense, a command did something unexpected, or the docs left you guessing — that's a bug.
- **Security review.** If you find a containment bypass, credential leak, or policy gap — please report it. We take these seriously.
- **Linux port.** The architecture is OS-agnostic (user isolation + kernel sandbox + firewall). The primitives are different (namespaces, seccomp, nftables). This is the biggest open project.

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions and PR guidelines.

## License

MIT
