# DeepSeek TUI and CodeWhale Harness Candidate Evaluation

Status: Compatibility decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.4.5`
Follow-up implemented: `sandboxing-5na6`
Parent: `sandboxing-lg07.4`

Sources:

- CodeWhale repository: <https://github.com/Hmbown/CodeWhale>
- DeepSeek TUI configuration documentation:
  <https://github.com/Hmbown/DeepSeek-TUI/blob/main/docs/CONFIGURATION.md>
- DeepSeek TUI package entrypoint:
  <https://github.com/Hmbown/DeepSeek-TUI/blob/main/crates/deepseek-tui-cli/src/lib.rs>
- DeepSeek agent index entry:
  <https://github.com/deepseek-ai/awesome-deepseek-agent/blob/main/docs/deepseek-tui.md>
- Open Design DeepSeek runtime definition:
  `/Users/dr/workspace/opendesign/apps/daemon/src/runtimes/defs/deepseek.ts`
- Open Design adapter notes:
  `/Users/dr/workspace/opendesign/docs/agent-adapters.md`

## Decision

Do not add `hazmat deepseek` or `hazmat codewhale` in the next release.

DeepSeek TUI, now continued as CodeWhale, is a plausible future foreground
harness. It is a terminal coding agent with interactive and one-shot command
surfaces, model selection, file edits, shell commands, Git integration, MCP,
sub-agents, and provider credentials. Open Design already drives it as a
single-request adapter with `deepseek exec --auto`, an optional model flag, and
the prompt passed as a positional argument.

The current shape is too loose for immediate first-class Hazmat support.
Upstream uses companion binaries (`deepseek` with `deepseek-tui`, or
`codewhale` with `codewhale-tui`), stores API-key config under user state, can
also read environment credentials, loads workspace-local `.env`, and exposes a
broad auto-approval path through `--auto`. Open Design also has to budget the
prompt as argv bytes, which is a brittle interface for large prompts and
Windows compatibility. The plain stdout stream is workable for recipe use, but
Hazmat needs parser and failure-mode tests before presenting it as a managed
harness.

For now, keep DeepSeek TUI and CodeWhale recipe-only through `hazmat exec` or
`hazmat shell`. `sandboxing-5na6` adds `~/.deepseek` and `~/.codewhale` to
Hazmat's credential deny floor and host credential hardening specs while the
harness remains unsupported as a first-class command. In TLA+, this is covered
by the existing `agentCliStateDir` representative for external agent
CLI/service state roots.

## Upstream Surface

Important surfaces for Hazmat:

- DeepSeek TUI prompts for a DeepSeek API key on first launch and stores it in
  `~/.deepseek/config.toml`.
- CodeWhale stores current config under `~/.codewhale/config.toml` and may
  still read legacy DeepSeek config for compatibility.
- Config can also be supplied through environment variables such as
  `DEEPSEEK_API_KEY`.
- The configuration layer loads workspace-local `.env` files, which creates a
  repo-secret ingestion path that Hazmat should model explicitly before
  shipping a wrapper.
- The dispatcher binary requires a companion runtime binary and returns a
  missing-companion error when that binary is unavailable.
- One-shot automation uses `exec --auto`, which changes the approval posture
  and must not be hidden behind a friendly Hazmat command name.
- Open Design passes the prompt as a positional argument and limits it to a
  30000 byte budget.
- The output surface is plain text rather than typed JSON events.
- The tool surface includes file reads and writes, shell commands, web search,
  Git operations, MCP servers, and sub-agents.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Foreground terminal agent | Strong | Good future adapter candidate |
| One-shot `exec` mode | Mixed | Needs explicit `--auto` policy and fake tests |
| Host config roots | Risky | Deny and harden `~/.deepseek` and `~/.codewhale` |
| Environment API key | Risky | Requires typed credential materialization |
| Workspace `.env` loading | Risky | Must be disabled, fenced, or explicitly modeled |
| Companion binary | Mixed | Add install/version checks before launch |
| Prompt as argv | Mixed | Enforce budget or require upstream stdin support |
| Plain text stream | Mixed | Needs parser/error handling tests |
| MCP and sub-agents | Risky | Require command/env/network allowlisting |

## Recipe-Only Shape

Users who already installed and authenticated the tool inside the contained
agent account can run an interactive session:

```bash
hazmat shell -C ~/workspace/project
deepseek
```

For the renamed CLI:

```bash
hazmat shell -C ~/workspace/project
codewhale
```

A one-shot contained run is possible when the user explicitly chooses the
tool's auto-approval mode and provides a credential through a narrow env grant:

```bash
hazmat exec -C ~/workspace/project -- env DEEPSEEK_API_KEY="$DEEPSEEK_API_KEY" deepseek exec --auto "summarize the current git diff"
```

This is not first-class support. Hazmat contains the process, project paths,
network policy, and credential deny zones, but it does not manage DeepSeek or
CodeWhale auth, companion binaries, `.env` loading, MCP servers, sub-agents,
auto-approval semantics, prompt-size limits, or stream parsing.

## First-Class Requirements

Before `hazmat deepseek` or `hazmat codewhale` is supportable:

- decide whether the built-in harness id is DeepSeek, CodeWhale, or a legacy
  alias pair
- add explicit harness metadata and explain output
- redirect config to a session-local root; never import host `~/.deepseek` or
  `~/.codewhale`
- define a typed `DEEPSEEK_API_KEY` credential grant and reject broad env
  passthrough
- check dispatcher and companion runtime binaries before launch
- disable workspace-local `.env` loading or document and model the exact
  precedence rules
- decide and document the auto-approval posture for `exec --auto`
- replace prompt-as-argv with stdin if upstream supports it, or enforce a
  deterministic prompt byte budget with actionable errors
- add plain-stream parser tests for success text, tool failures, auth errors,
  missing companion binary, empty output, oversized prompts, and schema drift
- add fake CLI coverage for typed credential materialization, host-state
  denial, session-local cleanup, model selection, `.env` policy, MCP rejection,
  and git dirty state
- add manual test notes for an authenticated contained account only after fake
  coverage passes

## Follow-Up

DeepSeek TUI and CodeWhale remain recipe-only until Hazmat owns their state
roots, credential path, companion binary checks, `.env` policy, auto-approval
posture, prompt transport, MCP/sub-agent policy, and stream contract. The
immediate deny-list hardening from `sandboxing-5na6` should ship independently
because it protects users even when the tool is only run through `hazmat exec`.
