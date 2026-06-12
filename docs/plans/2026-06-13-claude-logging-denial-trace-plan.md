# Claude Logging-Denial Trace Plan

This plan tracks `sandboxing-jeir`: determine whether Claude Code logout or
sandbox-detection behavior correlates with macOS logging-related sandbox denials
seen in an earlier `hazmat trace claude` run.

No policy change is proposed here. Do not add SBPL allowances for logging,
Notification Center shared memory, DTrace helpers, or Cryptex metadata based on
one denial transcript. If a later experiment proposes changing native policy,
start with the governed seatbelt model/design note and run the relevant TLC lane
before editing implementation policy.

## Known Signal

The first captured run mentioned denials around:

- `mach-lookup` for `com.apple.logd`
- `apple.shm.notification_center`
- `/dev/dtracehelper`
- System Cryptex metadata probes

These may be harmless framework probes. The experiment must distinguish
background framework noise from denials that affect Claude auth/session state or
explicit sandbox-detection paths.

## Controls

Use the same Hazmat checkout, Claude version, project, prompt, and network mode
for each comparable run.

1. Baseline contained Claude trace.
2. Network-none contained Claude trace.
3. Nearest passing control, if known: same prompt with a host Claude run or a
   contained run where the reported logout/sandbox-detection symptom does not
   occur.
4. Optional comparison harness trace only if needed to identify macOS framework
   noise common to non-Claude CLIs.

The strongest evidence is a denial or probe that appears in failing Claude runs,
is absent from passing controls, and temporally aligns with the logout,
credential refresh, or sandbox-detection event in the transcript.

## Approval-Gated Commands

The trace path is sudo-adjacent because macOS tracing uses DTrace/dtruss through
`sudo -n`. Agents must ask for exact approval before running these commands.

Prerequisite smoke:

```bash
scripts/check-macos-trace-smoke.sh --run --i-understand-this-runs-sudo-dtrace-probes
```

Interactive Claude reproduction:

```bash
make hazmat-debug TRACE_ACK=1
HAZMAT_TRACE_ROOT="$HOME/.hazmat/traces" \
  "$HOME/.hazmat/bin/hazmat-trace-claude" \
  --i-understand-this-runs-sudo-dtrace-probes \
  --name claude-logging-denial-baseline \
  -C <project>
```

Non-interactive contained trace, if the symptom can be reproduced with a prompt:

```bash
make hazmat-debug TRACE_ACK=1
"$HOME/.hazmat/bin/hazmat-debug" trace claude \
  --out "$HOME/.hazmat/traces" \
  --name claude-logging-denial-baseline \
  -- --no-backup -C <project> -p '<prompt>'
```

Repeat with `--network none` in the forwarded Hazmat arguments when comparing
network effects.

## Evidence Checklist

For each trace bundle, preserve:

- `manifest.json`
- `command.txt`
- `experiments.md`
- `terminal.typescript`
- `dtruss.log`
- `fs_usage.log`
- `opensnoop.log`
- `sandbox-log.json`
- `unified-log.json`
- `indicators.md`

Record the absolute bundle paths in the bead notes. Summarize whether the
logging denials are:

- present in both failing and passing runs, likely framework noise
- present only in failing Claude runs, plausible causal signal
- temporally unrelated to the symptom, low-confidence signal
- accompanied by credential/session file errors, stronger auth-state signal

## Decision Rules

Do not treat the existence of a denial as a bug by itself. Move to policy work
only when the same denial class is reproducibly tied to the user-visible Claude
failure and the nearest passing control lacks the denial.

If the evidence points to auth/session state instead of logging IPC, file a
follow-up for credential/session handling rather than widening SBPL. If the
evidence points to harmless framework probes, close `sandboxing-jeir` with the
bundle references and no policy change.
