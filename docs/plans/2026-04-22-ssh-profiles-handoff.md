# SSH Profiles + Multi-Key Git-SSH — Review Handoff

**Date:** 2026-04-22
**Bead:** `sandboxing-nm5o` (depends on closed `sandboxing-vmg1`)
**Branch:** `master` at original handoff time; work later landed and the bead is closed
**Audience:** code reviewer

Archive note: this review packet was recovered from untracked working-tree
state after `sandboxing-nm5o` and `sandboxing-vmg1` were already closed. Keep
it as historical review context, not as the live task tracker.

## 1) What shipped

Two stacked features for per-project Git-SSH, both formally verified via
an extended `MC_GitSSHRouting` TLA+ module:

1. **Multi-key per project** (`sandboxing-vmg1`, merged locally): a
   project can now configure N named SSH keys, each with its own host
   allowlist. The wrapper routes the destination host to exactly one
   key's identity-agent socket or rejects. Overlap, legacy ambiguity,
   and socket collisions are rejected at config-set or session-prep
   time. Single-key configs keep today's any-host behavior.

2. **Reusable SSH profiles** (`sandboxing-nm5o`, this handoff): a
   top-level `ssh_profiles:` map defines a named identity once; any
   project key can reference it via `profile: <name>`. Profiles carry
   `default_hosts` that project keys inherit when they declare no hosts
   of their own. Declared `--host` always overrides the default. Profile
   removal is referrer-safe (refuses with a list of referrers; `--force`
   cascades). Rename updates every referrer in one save.

Generic profiles for non-SSH configuration (read dirs, backup, etc.)
are **deliberately out of scope** — rationale recorded in the bead.

## 2) Commit map

| Commit | Area | What it does |
|--------|------|--------------|
| `2479f2c` | tla | Initial `MC_GitSSHRouting` spec stub for multi-key routing |
| `829b974` | config | Multi-key schema + `ValidateProjectSSHConfig` + unit tests |
| `ec40b6e` | session | `sessionGitSSHConfig.Keys`, per-key agent sockets, routed wrapper |
| `c6151ce` | ux | `hazmat config ssh add/remove` CLI for multi-key |
| `7edbcba` | tla | Wire multi-key spec into `check_suite.sh` + `VERIFIED.md` |
| `0acab7b` | tla | Extend spec with profile resolution layer (884,736 states) |
| `2cade5e` | config | `ssh_profiles` schema + `ValidateSSHProfiles` + `ValidateProjectSSHProfileRefs` |
| `718b7b1` | session | Resolver walks `Profile` references + inherits `default_hosts` |
| `64a3320` | ux | `hazmat config ssh profile add/list/show/remove/rename` + `--profile` flag |
| `5f51e30` | docs | `docs/usage.md` Reusable SSH profiles section + CHANGELOG |

## 3) TLA+ verification scope

`tla/MC_GitSSHRouting.tla` models the routing relation after glob
expansion plus the profile resolution layer above. Nine invariants, all
passing TLC on a model with 2 hosts, 2 keys, 2 sockets, 2 profiles:

| Invariant | Claim |
|-----------|-------|
| `DeterministicRouting` | No host resolves to two keys in a ready config |
| `OverlapRejectedAtConfigTime` | Two keys with intersecting effective hosts never reach ready |
| `HostsOutsideAllowlistRejected` | Unserved hosts → wrapper rejects |
| `LegacyFallbackSingleOnly` | Any-host fallback only with exactly one inline key |
| `SocketsDistinctForPresent` | Two present keys never share a socket (even when referencing the same profile) |
| `NoDanglingProfileRefs` | Every `profile: X` resolves to a defined profile |
| `NoProfileInlineConflict` | A key never has both `profile:` and `private_key:` |
| `PresentKeysHaveIdentity` | Every present key has an identity source |
| `NoCrossKey` | Lookup returns the selected key's bound socket; no other key shares that socket |

**State count:** 884,736 distinct (1,327,104 generated), depth 2, ~1 min TLC.
**Full suite:** 10 specs clean end to end.

**Out of scope for TLC (covered by Go tests):**
- Glob syntax, shell quoting, signal handling
- ssh-agent liveness
- Concrete `IdentityAgent` emission in the wrapper
- Profile rename/removal cascade semantics (pure Go logic)

## 4) Go changes at a glance

**Schema (`hazmat/config.go`):**
- `HazmatConfig.SSHProfiles map[string]SSHProfile`
- `SSHProfile` struct: `PrivateKeyPath`, `KnownHostsPath`, `DefaultHosts`, `Description`
- `ProjectSSHKey.Profile` field (alongside existing `PrivateKeyPath`, `Key`, `Hosts`)

**Validation (`hazmat/config.go`):**
- `ValidateProjectSSHConfig(c)` — format-level, no profile context. Enforces
  single-identity-source-per-key, legacy-multi rejection, declared-host overlap.
- `ValidateSSHProfiles(profiles)` — the profile map itself.
- `ValidateProjectSSHProfileRefs(c, profiles)` — cross-reference: dangling
  refs rejected, effective-host overlap (after inheritance) rejected.
- `loadConfig()` runs both validators.
- `ProjectSSHKey.EffectiveHosts(profiles)` returns the resolved host list
  for display / resolver use.

**Resolver (`hazmat/git_ssh.go`):**
- `resolveManagedGitSSH` loads the full config and passes `SSHProfiles` to
  `resolveProjectSSHKeys`.
- `resolveProjectSSHKeyIdentity` now tries `Profile` first, then
  `PrivateKeyPath`, then the legacy inventory `Key`.
- `resolveProjectSSHKeyEntry` inherits `profile.DefaultHosts` when the
  project key declares no hosts; declared hosts always override.
