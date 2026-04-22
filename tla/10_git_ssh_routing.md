# Git-SSH Routing — Multi-Key Per-Project With Reusable Profiles

**Status: Proved and wired into `check_suite.sh`. Governed code: `hazmat/config.go`, `hazmat/git_ssh.go`. Profile resolution layer modeled; Go side under `sandboxing-nm5o`.**

## What this governs

Per-project SSH configs with N>=1 named keys, each carrying a declared
destination-host set, an identity-agent socket binding, and an optional
reference to a named profile. The routing relation maps a destination host
to either a unique key (with its bound socket) or a rejection. The spec is
wrapper/broker-agnostic: it models the policy contract, not the transport
mechanism.

Governed code:

- `hazmat/config.go` — config schema, config-set-time host-overlap
  detection, legacy single-key normalization, profile/inline mutual-exclusion,
  dangling-profile rejection at config load, profile default-host
  inheritance.
- `hazmat/git_ssh.go` — session-time socket allocation and collision check
  (sockets are per-session runtime artifacts, not stored in the config file),
  wrapper socket selection, host allowlist enforcement, `IdentityAgent`
  emission.

Empty configs (`present = {}`) are out of scope — they represent "no SSH
capability for this project" and never enter the routing path. The Init
clause excludes them.

## Inline keys must declare hosts

Every inline key must declare at least one host. An inline key with no
declared hosts is rejected at config load with a migration snippet. The
legacy any-host fallback that previously admitted such a config has been
retired. Profile-referencing keys are unchanged — they inherit the
profile's `default_hosts` when their own declared host list is empty.

## Profile resolution

A project key may reference a profile by name. When it does and the key
declares no hosts of its own, the profile's `default_hosts` are used.
Project-declared hosts always take precedence over profile defaults — the
spec models this as "if `assignment[k] = {}` then inherit, else use
`assignment[k]` as-is."

A profile reference that does not appear in `definedProfiles` is a
config-load failure. Two project keys may safely reference the same profile;
they still allocate distinct per-session identity-agent sockets, so
`SocketsDistinctForPresent` is unchanged.

## Invariants

| Name | What it guarantees |
|------|-------------------|
| `DeterministicRouting` | No destination host resolves to two configured keys in a ready config. |
| `OverlapRejectedAtConfigTime` | A config with two keys whose effective host sets intersect is refused before it reaches a session. |
| `HostsOutsideAllowlistRejected` | A destination host not matched by any configured key is rejected by the wrapper. |
| `InlineKeysHaveDeclaredHosts` | Every present inline key declares at least one host; inline keys with empty declared hosts are rejected at config load. |
| `SocketsDistinctForPresent` | No two present keys share an identity-agent socket, even when both reference the same profile. |
| `NoDanglingProfileRefs` | Every profile reference in a ready config resolves to a defined profile; dangling references are rejected at config load. |
| `NoProfileInlineConflict` | No present key declares both a profile reference and inline identity material. |
| `PresentKeysHaveIdentity` | Every present key has an identity source (profile reference or inline material); orphan keys are rejected. |
| `NoCrossKey` | When exactly one key matches the host, the lookup returns that key's name and its bound socket, and no other present key binds to the same socket. |

## Scope boundary

The spec models the routing relation after glob expansion, the
socket-to-key binding, and the profile resolution layer above both. Glob
syntax, shell quoting, signal handling, ssh-agent liveness, concrete
`IdentityAgent` emission, and profile rename / removal cascade semantics
are out of scope and remain governed by code-level tests in
`hazmat/git_ssh_test.go` and the CLI surface in `hazmat/config.go`.

Generic profiles for other configuration types (read dirs, backup
destinations, etc.) are intentionally NOT modeled — see the rationale in
`sandboxing-nm5o`.

## Model bounds

Default config:

- `Hosts        = {h_github, h_prod}`
- `KeyNames     = {k_1, k_2}`
- `Sockets      = {s_a, s_b}`
- `ProfileNames = {p_shared, p_other}`
- `NoProfile    = no_profile`

Two profiles with independently-chosen `default_hosts` let TLC explore
configurations where two present keys inherit from distinct profiles,
covering both disjoint and overlapping inherited host sets. The
`inlineMaterial` boolean lets TLC reach the profile+inline conflict state
and the orphan (no-identity) state, so both rejections are proved rather
than assumed away by the model shape. Two hosts are sufficient to witness
all single/pair host-set partitions; the smaller host bound keeps the
state count tractable given the expanded profile and identity variables.

Current run: 884,736 distinct states, ~1 minute TLC completion.

## How to run

```sh
cd tla
bash run_tlc.sh MC_GitSSHRouting
```

Both `check_suite.sh` and `VERIFIED.md` include this spec.
