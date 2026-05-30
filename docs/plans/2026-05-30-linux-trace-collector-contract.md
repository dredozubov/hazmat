# Linux Trace Collector Contract

`hazmat trace <harness>` is an observational debugging mode. Linux support must
not relax Hazmat containment policy, add harness permissions, or make traced
sessions behave differently except for the tracing wrapper itself. Any elevated
host capability needed for richer Linux observability is optional and must be
recorded as a degraded probe when unavailable.

## Bundle Contract

Every Linux trace bundle writes the shared files already produced by the core
trace orchestration:

- `manifest.json`, with `backend: "linux"`, harness id, launch args, forwarded
  args, timing, transcript/syscall flags, exit code, and error text.
- `harness.json`, `command.txt`, `experiments.md`, and either `explain.json` or
  `explain-error.txt`.
- `indicators.md`, summarizing suspicious lines from backend evidence files.

The Linux backend writes these best-effort evidence files:

- `tool-probe-01-uname.txt`: `uname -a`.
- `tool-probe-02-os-release.txt`: `/etc/os-release` when readable.
- `tool-probe-03-which.txt`: availability of `strace`, `ps`, `journalctl`,
  `dmesg`, `ls`, and `stat`.
- `tool-probe-04-ptrace-scope.txt`: `/proc/sys/kernel/yama/ptrace_scope` when
  present.
- `tool-probe-05-capabilities.txt`: current process capability facts from
  `/proc/self/status`.
- `before-ps.txt` and `after-ps.txt`: `ps` snapshots filtered later through the
  shared process-filter routing.
- `before-proc-self-status.txt` and `after-proc-self-status.txt`.
- `before-agent-*-ls.txt` and `after-agent-*-ls.txt` for declared harness agent
  state paths, using metadata only. Missing users, paths, or permissions are
  captured in the file and do not abort tracing.
- `before-host-*-ls.txt` and `after-host-*-ls.txt` for declared host state paths,
  using metadata only.
- `strace.log` or `strace.log.<pid>` files when syscall tracing is enabled and
  usable.
- `journal.log` and `dmesg.log` only when ordinary user access is available.

The backend must never read credential file contents for snapshots. It should
avoid `/proc/*/environ` by default because environment values can contain
tokens. `strace` output is private diagnostic material and may include prompts,
paths, argv values, request metadata, and error strings; the bundle mode remains
`0700` for the trace root directory and `0600` for files.

## Default Collection

Syscall tracing uses `strace` as the default Linux collector. The preferred
shape is to launch the traced Hazmat child under `strace -ff -ttt -T -s 256 -yy`
and write output with `-o strace.log`. Tracing the child from process start is
more reliable than polling for a process name and attaching later. This means
the Linux implementation may extend the trace backend interface with a launch
wrapper hook, while keeping command construction and manifest writing in the
shared core.

Process discovery uses the existing harness `ProcessFilters` plus `hazmat`,
`agentUser`, and `agentHome`. Linux-specific process sampling can use `ps -eo
pid,ppid,pgid,user,stat,etime,args` and `/proc/<pid>/status` metadata for
matched processes. `/proc/<pid>/cmdline` is acceptable because it is argv-level
evidence comparable to `ps`; `/proc/<pid>/environ` is not collected by default.

`journalctl` and `dmesg` are optional. Many containers and non-root hosts deny
them. The backend should write the attempted command, timeout, and denial text
instead of treating this as a trace failure.

## Degraded Modes

Linux tracing succeeds as a bundle even when some observers fail:

- Missing `strace`: write tool probe output and `trace-errors.log`, skip syscall
  files, and continue with snapshots/log probes.
- Ptrace denied by Yama, seccomp, or container policy: keep the `strace` stderr
  in the relevant log file, record ptrace/capability probes, and return the
  traced harness exit status when the harness actually ran.
- Missing agent user or agent home: write snapshot files with the failure text.
- Missing `journalctl`, denied `dmesg`, or absent `/proc/sys/kernel/yama`: record
  the failed probe and continue.
- `--no-syscalls`: skip `strace` and live process sampling, but still emit tool
  probes, pre/post snapshots, manifest, harness metadata, and indicators.

The traced harness exit code remains authoritative. A probe failure should only
change the trace command exit status if the probe wrapper prevented the harness
from launching at all.

## Docker Test Contract

Ordinary Docker tests must cover behavior that does not require privileged host
state:

- Linux build/compile of trace code.
- Unit tests for command construction, indicator file lists, process-filter
  routing, snapshot file naming, and degraded probe recording.
- A container smoke that runs `hazmat trace codex --no-syscalls --no-transcript
  -- --help` and validates `manifest.json`, `harness.json`, `command.txt`,
  `before-ps.txt`, `after-ps.txt`, and `indicators.md`.

Syscall tracing smoke is separate because Docker hosts vary. It can run in an
image with `strace` installed and should first try ordinary Docker. If the host
requires it, the documented privileged variant may add `--cap-add=SYS_PTRACE`
and a compatible seccomp profile. That variant is useful evidence, but it is
not a default gate for local development.

## Privileged Collector Decision

Privileged Linux host extensions are not enabled by default. The first backend
stays with `strace`, `/proc`, `ps`, `journalctl`, and `dmesg` because those can
degrade cleanly in ordinary Docker and on unprivileged hosts.

| Collector | Signal | Typical requirement | Docker fit | Decision |
| --- | --- | --- | --- | --- |
| `perf trace` | Low-overhead syscall and scheduler context. | `perf_event_paranoid` relaxed, `CAP_PERFMON` or root on many hosts. | Usually needs extra caps and host kernel support. | Future opt-in task only if syscall timing from `strace` is not enough. |
| eBPF / `bpftrace` | Kernel tracepoints, uprobes, richer process/network evidence. | `CAP_BPF`, `CAP_PERFMON`, BTF/kernel config, mounted bpffs, often privileged container. | Not ordinary Docker-safe. | Future opt-in research, not a default trace collector. |
| auditd | Persistent syscall/path audit events. | Host audit daemon policy and root-managed rules. | Host or privileged VM only. | Do not manage audit rules from `hazmat trace`; document manual correlation if users already run auditd. |
| fanotify | File access notification. | `CAP_SYS_ADMIN` for broad watches and careful lifecycle cleanup. | Privileged only. | Defer; too invasive for observational default tracing. |
| seccomp user notification | Policy-level syscall mediation evidence. | Launch policy redesign plus broker process. | Requires containment semantics changes. | Out of scope unless a future verified model changes launch policy first. |
| Cgroup/network telemetry | Per-session CPU, IO, and network counters. | Cgroup ownership or container/runtime integration. | Possible in Docker-specific backend. | Consider later as a separate Docker telemetry task, not part of native Linux trace v1. |

Any future privileged collector must be explicitly enabled by the user, record
its privilege prerequisites in the bundle, and degrade to a written explanation
without altering the traced harness containment policy.
