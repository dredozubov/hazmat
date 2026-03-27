# Hazmat UX Analysis

## User Journey Map

### Flow 1: First-Time Setup (Happy Path)

```
 USER                          HAZMAT                         SYSTEM
  │                              │                              │
  │  hazmat setup                │                              │
  │─────────────────────────────>│                              │
  │                              │  create agent user ──────────>│
  │  [sudo password]             │  create dev group ───────────>│
  │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >│  prepare ~/workspace ───────>│
  │                              │  harden gaps ────────────────>│
  │  [set agent password]        │  install seatbelt ───────────>│
  │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >│  install wrappers ───────── >│
  │                              │  verify hazmat-launch ───────>│
  │                              │  configure sudo ─────────────>│
  │                              │  configure pf firewall ──────>│
  │  [y/n DNS blocklist?]        │  configure DNS ──────────────>│
  │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >│  install LaunchDaemon ──────>│
  │                              │                              │
  │  "Setup complete"            │                              │
  │  "Next: hazmat bootstrap"    │                              │
  │<─────────────────────────────│                              │
  │                              │                              │
  │  hazmat bootstrap            │                              │
  │─────────────────────────────>│  install Claude Code ────────>│
  │  [sudo password]             │  write settings.json ────────>│
  │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ >│  create hooks skeleton ─────>│
  │                              │                              │
  │  "Next: set API key"         │                              │
  │<─────────────────────────────│                              │
  │                              │                              │
  │  ** MANUAL STEPS **          │                              │
  │  sudo -u agent -i            │                              │
  │  set ANTHROPIC_API_KEY       │                              │
  │  configure git credentials   │                              │
  │  exit                        │                              │
  │                              │                              │
  │  hazmat test                 │                              │
  │─────────────────────────────>│  verify 83 checks ──────────>│
  │  "All checks passed"         │                              │
  │<─────────────────────────────│                              │
  │                              │                              │
  │  hazmat claude               │                              │
  │─────────────────────────────>│  generate SBPL ─────────────>│
  │                              │  sudo -u agent ─────────────>│
  │                              │  sandbox-exec -f policy ────>│
  │  [Claude Code session]       │                              │
  │<═══════════════════════════=>│                              │
```

**Command count:** 4 commands + 3 manual steps inside agent shell
**Sudo prompts:** 5-8 (varies by step)
**Time:** ~15 minutes
**Failure points:** 6 (any step can fail; recovery = `hazmat rollback`)


### Flow 2: Daily Usage

```
  USER                          HAZMAT                     AGENT SANDBOX
   │                              │                              │
   │  cd ~/workspace/myproject    │                              │
   │  hazmat claude               │                              │
   │─────────────────────────────>│                              │
   │                              │  resolve project path        │
   │                              │  check Docker artifacts      │
   │                              │  generate per-session SBPL   │
   │                              │  write /tmp/hazmat-PID.sb    │
   │                              │  sudo -u agent hazmat-launch │
   │                              │─────────────────────────────>│
   │                              │                              │  validate policy
   │                              │                              │  sandbox-exec -f
   │                              │                              │  env -i (clean env)
   │                              │                              │  source agent-env
   │                              │                              │  cd project
   │                              │                              │  exec claude
   │  [interactive Claude session]│                              │
   │<═════════════════════════════╪══════════════════════════════>│
   │                              │                              │
   │  (exit / Ctrl-C)             │  rm /tmp/hazmat-PID.sb       │
   │                              │                              │
```

**Zero configuration.** Just `cd` and `hazmat claude`. This is good.


### Flow 3: Backup / Restore

```
  Local backup:                    Cloud backup:
  ┌──────────────────────┐         ┌──────────────────────┐
  │ hazmat backup /dest  │         │ hazmat setup --cloud  │  (one-time)
  │   rsync ~/workspace  │         │   S3 endpoint         │
  │   apply excludes     │         │   access key          │
  │   additive by default│         │   secret key          │
  │                      │         │   bucket name         │
  │ hazmat backup --sync │         │   encryption password │
  │   /dest              │         │                       │
  │   mirror (deletes!)  │         │ hazmat backup --cloud │
  │   needs .backup-     │         │   kopia snapshot      │
  │   target marker      │         │   incremental         │
  └──────────────────────┘         │   encrypted at rest   │
                                   │   deduped             │
  Local restore:                   │                       │
  ┌──────────────────────┐         │ hazmat restore --cloud│
  │ hazmat restore /src  │         │   latest snapshot     │
  │   rsync → ~/workspace│         │   full restore        │
  └──────────────────────┘         └──────────────────────┘
```


### Flow 4: Rollback / Uninstall

