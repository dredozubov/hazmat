# Using Hazmat

Hazmat runs AI agents on your Mac with full permissions — inside containment. Every session prints a contract telling you exactly what the agent can do, which mode was selected, and why.

> **Picking which agent to install?** [docs/harnesses.md](harnesses.md) is the per-harness setup matrix — tested versions, three auth paths (subscription / API key / host import), and verification commands for claude, codex, opencode, and gemini.
>
> **Verifying a fresh install or a release candidate?** [docs/manual-testing.md](manual-testing.md) is the human-driven checklist with preconditions, per-harness flows, regression scenarios, and recovery moves.

## Quick Start

Install:

```bash
# Homebrew
brew install dredozubov/tap/hazmat

# Or GitHub releases (downloads, verifies checksum, installs)
curl -fsSL https://raw.githubusercontent.com/dredozubov/hazmat/master/scripts/install.sh | bash
```

The release installer targets the current host by default and currently
publishes only `darwin/arm64` and `darwin/amd64` artifacts. Linux is
compile-only until its setup and rollback resources are modeled and implemented.

Then two commands:

```bash
hazmat init --bootstrap-agent claude   # one-time setup (~10 min, needs sudo)
hazmat claude     # launch Claude Code in containment
```

That's it. `init` creates a contained environment and lets you choose whether to bootstrap Claude Code, Codex, OpenCode, or skip agent installation for now. When you bootstrap Claude during init, Hazmat can also ask for your API key, git credentials, and optionally import portable Claude basics from your existing setup.

```mermaid
flowchart LR
    subgraph once ["One-time setup (hazmat init)"]
        direction TB
        I1[Create agent user] --> I2[Set up workspace ACLs]
        I2 --> I3[Init snapshot repo]
        I3 --> I4[Install firewall + DNS blocklist]
        I4 --> I5["Optional: bootstrap Claude, Codex, or OpenCode"]
        I5 --> I6["Optional: configure Claude API key + git creds"]
    end
    subgraph daily ["Every session (hazmat claude)"]
        direction TB
        D1[Snapshot project] --> D2[Generate seatbelt policy]
        D2 --> D3[Resolve integrations and path extensions]
        D3 --> D4[Print session contract]
        D4 --> D5[Launch agent in containment]
    end
    once --> daily

    style once fill:#f5f5ff,stroke:#33a,color:#000
    style daily fill:#f5fff5,stroke:#3a3,color:#000
```

## What `hazmat init` Does

When you run `hazmat init`, it:

1. Creates a hidden `agent` macOS user (separate from yours)
2. Adds the host-side access needed for contained sessions to reach the selected project directories
3. Initializes the local Kopia repository for automatic pre-session snapshots
4. Installs a firewall that blocks the agent from SMTP, IRC, FTP, Tor, and other exfiltration protocols
5. Adds a DNS blocklist for tunnel and paste services (ngrok, pastebin, etc.)
6. Optionally bootstraps a supported AI coding agent for the agent user
7. If you choose Claude, offers to configure your Anthropic API key and git credentials
8. If you choose Claude, can optionally import portable Claude basics such as sign-in state, commands, and skills

Everything is interactive — it explains each step and asks for confirmation. To preview without making changes:

```bash
hazmat init --dry-run
```

## The Session Contract

Every session starts with a plain-language summary of what the agent can and can't do:

```
hazmat: session
  Mode:                 Native containment
  Why this mode:        using native containment by default (Docker routing: none)
  Project (read-write): /Users/dr/workspace/my-app
  Integrations:         go
  Host changes:          project ACL repair
  Auto read-only:       /Users/dr/go/pkg/mod
  Read-only extensions: /Users/dr/reference-docs
  Read-write extensions: /Users/dr/.venvs/my-app
  Service access:       none
  Credential env grants: none
  Pre-session snapshot: on
  Snapshot excludes:    vendor/
```

Each line maps to a concrete boundary:

