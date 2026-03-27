# Hazmat UX Analysis

## User Journey Map

### Flow 1: First-Time Setup (Happy Path)

```
 USER                          HAZMAT                         SYSTEM
  |                              |                              |
  |  hazmat setup                |                              |
  |----------------------------->|                              |
  |                              |  pre-flight checks           |
  |                              |    macOS? hazmat-launch?     |
  |                              |    UID/GID available?        |
  |  [sudo password]             |                              |
  |< - - - - - - - - - - - - - >|  create agent user --------->|
  |                              |  create dev group ---------->|
  |  [set agent password]        |  prepare ~/workspace ------->|
  |< - - - - - - - - - - - - - >|  harden gaps --------------->|
  |                              |  install seatbelt ---------->|
  |                              |  install wrappers ---------->|
  |                              |  verify hazmat-launch ------>|
  |                              |  configure sudo ------------>|
  |                              |  configure pf firewall ----->|
  |  [y/n DNS blocklist?]        |  configure DNS ------------->|
  |< - - - - - - - - - - - - - >|  install LaunchDaemon ------>|
  |                              |                              |
  |                              |  --- bootstrap ---           |
  |                              |  install Claude Code ------->|
  |                              |  write settings.json ------->|
  |                              |  create hooks skeleton ----->|
  |                              |                              |
  |                              |  --- enroll ---              |
  |  API key: [paste]            |  write to agent .zshrc ----->|
  |  Git name: [Your Name]       |  git config --global ------->|
  |  Git email: [you@email]      |  git config --global ------->|
  |  Credential helper? [Y]      |  git config --global ------->|
  |                              |                              |
  |  "Setup complete"            |  verify setup checks         |
  |  "hazmat claude"             |                              |
  |<-----------------------------|                              |
  |                              |                              |
  |  hazmat claude               |                              |
  |----------------------------->|  generate SBPL ------------->|
  |                              |  sudo -u agent ------------->|
  |                              |  sandbox-exec -f policy ---->|
  |  [Claude Code session]       |                              |
  |<=============================>                              |
```

**Commands:** 1 (`hazmat setup`) then `hazmat claude`
**Sudo prompts:** 3-5 (setup steps + bootstrap install)
**Time:** ~10 minutes (mostly Claude Code installer download)
**Recovery:** `hazmat rollback` undoes everything


### Flow 2: Daily Usage

```
  USER                          HAZMAT                     AGENT SANDBOX
   |                              |                              |
   |  cd ~/workspace/myproject    |                              |
   |  hazmat claude               |                              |
   |----------------------------->|                              |
   |                              |  resolve project path        |
   |                              |  check Docker artifacts      |
   |                              |  generate per-session SBPL   |
   |                              |  write /tmp/hazmat-PID.sb    |
   |                              |  sudo -u agent hazmat-launch |
   |                              |----------------------------->|
   |                              |                              |  validate policy
   |                              |                              |  sandbox-exec -f
   |                              |                              |  env -i (clean env)
   |                              |                              |  source agent-env
   |                              |                              |  cd project
   |                              |                              |  exec claude
   |  [interactive Claude session]|                              |
   |<=============================|=============================>|
   |                              |                              |
   |  (exit / Ctrl-C)             |  rm /tmp/hazmat-PID.sb       |
   |                              |                              |
```

**Zero configuration.** Just `cd` and `hazmat claude`.


### Flow 3: Backup / Restore

```
  Local backup:                    Cloud backup:
  +----------------------+         +----------------------+
  | hazmat backup /dest  |         | hazmat setup --cloud |  (one-time)
  |   rsync ~/workspace  |         |   S3 endpoint        |
  |   apply excludes     |         |   access key         |
  |   additive by default|         |   secret key         |
  |                      |         |   bucket name        |
  | hazmat backup --sync |         |   encryption password|
  |   /dest              |         |                      |
  |   mirror (deletes!)  |         | hazmat backup --cloud|
  |   needs .backup-     |         |   kopia snapshot     |
  |   target marker      |         |   incremental        |
  +----------------------+         |   encrypted at rest  |
                                   |   deduped            |
  Local restore:                   |                      |
  +----------------------+         | hazmat restore --cloud|
  | hazmat restore /src  |         |   latest snapshot    |
  |   rsync -> ~/workspace|        |   full restore       |
  +----------------------+         +----------------------+
```


