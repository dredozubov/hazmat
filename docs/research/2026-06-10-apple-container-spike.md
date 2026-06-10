# Apple Container Host Behavior Spike — 2026-06-10

**Bead:** `sandboxing-ajmn` (epic `sandboxing-kwm2`)
**Host:** macOS 26.5 (25F71), Apple silicon (arm64), apple/container **1.0.0**
(commit `ee848e3`), image `alpine:latest`.
**Harness:** `scripts/spike-apple-container.sh` plus supplemental probes below.
Raw transcript: `spike-apple-container-results/spike-20260610-151209.md` (local,
not committed).

## Findings

### F1 — The CLI cannot run as the dedicated `agent` user (design-impacting)

The `container` CLI talks XPC to a **per-user-session** apiserver managed by
launchd in the invoking user's GUI session
(`gui/<uid>/com.apple.container.apiserver`, state under
`~/Library/Application Support/com.apple.container/`).

Observed as `agent` (uid 599, no login session):

- `container system status` → `{"status":"not running", ...}` (empty fields)
- `container ls` → `Error: failed to list containers (cause: "invalidState:
  "unauthorized request"")`
- `sudo -n -u agent container system start` → `Launching
  container-apiserver... Error: failed to get a response from apiserver:
  invalidState: "unauthorized request"`, and the services it bootstrapped ran
  as **dr**, not agent (`launchctl` addressed `gui/599`, which "does not
  support specified action" — the domain doesn't exist without a login
  session).

Consequence: the design doc's identity model ("run the Apple `container` CLI
as Hazmat's dedicated macOS `agent` user") **fails as specified** on 1.0.0.
Admission item 6 of the backend design cannot be satisfied today. Options for
the runtime bead (`sandboxing-ifkc`), all needing a design revision first:

1. Run the CLI as the invoking user and accept that the host-side write
   boundary is the invoking user's authority, with containment resting on the
   VM boundary plus mount planning (weaker than the design's first boundary).
2. Investigate a launchd background/daemon domain bootstrap for an
   agent-owned apiserver (upstream support unclear; may require upstream
   changes or a logged-in agent session, which Hazmat will not create).
3. Keep the backend plan-only until upstream supports a non-session service
   identity.

### F2 — VirtioFS performs host IO as the CLI-invoking user, regardless of guest UID

- Guest default identity is **root** (`uid=0`) when `--user` is not passed.
- With `--user 502:20`, the guest sees `uid=502 gid=20` and guest-side `ls`
  shows `502:20` ownership.
- In both cases, files written to a bind mount appear on the host owned by
  the **invoking user** (observed `501:0`), not the guest UID.
- A guest UID with no host-side permission on the mounted directory could
  still write: VirtioFS executes host IO with the CLI user's authority.

Consequences: `--user` controls only the in-guest identity (still worth
setting — the MVP must always pass a non-root `--user`); the host-side write
boundary is exactly the CLI user's authority, which is why F1 matters; and
the mount plan is the real filesystem boundary, as the proved model assumes.

### F3 — A true no-network mode exists at 1.0.0

`container run --network none` is **special-cased** (it is not in `--help`,
and unknown network names error out: `Error: network does-not-exist-zzz not
found`). The guest comes up with **loopback only** — no eth0, no route — and
egress fails (`nc -z 1.1.1.1 443` → exit 1).

Consequence: the backend's fail-closed rejection of `--network none` stays
correct for now, but a supported network-none mode is plausibly within reach.
Adding it requires extending `SupportedNetworkModes` in
`tla/MC_AppleContainerLaunch` with policy-before-launch invariants and
pinning this behavior in gated smoke tests (per the spec change rules) — it
must not be flipped on from this one observation.

### F4 — Host services bound to 0.0.0.0 are reachable from BOTH default and internal networks

With a bare TCP listener on host `0.0.0.0:894x` (no data served):

- From a `container network create --internal` network (mode `hostOnly`,
  gateway `192.168.128.1`): connect **succeeded**.
- From the builtin `default` NAT network (gateway `192.168.64.1`): connect
  **succeeded**.
- External egress from the internal/hostOnly network fails
  (`1.1.1.1:443` → exit 1); DNS with `--no-dns` fails (no resolver).

This confirms discussion #719 on GA 1.0.0: there is no Hazmat-grade
isolation from host-bound services. The session contract for any future
executable mode must warn that host services on `0.0.0.0` are reachable from
the guest, and no deny-mode/allowlist claim may be made without the phase-2
proxy/firewall model work.

### F5 — Cleanup semantics match the planned contract

- `container rm <exact-name>` removes a named, exited container; nothing
  else was touched, `container ls --all` shows zero residue.
- `--rm` cleans up run-and-exit containers.
- `container volume ls` stayed empty (no anonymous volumes created by bind
  mounts).

### F6 — Admission JSON shapes (for the future probe parser)

- `container system version --format json` → **array** of components:
  `[{"appName":"container","version":"1.0.0",...},
  {"appName":"container-apiserver","version":"container-apiserver version
  1.0.0 (build: release, commit: ee848e3)",...}]` — note the apiserver
  version string is prose, not bare semver.
- `container system status --format json` → object with `status: "running"`,
  `appRoot` (per-user path), `installRoot`.

## What this changes

1. `docs/plans/2026-06-10-apple-container-backend-design.md` — identity model
   needs revision (F1); network-none open question answered (F3); host
   gateway reachability confirmed (F4). Annotated in place.
2. `sandboxing-ifkc` (experimental runtime) is gated on the **identity-model
   decision** from F1, not just on this spike.
3. The compiler's always-pass `--user` posture and never-prune cleanup
   contract are confirmed (F2, F5).
4. `MC_AppleContainerLaunch` needs no change now; a future supported
   network-none mode requires the modeled extension path (F3).
