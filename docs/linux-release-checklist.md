# Linux Release Checklist

Status: checklist only. Linux native support remains split by provider lane and
must not be promoted by a broad "Linux support" claim.

Design sources:

- [Runtime provider status](runtime-provider-status.md)
- [Linux current-user VM smoke matrix](linux-current-user-vm-smoke-matrix.md)
- [Linux agent-user VM lifecycle matrix](linux-agent-user-vm-lifecycle-matrix.md)
- [Linux Run-Agent Readiness Gates](plans/2026-06-13-linux-run-agent-readiness-gates.md)
- [Linux Support Two-Lane Design](plans/2026-06-27-linux-support-two-lane-design.md)

## Current-User Lane

Provider: `linux-current-user`

Minimum release evidence before promotion beyond `plan-only`:

| Gate | Required evidence | Bead |
| --- | --- | --- |
| Contract coverage | Linux consumes the same contract fields as other providers and rejects credential-deny overlaps. | `sandboxing-xuar.3.1` |
| Launch spec compiler | `containment/linux` tests cover mounts, temp, agent home, network, process policy, and gaps. | `sandboxing-xuar.3.1` |
| Admission ordering | `MC_LinuxNativeLaunch` and admission tests prove metadata after enforcement. | `sandboxing-xuar.3.2` |
| Fake helper/result path | Fake-helper tests prove metadata phases, raw streams, failures, and cancellation cleanup. | `sandboxing-xuar.3.4` |
| Runner/result contract | Experimental launch specs carry command argv; runner tests prove gate refusal, gap refusal before side effects, metadata-before-exec, raw stream preservation, and cancellation cleanup through an injected enforcer. | `sandboxing-xuar.3.6` |
| Kernel enforcer | Linux implementation creates rootless user/mount/network namespaces, applies mounts, sets no-new-privs, applies Landlock and seccomp, then execs the harness. | `sandboxing-xuar.3.6` |
| Distro/capability fixtures | Ubuntu, Debian, Fedora, Arch, unknown distro parser/gap fixtures are present. | `sandboxing-xuar.3.5` |
| GitHub-hosted Ubuntu evidence | `.github/workflows/linux-evidence.yml` uploads the Ubuntu current-user transcript on demand and on release tags. | `sandboxing-ebm7` |
| VM smoke matrix | U1, D1, F1, A1 transcripts satisfy S1-S7 in the current-user matrix. | `sandboxing-xuar.3.5` |
| Docs/status | Runtime provider docs keep `linux-current-user` separate and state the correct status. | `sandboxing-xuar.5.1` |

Promotion rule: keep `linux-current-user` at `plan-only` until every gate above
is complete. If any VM row has a typed capability gap, the release claim must
stay experimental or plan-only and name the gap.

## Agent-User Lane

Provider: `linux-agent-user`

Minimum release evidence before promotion beyond `setup-required`:

| Gate | Required evidence | Bead |
| --- | --- | --- |
| Setup model | `MC_SetupRollback` proves Linux agent-user setup graph, rollback order, and destructive rollback boundary. | `sandboxing-xuar.4.1` |
| Setup resource design | The two-lane design names setup labels, diagnostics, dry-run/fix behavior, and rollback flags. | `sandboxing-xuar.4.2` |
| Diagnostics/gaps | Read-only Linux reports emit `linux.setup-required`, helper, cgroup, distro, runtime, and helper-strategy gaps. | `sandboxing-xuar.4.3` |
| Root-helper admission | Unit tests prove `agent-user` + `root-helper`, setup gap refusal, metadata order, and no current-user fallback. | `sandboxing-xuar.4.4` |
| Setup implementation | `internal/setup/linux` implements modeled resources; diagnostics apply/verify setup resources; `hazmat rollback` dispatches modeled Linux rollback callbacks. | `sandboxing-xuar.4.6`, `sandboxing-xuar.4.8`, `sandboxing-r8fx` |
| GitHub-hosted Ubuntu scaffold | `.github/workflows/linux-evidence.yml` uploads an agent-user Ubuntu scaffold transcript for host facts and pending lifecycle rows. | `sandboxing-ebm7` |
| GitHub-hosted Ubuntu lifecycle evidence | `.github/workflows/linux-evidence.yml` has an explicit manual `agent-user-live` lane that uploads setup, root-helper launch, default rollback, and destructive rollback evidence for Ubuntu only. | `sandboxing-3nn1` |
| VM lifecycle matrix | U1, D1, F1, A1 transcripts satisfy A1-A11 in the agent-user matrix. | `sandboxing-xuar.4.5` |
| Docs/status | Runtime provider docs keep `linux-agent-user` separate and state the correct status. | `sandboxing-xuar.5.1` |

Promotion rule: keep `linux-agent-user` at `setup-required` until every gate
above is complete. Do not promote agent-user from fake-helper, admission, or
checklist evidence alone.

## Release Audit Rows

Every `scripts/pre-release-audit.sh` output must include separate rows for:

- `linux-current-user`
- `linux-agent-user`

Each row needs either completed transcript evidence or an explicit skip reason
that preserves the current provider status.
