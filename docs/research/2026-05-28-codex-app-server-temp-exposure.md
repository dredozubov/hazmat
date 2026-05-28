# Contained Codex App-Server Temp Exposure Assessment

**Date**: 2026-05-28
**Scope**: Assess whether the contained Codex app-server path should narrow
Hazmat's native `/private/tmp` and `/private/var/folders` grants before a live
Codex desktop attach smoke.

## Decision

Keep the current temp policy unchanged for the first contained app-server path
and treat broad temp access as documented residual risk.

Do not narrow `compileDarwinSBPL()` in this bead. Native Seatbelt policy
generation is a verified area, and temp narrowing changes both filesystem and
`process-exec` authority. The next implementation step is
`sandboxing-zz6k.8`, which must update `tla/MC_SeatbeltPolicy.tla` and
`tla/02_seatbelt_policy_structure.md`, prove the new design with TLC, and only
then update the native SBPL implementation and tests.

## Current Exposure

The native policy currently grants:

- `file-read* file-write*` under `/private/tmp`
- `file-read* file-write*` under `/private/var/folders`
- `process-exec` under both trees

Those broad grants exist for compatibility with compilers and runtimes that
create temporary build or helper artifacts and then execute them. This is
especially relevant for `go test`, Rust builds, C/CGO/clang flows, shell tools,
and Node-based tooling.

For the Codex app-server harness, the practical consequence is sharper than in
normal Codex CLI use: app-server `fs/*` APIs are server-side capabilities. They
are not protected by Codex's inner CLI sandbox, so Hazmat's outer user,
Seatbelt, and network boundaries are the actual enforcement layer. A contained
app-server can therefore read host-readable files in `/private/tmp` or
`/private/var/folders` until this policy is narrowed.

The host-state classification also identified these temp capability endpoints:

- `/tmp/codex-browser-use/*.sock`
- `/tmp/codex-ipc/*.sock`
- `/var/folders/.../T/codex-ipc/*.sock`

These are not credential files under agent home, so they do not belong in
`credentialDenySubs`. They should be handled by the temp path-policy design.

## Why Not Patch Immediately

Changing temp rules directly would alter a verified policy boundary without the
required model update. It also risks breaking core development workflows because
the current policy relies on broad temp read/write/exec for build tooling.

The safer path is to model the intended rule ordering and test the runtime
effect. Candidate designs include:

1. Per-session temp roots with narrow read/write/exec grants.
2. Explicit deny rules for known Codex App temp/control socket paths after any
   broad temp allow, if Seatbelt enforcement for Unix-domain socket path access
   proves effective.
3. A hybrid design that keeps minimal compatibility grants for system temp
   metadata while routing writable temp output through session-owned roots.

The model should preserve the existing credential deny last-match behavior and
make any new temp/socket deny ordering explicit.

## Test Requirements For Narrowing

`sandboxing-zz6k.8` should not close until autonomous tests cover:

- app-server `fs/readFile` denied for an outside-project, host-readable temp
  file;
- app-server access to Codex App temp/control socket paths denied, or a
  documented reason why the chosen policy cannot enforce that class;
- Go, Rust, Node, shell, and C/CGO temp artifact flows still work;
- `--network none` behavior is unchanged;
- credential and host-state deny tests still pass.

## Residual Risk Until Fixed

Contained app-server sessions are still isolated from host credentials,
project-unrelated home and Library state, and network egress when
`--network none` is selected. However, host-readable files and capability
pathnames under `/private/tmp` and `/private/var/folders` remain in scope for
the contained agent until the model-first temp narrowing work lands.

Do not describe the current app-server path as fully host-temp-isolated.

## Related Beads

- `sandboxing-8tj4`: this assessment.
- `sandboxing-zz6k.8`: model and implement native temp policy narrowing.
- `sandboxing-wsd1`: Codex App host-state classification.
- `sandboxing-zz6k.6`: opt-in live desktop attach smoke, blocked on explicit
  human approval before touching the stock desktop app.