- **Mode** — Native containment (kernel sandbox + user isolation) or Docker Sandbox (private Docker daemon in an isolated runtime)
- **Why this mode** — what triggered the mode selection (`--docker=sandbox`, `--docker=auto`, project config, or the default native mode)
- **Project (read-write)** — the only directory the agent can modify
- **Integrations** — active stack integrations and what they add automatically
- **Host changes** — persistent host-side mutations Hazmat may apply before launch, such as project ACL repair, agent Git safe-directory trust, or a bounded toolchain permission fix. Permission-repair classes are modeled in TLA+; non-permission host changes are governed by tests and documentation.
- **Auto read-only** — read-only directories that Hazmat resolved on your behalf
- **Read-only extensions** — explicit additional read-only directories from `-R` or config
- **Read-write extensions** — explicit additional writable directories from `-W` or config
- **Service access** — external services the agent can authenticate to
- **Credential env grants** — redacted environment-delivered credentials granted explicitly for this session
- **Pre-session snapshot** — whether a rollback point was created
- **Snapshot excludes** — patterns skipped by the snapshot (often from integrations)

Preview any session without running it:

```bash
hazmat explain                      # preview current project
hazmat explain --json               # machine-readable preview for automation
hazmat explain --docker=sandbox     # preview Docker Sandbox mode
hazmat explain --docker=auto        # preview marker-based Docker routing
hazmat explain --integration node   # preview with an integration
hazmat explain --github             # preview explicit GitHub API access
```

`hazmat explain` previews these changes but does not apply them. A real session
may execute the listed host mutations before launch if they are still needed at
that point. The verified TLA+ model covers the permission-repair subset of that
preview-vs-launch split and the current non-reverting rollback contract for
those repairs; non-permission host changes are covered by tests and docs.

`hazmat explain --json` emits the same prepared session state in a stable
machine-readable form, including suggested integrations, active integrations,
resolved integration sources and details, planned host changes,
read-only access, snapshot excludes, and routing notes.

## Daily Usage

```bash
cd ~/workspace/my-project
hazmat claude
hazmat opencode
```

This generates a per-session security policy, switches to the agent user, and launches the agent inside containment. When you exit, the session is cleaned up.

### Giving the Agent Access to Other Directories

By default, the agent can only write to the project directory (your current
directory). To let it read or write other directories explicitly:

```bash
hazmat claude -R ~/workspace              # read all of ~/workspace
hazmat claude -R ~/code/lib -R ~/docs     # cherry-pick specific dirs
hazmat claude -W ~/.venvs/my-app          # add another writable root
hazmat config access add -C ~/workspace/my-project --read ~/docs --write ~/.venvs/my-app
```

Read directories are strictly read-only. Write directories are explicit
extensions to the writable contract and show up separately in the session
summary.

### Session Integrations

Integrations let you carry stack-specific ergonomics into a session without
weakening Hazmat's trust boundaries:

```bash
hazmat integration list
hazmat integration show node
hazmat claude --integration node
hazmat claude --integration python-uv
hazmat config set integrations.pin "~/workspace/my-project:node,go"
```

Today integrations can:

- add auto-resolved read-only toolchain or cache directories
- add snapshot excludes for reproducible build artifacts
- pass through a small safe set of environment selectors such as `GOPATH` or `VIRTUAL_ENV`

They do not widen write access, expose blocked credentials, or change firewall
policy. Explicit extra writable scope is handled separately through `-W` or
`hazmat config access`, not through integrations.

Built-in integrations may also plan narrowly-scoped host permission repairs for
known local toolchains when the current host permissions would otherwise block
the agent user. These changes are shown under `Host changes` before launch,
are never applied by `hazmat explain`, and the permission-repair subset shares
the same TLA+ state-machine coverage as the other modeled session mutation
classes.

Repos can still ship a `.hazmat/integrations.yaml` listing recommended integrations.
On first use, hazmat prompts once for approval; after that, the approved
integrations activate automatically until the file changes. Write your own
integration manifest in `~/.hazmat/integrations/` for environments that
built-ins do not cover. Full reference: [integrations.md](integrations.md).

For mixed-stack repos, prefer declaring the full set explicitly. Example:

```yaml
integrations:
  - python-uv
  - node
  - tla-java
```

### Repo-local Git Hooks

Hazmat also supports a narrow repo-local Git hook flow for the common cases:
`pre-commit`, `commit-msg`, and `pre-push`.

The repo declares intent in tracked files under `.hazmat/hooks/`. The host owns
activation. Hazmat only runs hook code after approval, and it runs the approved
snapshot bytes from host-owned storage rather than the live repo copy. Hook
bundles can also include tracked auxiliary files under `.hazmat/hooks/` such as
scanner config; those files are hashed, approved, and snapshotted alongside the
declared hook entrypoints.

