# Claude Code tracing

`hazmat trace claude` runs the normal `hazmat claude` entrypoint and collects a
host-side trace bundle around it. Use it when Claude Code behaves differently
inside Hazmat and you need evidence for whether the trigger is Seatbelt, the
agent user, environment variables, network policy, or Claude's own runtime
checks.

The traced child Hazmat launch is run with `--yes` so repo-hook and setup
prompts cannot hang an automated print-mode trace.

Start with a short print-mode prompt:

```bash
hazmat trace claude --name baseline -- --no-backup -p "say ok"
```

Trace flags go before `--`. Normal `hazmat claude` flags and Claude Code flags
go after `--`.

By default, the trace bundle is written under `~/.hazmat/traces` with mode
`0700`. Each bundle contains:

- `manifest.json` — trace timing, forwarded args, exit status, and output dir
- `command.txt` — the exact traced Hazmat command shape
- `explain.json` — the planned Hazmat session contract for the run
- `terminal.typescript` — PTY transcript when run from a terminal
- `dtruss.log`, `fs_usage.log`, `opensnoop.log` — syscall/filesystem probes
  when non-interactive sudo/DTrace permissions allow them
- `process-samples.log` — sampled process tree around Claude/Hazmat/agent
- `unified-log.json` and `sandbox-log.json` — macOS unified log extracts for
  Claude, Hazmat, sandboxd, sandbox, deny, and automation signals
- `indicators.md` — a first-pass grep over the noisy logs for audit keywords
- `experiments.md` — suggested follow-up experiments to compare against the run

The syscall probes are intentionally started with `sudo -n` so tracing never
hangs on a password prompt. If sudo credentials or DTrace privileges are not
available, the bundle records the failure and still runs the Claude session.
Pre-authorize sudo in a separate terminal if you want those probes:

```bash
sudo -v
hazmat trace claude --name baseline -- --no-backup -p "say ok"
```

Recommended comparison sequence:

```bash
hazmat trace claude --name baseline -- --no-backup -p "say ok"
hazmat trace claude --name network-none -- --no-backup --network none -p "say ok"
hazmat trace claude --name docker -- --no-backup --docker=sandbox -p "say ok"
```

For a Claude-permission-control comparison, temporarily disable Hazmat's default
Claude permission bypass, rerun the baseline, then restore it:

```bash
hazmat config set session.skip_permissions false
hazmat trace claude --name claude-prompts-on -- --no-backup -p "say ok"
hazmat config set session.skip_permissions true
```

Treat a hypothesis as strong only when a specific syscall, denied path, log
message, process check, or state mutation appears in failing runs and disappears
in the closest passing control.

Early signals worth checking first are `mach-lookup com.apple.logd`,
`apple.shm.notification_center`, `/dev/dtracehelper`, and `/System/Cryptexes`
metadata probes. These are common macOS runtime probes, but if they correlate
with the logout behavior, any policy experiment must start with the Seatbelt
TLA+ model and design note before changing `session_policy_sbpl.go`.
