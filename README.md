<p align="center">
  <a href="#"><img src="assets/hazmat-final.png" alt="Hazmat" width="400"></a>
</p>

<h1 align="center">Hazmat</h1>

<p align="center">
  <strong>Full autonomy. Controlled environment.</strong><br>
  OS-level containment for AI coding agents on macOS
</p>

---

AI coding agents are most useful when you let them work autonomously. But full autonomy means the agent runs with your full privileges, your credentials, your files.

Hazmat makes that safe.

```bash
hazmat claude                     # Claude Code with full containment
hazmat opencode                   # OpenCode with full containment
hazmat exec ./my-agent-loop.sh    # any agent, any script
```

One command. The agent gets its own macOS user, a kernel-enforced sandbox, a firewall, and automatic pre-session backups. You get full productivity without the risk.

## What You See

Every session starts with a contract — a plain-language summary of what the agent can and can't do:

```
hazmat: session
  Mode:                 Native containment
  Why this mode:        using native containment because no Docker requirement was detected
  Project (read-write): /Users/dr/workspace/my-app
  Integrations:         go
  Auto read-only:       /Users/dr/go/pkg/mod
  Read-only extensions: none
  Read-write extensions: none
  Service access:       none
  Pre-session snapshot: on
  Snapshot excludes:    vendor/
```

If the project looks compatible with a private Docker daemon, Hazmat switches modes automatically:

```
hazmat: session
  Mode:                 Docker Sandbox
  Why this mode:        using Docker Sandbox because this project appears compatible with a private Docker daemon (Dockerfile)
  Project (read-write): /Users/dr/workspace/api-service
  Integrations:         node
  Auto read-only:       none
  Read-only extensions: none
  Read-write extensions: none
  ...
```

Preview any session before running it with `hazmat explain`.

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

### Docker Projects

Hazmat distinguishes between two Docker shapes:

- **Private-daemon Docker projects** auto-route into Docker Sandbox mode. The agent runs inside an isolated sandbox with its own private Docker daemon.
- **Shared-daemon Docker projects** do not. If the repo appears to depend on the host Docker daemon, Hazmat stops and asks you to choose an explicit code-only session (`--docker=none`) or move the workflow to Tier 4.

Use `hazmat config docker none -C /path/to/project` to persist code-only routing for a shared-daemon repo. See [docs/tier3-docker-sandboxes.md](docs/tier3-docker-sandboxes.md) and [docs/shared-daemon-projects.md](docs/shared-daemon-projects.md).

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

# Or install from GitHub releases
curl -fsSL https://raw.githubusercontent.com/dredozubov/hazmat/master/scripts/install.sh | bash

# One-time setup (~10 min)
hazmat init --bootstrap-agent claude

# Start working
cd your-project
hazmat claude
```

`hazmat init` creates the agent user, configures containment, and sets up automatic snapshots. During interactive setup you can choose to bootstrap Claude Code, Codex, OpenCode, or skip agent installation and add one later with `hazmat bootstrap ...`. It can also seed Claude with portable conveniences from an existing setup while keeping Hazmat in control of runtime and safety settings. Interactive setup now also offers an explicit opt-in for a broader passwordless `sudo -u agent` rule if you want bootstrap and other maintenance commands to stop re-prompting for your password. Preview first with `hazmat init --dry-run`.

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

# Continue a hazmat Claude session as your normal user
claude --resume "$(hazmat export claude session)" --fork-session

# Interactive shell
hazmat shell

# See what the agent changed
hazmat diff
hazmat snapshots
hazmat restore          # undo last session
```

### Extra Directories

The agent can only write to the project directory by default. Expose
additional read-only or read-write paths explicitly:

```bash
hazmat claude -R ~/workspace/shared-lib -R ~/reference-docs
hazmat claude -W ~/.venvs/my-app
hazmat config access add -C ~/workspace/my-app --read ~/reference-docs --write ~/.venvs/my-app
```

`-R` stays read-only. `-W` adds another explicit writable root for that
project or session. Both are enforced by the kernel sandbox — not advisory.

### Session Integrations

```bash
hazmat integration list
hazmat integration show node
hazmat claude --integration node
hazmat claude --integration python-uv
hazmat config set integrations.pin "~/workspace/my-app:node,go"
```

Session integrations are ergonomic overlays for common stacks. They may add
auto-resolved read-only toolchain paths, extend snapshot excludes, and pass
through safe environment selectors like `GOPATH` or `VIRTUAL_ENV`. They do
not widen write access, relax the credential deny list, or change the network
policy.

If a repo mixes stacks across subdirectories, add `.hazmat/integrations.yaml`
to the repo so users do not have to discover nested frontend or TLA+
integrations manually.