- Session-time runs `ValidateProjectSSHProfileRefs` alongside the existing
  format-level check, so dangling refs and post-inheritance overlap are
  caught before any agent socket is allocated.

**CLI (`hazmat/config.go`):**
- New subcommand tree: `hazmat config ssh profile add | list | show | remove | rename`.
- `remove` refuses while referrers exist and lists them; `--force` detaches
  every reference and removes the profile in one save.
- `rename` updates every referrer in one save.
- Existing `hazmat config ssh add` gains `--profile <name>`.

## 5) How to verify

### Automated
```bash
# Go unit and integration tests
cd hazmat
go test ./...

# Full TLA+ suite
cd ../tla
bash check_suite.sh
```
Both should pass. TLA+ suite ends with `MC_GitSSHRouting` at 884,736
distinct states.

### Manual smoke test
```bash
# Prep
ssh-keygen -t ed25519 -N '' -f /tmp/shared_key
: > /tmp/known_hosts

# Define a profile
./hazmat/hazmat config ssh profile add github /tmp/shared_key \
    --known-hosts /tmp/known_hosts \
    --default-host github.com --description "test profile"

./hazmat/hazmat config ssh profile list
# → shows github with Default hosts: github.com, Referrers: (none)

# Attach to two projects
mkdir -p /tmp/proj-a /tmp/proj-b
./hazmat/hazmat config ssh add -C /tmp/proj-a --name work --profile github
./hazmat/hazmat config ssh add -C /tmp/proj-b --name work --profile github \
    --host enterprise.internal   # declared overrides default

./hazmat/hazmat config ssh profile list
# → Referrers: 2 project(s)

# Referrer-safe removal
./hazmat/hazmat config ssh profile remove github
# → refuses and lists /tmp/proj-a, /tmp/proj-b

./hazmat/hazmat config ssh profile remove github --force
# → detaches both projects and removes the profile

# Rename
./hazmat/hazmat config ssh profile add github /tmp/shared_key --default-host github.com
./hazmat/hazmat config ssh add -C /tmp/proj-a --name work --profile github
./hazmat/hazmat config ssh profile rename github work-github
cat ~/.hazmat/config.yaml | grep -A 1 'work-github\|github'
# → both the ssh_profiles entry and the project reference now use work-github

# Dangling rejection at config load
sed -i '' 's/work-github/ghost/' ~/.hazmat/config.yaml
./hazmat/hazmat config    # loadConfig should error with "not defined in ssh_profiles"
```

## 6) What a reviewer should look at

**Correctness-critical paths:**
- `ValidateProjectSSHProfileRefs` in `hazmat/config.go` — this is the only
  place that rejects dangling references. Worth a careful read.
- `resolveProjectSSHKeyIdentity` and `resolveProjectSSHKeyEntry` in
  `hazmat/git_ssh.go` — the inheritance rule (declared overrides,
  otherwise inherit) lives here and has to match the TLA `Normalize`.
- `runConfigSSHProfileRemove` / `runConfigSSHProfileRename` in
  `hazmat/config.go` — the cascade logic. Make sure partial failures
  can't leave the config in an inconsistent state (they can't today
  because we save after all in-memory edits; worth confirming).

**TLA/Go correspondence:**
- Compare `tla/MC_GitSSHRouting.tla`'s `Normalize(k)` with the Go
  `resolveProjectSSHKeyEntry` inheritance branch and
  `effectiveKeyHosts` — the branches should line up.
- `NoProfileInlineConflict` and `PresentKeysHaveIdentity` in the spec
  correspond to `validateProjectSSHKeyIdentity` in Go.

**Documentation claims vs. reality:**
- `tla/VERIFIED.md § 10` lists the 9 invariants and the state count.
- `docs/usage.md` "Reusable SSH profiles" section describes the inheritance
  model and referrer-safe removal.
- `CHANGELOG.md` Unreleased entry summarizes both stacked features.

## 7) Known limitations / deferred work

- **One socket per key even when two project keys reference the same
  profile.** Intentional — keeps `SocketsDistinctForPresent` unchanged.
  Two agents instead of one is a minor resource cost; we can optimize
  later if it matters.
- **Brokered-transport work (`sandboxing-n1xy`) is unchanged** by this
  work. When it lands, it inherits the routing contract from
  `MC_GitSSHRouting` without spec modifications.
- **No `ssh profile import` or `ssh profile export`.** If a user wants to
  promote an inline project key to a profile, they currently remove and
  re-add. A future `profile import` convenience is possible but not
  needed for this drop.
- **Wildcard overlap detection is partial.** Two overlapping wildcard
  patterns (e.g., `*.example.com` and `*.prod.example.com`) that share
  no concrete host in the declared + default sets pass the static check
  but could collide at session time if a real host matches both. The
  wrapper would then emit two `case` arms for the same host, and shell
  semantics pick the first one — a non-deterministic, non-obvious
  outcome. Reviewer input welcome on whether to make this a hard reject.

## 8) Review checklist

- [ ] `go test ./...` passes
- [ ] `bash tla/check_suite.sh` passes, last spec is `MC_GitSSHRouting`
- [ ] Manual smoke in § 5 runs end to end
- [ ] `ValidateProjectSSHProfileRefs` handles every caller (loadConfig,
      runConfigSSHAdd, resolveProjectSSHKeys)
- [ ] `runConfigSSHProfileRemove --force` leaves the config internally
      consistent when a referring project has mixed inline + profile keys
- [ ] TLA `Normalize(k)` matches Go `effectiveKeyHosts` / resolver
      inheritance behavior
- [ ] VERIFIED.md, usage.md, CHANGELOG all reflect the shipped scope
- [ ] No unrelated changes in the five commits
