# Hazmat UX Analysis

## User Journey Map

### Flow 1: First-Time Setup (Happy Path)

```
 USER                          HAZMAT                         SYSTEM
  |                              |                              |
  |  hazmat init                |                              |
  |----------------------------->|                              |
  |                              |  pre-flight checks           |
  |                              |    macOS? hazmat-launch?     |
  |                              |    UID/GID available?        |
  |  [sudo password]             |                              |
  |< - - - - - - - - - - - - - >|  create agent user --------->|
  |                              |  create dev group ---------->|
  |  [set agent password]        |  prepare ~/workspace ------->|
  |                              |  configure snapshot backup ->|
  |  Cloud backup? [y/N]         |  (optional: S3 credentials) |
  |< - - - - - - - - - - - - - >|  harden gaps --------------->|
  |                              |  install seatbelt ---------->|
  |                              |  install wrappers ---------->|
  |                              |  verify hazmat-launch ------>|
  |                              |  configure sudo ------------>|
  |                              |  configure pf firewall ----->|
  |                              |  configure DNS ------------->|
  |                              |  install LaunchDaemon ------>|
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

**Commands:** 1 (`hazmat init`) then `hazmat claude`
**Sudo prompts:** 3-5 (setup steps + bootstrap install)
**Time:** ~10 minutes (mostly Claude Code installer download)
**Recovery:** `hazmat init rollback` undoes everything


### Flow 2: Daily Usage

```
  USER                          HAZMAT                     AGENT SANDBOX
   |                              |                              |
   |  cd ~/workspace/myproject    |                              |
   |  hazmat claude               |                              |
   |----------------------------->|                              |
   |                              |  resolve project path        |
   |                              |  snapshot project (Kopia)    |
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
  Automatic (every session):         Cloud (optional):
  +----------------------------+     +----------------------------+
  | hazmat claude/exec/shell   |     | hazmat config cloud        |  (one-time)
  |   pre-session Kopia        |     |   S3 endpoint              |
  |   snapshot of project dir  |     |   access key               |
  |   incremental, sub-second  |     |   secret key → credential  |
  |   stored in local repo     |     |   bucket + encryption pw   |
  +----------------------------+     |                            |
                                     | hazmat backup --cloud      |
  Recovery:                          |   workspace Kopia snapshot |
  +----------------------------+     |   incremental, encrypted   |
  | hazmat snapshots           |     |   deduped                  |
  |   list project snapshots   |     |                            |
  | hazmat diff                |     | hazmat restore --cloud     |
  |   changes since snapshot   |     |   latest snapshot          |
  | hazmat restore             |     |   full workspace restore   |
  |   restore project from     |     +----------------------------+
  |   local snapshot           |
  |   (snapshots current state |     Configuration:
  |   first = undo-the-undo)   |     +----------------------------+
  +----------------------------+     | hazmat config              |
                                     |   view current settings    |
                                     | hazmat config edit         |
                                     |   open config.yaml         |
                                     | hazmat config set K V      |
                                     |   change retention, etc.   |
                                     +----------------------------+
```


### Flow 4: Rollback / Uninstall

```
  hazmat init rollback
  |
  +-- remove LaunchDaemon
  +-- remove pf anchor + restore pf.conf from backup
  +-- remove DNS blocklist from /etc/hosts
  +-- remove sudoers entry
  +-- remove seatbelt wrapper
  +-- remove command wrappers + shell blocks
  +-- remove workspace symlink + ACLs
  +-- remove umask blocks
  +-- remove local snapshot repository
  |
  +-- --delete-user -> delete agent account + home dir
  +-- --delete-group -> delete dev group
      |
      +-- your files are not deleted (back up first with hazmat backup)
```


### Flow 5: Status / Orientation

```
  $ hazmat status

    Hazmat -- AI agent containment for macOS

    [ok] Containment configured   hazmat init
    [ok] Claude Code installed    hazmat init
    [->] Credentials set          hazmat init enroll   < next

    Next step: hazmat init enroll
```

`hazmat status` shows where the user is in the flow at any time.
Individual concerns can be re-run: `hazmat init enroll` (update API key
or git config), `hazmat init check` (verify setup).


---

## Command Reference

### Help Output

```
Hazmat -- AI agent containment for macOS

Setup (run once):
  init        Set up containment, install Claude Code, configure credentials

  Subcommands:
    init check      Verify the setup is working
    init rollback   Undo all setup changes
    init enroll     Re-configure API key and git credentials
    init cloud      Configure S3 cloud backup

Run agents:
  claude      Launch Claude Code in containment
  shell       Open a contained shell
  exec        Run a command in containment

Snapshots:
  diff        Show changes since the last snapshot
  restore     Restore project from snapshot or workspace from cloud
  snapshots   List local snapshots for the current project

Workspace:
  backup      Back up workspace to cloud (Kopia)
  config      View or edit hazmat configuration
  status      Show setup progress and health
```


---

## Configuration

All backup settings live in `~/.config/hazmat/config.yaml`. Cloud secret key
is stored separately in `~/.config/hazmat/cloud-credentials` (0600).

| Setting | Default | Override |
|---------|---------|---------|
| Workspace path | `~/workspace` | `HAZMAT_WORKSPACE` env var |
| Agent UID | 599 | `--agent-uid` flag on init |
| Group GID | 599 | `--group-gid` flag on init |
| Snapshot retention | 20 latest, 7 daily, 4 weekly | `hazmat config set backup.retention.keep_latest N` |
| Snapshot excludes | node_modules, .venv, dist, build, target | `hazmat config set backup.excludes.add PATTERN` |
| Cloud endpoint | — | `hazmat config cloud` or `hazmat config set` |
| Cloud secret key | — | `hazmat config cloud` or `HAZMAT_CLOUD_SECRET_KEY` env |
| Blocked ports | Hardcoded (SMTP, IRC, FTP, Tor, etc.) | Opinionated, not configurable |
| Blocked domains | Hardcoded (ngrok, pastebin, etc.) | Opinionated, not configurable |
| Seatbelt policy | Generated per-session | Dynamic, not configurable |
| Claude deny rules | `~/.claude/settings.json` | Edit after bootstrap |


---

## Non-Obvious Behaviors

1. **SSH is blocked.** The seatbelt denies `~/.ssh` access to prevent credential exfiltration. Use HTTPS + personal access token for git.

2. **`--ignore-docker` escape hatch.** If Docker files are detected, `hazmat claude` refuses to run. Pass `--ignore-docker` if you have Docker files but don't need Docker support.

3. **Read-only vs. read-write.** `-C` (project) = read-write. `-R` (read) = read-only. Only the project directory can be modified.

4. **Pre-session snapshots are automatic.** Every `hazmat claude/exec/shell` snapshots the project before launching. Skip with `--no-backup`.

5. **Agent password.** Set during setup but rarely needed. `hazmat enroll` and `hazmat claude` use passwordless sudo. To reset: `sudo passwd agent`.

6. **Pre-flight checks.** `hazmat init` validates platform, hazmat-launch binary, UID/GID availability before making any system changes. If a check fails, nothing is modified.


---

## Remaining UX Improvements (Backlog)

| # | Issue | Severity |
|---|-------|----------|
| 1 | Surface createhomedir/pfctl errors as warnings instead of silencing | Medium |
| 2 | `hazmat version` command for support and upgrades | Medium |
| 3 | Per-workspace backup configuration | Low |
| 4 | Add action hints to ambiguous test warnings | Low |
