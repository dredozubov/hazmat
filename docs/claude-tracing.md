# Harness tracing

`hazmat trace <harness>` is developer-only debug tooling. Release builds do not
include the command. Install the debug trace tools only after the prerequisite
check passes:

```bash
sudo -v
make hazmat-debug
```

This installs a developer-only binary and Claude helper outside the source
checkout:

```text
~/.hazmat/bin/hazmat-debug
~/.hazmat/bin/hazmat-trace-claude
```

For interactive Claude Code investigations, start in the affected project and
run the wrapper:

```bash
cd ~/workspace/project-that-reproduces
sudo -v
~/.hazmat/bin/hazmat-trace-claude --name claude-interactive-repro
```

The wrapper prints the debug binary, project directory, trace root, and trace
label before launching Claude. Use Claude normally; exit Claude to finalize the
bundle.

The debug trace command runs a normal contained harness entrypoint and collects a
host-side trace bundle around it. Use it when a supported harness behaves
differently inside Hazmat and you need evidence for whether the
trigger is Seatbelt, the agent user, environment variables, network policy,
credential materialization, or the harness runtime itself.

Supported trace targets:

```bash
~/.hazmat/bin/hazmat-debug trace claude --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace codex --name baseline -- --no-backup exec "say ok"
~/.hazmat/bin/hazmat-debug trace opencode --name baseline -- --no-backup run "say ok"
~/.hazmat/bin/hazmat-debug trace gemini --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace hermes --name baseline -- --no-backup -- --version
~/.hazmat/bin/hazmat-debug trace qwen --name baseline -- --no-backup -p "say ok"
~/.hazmat/bin/hazmat-debug trace cursor-agent --name baseline -- --no-backup -- --version
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
| `terminal.typescript` | PTY transcript. Trace requires terminal stdin. |
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
| `strace.log` or `strace.log.<pid>` | `strace -ff` output. |
| `strace-stderr.log` | `strace` stderr and traced child stderr. |
| `journal.log`, `dmesg.log` | Required system log captures. |

The macOS syscall probes use `sudo -n` so tracing never hangs on a password
prompt. If sudo credentials or DTrace privileges are not available, trace fails
before launching the harness. Pre-authorize sudo in a separate terminal before
running `make hazmat-debug` or the trace wrapper:

```bash
sudo -v
~/.hazmat/bin/hazmat-debug trace claude --name baseline -- --no-backup -p "say ok"
```

On Linux, `strace` is required from process start. Missing `strace`,
Yama/ptrace restrictions, container seccomp policy, unavailable `journalctl`,
or denied `dmesg` access fail the debug trace prerequisite check or runtime
preflight. Trace does not relax Hazmat containment and does not launch a partial
trace bundle.

Privileged Linux collectors such as `perf`, eBPF or `bpftrace`, auditd,
fanotify, and seccomp event capture are not enabled by default. They require a
separate opt-in design because they depend on host privileges, kernel config, or
system daemons that ordinary Docker traces do not have.

Debug smoke commands:

```bash
scripts/check-macos-trace-smoke.sh
scripts/check-linux-trace-smoke.sh
```

These smokes are intentionally not part of normal release gates. They configure
and build a debug Hazmat binary, then validate a full trace bundle. Missing trace
dependencies fail the smoke.

Recommended comparison sequence for any harness:

```bash
~/.hazmat/bin/hazmat-debug trace <harness> --name baseline -- --no-backup <non-interactive args>
~/.hazmat/bin/hazmat-debug trace <harness> --name network-none -- --no-backup --network none <non-interactive args>
~/.hazmat/bin/hazmat-debug trace <harness> --name docker -- --no-backup --docker=sandbox <non-interactive args>
```

Treat a hypothesis as strong only when a specific syscall, denied path, log
message, process check, or state mutation appears in failing runs and disappears
in the closest passing control.

For Claude and Codex, Hazmat may also apply the configured permission-bypass
behavior inside the contained session. To compare with the harness' own prompts
enabled, temporarily disable that behavior, rerun the baseline, then restore it:

```bash
hazmat config set session.skip_permissions false
~/.hazmat/bin/hazmat-trace-claude --name prompts-on
hazmat config set session.skip_permissions true
```

Early signals worth checking first are `mach-lookup com.apple.logd`,
`apple.shm.notification_center`, `/dev/dtracehelper`, keychain/securityd access,
and protected System Policy path probes. These are common macOS runtime probes,
but if they correlate with bad behavior, any policy experiment must start with
the Seatbelt TLA+ model and design note before changing `session_policy_sbpl.go`.