```bash
hazmat hooks status
hazmat hooks review
hazmat hooks install
hazmat hooks install --replace   # only when another local core.hooksPath owner exists
hazmat hooks uninstall
```

On the next session launch, Hazmat can also surface the same approval/install
flow automatically. The prompt is manifest-driven: hook type, purpose,
interpreter, and required binaries, plus a calm drift summary when the repo
bundle changes.

V1 scope is intentionally narrow:

- repo-local hooks only
- `pre-commit`, `commit-msg`, `pre-push` only
- explicit install / uninstall through Hazmat
- refusal when another local `core.hooksPath` owner already exists unless you
  pass `hazmat hooks install --replace`

V1 does **not** support global hooks, `init.templateDir`, package-manager
auto-install, `post-*` hooks, or server-side hooks.

If that flow feels stricter than normal Git hooks, see
[git-hooks.md](git-hooks.md) for the threat model, attack vectors, and the
specific risks Hazmat is trying to close.

### Docker Projects

Hazmat treats Docker routing as an explicit daemon-boundary choice, not just
"does this repo have Docker files?"

- By default, sessions use native containment with Docker disabled, even when
  Docker files are present.
- Use `--docker=sandbox` to force Docker Sandbox mode for a private-daemon
  workflow.
- Use `--docker=auto` or `hazmat config docker auto` when you want Hazmat to
  inspect Docker markers and route private-daemon fits automatically.
- If auto mode sees **shared host daemon** signals (for example external Docker
  networks or Traefik Docker labels), Hazmat stops and asks you to use native
  code-only mode or move the workflow to Tier 4.

```bash
hazmat claude                       # native code-only session
hazmat claude --docker=sandbox      # force Docker Sandbox mode
hazmat claude --docker=auto         # marker-based Docker routing
hazmat config docker auto -C ~/workspace/my-project
```

Docker Sandbox sessions are available through every harness entrypoint:
`hazmat claude`, `hazmat codex`, `hazmat opencode`, `hazmat gemini`,
`hazmat shell`, and `hazmat exec`. `--docker=auto` keeps the default native
path for code-only repos and routes Docker-heavy private-daemon fits into the
matching harness automatically.

If `.devcontainer/` is the only Docker-related directory, Hazmat stays in
native containment unless the devcontainer.json positively indicates Docker
is needed (e.g., it contains `image`, `dockerFile`, or `dockerComposeFile`).

Native code-only mode is the default for editing against externally managed
local services. Docker commands still fail inside the session. If the agent must
restart containers, inspect logs, run `docker exec`, or debug the live Docker
topology, Tier 4 is the right fit.

For setup details, network policy, and Compose hardening guidance, see
[tier3-docker-sandboxes.md](tier3-docker-sandboxes.md). For shared-daemon
projects and the code-only fallback, see
[shared-daemon-projects.md](shared-daemon-projects.md).

### Specifying a Different Project Directory

```bash
hazmat claude -C ~/workspace/other-project
```

### Running Commands With Flags

`hazmat exec` forwards the command after Hazmat parses its own flags. When the
forwarded command has flags of its own, insert `--` before it:

```bash
hazmat exec -- make test
hazmat exec -- /bin/zsh -lc 'uv run pytest -q'
hazmat exec --docker=none -C ~/workspace/app -- /bin/zsh -lc 'cd frontend && npm run build'
```

### Resuming a Conversation Inside Native Containment

When you start a conversation as yourself (`claude`) and later want to continue it inside **native containment**, `--resume` and `--continue` work seamlessly:

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

