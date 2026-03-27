# Using Hazmat

Hazmat runs AI agents on your Mac with full permissions — inside containment. The agent gets its own system user, can't read your credentials, and is blocked from dangerous network protocols. If it breaks something, you roll back.

## Quick Start

Two commands:

```bash
hazmat init       # one-time setup (~10 min, needs sudo)
hazmat claude     # launch Claude Code in containment
```

That's it. `init` creates a contained environment, installs Claude Code, and asks for your API key and git credentials. After that, you just use `hazmat claude` every day.

## What `hazmat init` Does

When you run `hazmat init`, it:

1. Creates a hidden `agent` macOS user (separate from yours)
2. Sets up a shared workspace at `~/workspace` with proper permissions
3. Installs a firewall that blocks the agent from SMTP, IRC, FTP, Tor, and other exfiltration protocols
4. Adds a DNS blocklist for tunnel and paste services (ngrok, pastebin, etc.)
5. Installs Claude Code for the agent user
6. Asks for your Anthropic API key and git credentials

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
hazmat init enroll             # re-enter API key, git name/email
```

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
