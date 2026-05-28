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
   a policy where credential reads are allowed? (The deny rules must always
   be the last matching rules for credential paths.)

2. **Read dir write isolation** — can read-only directories accidentally
   receive write access?

3. **Credential write protection** — do the final deny rules also prevent
   writes to credential directories, even when earlier project or static allow
   sections would otherwise cover them?

4. **Read dir subsumption** — are redundant read dir rules correctly elided?

5. **Per-session network denial** — when the caller requests `--network none`,
   does the native policy omit both outbound network and DNS lookup authority
   without changing any global firewall state?

6. **Temp exposure control** — does Hazmat avoid implicit broad reads, writes,
   and execs under `/private/tmp` and `/private/var/folders`, while keeping a
   session-owned temp root usable for compiler/runtime artifacts?

7. **Temp socket capability protection** — do explicit temp socket denies win
   even if a user grants a broad host temp path as a project or read dir?

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
Section 4: Agent home config allows (static — .claude, .local, .config, etc.)
Section 5: Session temp root read+write+exec (agent-owned per-session dir)
Section 6: Project write re-assertion (if a read dir is a parent of the project)
Section 7: Host temp socket/capability denies
Section 8: Credential denies (static — .ssh, .aws, .config/gcloud, etc.)
```

Credential denies are ALWAYS last (section 8). Since SBPL is last-match-wins,
any earlier allow for the same path is overridden by the deny.

The native policy does not grant broad `/private/tmp` or
`/private/var/folders` read/write/exec by default. Runtime `TMPDIR` is pointed
at an agent-owned session temp root; that root is granted read/write/exec so
toolchains can create and execute temporary artifacts without exposing unrelated
host temp files.

Codex App temp/control socket paths are denied after project/read/temp allows
and before credential denies. This keeps socket capability endpoints denied
even when a user explicitly grants a broad host temp directory.

Network authority is modeled separately from filesystem path matching. Default
native sessions emit outbound network and DNS lookup grants; `--network none`
emits neither. The local inbound rule is intentionally not an egress grant.

## TLA+ Model

### Abstract Path Model

Twelve abstract paths with a containment relation:

| Path | Represents | Contains |
|------|-----------|----------|
| `normalProj` | `/Users/dr/workspace/myproject` | (nothing) |
| `agentHome` | `/Users/agent` | sshDir, configDir, gcloudDir |
| `configDir` | `/Users/agent/.config` | gcloudDir |
| `sshDir` | `/Users/agent/.ssh` | (nothing) |
| `gcloudDir` | `/Users/agent/.config/gcloud` | (nothing) |
| `outsideRef` | `/Users/dr/reference` | (nothing) |
| `invokerSess` | `/Users/dr/.claude/projects/-foo` | (nothing) |
| `hostTempRoot` | `/private/tmp` | hostTempOutside, codexTempSocket |
| `hostTempOutside` | `/private/tmp/outside-host-readable.txt` | (nothing) |
| `sessionTempRoot` | `/Users/agent/.cache/hazmat/tmp/<session>` | sessionTempFile |
| `sessionTempFile` | `/Users/agent/.cache/hazmat/tmp/<session>/artifact` | (nothing) |
| `codexTempSocket` | `/private/tmp/codex-ipc/app.sock` | (nothing) |

### Nondeterministic Inputs

- `ProjectDir ∈ {normalProj, agentHome, sshDir, configDir, hostTempRoot, hostTempOutside}` — tests dangerous choices and explicit host temp grants
- `ReadDirs ⊆ {normalProj, agentHome, outsideRef, hostTempRoot}` — tests broad read dirs, including explicit host temp grants
- `NetworkMode ∈ {default, none}` — tests the default outbound mode and
  per-session deny-all egress mode

### Variables

- `rules` — set of emitted policy rules `[section, action, path]`
- `section` — current policy generation phase (0..9)

### Evaluation: Last-Match-Wins

For a target path, find all rules whose path covers the target. The rule with
the highest section number determines the outcome. This models SBPL semantics.

## What TLC Finds

### Invariants That Pass (5,760 states, <1s)

| Invariant | Meaning |
|-----------|---------|
| `CredentialReadDenied` | Credential file-read* is always denied — section 8 deny always wins |
| `CredentialWriteDenied` | Credential file-write* is always denied — section 8 deny always wins |
| `ReadDirsNoWrite` | Read-only dirs never get file-write* rules |
| `ProjectDirWritable` | Project directory always has write access |
| `ReadDirSubsumption` | Read dirs within project dir correctly elided |
| `ResumeDirNotCredential` | Optional resume dir cannot overlap credential paths |
| `HostTempNotImplicitlyReadable` | Outside host temp files are not readable unless explicitly granted as project/read dirs |
| `HostTempNotImplicitlyWritable` | Outside host temp files are not writable unless explicitly granted as project |
| `HostTempNotImplicitlyExecutable` | Outside host temp artifacts are not executable unless explicitly granted as project/read dirs |
| `SessionTempWritable` | The agent-owned session temp root stays readable, writable, and executable |
| `TempSocketsDenied` | Codex App temp/control socket paths are denied even under broad host-temp grants |
| `NetworkNoneDeniesOutbound` | `--network none` emits no outbound network grant |
| `NetworkNoneDeniesDNS` | `--network none` emits no DNS lookup grant |
| `NetworkDefaultAllowsOutbound` | Default sessions preserve outbound network + DNS grants |

### Result

`CredentialWriteDenied` is part of the checked suite now. The current policy
model proves that the final credential deny section overrides both project
write access and earlier static config allows for all modeled credential paths.
It also proves that host temp access is no longer implicit, while the
agent-owned session temp root remains usable for compiler/runtime artifacts and
Codex App temp socket capability paths stay denied.

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Paths | 12 | Covers: normal project, agent home, credential dirs, config overlap, outside ref, invoker resume dir, host temp, session temp, and Codex temp socket paths |
| ProjectChoices | 6 | Includes adversarial choices: agentHome, sshDir, configDir, and host temp paths |
| ReadChoices | 4 | Includes broad choices: agentHome and hostTempRoot |
| NetworkChoices | 2 | Covers default outbound mode and deny-all egress mode |

**Confirmed state space:** 6,336 states generated, 5,760 distinct. Runtime: <1s.
