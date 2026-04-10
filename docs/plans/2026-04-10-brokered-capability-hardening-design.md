# Brokered Capability Hardening for External Audit

Status: Proposed
Date: 2026-04-10
Owner: Hazmat
Primary issue: `sandboxing-cl2p`

## Purpose

This document turns the 2026-04-10 sandbox-escape audit into a concrete
hardening plan for **native Hazmat sessions**.

The intended audience is an external security reviewer, auditor, or red-team
partner who needs more than a list of findings. The goal is to describe:

- the current runtime shape that led to the findings
- which findings are product defaults versus operator choices
- the concrete features Hazmat should build next
- how those features reduce risk without adding day-to-day UX friction
- what migration, verification, and residual risk remain

This is a planning document. It does not change runtime behavior by itself.

## Scope Notes

This plan is intentionally focused on findings caused by **ambient authority
inside the contained session**:

- generic SSH signing capability exposed to the session
- long-lived harness or Git credentials stored in readable agent-home files
- broad `agent` home visibility as the main compatibility mechanism
- unrestricted HTTPS egress combined with readable valuable session state

It is **not** a proposal to remove explicit host-owned read extensions such as
`session.read_dirs`. In particular, a workspace-wide read grant such as
`~/workspace` is an operator configuration choice, not a hardcoded product
default. Hazmat should keep that explicit flexibility and describe its blast
radius honestly in the session contract and audit docs.

## Current State

Hazmat's native mode currently combines four main layers:

1. host/agent user separation
2. per-session SBPL generation
3. per-user pf blocklist
4. host-owned launch orchestration

That stack is useful, but the current runtime still places meaningful authority
directly inside the contained session:

- the generated seatbelt policy currently grants broad read/write access to the
  whole `agent` home, then denies selected credential subpaths afterward
- managed Git-over-SSH currently creates a session-local `ssh-agent` socket and
  a generated `git-ssh` wrapper under an agent-writable runtime directory
- Claude and OpenCode import flows currently copy sign-in state into
  `/Users/agent/.claude/.credentials.json`,
  `/Users/agent/.claude.json`, and
  `/Users/agent/.local/share/opencode/auth.json`
- `hazmat config agent` currently stores `ANTHROPIC_API_KEY` in
  `/Users/agent/.zshrc` and Git HTTPS credentials in
  `/Users/agent/.config/git/credentials`
- the current seatbelt checks and docs explicitly treat readable `~/.claude`
  state as part of the success path for contained Claude sessions

Relevant code and doc points an auditor should inspect:

- `hazmat/session.go`
  - `resolvePreparedSession`
  - `generateSBPL`
  - `agentEnvPairs`
- `hazmat/git_ssh.go`
  - `prepareGitSSHRuntime`
  - `prepareSSHIdentityRuntime`
  - `buildGitSSHWrapperScript`
- `hazmat/config_agent.go`
- `hazmat/config_import.go`
- `hazmat/config_import_opencode.go`
- `hazmat/test.go`
- `docs/design-assumptions.md`
- `docs/brief-supply-chain-hardening.md`

## Findings Mapped to Root Causes

| Audit finding | Current root cause | Planned response |
| --- | --- | --- |
| Generic SSH use can bypass Git wrapper | The session receives a reusable `ssh-agent` socket and the wrapper is advisory | Replace socket exposure with a brokered Git transport capability |
| Git SSH wrapper is writable | Enforcement lives in an agent-writable generated script | Move enforcement into immutable helper + host-side broker |
| Claude/OpenCode tokens are readable and exfiltrable | Long-lived auth files are copied into agent home | Move secrets to host-owned storage and use harness-specific delivery adapters |
| Git HTTPS and API key material remain readable inside session | Credentials are stored in `.zshrc` and Git credential store under `/Users/agent` | Replace with host-owned secret store plus brokered or ephemeral delivery |
| HTTPS egress can exfiltrate anything readable in session | pf uses soft allow-by-default egress and the session still contains valuable files | Keep soft egress model, but reduce readable valuables and add first-party egress telemetry |

## Design Position

Hazmat should move from an **ambient agent-home authority model** to a
**brokered capability model**.

The key product claim should become:

