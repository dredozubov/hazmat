# Hazmat Design Assumptions

Every design decision that isn't obvious from reading `hazmat --help`.

## Platform

**macOS only.** Requires `sandbox-exec`, `dscl`, `pfctl`, `launchctl`. No Linux support, no WSL, no Docker-based alternative. This is intentional — the whole value proposition is native macOS containment without VMs.

**No minimum version specified.** Works on Sequoia. Probably works on Ventura+. Untested on older. We don't check `sw_vers` — if the required binaries exist, it runs.

**Both Intel and Apple Silicon.** PATH includes `/opt/homebrew` (ARM) and `/usr/local` (x86). Both are always present; the wrong one is harmless dead entries.

## The Agent User

**One agent user, period.** Username `agent`, UID 599, home `/Users/agent`. These are hardcoded constants, not configuration. You can override UID/GID at setup time (`--agent-uid`, `--group-gid`), but the username and home path are fixed.

**One human controller.** Setup creates ACLs for the user who runs `hazmat setup`. A second human user on the same Mac cannot co-manage the workspace without manual ACL changes.

**Concurrent sessions are possible but racy.** Each `hazmat claude` gets a unique seatbelt policy (PID-based filename). But both sessions share the same agent home, Claude config, git identity, and API key. Two Claude instances writing to `~/.claude/` simultaneously is undefined behavior. We don't prevent it.

## Shell

**zsh only.** The agent user's shell is `/bin/zsh`. Environment setup writes to `.zshrc`. Host wrappers are added to the host user's `.zshrc`. If you use bash or fish as your login shell, the PATH modifications from setup won't take effect — you'd need to source them manually. We don't detect or warn about this.

## Network Security Model

**Allow by default, block known bad.** This is the core security tradeoff. The agent can make any HTTPS request to any host. We block specific dangerous protocols:

| Blocked | Why |
|---------|-----|
| SMTP (25, 465, 587) | Email exfiltration |
| IRC (6660-6669, 6697) | C2 channels |
| FTP (20, 21), Telnet (23) | Legacy insecure protocols |
| SMB (445), RDP (3389), VNC (5900-5901) | Lateral movement |
| Tor (9050, 9150), SOCKS (1080) | Anonymous exfiltration |
| VPN (1194, 1723, 4500) | Tunnel escape |
| XMPP (5222, 5269) | Messaging exfiltration |
| ICMP | Tunnel/covert channel |

**What's NOT blocked:** HTTP (80), HTTPS (443), DNS (53), WebSockets on 80/443, any custom protocol on an unblocked port. The agent can `curl` any URL, push to any git remote, or call any API.

**DNS blocklist is domain-exact, not wildcard.** `/etc/hosts` blocks `ngrok.io` but not `*.ngrok.io`. Subdomains pass through. For wildcard blocking, you need dnsmasq or NextDNS (documented, not automated).

**DNS blocklist is system-wide.** It modifies `/etc/hosts`, which affects ALL users on the machine, not just the agent. This is the only system-wide side effect that isn't scoped to the agent user.

## Credential Storage

**Plain text, no encryption.** API key is `export ANTHROPIC_API_KEY="sk-ant-..."` in `/Users/agent/.zshrc`. Git credentials are in `/Users/agent/.config/git/credentials` (git's built-in store). No Keychain integration.

**Seatbelt protects the host user's credentials.** The deny list blocks: `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/Library/Keychains`, `~/.config/gh`. The agent cannot read the host user's SSH keys, AWS tokens, or GitHub CLI tokens.

**The deny list is not exhaustive.** Not blocked: `~/.docker/config.json`, `~/.kube/config`, `~/.netrc`, `~/.m2/settings.xml`, `/Library/Preferences`. If you have credentials in these locations, the seatbelt won't prevent the agent from reading them. We should expand this list.

**Credentials in the project directory are exposed.** If your project has `.env`, `.env.local`, or embedded secrets, the agent can read them — the project directory is read-write by design.

## Seatbelt (sandbox-exec) Containment

**Defense in depth, not a security boundary.** The seatbelt is a soft sandbox. Apple's SBPL enforcement is undocumented, has known bypasses via mach services, and is not designed as a security jail. It prevents accidental damage and blocks obvious credential access, but a determined adversary in the agent session could likely escape.

**Mach services are broadly allowed.** The policy permits `mach-host*` (all host-level mach services), plus `logger`, `coreservicesd`, `notification_center`, `mDNSResponder`. These are necessary for normal operation but expand the attack surface. We allow them because blocking them breaks basic tooling (git, node, python).

**Per-session policies with literal paths.** Each session generates a fresh SBPL file with absolute paths embedded as string literals. This means:
- Symlink resolution happens once at session start
- If the filesystem changes during a session, the policy doesn't update
- Policy files are written to `/private/tmp/hazmat-<pid>.sb` and cleaned up on exit

**/tmp is shared.** The agent can read and write `/private/tmp` and `/private/var/folders`. These are shared with all users on the system. Sensitive temp files from other processes are accessible.

## Workspace Model

**One canonical workspace.** `~/workspace` (or `HAZMAT_WORKSPACE`). All backup, restore, and scope operations target this single root. You can run `hazmat claude` on projects outside this path (with a warning), but they won't be covered by `hazmat backup`.

**Project = read-write, everything else = read-only.** The `-C` flag (project) grants full write access. The `-W` flag (workspace root) and `-R` flags (references) grant read-only access. This is enforced by the seatbelt, not advisory.

**No workspace isolation between projects.** If you pass `-W ~/workspace`, the agent can read ALL projects in your workspace. There's no per-project read boundary. The project flag only controls write scope.

## Claude Code Coupling

**Tightly coupled to Claude Code.** The bootstrap step installs Claude Code specifically (`curl -fsSL https://claude.ai/install.sh`). The seatbelt allows `~/.claude/` read-write. Settings and hooks use Claude Code's format. The `hazmat claude` command runs the Claude binary.

**Cannot run other agents without code changes.** To use Cursor, aider, or OpenAI's tools, you'd need to modify: the install step, the seatbelt policy paths, the wrapper script, and the settings format. There's no agent abstraction layer.

**The vision is agent-agnostic containment.** The current implementation is Claude-specific because that's the first use case, but the underlying containment (user isolation, pf, seatbelt) is generic. A future `hazmat run <anything>` would generalize this.

## Backup

**Excludes are for common web/Python/Rust/Node projects.** Built-in excludes: `node_modules/`, `.venv/`, `__pycache__/`, `.next/`, `dist/`, `build/`, `target/`. If your project uses different conventions (e.g., Java `out/`, Go `vendor/` not excluded by default), those directories get backed up.

**Credentials may be in backups.** The agent's `.zshrc` (containing the API key) and git credentials file are inside the agent home, not the workspace, so they're NOT in workspace backups. But if you have `.env` files in your project, those ARE backed up.

**Cloud backup encrypts at rest.** Kopia encrypts all blobs with the repository password. Local rsync backups are unencrypted.

## Rollback

**Rollback does not touch the workspace.** `hazmat rollback` removes system configuration (users, firewall, sudoers, wrappers) but leaves `~/workspace` intact. Any files the agent created or modified in the workspace persist.

**Agent user persists by default.** Rollback leaves the agent account unless you pass `--delete-user`. This means `/Users/agent` and all its contents (Claude cache, settings, credentials) survive rollback.

**pf.conf restoration depends on backup.** Setup creates a timestamped backup of `/etc/pf.conf` before modifying it. Rollback restores from this backup. If the backup is missing or was modified after setup, rollback strips the anchor lines in-place, which is fragile.
