# Contained Codex App-Server Temp Exposure Assessment

**Date**: 2026-05-28
**Scope**: Assess whether the contained Codex app-server path should narrow
Hazmat's native `/private/tmp` and `/private/var/folders` grants before a live
Codex desktop attach smoke.

## Decision

The initial `sandboxing-8tj4` assessment kept the temp policy unchanged because
native Seatbelt policy generation is a verified area.

`sandboxing-zz6k.8` then implemented the model-first narrowing:

- `TMPDIR`, `TMP`, and `TEMP` now point at an agent-owned per-session temp root
  under `/Users/agent/.cache/hazmat/tmp/`.
- The native SBPL grants read/write/exec for that session temp root.
- The native SBPL no longer grants implicit broad read/write/exec over
  `/private/tmp` or `/private/var/folders`.
- Codex App temp/control socket families get explicit deny rules after
  project/read/temp allows and before final credential denies.

## Original Exposure

Before `sandboxing-zz6k.8`, the native policy granted:

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
`/private/var/folders` unless this policy is narrowed.

The host-state classification also identified these temp capability endpoints:

- `/tmp/codex-browser-use/*.sock`
- `/tmp/codex-ipc/*.sock`
- `/var/folders/.../T/codex-ipc/*.sock`

These are not credential files under agent home, so they do not belong in
`credentialDenySubs`. They should be handled by the temp path-policy design.

## Model-First Rationale

Changing temp rules directly would alter a verified policy boundary without the
required model update. It also risks breaking core development workflows because
the current policy relies on broad temp read/write/exec for build tooling.

The implemented design uses per-session temp roots with narrow read/write/exec
grants, plus explicit deny rules for known Codex App temp/control socket paths.

The model should preserve the existing credential deny last-match behavior and
make any new temp/socket deny ordering explicit.

## Implemented Test Coverage

`sandboxing-zz6k.8` added or extended coverage for:

- TLA model invariants for host temp denial, session temp usability, temp
  socket denial, credential denial, read-dir isolation, project writability,
  and network-none behavior.
- Unit tests proving the generated SBPL does not include broad host temp grants,
  does include session temp grants, and emits Codex temp socket denies after a
  broad user temp grant.
- The app-server smoke proving `fs/readFile` is denied for an outside-project,
  host-readable temp file.
- The app-server smoke proving shell, Node, Rust, and available Go/CGO temp
  artifacts use the agent session temp root. On this machine Go/CGO is skipped
  because the agent-visible `go` binary has no usable `GOROOT`; the probe runs
  when `go env GOROOT` succeeds.
- Existing credential denial and `--network none` app-server smoke checks.

## Residual Risk After Narrowing

Contained app-server sessions are still isolated from host credentials,
project-unrelated home and Library state, and network egress when
`--network none` is selected. Host-readable files under `/private/tmp` and
`/private/var/folders` are no longer implicitly readable just because they are
in temp.

If a user explicitly selects a broad host temp path as the project or a read
dir, non-socket files under that grant are intentionally exposed. Codex App
temp/control socket paths remain explicitly denied as defense in depth.

## Related Beads

- `sandboxing-8tj4`: this assessment.
- `sandboxing-zz6k.8`: model and implement native temp policy narrowing.
- `sandboxing-wsd1`: Codex App host-state classification.
- `sandboxing-zz6k.6`: opt-in live desktop attach smoke, blocked on explicit
  human approval before touching the stock desktop app.