### Flow 4: Rollback / Uninstall

```
  hazmat rollback
  |
  +-- remove LaunchDaemon
  +-- remove pf anchor + restore pf.conf from backup
  +-- remove DNS blocklist from /etc/hosts
  +-- remove sudoers entry
  +-- remove seatbelt wrapper
  +-- remove command wrappers + shell blocks
  +-- remove workspace symlink + ACLs
  +-- remove umask blocks
  +-- remove backup scope file
  |
  +-- --delete-user -> delete agent account + home dir
  +-- --delete-group -> delete dev group
      |
      +-- ~/workspace left intact (user must back up first)
```


### Flow 5: Status / Orientation

```
  $ hazmat status

    Hazmat -- AI agent containment for macOS

    [ok] Containment configured   hazmat setup
    [ok] Claude Code installed    hazmat bootstrap
    [->] Credentials set          hazmat enroll   < next
    [ ]  Verified                 hazmat test

    Next step: hazmat enroll
```

`hazmat status` shows where the user is in the setup flow at any time.
Individual phases can be re-run standalone: `hazmat bootstrap` (reinstall
Claude), `hazmat enroll` (update API key or git config).


---

## Command Reference

### Help Output (grouped by workflow phase)

```
Hazmat -- AI agent containment for macOS

Get started:
  setup       Set up everything: containment, Claude Code, and credentials
  bootstrap   Install Claude Code for the agent user (re-run standalone)
  enroll      Set API key and git credentials (re-run standalone)
  test        Verify the hazmat setup is working correctly

Daily use:
  claude      Launch Claude Code inside the sandbox as the agent user
  shell       Open an interactive sandboxed shell as the agent user
  exec        Run a single command inside the sandbox as the agent user

Maintenance:
  backup      Back up the workspace (local rsync or cloud Kopia)
  restore     Restore the workspace from backup
  status      Show setup progress and health check
  rollback    Undo all setup changes
```


---

## Configuration

| Setting | Default | Override |
|---------|---------|---------|
| Workspace path | `~/workspace` | `HAZMAT_WORKSPACE` env var |
| Agent UID | 599 | `--agent-uid` flag on setup |
| Group GID | 599 | `--group-gid` flag on setup |
| Backup excludes | `~/workspace/.backup-excludes` | Edit file directly |
| Cloud backup | `~/.config/hazmat/cloud-backup.json` | `hazmat setup --cloud` |
| Blocked ports | Hardcoded (SMTP, IRC, FTP, Tor, etc.) | Opinionated, not configurable |
| Blocked domains | Hardcoded (ngrok, pastebin, etc.) | Opinionated, not configurable |
| Seatbelt policy | Generated per-session | Dynamic, not configurable |
| Claude deny rules | `~/.claude/settings.json` | Edit after bootstrap |


---

## Non-Obvious Behaviors

1. **SSH is blocked.** The seatbelt denies `~/.ssh` access to prevent credential exfiltration. Use HTTPS + personal access token for git.

2. **`--ignore-docker` escape hatch.** If Docker files are detected, `hazmat claude` refuses to run. Pass `--ignore-docker` if you have Docker files but don't need Docker support.

3. **Read-only vs. read-write.** `-C` (project) = read-write. `-W` (workspace) and `-R` (references) = read-only. Only the project directory can be modified.

4. **`.backup-target` sentinel.** `hazmat backup --sync` requires a `.backup-target` file in the destination to prevent accidental deletions.

5. **Agent password.** Set during setup but rarely needed. `hazmat enroll` and `hazmat claude` use passwordless sudo. To reset: `sudo passwd agent`.

6. **Pre-flight checks.** `hazmat setup` validates platform, hazmat-launch binary, UID/GID availability before making any system changes. If a check fails, nothing is modified.


---

## Remaining UX Improvements (Backlog)

| # | Issue | Severity |
|---|-------|----------|
| 1 | Surface createhomedir/pfctl errors as warnings instead of silencing | Medium |
| 2 | `hazmat version` command for support and upgrades | Medium |
| 3 | Per-workspace backup configuration | Low |
| 4 | Add action hints to ambiguous test warnings | Low |