- the contained session can still do useful work autonomously
- the contained session no longer receives long-lived credentials or generic
  signing oracles as ordinary readable files or sockets
- when Hazmat delegates external authority, that delegation is explicit,
  host-owned, narrow, auditable, and fail-closed

This keeps the UX goal intact:

- `hazmat claude`, `hazmat codex`, and `hazmat opencode` stay one-command
  launches
- managed Git-over-SSH continues to work for normal `git clone/pull/push`
- normal package managers, browsing, and docs lookup continue to work
- the operator should not see new per-command prompts in the happy path

## Design Principles

1. No new routine prompts for common workflows.
2. Repo-controlled files must remain unable to request credential capabilities.
3. Long-lived secrets should live only in host-owned storage.
4. The session should receive the narrowest usable capability, not the raw
   underlying credential material.
5. The session contract should describe capability lanes explicitly.
6. Migration must preserve existing user data where safe and fail closed where
   not.
7. The design should degrade safely even before every harness has a perfect
   adapter.

## Feature 1: Brokered Git SSH Transport

### Problem

Current managed Git SSH is better than forwarding the host user's main
`SSH_AUTH_SOCK`, but it still gives the contained session a generic signing
oracle:

- a session-local `ssh-agent` socket is created under a runtime directory
- Git is pointed at a generated wrapper that uses `IdentityAgent=<socket>`
- the session can bypass the wrapper by invoking `/usr/bin/ssh` directly
- the wrapper itself is generated into an agent-writable path

This means the policy boundary is in the wrong place. The final authority check
is inside a mutable session artifact rather than in a host-owned control point.

### Proposed Model

Replace the current wrapper-plus-socket runtime with a **brokered Git SSH
transport capability**.

The new split:

- `hazmat-git-ssh`: immutable helper binary installed with Hazmat
- `hazmat-capd`: per-session host-side capability broker process
- host-owned project config: key path, `known_hosts`, allowed hosts, optional
  future routing metadata

At launch time:

1. Hazmat resolves the project's managed Git SSH config as it does today.
2. Hazmat starts a host-side broker for the session before entering
   containment.
3. The broker receives the selected private key path, `known_hosts`, allowed
   hosts, and a one-session capability nonce.
4. The session receives only:
   - `GIT_SSH_COMMAND=/usr/local/libexec/hazmat-git-ssh`
   - broker socket path or fd
   - session nonce
5. Git invokes `hazmat-git-ssh`, which forwards the SSH-style argv plus
   stdio streams to the broker.
6. The broker validates the request and performs the actual SSH connection on
   the host side.

The contained session never sees:

- a generic `ssh-agent` socket
- raw private key material
- a writable shell wrapper that defines the policy

### Broker Enforcement Rules

V1 broker enforcement should validate:

- destination host
- destination port
- allowed Git transport verbs only
  - `git-upload-pack`
  - `git-receive-pack`
  - `git-upload-archive`
- `BatchMode=yes`
- `StrictHostKeyChecking=yes`
- pinned `UserKnownHostsFile`
- no agent forwarding
- no PTY
- no arbitrary `-o` escape hatches from the session side

V1 does **not** need to solve every SSH routing feature on day one.
Specifically:

- arbitrary remote shell access remains unsupported
- host `~/.ssh/config` alias semantics can remain a separate follow-up slice
- repo-path allowlisting is desirable but not required for the first cut

### Why This Improves the Security Story

The session still has a capability, but it is no longer a generic OpenSSH
credential oracle. It has one narrow delegated action:

- "perform a Git transport connection using the configured project key"

That is a materially smaller attack surface than:

- "access a socket that will sign arbitrary SSH challenges for any caller"

### UX Impact

Expected daily UX should remain unchanged:

- `git fetch`, `git pull`, `git push`, and `git clone` continue to work
- no new prompts on the happy path
- existing project-level SSH config continues to be the operator's entry point

Session contract language should change from a note about a session-local
`ssh-agent` to a note about a brokered Git transport capability.

### Migration

1. Ship immutable helper binary.
2. Add broker implementation behind the existing managed Git SSH feature flag.
3. Keep existing config schema for selected key and `known_hosts`.
4. Remove session-local `ssh-agent` and generated wrapper from native mode.
5. Update docs and tests to assert the absence of raw session SSH sockets.