```
  hazmat rollback
  │
  ├── remove LaunchDaemon
  ├── remove pf anchor + restore pf.conf from backup
  ├── remove DNS blocklist from /etc/hosts
  ├── remove sudoers entry
  ├── remove seatbelt wrapper
  ├── remove command wrappers + shell blocks
  ├── remove workspace symlink + ACLs
  ├── remove umask blocks
  ├── remove backup scope file
  │
  ├── --delete-user → delete agent account + home dir
  └── --delete-group → delete dev group
      │
      └── ~/workspace left intact (user must back up first)
```

---

## UX Problems and Recommendations

### P0: The Manual Gap

**Problem:** After `hazmat bootstrap`, users must open an agent shell and manually run 3-5 commands (API key, git config, credential helper). This is the biggest drop-off point. New users will:
- Forget which commands to run
- Mistype the API key
- Not understand why SSH is blocked
- Not configure git credentials at all, then wonder why `git push` fails

**Current state:** DoneBox prints 5 numbered steps. User must copy/paste each.

**Recommendation:** Add `hazmat enroll` that automates the manual steps:

```
hazmat enroll
  API key: [paste or enter "login" for Claude.ai] ─────> writes to agent .zshrc
  Git name: [Your Name] ───────────────────────────────> git config --global
  Git email: [you@example.com] ────────────────────────> git config --global
  Git credential helper: [y] ──────────────────────────> git config --global
  ✓ Agent user configured. Run: hazmat test
```

This collapses 5 manual commands + context-switching into 1 interactive command.


### P1: No Guidance After Commands

**Problem:** A new user types `hazmat` and gets a command list. Nothing tells them what to do first. After `hazmat setup` completes, the DoneBox is helpful but scrolls off screen. There's no way to see "what should I do next?" after the fact.

**Recommendation:** Make `hazmat status` the orientation command. Currently it runs verifySetup (checks system state). Enhance it to also show a progress checklist:

```
$ hazmat status

  Hazmat — AI agent containment for macOS

  [✓] System setup          hazmat setup
  [✓] Claude Code installed  hazmat bootstrap
  [✗] Agent credentials      hazmat enroll       <── next step
  [ ] Verified               hazmat test

  Quick start:
    cd ~/workspace/my-project && hazmat claude
```

This gives the user a mental model of the flow at any time.


### P2: Silent Failures in Setup

**Problem:** Several setup steps silently swallow errors:
- `createhomedir` error ignored (line 430)
- `pfctl -e` error ignored (line 927)
- DNS cache flush errors ignored (line 959)

If these fail, `hazmat test` will catch them later, but the user thinks setup succeeded.

**Recommendation:** Don't silently ignore. Print a warning:

```diff
- r.Sudo("createhomedir", "-c", "-u", agentUser) //nolint:errcheck
+ if err := r.Sudo("createhomedir", "-c", "-u", agentUser); err != nil {
+     ui.WarnMsg(fmt.Sprintf("createhomedir returned error (may be harmless): %v", err))
+ }
```

If it's genuinely harmless (createhomedir exits 1 even on success), document why in a comment and still print the warning so the user knows something happened.


### P3: Password Handling

**Problem:** In interactive mode, setup runs `sudo passwd agent` — the user sets a password they may never use again (because `hazmat claude` uses passwordless sudo). In non-interactive mode (`--yes`), a random password is set but never shown. If the user later needs `sudo -u agent -i` (for manual config), they can't authenticate.

**Recommendation:**
1. After `sudo passwd agent`, print: `Agent password set. You'll need this for: sudo -u agent -i`
2. In `--yes` mode, print: `Agent password set to random value. To reset: sudo passwd agent`
3. Long-term: `hazmat enroll` eliminates the need for `sudo -u agent -i` entirely, making the password irrelevant for most users.


### P4: First-Run Help Text

**Problem:** `hazmat --help` shows a flat list of 10 commands with no structure. A new user can't tell that `setup` must come before `bootstrap` which must come before `claude`.

**Recommendation:** Group commands in help output and add a header:

```
Hazmat — AI agent containment for macOS

  Get started:
    setup       Configure macOS containment (run first)
    bootstrap   Install Claude Code for the agent user
    enroll      Set API key and git credentials for the agent user
    test        Verify everything works

  Daily use:
    claude      Launch Claude Code in containment
    shell       Open a contained shell
    exec        Run a single command in containment

  Maintenance:
    backup      Back up the workspace (local rsync or cloud)
    restore     Restore the workspace from backup
    status      Quick health check
    rollback    Undo all setup changes
```


### P5: Pre-Flight Checks

**Problem:** `hazmat setup` makes 13 system modifications. If step 8 fails (e.g., hazmat-launch not installed), steps 1-7 already ran. User must rollback.

**Recommendation:** Add a pre-flight check at the start of setup that validates prerequisites without making changes:

```
hazmat setup
  Pre-flight checks:
  ✓ macOS detected
  ✓ Running as normal user (not root)
  ✓ UID 599 available
  ✓ GID 599 available
  ✓ hazmat-launch installed at /usr/local/libexec/hazmat-launch
  ✓ /etc/pf.conf writable
  ✓ stdin is a terminal

  Ready to configure. Proceed? [y/n]
```

