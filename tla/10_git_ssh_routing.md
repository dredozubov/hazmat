# Git-SSH Routing — Multi-Key Per-Project

**Status: Proved and wired into `check_suite.sh`; governed code in `hazmat/config.go` and `hazmat/git_ssh.go`.**

## What this governs

Per-project SSH configs with N>=1 named keys, each carrying a declared
destination-host set and an identity-agent socket binding. The routing
relation maps a destination host to either a unique key (with its bound
socket) or a rejection. This spec is wrapper/broker-agnostic: it models the
policy contract, not the transport mechanism.

Governed code, once landed:

- `hazmat/config.go` — config schema, config-set-time host-overlap detection,
  legacy single-key normalization.
- `hazmat/git_ssh.go` — session-time socket allocation and collision check
  (sockets are per-session runtime artifacts, not stored in the config file),
  wrapper socket selection, host allowlist enforcement, `IdentityAgent`
  emission.

Empty configs (`present = {}`) are out of scope for this spec — they
represent "no SSH capability for this project" and never enter the routing
path. The Init clause excludes them.

## Legacy single-key fallback

Exactly one present key with an empty declared host set normalizes to "any
host," preserving today's single-key behavior. Two or more present keys
where any key has an empty declared set is rejected at config-set time.

## Invariants

| Name | What it guarantees |
|------|-------------------|
| `DeterministicRouting` | No destination host resolves to two configured keys in a ready config. |
| `OverlapRejectedAtConfigTime` | A config with two keys whose effective host sets intersect is refused before it reaches a session. |
| `HostsOutsideAllowlistRejected` | A destination host not matched by any configured key is rejected by the wrapper. |
| `LegacyFallbackSingleOnly` | The any-host fallback is only reachable with exactly one present key. |
| `SocketsDistinctForPresent` | No two present keys share an identity-agent socket. |
| `NoCrossKey` | When exactly one key matches the host, the lookup returns that key's name and its bound socket, and no other present key binds to the same socket. |

## Scope boundary

The spec models the routing relation after glob expansion and the
socket-to-key binding. Glob syntax, shell quoting, signal handling,
ssh-agent liveness, and concrete `IdentityAgent` emission (path quoting
around `git_ssh.go:1216`) are out of scope and are covered by code-level
tests.

## Model bounds

Default config:

- `Hosts    = {h_github, h_prod, h_other}`
- `KeyNames = {k_github, k_prod, k_unused}`
- `Sockets  = {s_a, s_b, s_c}`

These bounds exhaustively cover the cases of interest: matching, rejection,
legacy single-key fallback, legacy multi-key rejection, overlap rejection,
and socket collision. Current run: 221,184 distinct states, 2-second TLC
completion.

## How to run

```sh
cd tla
bash run_tlc.sh MC_GitSSHRouting
```

Both `check_suite.sh` and `VERIFIED.md` include this spec.
