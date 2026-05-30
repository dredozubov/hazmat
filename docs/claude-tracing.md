# Harness tracing

`hazmat trace <harness>` runs a normal contained harness entrypoint and collects
a host-side trace bundle around it. Use it when Claude Code, Codex, OpenCode, or
Gemini behaves differently inside Hazmat and you need evidence for whether the
trigger is Seatbelt, the agent user, environment variables, network policy,
credential materialization, or the harness runtime itself.

Supported trace targets:

```bash
hazmat trace claude --name baseline -- --no-backup -p "say ok"
hazmat trace codex --name baseline -- --no-backup exec "say ok"
hazmat trace opencode --name baseline -- --no-backup run "say ok"
hazmat trace gemini --name baseline -- --no-backup -p "say ok"
```

Trace flags go before `--`. Normal `hazmat <harness>` flags and harness CLI
flags go after `--`. The traced child launch is run with `--yes` so repo-hook
and setup prompts cannot hang an automated trace.

The same examples work on macOS and Linux. By default, the trace bundle is
written under `~/.hazmat/traces` with mode `0700`; files are written `0600`.

Common bundle files:

| File | Meaning |
| --- | --- |
| `manifest.json` | Trace timing, backend, harness id, forwarded args, exit status, and output dir. |
| `harness.json` | Supported harness metadata, installed probe, watched state paths, and process filters. |
| `command.txt` | The exact traced Hazmat command shape. |
| `explain.json` or `explain-error.txt` | Planned Hazmat session contract, or why it could not be rendered. |
| `terminal.typescript` | PTY transcript when enabled and run from a terminal. |
| `before-*-ls.txt`, `after-*-ls.txt` | Metadata snapshots for declared harness state paths. |
| `process-samples.log` | Sampled process tree around Hazmat, the harness, and the agent user when syscall/process observers are enabled. |
| `indicators.md` | A first-pass grep over noisy logs for audit keywords. |
| `experiments.md` | Suggested follow-up experiments to compare against the run. |

macOS-specific files:

| File | Meaning |
| --- | --- |
| `dtruss.log` | Syscall probe when non-interactive sudo and DTrace permissions allow it. |
| `fs_usage.log` | Filesystem probe when `fs_usage` can observe the harness. |
| `opensnoop.log` | Open-file probe filtered to the harness process name. |
| `unified-log.json` | Unified log extract for Hazmat, the harness, sandbox, deny, and automation signals. |
| `sandbox-log.json` | Focused unified log extract for sandbox and denial events. |

Linux-specific files:

| File | Meaning |
| --- | --- |
| `tool-probe-*.txt` | Kernel, distro, tool availability, ptrace scope, and capability facts. |
| `before-ps.txt`, `after-ps.txt` | `ps` snapshots around launch. |
| `before-proc-self-status.txt`, `after-proc-self-status.txt` | `/proc/self/status` snapshots for the tracing process. |
| `before-proc-process-status.txt`, `after-proc-process-status.txt` | `/proc/<pid>/status` snippets for matching Hazmat/harness/agent processes. |
| `strace.log` or `strace.log.<pid>` | `strace -ff` output, or a degraded-mode explanation when `strace` is missing. |
| `strace-stderr.log` | `strace` stderr and traced child stderr. |
| `journal.log`, `dmesg.log` | Best-effort system logs when ordinary user/container permissions allow them. |

The macOS syscall probes are intentionally started with `sudo -n` so tracing
never hangs on a password prompt. If sudo credentials or DTrace privileges are
not available, the bundle records the failure and still runs the harness
session. Pre-authorize sudo in a separate terminal if you want those probes:

```bash
sudo -v
hazmat trace claude --name baseline -- --no-backup -p "say ok"
```

On Linux, `strace` is used from process start when available. Missing `strace`,
Yama/ptrace restrictions, container seccomp policy, unavailable `journalctl`,
or denied `dmesg` access are recorded as degraded evidence instead of relaxing
Hazmat containment or changing the harness policy. `--no-syscalls` skips
`strace` and live process sampling but still writes the shared bundle files and
pre/post metadata snapshots.

Privileged Linux collectors such as `perf`, eBPF or `bpftrace`, auditd,
fanotify, and seccomp event capture are not enabled by default. They require a
separate opt-in design because they depend on host privileges, kernel config, or
system daemons that ordinary Docker traces do not have.

Regression smoke commands:

```bash
scripts/check-macos-trace-smoke.sh
scripts/check-linux-trace-smoke.sh --skip-if-missing-prereqs
HAZMAT_LINUX_TRACE_SMOKE=1 scripts/check-fast.sh
```

The Linux smoke cross-builds Hazmat, runs it in Docker, tries to install/use
`strace` and `procps`, executes `hazmat trace codex -- --help`, and validates
the bundle shape. It accepts either real `strace.log.<pid>` output or a recorded
degraded `strace.log`/`trace-errors.log` when the container cannot use strace.

Recommended comparison sequence for any harness:

```bash
hazmat trace <harness> --name baseline -- --no-backup <non-interactive args>
hazmat trace <harness> --name network-none -- --no-backup --network none <non-interactive args>
hazmat trace <harness> --name docker -- --no-backup --docker=sandbox <non-interactive args>
```

Treat a hypothesis as strong only when a specific syscall, denied path, log
message, process check, or state mutation appears in failing runs and disappears
in the closest passing control.

For Claude and Codex, Hazmat may also apply the configured permission-bypass
behavior inside the contained session. To compare with the harness' own prompts
enabled, temporarily disable that behavior, rerun the baseline, then restore it:

```bash
hazmat config set session.skip_permissions false
hazmat trace claude --name prompts-on -- --no-backup -p "say ok"
hazmat config set session.skip_permissions true
```

Early signals worth checking first are `mach-lookup com.apple.logd`,
`apple.shm.notification_center`, `/dev/dtracehelper`, keychain/securityd access,
and protected System Policy path probes. These are common macOS runtime probes,
but if they correlate with bad behavior, any policy experiment must start with
the Seatbelt TLA+ model and design note before changing `session_policy_sbpl.go`.
