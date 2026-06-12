# Problem 2 — Seatbelt Policy Structure

## Problem Statement

`generateSBPL()` produces a per-session macOS Seatbelt policy from
user-provided inputs: `ProjectDir` (writable working directory), `ReadDirs`
(read-only reference directories), the agent-owned session temp root, and the
requested native session network mode. The policy embeds literal paths and
relies on SBPL's **last-match-wins** semantics to deny credential and temp
capability access.

The correctness questions:

1. **Credential read protection** — can any combination of user inputs produce
   a policy where credential reads are allowed outside explicitly modeled
   compatibility exceptions?

2. **Read dir write isolation** — can read-only directories accidentally
   receive write access?

3. **Credential write protection** — do the final deny rules also prevent
   writes to credential directories outside explicitly modeled compatibility
   exceptions, even when earlier project or static allow sections would
   otherwise cover them?

4. **Read dir subsumption** — are redundant read dir rules correctly elided?

5. **Per-session network denial** — when the caller requests `--network none`,
   does the native policy omit both outbound network and DNS lookup authority
   without changing any global firewall state?

6. **Temp exposure control** — does Hazmat avoid implicit broad reads, writes,
   and execs under `/private/tmp` and `/private/var/folders`, while keeping a
   session-owned temp root usable for compiler/runtime artifacts?

7. **Temp socket capability protection** — do explicit temp socket denies win
   even if a user grants a broad host temp path as a project or read dir?

8. **Claude Keychain exception scoping** — when native Claude OAuth needs the
   agent login keychain, is the post-deny exception limited to the exact
   agent-owned login keychain DB and sidecar files, while the broader
   Keychains directory remains denied?

9. **Host authority key protection** — do Beadpost broker attestation keys stay
   denied even when a user selects the key directory as a project or read dir,
   and without inheriting the Claude Keychain compatibility exception?

