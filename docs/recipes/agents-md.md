# AGENTS.md Hazmat Security Snippet

Use this recipe when you maintain a repository and want coding agents to run
risky autonomous work through Hazmat.

`AGENTS.md` is an instruction contract, not an enforcement boundary. The snippet
below tells agents when to use Hazmat and how to preview a session, but Hazmat's
runtime contract and backend policy still enforce access. See
[../session-contract-vocabulary.md](../session-contract-vocabulary.md) for the
full vocabulary.

## Copyable Section

Replace the placeholders before pasting this into your repo's `AGENTS.md`.

````markdown
## Hazmat Security

Use Hazmat for autonomous commands that execute project code, install or run
tooling, start dev servers, run tests, generate code, or inspect untrusted build
artifacts. Simple file reads, search, and small text edits may run normally.

Preview the contained session before a new workflow shape:

```bash
hazmat explain --for <harness> --integration <integration>
```

Run autonomous shell commands through Hazmat:

```bash
hazmat exec --integration <integration> -- <command>
```

Run an interactive agent through Hazmat:

```bash
hazmat <harness> --integration <integration>
```

Required integrations for this repo:

- `<integration>`: `<why this repo needs it>`

Network posture:

- Default: `<default | none>`
- Use `--network none` for offline review or local-only tests.
- Do not loosen network scope unless the maintainer explicitly asks.

Access rules:

- Treat the project directory as the only writable root unless this file names
  another required writable path.
- If a command needs extra read access, report the exact path and propose the
  smallest `-R <path>` grant.
- If a command needs extra write access, stop and ask before proposing
  `-W <path>`.
- Never request grants for credential directories or host authority paths such
  as `~/.ssh`, `~/.aws`, `~/.config/gcloud`, browser profiles, keychains,
  password managers, socket directories, or host Docker daemon sockets.

Consent rules:

- Do not run `hazmat init`, `hazmat doctor --fix`, `hazmat rollback`, setup
  repair commands, native/live harness smokes, or other host-mutating paths
  unless the maintainer explicitly asks.
- `hazmat explain` is the safe preview path. It describes the planned session
  contract; the JSON or text output is not itself enforcement.
- If Hazmat reports a capability gap or required repair, summarize it and wait
  for maintainer direction instead of chaining more privileged commands.
````

## Filling In The Placeholders

Pick the harness your repo expects agents to use:

- `claude`
- `codex`
- `opencode`
- `gemini`
- `qwen`
- `cursor-agent`
- `exec` for one-shot commands without an agent harness

Name integrations by their Hazmat IDs. Check available integrations with:

```bash
hazmat integration list
hazmat integration show node
hazmat integration show python-uv
```

For example, a TypeScript service might use:

```markdown
Required integrations for this repo:

- `node`: gives read-only access to the resolved Node/Homebrew toolchain and
  applies Node snapshot excludes.
```

A Python project using `uv` might use:

```markdown
Required integrations for this repo:

- `python-uv`: gives read-only access to uv/Python cache roots and applies
  Python snapshot excludes.
```

## What Not To Put In AGENTS.md

Repo-local instructions must not approve host authority. Keep these out of
`AGENTS.md`:

- broad grants such as `-R ~`, `-W ~`, `-R /`, or `-W /`
- credential paths, cloud config directories, keychains, browser profiles, and
  password-manager state
- host Docker socket access for native sessions
- blanket permission to run setup, rollback, doctor fix paths, or sudo-adjacent
  validation
- claims that `hazmat explain --json` enforces access

If the repo truly needs host Docker control, use a Docker Sandbox or VM-oriented
workflow instead of punching the host daemon into native containment. See
[../shared-daemon-projects.md](../shared-daemon-projects.md).

## Maintainer Checklist

Before publishing the snippet:

1. Run `hazmat explain --for <harness> --integration <name>` and confirm the
   contract matches the repo's expected workflow.
2. Keep the required integration list short and source-backed.
3. Prefer `--network none` for offline review, tests that do not need
   downloads, and security-sensitive inspection.
4. Document any extra read-only path as a narrow `-R` grant.
5. Avoid repo instructions that imply an agent may approve host repairs on your
   behalf.

For integration details, read [../integrations.md](../integrations.md). For the
daily user flow and session contract output, read [../usage.md](../usage.md).