Repos can declare recommended integrations in `.hazmat/integrations.yaml`;
Hazmat prompts once for approval, then reuses that approval until the file
changes. See [docs/integrations.md](docs/integrations.md).

### Handing a Hazmat Session Back to Host Claude

If you start a conversation inside `hazmat claude` and later want to continue it outside containment, export it into your normal Claude session store:

```bash
claude --resume "$(hazmat export claude session)" --fork-session
claude --resume "$(hazmat export claude session <session-id>)" --fork-session
```

`hazmat export claude session` exports the latest hazmat Claude session for the current project by default, or a specific session when you pass an ID. It copies the session bundle into your host `~/.claude/projects/...` directory and prints the resume ID on stdout.

## Configuration

```bash
hazmat config                                        # view everything
hazmat config edit                                   # open config in $EDITOR
hazmat config agent                                  # set API key + git identity
hazmat config import claude                          # import portable basics from an existing setup
hazmat bootstrap claude                              # install Claude Code for the agent user
hazmat bootstrap codex                               # install Codex for the agent user
hazmat config import opencode                        # import portable OpenCode basics from an existing setup
hazmat bootstrap opencode                            # install OpenCode for the agent user
hazmat opencode                                      # launch OpenCode in containment
hazmat integration list                              # inspect built-in and user integrations
hazmat config set integrations.pin "~/workspace/app:node,go" # auto-activate integrations for a project
hazmat config access add -C ~/workspace/app --write ~/.venvs/app # persist project read/write extensions
hazmat config docker none -C ~/workspace/app         # persist code-only mode for a shared-daemon repo
hazmat config cloud                                  # set up S3 backup
hazmat config set session.skip_permissions false      # re-enable Claude's permission prompts
hazmat config set backup.retention.keep_latest 30     # change snapshot retention
```

All settings live in `~/.hazmat/config.yaml`.

Portable import keeps Hazmat's runtime and safety config separate from whatever you use outside containment. See [docs/claude-import.md](docs/claude-import.md) for the current import rules and non-goals.
OpenCode follows the same curated story; see [docs/opencode-import.md](docs/opencode-import.md).
Session integrations are documented in [docs/integrations.md](docs/integrations.md).

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

The default passwordless sudoers rule stays narrow and only covers
`hazmat-launch`. A separate optional rule can allow generic
`sudo -u agent ...` maintenance commands without repeated password prompts.

Three OS-level enforcement layers:
1. **Unix user** — the agent runs as a different user. Your home directory is structurally inaccessible.
2. **Seatbelt** — kernel-level filesystem policy. Default deny, explicit allows for project and toolchain paths.
3. **pf firewall** — packet filter rules scoped to `user agent`. Blocks dangerous protocols.

Setup ordering, seatbelt policy structure, backup safety, version migration,
session-time host permission repairs, harness lifecycle state, Tier 3 launch
containment, Tier 2/Tier 3 core policy equivalence, and native launch fd
isolation are
[formally verified with TLA+](tla/VERIFIED.md).

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
| [claude-import.md](docs/claude-import.md) | Portable Claude basics import: scope, conflicts, and non-goals |
| [opencode-import.md](docs/opencode-import.md) | Portable OpenCode basics import: scope, conflicts, and non-goals |
| [integrations.md](docs/integrations.md) | Session integrations: activation, project extensions, repo recommendations, trust model |
| [testing.md](docs/testing.md) | Test suite map: local loops, e2e scripts, CI, and VM-backed verification |
| [tier3-docker-sandboxes.md](docs/tier3-docker-sandboxes.md) | Docker Sandbox mode: setup, network policy, Compose hardening |
| [cve-audit.md](docs/cve-audit.md) | How hazmat defends against every known Claude Code CVE |
| [threat-matrix.md](docs/threat-matrix.md) | Risk-by-risk coverage analysis |
| [design-assumptions.md](docs/design-assumptions.md) | Every non-obvious design decision |
| [brief-supply-chain-hardening.md](docs/brief-supply-chain-hardening.md) | Supply chain attack analysis and mitigations |
| [tla/VERIFIED.md](tla/VERIFIED.md) | TLA+ formal verification specs |

## Blog Post

[How I Made --dangerously-skip-permissions Safe in Claude Code](https://codeofchange.io/how-i-made-dangerously-skip-permissions-safe-in-claude-code/)

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

---

<sub>The Simpsons and all related characters are property of 20th Television and The Walt Disney Company. The Claude logo is property of Anthropic. We do not claim any rights to these properties. Their use here is purely for entertainment purposes.</sub>
