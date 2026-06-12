# Qoder CLI Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.4.3`
Follow-up implemented: `sandboxing-3v5g`
Parent: `sandboxing-lg07.4`

Sources:

- Qoder CLI quick start: <https://docs.qoder.com/en/cli/quick-start>
- Qoder CLI usage: <https://docs.qoder.com/en/cli/using-cli>
- Qoder cloud mode: <https://docs.qoder.com/en/cli/cloud-mode>
- Qoder SDK authentication:
  <https://docs.qoder.com/en/cli/sdk/authentication>
- Open Design Qoder runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/qoder.ts`
- Open Design adapter notes:
  `/Users/dr/workspace/opendesign/docs/agent-adapters.md`

## Decision

Do not add `hazmat qoder` in the next release.

Qoder CLI is a plausible future foreground harness. It has an interactive TUI
and a documented print mode with output formats including `stream-json`.
Open Design already treats it as a single-request foreground adapter by running
Qoder in print mode, sending the prompt on stdin, and parsing stream-json
events.

The current shape is still too broad for immediate first-class Hazmat support.
Qoder owns login, account scope, model tiers, workspace memory, permissions,
auto-update, attachments, extra directory grants, and optional cloud mode. Its
non-interactive path needs a permission posture such as `--yolo` or equivalent
bypass mode, and automation can use `QODER_PERSONAL_ACCESS_TOKEN`. Hazmat
should not inherit a host Qoder profile, broad token env, cloud execution
settings, or implicit approval policy.

For now, keep Qoder recipe-only through `hazmat exec` or `hazmat shell`.
`sandboxing-3v5g` adds `~/.qoder` to Hazmat's credential deny floor and host
credential hardening specs while Qoder remains unsupported as a first-class
harness. In TLA+, this is covered by the existing `agentCliStateDir`
representative for external agent CLI/service state roots.

## Upstream Surface

Important surfaces for Hazmat:

- `qodercli` starts an interactive TUI by default.
- TUI slash commands include login, status, config, memory, agents, background
  tasks, review, resume, usage, update, and logout.
- Qoder CLI ships file and shell tools such as Grep, Read, Write, and Bash.
- Startup flags include workspace selection, continue/resume, allowed tools,
  disallowed tools, max turns, and `--yolo` to skip permission checks.
- Print mode is non-interactive and supports text, JSON, and stream-json output
  formats.
- Qoder auth can be interactive/browser/PAT login or
  `QODER_PERSONAL_ACCESS_TOKEN` for automation.
- Login state takes precedence over the environment variable when both exist.
- Automatic upgrade is enabled by default and can be disabled in
  `~/.qoder/settings.json`.
- Cloud mode launches tasks on a Qoder-managed VM and writes the selected cloud
  environment to user-level config.
- `--remote` does not operate on uncommitted local changes; it acts on the
  remote GitHub project/environment instead.
- Open Design forwards absolute skill/design-system roots with `--add-dir` and
  images as `--attachment`.
- Open Design and docs disagree in small ways over exact non-interactive
  permission spelling (`--yolo` versus permission-mode wording), so Hazmat
  needs version-gated fake CLI coverage before shipping.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Print mode | Strong | Good future fake-binary smoke entrypoint |
| `stream-json` output | Strong | Supportable with parser tests and schema drift handling |
| Interactive TUI | Strong | Recipe-only until adapter policy exists |
| `~/.qoder` state | Risky | Deny and harden host state by default |
| PAT env auth | Risky | Requires typed credential materialization |
| Persisted login state | Risky | Do not import host profile |
| `--yolo` / bypass permission | Risky | Never silently choose without documented Hazmat policy |
| `--add-dir` | Mixed | Stage or validate external assets before widening reads |
| Attachments | Mixed | Validate/stage image/file attachments |
| Auto-update | Risky | Disable or route through managed update policy |
| Cloud mode | Backend | Out of scope for local foreground harness support |
| ACP mode | Separate | Route through ACP/RPC driver evaluation if selected |

## Recipe-Only Shape

Users who already installed and authenticated Qoder inside the contained agent
account can run an interactive session:

```bash
hazmat shell -C ~/workspace/project
qodercli
```

A one-shot contained run is possible for users who explicitly accept Qoder's
permission posture:

```bash
hazmat exec -C ~/workspace/project -- qodercli --print --output-format stream-json --yolo "summarize the current git diff"
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage Qoder auth,
PATs, model entitlement, cloud mode, auto-update, workspace memory, allowed
tools, attachments, extra directories, or approval semantics.

## First-Class Requirements

Before `hazmat qoder` is supportable:

- add a built-in `HarnessQoder` entry with explicit metadata and explain output
- use a session-local Qoder config/state root; never import host `~/.qoder`
- define a typed `QODER_PERSONAL_ACCESS_TOKEN` credential grant and reject
  broad env passthrough
- decide and document the non-interactive permission posture; do not silently
  hide `--yolo` behind a friendly command name
- disable auto-update or route updates through managed install/update policy
- reject or explicitly fence cloud mode in local harness sessions
- validate and stage image/file attachments before passing them to Qoder
- validate or stage external skill/design-system directories instead of blindly
  forwarding arbitrary host paths
- add stream-json parser tests for text deltas, errors, usage/cost/duration,
  malformed records, empty output, and schema drift
- add fake CLI coverage for missing auth, typed PAT materialization, host-state
  denial, session-local cleanup, auto-update disabled, permission flags,
  attachment policy, add-dir policy, and git dirty state
- add manual test notes for an authenticated contained account only after fake
  coverage passes

## Follow-Up

Qoder remains recipe-only until Hazmat owns its state root, credential path,
permission posture, update behavior, attachment/add-dir policy, and stream-json
parser contract. The immediate deny-list hardening from `sandboxing-3v5g`
should ship independently because it protects users even when Qoder is only
run through `hazmat exec`.