**Current limitation:** Docker Sandbox mode uses sandbox-local Claude history.
Host transcript sync is not applied there yet.

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
hazmat opencode -C ~/workspace/proj
```

## Checking Status

```bash
hazmat                          # shows setup progress checklist
hazmat status                   # same thing
hazmat check               # run full verification suite
hazmat check --full        # include live network probes
```

`hazmat check` validates the current local Hazmat install and containment
behavior. It is not the full repo test suite. For lifecycle e2e, self-hosting,
repo-matrix, VM-backed verification, and CI mapping, see [testing.md](testing.md).

## Backup and Restore

### Local project snapshots

Hazmat automatically snapshots the current project directory before every
session:

```bash
hazmat snapshots
hazmat diff
hazmat restore
hazmat restore --session=2
```

These snapshots cover only the selected project directory, not the entire
workspace and not the extra read-only directories you pass via `-R`.

Default excludes live in `hazmat config`, and integrations can add stack-specific
snapshot excludes such as `node_modules/` or `target/` for the active session.

### Cloud backup (encrypted, incremental)

```bash
hazmat init cloud              # one-time: configure S3 credentials
hazmat backup --cloud          # incremental encrypted snapshot
hazmat restore --cloud         # restore latest snapshot
```

Cloud backup keeps endpoint and bucket in `~/.hazmat/config.yaml`.
Credential material is host-owned under `~/.hazmat/secrets/cloud/`; legacy
`backup.cloud.access_key`, `backup.cloud.recovery_key`/`password`, and
`~/.hazmat/cloud-credentials` entries migrate there automatically.

## Updating Credentials

```bash
hazmat config agent            # re-enter native-session API keys, git name/email
hazmat config github           # store a GitHub API token for explicit --github sessions
GH_TOKEN=... hazmat config github --token-from-env
```

`hazmat config agent` stores native-session provider API keys under
`~/.hazmat/secrets/providers/*` and injects them only into the matching native
harness. It does not persist API-key exports in `/Users/agent/.zshrc`.

`hazmat config github` stores the token under
`~/.hazmat/secrets/github/token`. Launch commands only expose it when you pass
`--github`, where it appears in the session as `GH_TOKEN` and as a redacted
`github.api-token` grant in the contract and `hazmat explain --json`. Repo
recommendations and integrations cannot activate it, and ambient
`GH_TOKEN`/`GITHUB_TOKEN` passthrough remains rejected.

Git HTTPS credentials are brokered per native session. Legacy agent-side
`/Users/agent/.config/git/credentials` entries migrate into
`~/.hazmat/secrets/git-https/credentials` on session launch, and `hazmat check`
reports any old helper or credential-store residue without printing tokens.

For the full registry inventory, run `hazmat check` and inspect the credential
section. It lists each surface by redacted registry ID, storage backend,
delivery mode, and legacy residue status.

## Managed Git SSH

A single-key project config (explicit host required):

```bash
hazmat config ssh list-keys
hazmat config ssh add -C ~/workspace/my-project \
    --name default --host github.com ~/.ssh/id_ed25519
hazmat config ssh test -C ~/workspace/my-project --host github.com
hazmat config ssh unset -C ~/workspace/my-project
```

Multi-key per project — one key for GitHub, a separate key for a remote
server:

```bash
hazmat config ssh add -C ~/workspace/my-project \
    --name github --host github.com ~/.ssh/id_ed25519
hazmat config ssh add -C ~/workspace/my-project \
    --name prod --host prod.example.com --host '*.prod.example.com' ~/.ssh/prod_key
hazmat config ssh test -C ~/workspace/my-project --host prod.example.com
hazmat config ssh remove -C ~/workspace/my-project --name prod
```

Every destination host resolves to exactly one configured key. Two keys
whose host lists overlap are rejected at config-save time. Every inline
key must declare at least one `--host` — the legacy any-host fallback
has been retired. Profile-referencing keys still inherit `default_hosts`
from the profile when they declare no hosts of their own.

This is an explicit per-project capability for Git transport only. Hazmat
resolves every key to a typed credential reference before launch. External
keys and profile keys remain explicit host-file references; provisioned
inventory keys live under `~/.hazmat/secrets/git-ssh/provisioned/<name>/`.
Hazmat routes Git through a session-scoped transport broker. The broker runs
outside the contained session, selects the matching key by destination host,
preserves supported OpenSSH routing such as `ProxyJump`, and performs only Git
transport commands. The session receives `GIT_SSH_COMMAND`, not a readable
private key or reusable `ssh-agent` socket.

### Reusable SSH profiles

Define one SSH identity that many projects share. Each profile carries
the private key, an optional known_hosts override, and an optional
`default_hosts` list that referring projects inherit unless they override:

```bash
hazmat config ssh profile add github ~/.ssh/keys/github/id_ed25519 \
    --default-host github.com --description "personal github"
hazmat config ssh profile add prod ~/.ssh/keys/prod/id_ed25519 \
    --default-host prod.example.com --default-host '*.prod.example.com'

# Attach a profile to a project. Inherits default_hosts:
hazmat config ssh add -C ~/workspace/my-project --name work --profile github

# Or override default_hosts for this project only:
hazmat config ssh add -C ~/workspace/my-project \
    --name enterprise --profile github --host enterprise.internal

hazmat config ssh profile list
hazmat config ssh profile show github
hazmat config ssh profile rename github personal-github
hazmat config ssh profile remove personal-github --force   # detaches referrers
```

A profile reference that points to an undefined profile is rejected at
config load, not at session launch. Two project keys may safely reference
the same profile; the broker still routes each destination host to exactly one
configured project key.

For guidance on how to choose and scope keys for GitHub, remote servers,
deploy keys, machine users, SSH certificates, and per-target `known_hosts`
layouts, see [ssh-key-hygiene.md](ssh-key-hygiene.md).

`hazmat config ssh test` is a host-side validation helper. It uses the selected
Hazmat key and `known_hosts`, but it also honors the host user's real OpenSSH
config for routing, so aliases and jump-host flows from `~/.ssh/config`
continue to work during the test. This includes common directives such as
`Host`, `HostName`, `User`, `Port`, `Include`, and `ProxyJump`.

Session-time alias-based Git remotes are still a separate limitation. General
SSH shells remain unsupported.

## Importing Portable Claude Basics

```bash
hazmat config import claude
hazmat config import claude --dry-run
hazmat config import claude --overwrite
hazmat config import claude --skip-existing
```

Hazmat treats this as a curated import, not a full Claude migration. It stores Claude sign-in state in `~/.hazmat/secrets/claude/`, imports git identity plus commands and skills, and only materializes file-backed auth into `/Users/agent` while a Claude session is active. Hazmat keeps its own runtime settings, hooks, MCP configuration, plugins, and safety controls.

Detailed scope, symlink behavior, conflict handling, and MCP migration guidance live in [claude-import.md](claude-import.md).

## Running OpenCode

```bash
hazmat bootstrap opencode
hazmat opencode
hazmat opencode -p "summarize this repo"
```

This is a prototype harness path alongside Claude. It uses the same containment, project preflight, and snapshot flow, but OpenCode keeps its own config and session state.

## Running Codex

```bash
hazmat bootstrap codex
hazmat codex
hazmat codex -p "review the recent changes"
```

Codex uses the same containment and project preflight model. Runtime state
lives under the agent user's home directory, while durable imported auth and
API keys live in Hazmat's host-owned secret store and are materialized only
for active Codex sessions.

## Importing Portable OpenCode Basics

```bash
hazmat config import opencode
hazmat config import opencode --dry-run
hazmat config import opencode --overwrite
hazmat config import opencode --skip-existing
```

Hazmat treats this as a curated import, not a full OpenCode migration. It stores OpenCode sign-in state in `~/.hazmat/secrets/opencode/auth.json`, imports git identity plus commands, agents, and skills, and only materializes file-backed auth into `/Users/agent` while an OpenCode session is active. Hazmat keeps its own runtime settings, plugins, project-local `.opencode` directories, and safety controls separate.

Detailed scope, symlink behavior, and migration guidance live in [opencode-import.md](opencode-import.md).

## Uninstalling

```bash
hazmat rollback                              # remove all system config
hazmat rollback --delete-user --delete-group  # also delete agent account

# Remove binaries (choose one):
brew uninstall hazmat                        # if installed via Homebrew
sudo rm /usr/local/bin/hazmat /usr/local/libexec/hazmat-launch  # if installed via script
```

Your project files are not deleted. Back them up first if needed. Rollback does
remove Hazmat-managed repo-local Git hook state: host approval records,
approved snapshots, per-repo wrappers, and managed `.git` dispatchers.

## What the Agent Can and Can't Do

**Can:**
- Read and write files in your project directory
- Read directories you expose with `-R`
- Make HTTPS requests to any host
- Run any command available to the agent user
- Access `/private/tmp` for temporary files
- Build and run Docker containers (Docker Sandbox mode only)

**Can't:**
- Read your SSH keys, AWS credentials, GPG keys, or Keychain
- Send email (SMTP blocked), use IRC, FTP, Tor, or VPN protocols
- Access the host Docker daemon (socket locked to your user only)
- Read files outside the approved directories
- Use `sudo`

For the current import policy and non-goals, see [claude-import.md](claude-import.md) and [opencode-import.md](opencode-import.md).
