# OpenClaw Harness Candidate Evaluation

Status: Monitor/service-platform decision plus deny-list hardening
Date: 2026-06-13
Related issue: `sandboxing-lg07.7.9`
Follow-up implemented: `sandboxing-wgow`
Parent: `sandboxing-lg07.7`

Sources:

- OpenClaw overview: <https://docs.openclaw.ai/>
- OpenClaw security guide: <https://docs.openclaw.ai/gateway/security>
- OpenClaw skills guide: <https://docs.openclaw.ai/tools/skills>
- OpenClaw repository: <https://github.com/openclaw/openclaw>
- Service harness boundary:
  [2026-06-12-service-harness-boundary-design.md](2026-06-12-service-harness-boundary-design.md)
- Local threat research:
  [docs/research/security-evidence.md](../research/security-evidence.md)

## Decision

Do not add `hazmat openclaw` in the next release.

OpenClaw is not a focused foreground coding harness. Current docs describe a
self-hosted gateway that bridges chat apps, plugins, mobile nodes, a browser
Control UI, CLI, and one or more AI agents. Its useful shape is a long-running
operator control plane with credentials, message channels, tools, skills,
memory, browser/media surfaces, node pairing, and remote access. That belongs
behind Hazmat's service-harness boundary, not the normal harness registry.

The immediate Hazmat action is host-state hardening. OpenClaw's own docs place
config at `~/.openclaw/openclaw.json` and map credentials, allowlists, model
auth profiles, Codex runtime state, secrets, and shared managed skills under
`~/.openclaw`. Local Hazmat threat research also treats `~/.openclaw` as a
high-value credential directory targeted by commodity infostealers.
`sandboxing-wgow` adds `~/.openclaw` to Hazmat's credential deny floor and host
credential hardening specs while OpenClaw remains monitor/service-platform
scope. In TLA+, this is covered by the existing `agentCliStateDir`
representative for external agent CLI/service state roots.

## Upstream Surface

Important surfaces for Hazmat:

- OpenClaw is a gateway, not just a CLI; one Gateway process is the source of
  truth for sessions, routing, and channel connections.
- It supports chat apps and channel plugins such as Discord, Google Chat,
  iMessage, Matrix, Microsoft Teams, Signal, Slack, Telegram, WhatsApp, Zalo,
  WebChat, and mobile nodes.
- The Control UI defaults to `127.0.0.1:18789`; remote access and tailnet
  setups are documented separately.
- Quickstart includes `openclaw onboard --install-daemon`, which is
  service-installing behavior and sudo-adjacent in spirit even when the package
  manager path is user-level.
- OpenClaw's security guide explicitly assumes a personal-assistant trust
  model, not hostile multi-tenant isolation.
- The security guide treats Gateway operator access as a trusted control-plane
  role and recommends separate gateways, OS users, or hosts for separate trust
  boundaries.
- Default trusted personal-assistant setup can allow host exec without approval
  prompts unless tightened.
- Credentials and state under `~/.openclaw` include channel credentials,
  allowlists, model auth profiles, Codex runtime home, optional secrets file,
  and legacy OAuth import.
- Skills load from workspace, project-agent, personal-agent, managed
  `~/.openclaw/skills`, bundled, extra-dir, and plugin skill roots.
- Skills can inject env and API keys into the host agent run for a turn; the
  docs emphasize this is not sandbox injection.
- ClawHub and uploaded/Git/local skill install paths add supply-chain and
  operator-policy surfaces.
- Plugins, browser control, media, nodes, exec tools, sandbox settings,
  telemetry/metrics, remote access, and diagnostics are all separate policy
  surfaces.

## Hazmat Fit

| Surface | Fit | Decision |
|---|---|---|
| Gateway process | Service harness | Do not wrap as foreground CLI |
| Control UI | Service/browser boundary | Wait for local attach, token, log-redaction, and UI policy |
| Channel plugins | Risky | Out of scope until credential/channel admission exists |
| Mobile/remote nodes | Risky | Requires remote node admission and paired-device authority model |
| Host exec defaults | Risky | Never inherit OpenClaw defaults as Hazmat policy |
| `~/.openclaw` | Risky | Deny and harden host state by default |
| Skills/ClawHub | Risky | Requires install policy, scanning, allowlists, and staged inputs |
| Skill env/API-key injection | Risky | Requires typed SecretRef/credential policy |
| Browser/media tools | Risky | Requires explicit TCC/browser/media policy |
| Remote access/tailnet | Backend | Monitor only; not a local harness |
| Security audit command | Useful | Future adapter can consume reports, not replace Hazmat policy |

## Recipe/Monitor Shape

OpenClaw should remain monitor-only for the main harness docs. A user can still
run OpenClaw manually inside a Hazmat shell for experimentation, but Hazmat
must not claim it manages the Gateway lifecycle or policy:

```bash
hazmat shell -C ~/workspace/project
openclaw dashboard
```

That is deliberately not a recommended setup recipe. It does not manage daemon
installation, Control UI tokens, channel credentials, node pairing, browser
profiles, plugin trust, skill installs, remote access, sandbox settings, or
cleanup. Any live OpenClaw experiment should be treated like a guarded manual
smoke under the service-harness research plan, with no sudo-adjacent commands
run by the agent.

## First-Class Requirements

Before OpenClaw can become first-class:

- complete the service-harness lifecycle model and fake-service suite
- start only session-scoped Gateway processes by default; never install a
  persistent daemon as a side effect of `hazmat init` or `hazmat openclaw`
- bind Control UI/attach surfaces to loopback or a Unix socket with a
  per-session token, and redact full secret-bearing URLs
- record service metadata for crash recovery and cleanup
- use a session-local OpenClaw home; never import host `~/.openclaw`
- define typed credentials for provider, channel, node, browser, GitHub, and
  remote-access authorities before exposing them
- default channel plugins, remote nodes, browser control, media tools, skills,
  ClawHub installs, and uploaded archives to disabled or explicitly allowlisted
- treat skill env/API-key injection as a credential delivery path, not prompt
  context
- reject host exec defaults that grant broad command execution without Hazmat
  policy
- decide how OpenClaw's own sandboxing, audit, and tool policy compose with
  Hazmat instead of treating them as substitutes
- add fake-service tests for readiness, attach, auth, denied binds, denied host
  state, channel credential rejection, skill install rejection, node pairing,
  crash cleanup, logs, redaction, and git dirty state

## Follow-Up

OpenClaw remains monitored service-platform research until Hazmat has a proved
service lifecycle and a concrete product reason to manage a multi-channel
agent gateway. The immediate deny-list hardening from `sandboxing-wgow` should
ship independently because it protects users even when OpenClaw is only present
on the host or run manually through a contained shell.
