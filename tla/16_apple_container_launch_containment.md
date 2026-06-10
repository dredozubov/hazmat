# Problem 16 — Apple Container Launch Containment

**Status:** design-proved launch boundary for the planned `apple-container`
backend (epic `sandboxing-kwm2`, bead `sandboxing-x4za`). Design spec:
`docs/plans/2026-06-10-apple-container-backend-design.md`. Closest sibling
spec: `MC_Tier3LaunchContainment` (Docker Sandbox host-side launch boundary).

## Problem Statement

Hazmat plans an `apple-container` backend that runs Linux agent sessions in
Apple Container microVMs via `container run`, with the CLI invoked as the
dedicated macOS `agent` user. Before any executable launch code lands, the
host-side launch boundary must be proved:

1. credential deny paths **and parents of credential paths** are never part
   of the mount plan — including the invoking user's home wholesale and the
   `agent` user's home wholesale;
2. integration env passthrough, SSH agent forwarding (`--ssh`), and socket
   publishing are rejected before any other launch work;
3. backend admission (macOS 26+ Apple silicon, approved CLI path, healthy
   API server, supported version, runnable as `agent`, policy-approved
   image) happens before launch;
4. network policies the backend cannot enforce (`none`, allowlists) fail
   closed rather than launching with a weaker-than-claimed policy;
5. generated credential artifacts (env/secret files) are session-scoped,
   exist only after admission and network gating pass, and are removed at
   session end or the cleanup failure is recorded in session metadata —
   including when `container run` itself fails after materialization;
6. cleanup is by exact session artifact only — containers Hazmat does not
   own are never touched (no `container prune`-style sweeps).

## Model

Linear launch pipeline with explicit failure exits, mirroring
`MC_Tier3LaunchContainment`'s phase style:

```text
0 forbidden-feature gate (integration env / --ssh / socket publish)
1 mount input validation (credential deny zones rejected, compile time)
2 host admission (abstract conjunction, hostAdmitted)
3 network policy enforceability (SupportedNetworkModes = {"default"})
4 credential materialization (session-scoped, only if requested)
5 container start: success | fail w/o residue | fail with container residue
6 session exit (normal | interrupted — same cleanup chain)
7 container cleanup: remove by exact name | record failure
8 credential file cleanup: remove | record failure
9 terminal
```

The path universe extends the Tier 3 model with `agentHome` and a credential
leaf under it (`agentSecretsDir`), so wholesale agent-home mounts are
rejected by the same deny-parent rule that already rejects `invokerHome`.
`ProjectChoices`/`ReadChoices` include the deny paths, so rejection is
exercised, not vacuous. The mount planner (`PlannedReadDirs`) keeps Tier 3's
covered-read-dir filtering semantics.

`StartFailWithResidue` models `container run` failing after the container
record exists; both start-failure variants still enter the cleanup chain, so
a credential file materialized before a failed start cannot leak silently.
A mutation test (start failure jumping straight to terminal) violates
`TerminalCredResidueHandled`, confirming the invariant has teeth.

`ForeignContainersUntouched` is a write-once witness in the genesis style of
`MC_BeadpostBrokerBoundary`: a foreign container chosen at `Init` must
survive every action, which fails if a future edit adds prune-style cleanup.

## Invariants

`CredentialPathsNeverMounted`, `InvokerHomeNeverMounted`,
`AgentHomeNeverMountedWholesale`, `ProjectMountedRW`,
`PlannedReadDirsMountedRO`, `CoveredReadDirsOmitted`,
`NoUnexpectedLaunchEnv`, `IntegrationEnvRejected`, `SSHForwardingRejected`,
`SocketPublishingRejected`, `AdmissionBeforeLaunch`,
`UnsupportedNetworkFailsClosed`, `CredentialMaterializationGated`,
`CredentialArtifactSessionScoped`, `TerminalCredResidueHandled`,
`TerminalContainerHandled`, `ForeignContainersUntouched`.

TLC: "No error has been found" across 134,720 distinct states (246,528
generated, depth 10, ~4s).

## Scope boundary / non-fits

- Apple Container VM internals, VirtioFS UID/GID ownership mapping, guest
  rootfs behavior, and image contents are NOT modeled. The VirtioFS
  ownership question is an explicit host-probe obligation
  (`sandboxing-ajmn`) before the experimental runtime ships.
- `hostAdmitted` abstracts the admission conjunction; concrete version/JSON
  parsing is a unit-test obligation.
- `container machine` (persistent state, home-mount defaults) is explicitly
  out of scope and needs its own model before any machine mode ships.
- Network allowlist/proxy profiles (phase-2 egress design) are out of
  scope; this model only proves they fail closed today.
- Eager credential-file deletion immediately after `container run` consumes
  it is a permitted implementation refinement of phase-8 cleanup; the model
  proves the weaker end-of-session bound.

## Change rules

- Any change to Apple Container mount planning, admission ordering, network
  gating, or credential artifact lifecycle must update
  `MC_AppleContainerLaunch.tla` first and re-run TLC before Go changes.
- Adding a supported network mode (e.g. a proxied allowlist profile)
  requires extending `SupportedNetworkModes` plus new invariants for
  policy-before-launch and network artifact cleanup — not just flipping the
  constant.
- Adding SSH forwarding, socket publishing, or integration env support
  requires updating the forbidden-feature gate and the corresponding
  rejection invariants first.
- Adding `container machine` support requires a separate persistent-state
  model, not an extension of this ephemeral-session spec.
- Any cleanup broader than exact session-owned artifact names must contend
  with `ForeignContainersUntouched`.
