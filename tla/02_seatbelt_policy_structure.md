# Problem 2 — Seatbelt Policy Structure

## Problem Statement

`generateSBPL()` produces a per-session macOS Seatbelt policy from
user-provided inputs: `ProjectDir` (writable working directory), `ReadDirs`
(read-only reference directories), and the requested native session network
mode. The policy embeds literal paths and relies on SBPL's
**last-match-wins** semantics to deny credential access.

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
Section 5: Project write re-assertion (if a read dir is a parent of the project)
Section 6: Credential denies (static — .ssh, .aws, .config/gcloud, etc.)
```

Credential denies are ALWAYS last (section 6). Since SBPL is last-match-wins,
any earlier allow for the same path is overridden by the deny.

Network authority is modeled separately from filesystem path matching. Default
native sessions emit outbound network and DNS lookup grants; `--network none`
emits neither. The local inbound rule is intentionally not an egress grant.

## TLA+ Model

### Abstract Path Model

Six abstract paths with a containment relation:

| Path | Represents | Contains |
|------|-----------|----------|
| `normalProj` | `/Users/dr/workspace/myproject` | (nothing) |
| `agentHome` | `/Users/agent` | sshDir, configDir, gcloudDir |
| `configDir` | `/Users/agent/.config` | gcloudDir |
| `sshDir` | `/Users/agent/.ssh` | (nothing) |
| `gcloudDir` | `/Users/agent/.config/gcloud` | (nothing) |
| `outsideRef` | `/tmp/reference` | (nothing) |

### Nondeterministic Inputs

- `ProjectDir ∈ {normalProj, agentHome, sshDir, configDir}` — tests dangerous choices
- `ReadDirs ⊆ {normalProj, agentHome, outsideRef}` — tests broad read dirs
- `NetworkMode ∈ {default, none}` — tests the default outbound mode and
  per-session deny-all egress mode

### Variables

- `rules` — set of emitted policy rules `[section, action, path]`
- `section` — current policy generation phase (0..7)

### Evaluation: Last-Match-Wins

For a target path, find all rules whose path covers the target. The rule with
the highest section number determines the outcome. This models SBPL semantics.

## What TLC Finds

### Invariants That Pass (1,536 states, <1s)

| Invariant | Meaning |
|-----------|---------|
| `CredentialReadDenied` | Credential file-read* is always denied — section 6 deny always wins |
| `CredentialWriteDenied` | Credential file-write* is always denied — section 6 deny always wins |
| `ReadDirsNoWrite` | Read-only dirs never get file-write* rules |
| `ProjectDirWritable` | Project directory always has write access |
| `ReadDirSubsumption` | Read dirs within project dir correctly elided |
| `ResumeDirNotCredential` | Optional resume dir cannot overlap credential paths |
| `NetworkNoneDeniesOutbound` | `--network none` emits no outbound network grant |
| `NetworkNoneDeniesDNS` | `--network none` emits no DNS lookup grant |
| `NetworkDefaultAllowsOutbound` | Default sessions preserve outbound network + DNS grants |

### Result

`CredentialWriteDenied` is part of the checked suite now. The current policy
model proves that the final credential deny section overrides both project
write access and earlier static config allows for all modeled credential paths.

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Paths | 7 | Covers: normal project, agent home, credential dirs, config overlap, outside ref, invoker resume dir |
| ProjectChoices | 4 | Includes adversarial choices: agentHome, sshDir, configDir |
| ReadChoices | 3 | Includes broad choice: agentHome |
| NetworkChoices | 2 | Covers default outbound mode and deny-all egress mode |

**Confirmed state space:** 1,728 states generated, 1,536 distinct. Runtime: <1s.