If any pre-flight check fails, stop before making any changes.


### P6: The Workspace Assumption

**Problem:** `~/workspace` is hardcoded everywhere. Users with `~/code`, `~/projects`, or `~/src` must either:
- Symlink their directory to `~/workspace`
- Move their code
- Live with the assumption

Backup/restore only operates on `~/workspace`. No way to override.

**Recommendation (minimal change):** Support `HAZMAT_WORKSPACE` environment variable:

```go
var sharedWorkspace = getWorkspace()

func getWorkspace() string {
    if ws := os.Getenv("HAZMAT_WORKSPACE"); ws != "" {
        return ws
    }
    return filepath.Join(os.Getenv("HOME"), "workspace")
}
```

This is backward-compatible (default is `~/workspace`) but lets power users override. Document it in `hazmat setup --help`.


---

## What Should Be Configurable

| Setting | Current | Recommendation |
|---------|---------|----------------|
| Workspace path | Hardcoded `~/workspace` | `HAZMAT_WORKSPACE` env var |
| Agent UID/GID | Flag at setup time only | Store in config file for re-runs |
| Backup excludes | `~/workspace/.backup-excludes` | Already configurable (good) |
| Cloud backup creds | `~/.config/hazmat/cloud-backup.json` | Already configurable (good) |
| Blocked ports | Hardcoded in setup.go | Fine — opinionated is correct here |
| Blocked domains | Hardcoded in setup.go | Fine — opinionated is correct here |
| Seatbelt policy | Generated per-session | Fine — dynamic is correct |
| Claude deny rules | `~/.claude/settings.json` | Already configurable (good) |
| Agent PATH | Hardcoded in main.go | Could be configurable but low priority |

**Principle:** Hazmat should be opinionated about security (ports, domains, deny rules) but flexible about environment (workspace path, UIDs, paths).

---

## What's Non-Obvious

### 1. Why SSH is blocked
The seatbelt policy denies `~/.ssh` access. This is intentional (prevents credential exfiltration) but surprises users who expect `git push` over SSH. The DoneBox mentions "SSH is blocked by the seatbelt" but doesn't explain *why*.

**Fix:** Add a one-liner: "Git over SSH is blocked to prevent credential access. Use HTTPS with a personal access token instead."

### 2. The `--allow-docker` escape hatch
If Docker files are detected, `hazmat claude` refuses to run. The error says to use Tier 3 (docker). The `--allow-docker` flag is hidden in the error message. Users who just have a Dockerfile in their repo (but don't need Docker) are blocked.

**Fix:** Lead with the escape hatch: "Docker artifacts detected. Use `--allow-docker` to proceed without Docker support, or use Tier 3 for full Docker."

### 3. Read-only workspace vs. read-write project
The `-W` (workspace) flag gives **read-only** access. The `-C` (project) flag gives **read-write** access. These are different! A user who passes `-W ~/workspace` expecting full write access will get errors.

**Fix:** Clarify in help: "The project directory (`-C`) is read-write. The workspace (`-W`) and references (`-R`) are read-only. Only the project directory can be modified."

### 4. `.backup-target` sentinel file
Local backup with `--sync` requires a `.backup-target` file in the destination. This prevents accidental deletions but confuses first-time users.

**Fix:** The error message already explains this well. No change needed.

### 5. The agent user's password
Users set it during setup but may never need it again. If they later need `sudo -u agent -i`, they may not remember it.

**Fix:** `hazmat enroll` (recommended above) eliminates this need.

### 6. What `hazmat test` failures mean
Test prints 83 checks with pass/fail/warn/skip. Failures like "agent user does not exist" are clear. But warnings like "agent can read ~/.zshrc" are unclear — is this a security issue? What should the user do?

**Fix:** Add a brief "(action: ...)" suffix to warnings that require user action.

---

## Recommended Priority

### Do First (highest user impact, smallest code change)
1. **Pre-flight checks** before setup — validate UID, GID, hazmat-launch, platform before touching anything
2. **Group the help output** — setup/bootstrap/enroll vs. claude/shell/exec vs. backup/restore/status/rollback
3. **Fix password UX** — print what to do after `sudo passwd agent` completes
4. **`HAZMAT_WORKSPACE` env var** — one-line change, big flexibility win
5. **Invert Docker warning** — lead with `--allow-docker`, not Tier 3

### Do Next (bigger changes, big UX wins)
6. **`hazmat enroll`** — eliminates the manual gap (P0 above)
7. **Enhanced `hazmat status`** — progress checklist showing what's done and what's next
8. **Surface warnings in setup** — don't silently swallow createhomedir / pfctl errors

### Do Later (nice to have)
9. **`hazmat version`** — needed for support and upgrades
10. **Per-workspace backup config** — matters when users have multiple projects
