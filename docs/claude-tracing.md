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

By default, the trace bundle is written under `~/.hazmat/traces` with mode
`0700`. Each bundle contains:

- `manifest.json` — trace timing, harness id, forwarded args, exit status, and output dir
- `harness.json` — supported harness metadata, installed probe, watched state paths, and process filters
- `command.txt` — the exact traced Hazmat command shape
- `explain.json` — the planned Hazmat session contract for the run
- `terminal.typescript` — PTY transcript when run from a terminal
- `process-samples.log` — sampled process tree around Hazmat, the harness, and the agent user
- `indicators.md` — a first-pass grep over the noisy logs for audit keywords
- `experiments.md` — suggested follow-up experiments to compare against the run

On macOS, bundles can also include `dtruss.log`, `fs_usage.log`,
`opensnoop.log`, `unified-log.json`, and `sandbox-log.json` when
non-interactive sudo, DTrace, and unified-log access allow those probes.

On Linux, bundles include tool probes, `before-ps.txt` / `after-ps.txt`,
`before-proc-self-status.txt` / `after-proc-self-status.txt`, matching
`/proc/<pid>/status` snapshots, optional `journal.log` / `dmesg.log`, and
`strace.log.<pid>` files when `strace` is installed and permitted.

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