### External Audit Questions

- Is host/port/verb enforcement sufficient for v1, or should repo-path
  allowlisting be mandatory in the first cut?
- Should broker-side alias resolution reuse current host-side `ssh test`
  logic, or remain out of scope for the initial migration?

## Feature 2: Host-Owned Secret Store and Auth Adapters

### Problem

Hazmat currently keeps multiple long-lived credentials inside the readable
`agent` home:

- Claude credential file and auth state
- OpenCode auth file
- API keys exported from `.zshrc`
- Git HTTPS credentials in Git's built-in store
- future harness-specific state under `~/.codex` or similar trees

This is the central reason unrestricted HTTPS remains dangerous in native mode:
the session can already read high-value secrets before it ever attempts network
exfiltration.

### Proposed Model

Introduce a **host-owned Hazmat secret store** and **adapter-specific session
delivery**.

Recommended host-owned storage root:

- `~/.hazmat/secrets/`

Properties:

- owned by the invoking host user
- not writable by the `agent` user
- outside the session filesystem contract
- structured by capability and harness, not by random copied dotfiles

Example layout:

```text
~/.hazmat/secrets/
  claude/
    credentials.json
    state.json
  opencode/
    auth.json
  git/
    credential-store.json
  providers/
    anthropic-api-key
```

This does not need Keychain integration to be valuable. A host-owned file store
already removes the secrets from the session's direct read surface. Native
Keychain support can remain an optional future enhancement.

### Delivery Model

The store alone is not enough. Hazmat must also stop handing long-lived secrets
straight back to the session.

The delivery rule should be:

- use a broker or proxy when the harness supports one
- otherwise materialize only the minimum short-lived session artifact required
  by that harness
- never copy long-lived refresh tokens or master credential files into the
  persistent `agent` home

This leads to two concrete subfeatures.

### 2A. Git HTTPS Credential Brokering

Replace the current Git `credential.helper store --file ...` model with an
immutable `hazmat-git-credential` helper plus host-owned credential storage.

Properties:

- Git inside the session asks the helper for credentials
- helper talks to a host-side credential broker
- broker returns credentials only for approved hosts/protocols
- no plaintext Git credential file remains in `/Users/agent/.config/git`

This is the HTTP equivalent of the brokered Git SSH transport lane.

### 2B. Harness-Specific Auth Adapters

For Claude, OpenCode, Codex, and future harnesses, Hazmat should define an
adapter interface:

- locate or migrate host-owned credential material
- prepare per-session delivery
- clean up session delivery artifacts on exit
- fail closed if the adapter cannot safely honor the request

Session delivery should be adapter-specific:

- **proxy mode** when the harness supports custom base URLs, proxy settings, or
  local API indirection
- **ephemeral materialization mode** when the harness requires local files but
  can operate with short-lived access tokens or a reduced session artifact

The important invariant is not "no file ever appears in the session." The
important invariant is:

- long-lived reusable secrets do not live in the persistent readable session
  home

### Migration of Existing Commands

Current commands should change as follows:

- `hazmat config agent`
  - stop writing `ANTHROPIC_API_KEY` to `/Users/agent/.zshrc`
  - stop configuring Git's plaintext credential store under the agent home
  - write to the host-owned Hazmat secret store instead
- `hazmat config import claude`
  - stop copying `.credentials.json` into `/Users/agent/.claude`
  - migrate auth state into the host-owned secret store
  - continue importing non-secret portable assets such as commands and skills
- `hazmat config import opencode`
  - same pattern for OpenCode auth

Existing agent-home secrets should be treated as migration candidates:

- detect them during launch, `hazmat check`, or import
- offer a one-time migration path
- deny them inside sessions once the corresponding adapter exists

### UX Impact

The user-facing UX should still be:

- sign in once
- reuse the sign-in across future sessions
- no repeated routine prompts

What changes is where the durable credential authority lives and how it is
handed to the session.

### Compatibility Notes

This feature intentionally changes several existing assumptions:

- readable `~/.claude` is no longer a success criterion
- `ANTHROPIC_API_KEY` in `.zshrc` is no longer the recommended setup path
- Git HTTPS access is no longer backed by Git's built-in plaintext store in the
  agent home

### External Audit Questions

