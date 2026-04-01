# Using Hazmat

Hazmat runs AI agents on your Mac with full permissions — inside containment. The agent gets its own system user, can't read your credentials, and is blocked from dangerous network protocols. If it breaks something, you roll back.

## Quick Start

Two commands:

```bash
hazmat init       # one-time setup (~10 min, needs sudo)
hazmat claude     # launch Claude Code in containment
```

That's it. `init` creates a contained environment, installs Claude Code, asks for your API key and git credentials, and can optionally import portable basics from your existing Claude setup. After that, you just use `hazmat claude` every day.

## What `hazmat init` Does

When you run `hazmat init`, it:

1. Creates a hidden `agent` macOS user (separate from yours)
2. Sets up a shared workspace at `~/workspace` with proper permissions
3. Installs a firewall that blocks the agent from SMTP, IRC, FTP, Tor, and other exfiltration protocols
4. Adds a DNS blocklist for tunnel and paste services (ngrok, pastebin, etc.)
5. Installs Claude Code for the agent user
6. Asks for your Anthropic API key and git credentials
7. Optionally imports portable Claude basics such as sign-in state, commands, and skills

Everything is interactive — it explains each step and asks for confirmation. To preview without making changes:

```bash
hazmat init --dry-run
```

## Daily Usage

```bash
cd ~/workspace/my-project
hazmat claude
```

This generates a per-session security policy, switches to the agent user, and launches Claude Code inside macOS seatbelt containment. When you exit Claude, the session is cleaned up.

### Giving the Agent Read Access to Other Directories

By default, the agent can only write to the project directory (your current directory). To let it *read* other directories:

```bash
hazmat claude -R ~/workspace              # read all of ~/workspace
hazmat claude -R ~/code/lib -R ~/docs     # cherry-pick specific dirs
```

Read directories are strictly read-only — the agent cannot modify them.

### Specifying a Different Project Directory

```bash
hazmat claude -C ~/workspace/other-project
```

### Resuming a Conversation Inside the Sandbox

When you start a conversation as yourself (`claude`) and later want to continue it inside containment, `--resume` and `--continue` work seamlessly:

```bash
# Start a conversation as yourself (no containment)
cd ~/workspace/my-project
claude

# Later, resume that same conversation inside containment
hazmat claude --resume              # interactive picker — shows your sessions
hazmat claude --continue            # resume the most recent session
hazmat claude --resume <session-id> # resume a specific session by ID
```

**How it works:** Hazmat detects `--resume` or `--continue` in the forwarded flags and copies the matching host Claude session transcripts into the agent user's local Claude session directory before launch.

- `hazmat claude --resume` copies the project's available sessions so Claude can show its picker UI
- `hazmat claude --continue` copies only the latest session
- `hazmat claude --resume <session-id>` copies one specific session
- Existing agent-local files are not overwritten, so contained continuations stay independent once they diverge

**Security note:** The sandbox does not get direct access to your host `~/.claude/projects/` directory. Hazmat stages copies into the agent-owned Claude store instead.

### Continuing a Hazmat Session Outside the Sandbox

When a conversation started inside containment and you want to continue it as your normal user, export the hazmat session into your host Claude session store and then resume it:

```bash
# Continue the latest hazmat Claude session for the current project
claude --resume "$(hazmat export claude session)" --fork-session

# Continue a specific hazmat session
claude --resume "$(hazmat export claude session <session-id>)" --fork-session

# Export from a different project directory
claude --resume "$(hazmat export claude session -C ~/workspace/other-project)" --fork-session
```

**What `hazmat export claude session` does:**

- Defaults to the latest hazmat Claude session for the current project
- Accepts an optional session ID to export a specific session
- Copies the transcript and session sidecar directory from the agent user's `~/.claude/projects/...`
- Updates your host Claude `sessions-index.json`
- Prints the Claude resume ID on stdout for scripting

`--fork-session` is recommended so your host-side continuation cleanly diverges from the contained hazmat session. The export is a point-in-time handoff, not a live sync. If the hazmat session advances later, run the export again before resuming.

### Running Other Commands in Containment

```bash
hazmat shell                    # interactive shell as the agent user
hazmat exec npm install         # run a single command
hazmat exec -C ~/workspace/proj npm test
```

## Checking Status

```bash
hazmat                          # shows setup progress checklist
hazmat status                   # same thing
hazmat check               # run full verification suite
hazmat check --full        # include live network probes
```

## Backup and Restore

### Local Backup (rsync)

```bash
hazmat backup /Volumes/BACKUP/workspace           # additive copy
hazmat backup --sync /Volumes/BACKUP/workspace     # mirror (deletes extras)
hazmat backup --show-scope                          # show what's included/excluded
```

Edit `~/workspace/.backup-excludes` to control what's excluded.

### Cloud Backup (encrypted, incremental)

```bash
hazmat init cloud              # one-time: configure S3 credentials
hazmat backup --cloud          # incremental encrypted snapshot
hazmat restore --cloud         # restore latest snapshot
```

## Updating Credentials

```bash
hazmat config agent            # re-enter API key, git name/email
```

## Importing Portable Claude Basics

```bash
hazmat config import claude
hazmat config import claude --dry-run
hazmat config import claude --overwrite
hazmat config import claude --skip-existing
```

Hazmat treats this as a curated import, not a full Claude migration. It can copy sign-in state, git identity, commands, and skills into the agent environment. Hazmat keeps its own runtime settings, hooks, MCP configuration, plugins, and safety controls.

Detailed scope, symlink behavior, conflict handling, and MCP migration guidance live in [claude-import.md](claude-import.md).

## Running OpenCode

```bash
hazmat bootstrap opencode
hazmat opencode
hazmat opencode -p "summarize this repo"
```

This is a prototype harness path alongside Claude. It uses the same containment, project preflight, and snapshot flow, but OpenCode keeps its own config and session state.

## Importing Portable OpenCode Basics

```bash
hazmat config import opencode
hazmat config import opencode --dry-run
hazmat config import opencode --overwrite
hazmat config import opencode --skip-existing
```

Hazmat treats this as a curated import, not a full OpenCode migration. It can copy sign-in state, git identity, commands, agents, and skills into the agent environment. Hazmat keeps its own runtime settings, plugins, project-local `.opencode` directories, and safety controls separate.

Detailed scope, symlink behavior, and migration guidance live in [opencode-import.md](opencode-import.md).

## Uninstalling

```bash
hazmat rollback                              # remove all system config
hazmat rollback --delete-user --delete-group  # also delete agent account
```

Your project files are not deleted. Back them up first if needed.

## What the Agent Can and Can't Do

**Can:**
- Read and write files in your project directory
- Read directories you expose with `-R`
- Make HTTPS requests to any host
- Run any command available to the agent user
- Access `/private/tmp` for temporary files

**Can't:**
- Read your SSH keys, AWS credentials, GPG keys, or Keychain
- Send email (SMTP blocked), use IRC, FTP, Tor, or VPN protocols
- Access Docker (socket locked to your user only)
- Read files outside the approved directories
- Use `sudo`

## Environment Variable

Set `HAZMAT_WORKSPACE` to override the default `~/workspace` path:

```bash
export HAZMAT_WORKSPACE=~/code
hazmat init    # uses ~/code instead of ~/workspace
```