10. **Agent-home narrowing** — does the policy avoid a blanket `/Users/agent`
    read/write/exec grant while preserving explicit durable harness, shell,
    tool, and XDG subtrees?

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/session.go` | `generateSBPL()`, `isWithinDir()` |
| `hazmat/session_policy_sbpl.go` | `compileDarwinSBPL()` |

## Policy Section Ordering (as implemented)

```
Section 0: System library allows (static — /usr/lib, /System/Library, etc.)
Section 1: Read-only directory allows (user input, filtered for subsumption)
Section 2: Project directory read+write (user input)
Section 3: Resume directory read+write (optional, invoking user's session dir)
Section 4: Explicit agent-home state/tooling allows (.claude, .local, .config, etc.)
Section 5: Session temp root read+write+exec, plus narrow harness runtime temp roots
Section 6: Project write re-assertion (if a read dir is a parent of the project)
Section 7: Host temp socket/capability denies
Section 8: Credential denies (static — .ssh, .aws, .config/gcloud, etc.)
Section 9: Claude agent login keychain exception (optional, exact files only)
```

Credential denies are the final broad credential boundary (section 8). Since
SBPL is last-match-wins, any earlier allow for the same path is overridden by
the deny. The only later filesystem allow is section 9, which is conditional on
native Claude OAuth mode and re-allows only the exact agent login keychain DB
and SQLite sidecar files. The broader Keychains directory remains denied.

The native policy does not grant broad `/private/tmp` or
`/private/var/folders` read/write/exec by default. Runtime `TMPDIR` is pointed
at an agent-owned session temp root; that root is granted read/write/exec so
toolchains can create and execute temporary artifacts without exposing unrelated
host temp files.

Claude Code 2.1.x also probes `/private/tmp/claude-<agent uid>` directly even
when `TMPDIR` and `BUN_TMPDIR` point at the session temp root. Hazmat grants
read/write to that agent-owned Claude runtime root only for Claude sessions; it
does not grant execute there and does not grant broader host temp access.

Codex App temp/control socket paths are denied after project/read/temp allows
and before credential denies. This keeps socket capability endpoints denied
even when a user explicitly grants a broad host temp directory.

Network authority is modeled separately from filesystem path matching. Default
native sessions emit outbound network and DNS lookup grants; `--network none`
emits neither. The local inbound rule is intentionally not an egress grant.

Harness-specific macOS Security framework compatibility grants are emitted in
section 0 alongside system library and service allows. They are modeled as part
of the abstract static system surface because they do not change the ordering of
project/read/temp/credential path rules checked by this spec.

The policy keeps `HOME=/Users/agent`, but section 4 is no longer a blanket
agent-home allow. It grants explicit durable state/tooling subtrees such as
`.claude`, `.codex`, `.agents`, `.opencode`, `.gemini`, `.qwen`, `.cursor`,
`.config`, `.cache`, and `.local`, plus known shell/config files. Credential
deny paths remain denied later by section 8.

## TLA+ Model

### Abstract Path Model

Twenty-three abstract paths with a containment relation:

| Path | Represents | Contains |
|------|-----------|----------|
| `normalProj` | `/Users/dr/workspace/myproject` | (nothing) |
| `agentHome` | `/Users/agent` | sshDir, configDir, gcloudDir, keychainDir, keychainDB, keychainSHM, keychainWAL, sessionTempRoot, sessionTempFile, agentStateDir, agentLocalDir, agentOtherFile |
| `configDir` | `/Users/agent/.config` | gcloudDir |
| `sshDir` | `/Users/agent/.ssh` | (nothing) |
| `gcloudDir` | `/Users/agent/.config/gcloud` | (nothing) |
| `keychainDir` | `/Users/agent/Library/Keychains` | keychainDB, keychainSHM, keychainWAL |
| `keychainDB` | `/Users/agent/Library/Keychains/login.keychain-db` | (nothing) |
| `keychainSHM` | `/Users/agent/Library/Keychains/login.keychain-db-shm` | (nothing) |
| `keychainWAL` | `/Users/agent/Library/Keychains/login.keychain-db-wal` | (nothing) |
| `outsideRef` | `/Users/dr/reference` | (nothing) |
| `invokerSess` | `/Users/dr/.claude/projects/-foo` | (nothing) |
| `hostTempRoot` | `/private/tmp` | hostTempOutside, claudeTempRoot, claudeTempFile, codexTempSocket |
| `hostTempOutside` | `/private/tmp/outside-host-readable.txt` | (nothing) |
| `sessionTempRoot` | `/Users/agent/.cache/hazmat/tmp/<session>` | sessionTempFile |
| `sessionTempFile` | `/Users/agent/.cache/hazmat/tmp/<session>/artifact` | (nothing) |
| `agentStateDir` | `/Users/agent/.claude` | (nothing) |
| `agentLocalDir` | `/Users/agent/.local` | (nothing) |
| `agentOtherFile` | `/Users/agent/unlisted.txt` | (nothing) |
| `claudeTempRoot` | `/private/tmp/claude-599` | claudeTempFile |
| `claudeTempFile` | `/private/tmp/claude-599/socket` | (nothing) |
| `codexTempSocket` | `/private/tmp/codex-ipc/app.sock` | (nothing) |
| `attestationKeyDir` | `/var/lib/hazmat/keys` | attestationKeyFile |
| `attestationKeyFile` | `/var/lib/hazmat/keys/attestation.key` | (nothing) |

### Nondeterministic Inputs

- `ProjectDir ∈ {normalProj, agentHome, sshDir, configDir, hostTempRoot, hostTempOutside, attestationKeyDir}` — tests dangerous choices, explicit host temp grants, and the host authority key deny root
- `ReadDirs ⊆ {normalProj, agentHome, outsideRef, hostTempRoot, attestationKeyDir}` — tests broad read dirs, including explicit host temp and host authority key grants
- `NetworkMode ∈ {default, none}` — tests the default outbound mode and
  per-session deny-all egress mode
- `AgentKeychainAccess ∈ BOOLEAN` — tests native Claude OAuth's exact
  agent-login-keychain exception and the normal no-exception path

### Variables

- `rules` — set of emitted policy rules `[section, action, path]`
- `agentKeychainAccess` — whether section 9 is emitted
- `section` — current policy generation phase (0..10)

### Evaluation: Last-Match-Wins

For a target path, find all rules whose path covers the target. The rule with
the highest section number determines the outcome. This models SBPL semantics.

## What TLC Finds

### Invariants That Pass

| Invariant | Meaning |
|-----------|---------|
| `CredentialReadDenied` | Credential file-read* is denied outside the exact Claude agent keychain exception |
| `CredentialWriteDenied` | Credential file-write* is denied outside the exact Claude agent keychain exception |
| `AttestationKeyReadDenied` | Beadpost broker attestation key file-read* is denied with no Claude-style exception |
| `AttestationKeyWriteDenied` | Beadpost broker attestation key file-write* is denied with no Claude-style exception |
| `AgentKeychainExceptionScoped` | The optional Claude keychain exception allows only the modeled login keychain DB and sidecar files, and only when requested |
| `ReadDirsNoWrite` | Read-only dirs never get file-write* rules |
| `NoBroadAgentHomeAllow` | Section 4 never emits a broad allow on the agent-home root |
| `AgentHomeSubsUsable` | Explicit modeled agent-home subtrees remain readable, writable, and executable |
| `UnlistedAgentHomeNotImplicitlyReadable` | Unlisted agent-home files are not readable unless the user explicitly grants the home as project/read input |
| `UnlistedAgentHomeNotImplicitlyWritable` | Unlisted agent-home files are not writable unless the user explicitly grants the home as project input |
| `UnlistedAgentHomeNotImplicitlyExecutable` | Unlisted agent-home files are not executable unless the user explicitly grants the home as project/read input |
| `ProjectDirWritable` | Project directory always has write access |
| `ReadDirSubsumption` | Read dirs within project dir correctly elided |
| `ResumeDirNotCredential` | Optional resume dir cannot overlap credential paths |
| `HostTempNotImplicitlyReadable` | Outside host temp files are not readable unless explicitly granted as project/read dirs |
| `HostTempNotImplicitlyWritable` | Outside host temp files are not writable unless explicitly granted as project |
| `HostTempNotImplicitlyExecutable` | Outside host temp artifacts are not executable unless explicitly granted as project/read dirs |
| `SessionTempWritable` | The agent-owned session temp root stays readable, writable, and executable |
| `ClaudeRuntimeTempScoped` | Claude's agent-owned runtime temp dir is readable/writable but not executable unless the user explicitly grants an overlapping project/read path |
| `TempSocketsDenied` | Codex App temp/control socket paths are denied even under broad host-temp grants |
| `NetworkNoneDeniesOutbound` | `--network none` emits no outbound network grant |
| `NetworkNoneDeniesDNS` | `--network none` emits no DNS lookup grant |
| `NetworkDefaultAllowsOutbound` | Default sessions preserve outbound network + DNS grants |

### Result

`CredentialWriteDenied` is part of the checked suite now. The current policy
model proves that the credential deny section overrides both project write
access and earlier static config allows for all modeled credential paths except
the explicit Claude agent login keychain files. It also proves that the
post-deny Keychain exception is absent unless requested and stays limited to the
login keychain DB plus SQLite sidecars; the broader Keychains directory remains
denied. `AttestationKeyReadDenied` and `AttestationKeyWriteDenied` prove the
host-owned Beadpost broker signing key directory and file stay denied with no
Keychain exception. `NoBroadAgentHomeAllow` proves the static compatibility
section has no blanket `/Users/agent` allow; `AgentHomeSubsUsable` preserves the
explicit modeled home subtrees; and the `UnlistedAgentHomeNotImplicitly*`
invariants prove unrelated home content is not exposed except through explicit
user-provided project/read grants. Host temp access is no longer implicit, while
the agent-owned session temp root remains usable for compiler/runtime artifacts,
Claude's runtime temp root adds no implicit execute access, and Codex App temp
socket capability paths stay denied.

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Paths | 23 | Covers: normal project, agent home, credential dirs, agent login keychain DB/sidecars, config overlap, explicit agent-home subtrees, unlisted agent-home content, outside ref, invoker resume dir, host temp, session temp, Claude runtime temp, Codex temp socket paths, and Beadpost attestation key paths |
| ProjectChoices | 7 | Includes adversarial choices: agentHome, sshDir, configDir, host temp paths, and attestationKeyDir |
| ReadChoices | 5 | Includes broad choices: agentHome, hostTempRoot, and attestationKeyDir |
| NetworkChoices | 2 | Covers default outbound mode and deny-all egress mode |
| AgentKeychainAccess | 2 | Covers both normal credential denial and native Claude OAuth keychain compatibility |

**Confirmed state space:** 32,256 states generated, 29,568 distinct, depth 11. Runtime: <5s.