- Which harnesses can support proxy mode immediately versus requiring
  ephemeral-file compatibility first?
- Should the canonical v1 secret store be host-owned files only, or should
  Keychain-backed storage be part of the first implementation slice?

## Feature 3: Session-Local Home Assembly

### Problem

Hazmat currently uses broad access to `/Users/agent` as the compatibility
escape hatch for normal tool behavior. That makes many things easy, but it also
means:

- secrets and non-secrets share one broad allow zone
- new harness state naturally accumulates in a readable persistent home
- the seatbelt policy has to rely on ever-growing deny exceptions

This is the wrong long-term shape if the goal is to remove ambient session
authority.

### Proposed Model

Replace the blanket `agent` home allowance with **session-local home assembly**
plus a curated set of explicit persistent subpaths.

At launch time Hazmat should build a session home such as:

- `/private/tmp/hazmat-home/<session-id>/home`

The session receives:

- `HOME=<session-home>`
- `XDG_CACHE_HOME=<session-home>/.cache`
- `XDG_CONFIG_HOME=<session-home>/.config`
- `XDG_DATA_HOME=<session-home>/.local/share`

Hazmat then assembles the home from three state classes.

### State Classes

#### 1. Persistent safe state

Examples:

- imported commands and skills
- non-secret harness settings owned by Hazmat
- selected transcript/session history paths that are intentionally persistent
- installed harness binaries under `.local/bin`
- Git identity config

This state should live in curated persistent directories that the session home
references explicitly.

#### 2. Ephemeral session state

Examples:

- temp files
- harness caches that do not need to survive session end
- adapter scratch space
- short-lived auth artifacts
- temporary lock files and runtime logs

This state should live entirely under the session home and be deleted on exit.

#### 3. Denied or absent state

Examples:

- long-lived auth files
- Git credential stores
- SSH runtime sockets
- future provider tokens copied into persistent home directories

This state should not be present as readable persistent session files at all.

### Assembly Mechanism

Hazmat does not need a kernel overlay filesystem to get value here.

V1 can use:

- directory creation
- symlinks to curated persistent subtrees
- small copied config stubs where symlinks are awkward

The critical change is in the seatbelt:

- remove the blanket `allow file-read* file-write* (subpath "/Users/agent")`
- replace it with explicit allow rules for the session home and explicit
  persistent subpaths that Hazmat assembled deliberately

### Compatibility Responsibilities

This feature must preserve:

- Claude `--resume` / `--continue` and `hazmat export claude session`
- portable commands and skills
- installed harness executables
- Git identity behavior

That means the home assembler needs a manifest or adapter layer describing
which persistent subpaths each harness still needs.

### UX Impact

The happy path should remain unchanged:

- the same session commands
- the same tools available in `PATH`
- the same visible commands/skills/history where those are part of the product

The operator should not need to think about the assembled home unless they are
debugging. `hazmat explain --json` can expose the detailed assembly map.

### External Audit Questions

- Which existing harness state paths are genuinely safe to persist long-term,
  and which should move to explicit export/sync flows instead?
- Should transcript history remain directly writable during the session or be
  synced in and out as a post-session step?

## Feature 4: Passive Egress Telemetry and Audit-Install Mode

### Problem

Hazmat's native mode intentionally keeps a soft egress model:

- block clearly bad protocols
- allow ordinary HTTPS, HTTP, DNS, and dev workflows

That is the right usability tradeoff for native containment, but it means
network policy alone cannot stop exfiltration to novel allowed destinations.

The right answer is therefore not "block more by default" in native mode. The
right answer is:

- reduce what is worth stealing
- add first-party visibility when network behavior does matter

### Proposed Model

Add **passive native-session egress telemetry** and an
**`--audit-install` workflow** for package-install commands.

Recommended split:

#### Always-on lightweight session summary

For every native session, Hazmat collects a lightweight summary of:

- remote endpoints contacted by the `agent` user
- first-seen / last-seen times
- protocol and port
- optional hostname resolution when available

This should be observational, not blocking.

#### High-signal install audit mode

Add a dedicated mode such as:

```bash
hazmat exec --audit-install -- npm install
hazmat exec --audit-install -- uv sync
```

The mode should:

1. snapshot current outbound connections
2. run the install command under the usual containment
3. collect new outbound destinations during the run
4. emit a concise report of unexpected destinations

### Data Source Strategy

The first-party implementation should prefer built-in host observability over
new mandatory third-party tools.

Candidate mechanisms:

- polling `lsof`/`nettop` for the `agent` user during the session
- optional pf logging for the agent anchor with a Hazmat parser

LuLu remains a useful optional operator layer, but it should not be Hazmat's
only story for external audit.

### Why This Is Deliberately Not a Full Allowlist

Native Hazmat should keep its current product stance:

- full HTTPS allowlisting is too brittle for the default macOS local-dev flow
- package managers, docs lookup, research, and API-driven tooling need wide
  network reach

This feature is therefore **visibility and investigation tooling**, not a
silent hard break in ergonomics.

### UX Impact

Expected UX:

- no new prompts during ordinary sessions
- optional summary on exit or in `hazmat explain --json`
- an explicit install-audit mode when the operator wants deeper inspection

### External Audit Questions

- Is lightweight default telemetry enough, or should Hazmat also offer an
  operator-configurable "warn on new domain class" mode later?
- Is pf-log-based capture robust enough on macOS to justify first-party support
  in the initial slice, or should v1 stay with user-space observation only?

## Rollout Plan

### Phase 0: Stopgap hardening

Before the larger migration lands, Hazmat should make the current risk easier
to audit:

- add explicit docs about readable agent-home auth material
- add targeted deny candidates for the most obvious credential files once the
  corresponding adapters exist
- add `hazmat check` output that calls out ambient secret material still stored
  in the persistent agent home

### Phase 1: Brokered Git SSH

This is the cleanest first move because:

- it closes a concrete demonstrated bypass
- it preserves the existing user-facing Git SSH workflow
- it does not depend on harness-specific auth work

### Phase 2: Host-owned secret store plus Git HTTPS broker

This removes the easiest non-SSH credential exfil paths and lays the storage
foundation for harness adapters.

### Phase 3: Harness auth adapters and session-home assembly

These two should be designed together:

- adapters define what must remain accessible
- home assembly defines where and for how long it remains accessible

### Phase 4: Egress telemetry and audit-install

This comes after the big ambient-authority cuts because telemetry is more
valuable once the session contains less inherently valuable secret material.

## Verification Plan

The implementation should ship with explicit regression checks for the new
security claims.

### Tests

- managed Git SSH cannot be bypassed via raw `/usr/bin/ssh`
- no generic `ssh-agent` socket is present in the session runtime
- Git HTTPS works without a readable plaintext credential file under
  `/Users/agent`
- Claude/OpenCode imports preserve commands and skills while no longer copying
  long-lived auth files into persistent agent-home locations
- assembled session homes preserve `--resume`, export, and harness startup
- session launch fails closed when a required broker or adapter is unavailable

### Product Checks

`hazmat check` should evolve to assert the new target state:

- brokered Git SSH present when configured
- no ambient long-lived harness secrets in persistent session home
- session-home assembly active
- egress telemetry plumbing healthy when enabled

### Audit Documentation

The following docs will need coordinated updates when the features land:

- `README.md`
- `docs/design-assumptions.md`
- `docs/claude-import.md`
- `docs/opencode-import.md`
- `docs/brief-supply-chain-hardening.md`
- `docs/cve-audit.md`

## Residual Risk After This Roadmap

Even after these features land, native Hazmat remains:

- a same-host containment model
- an allow-by-default HTTPS model
- a system where project contents are intentionally readable and writable

That means the residual risk becomes:

- abuse of explicitly granted live capabilities during the session
- exfiltration of project secrets that the operator intentionally exposed
- same-host kernel or OS escape research beyond Hazmat's control surface

Those are materially narrower and more honest residuals than the current
position where long-lived reusable credentials and generic signing oracles
remain directly reachable inside the contained session.

## Derived Implementation Issues

- `sandboxing-n1xy` — replace managed Git SSH runtime with brokered transport
  capability
- `sandboxing-gg16` — move harness and Git credentials to host-owned secret
  storage
- `sandboxing-93r8` — assemble a session-local home and remove blanket
  agent-home allowance
- `sandboxing-suhu` — add native-session egress telemetry and audit-install
  mode
