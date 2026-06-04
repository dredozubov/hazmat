# Runtime And Contract Ecosystem Alignment

**Date:** 2026-06-04
**Status:** research note; not implementation approval
**Owns bead:** `sandboxing-b2ti`
**Related docs:** [architecture](../architecture.md),
[remote launch envelope schema](../plans/2026-06-02-remote-launch-envelope-schema.md),
[agent sandbox product comparison](2026-05-28-agent-sandbox-product-comparison.md),
[sources](sources.md)

## Executive Summary

The useful market position is:

> Agent instructions and tool annotations describe intent. Hazmat turns a
> session contract into a host-enforced boundary.

That line fits the current codebase. Hazmat already has a redaction-safe
session contract, a backend-neutral containment contract, backend capability
gaps, macOS native enforcement, Docker Sandbox routing, snapshots, and a
remote envelope plan that says wire DTOs are not authority.

The best near-term work is not another broad provider wrapper. It is:

1. Publish a contract vocabulary around `hazmat explain --json` and the
   `sessioncontract` / `containment.Contract` fields.
2. Add integration recipes that map common agent ecosystem contracts into
   Hazmat sessions: AGENTS.md, MCP servers, Docker Sandboxes, and ACP agents.
3. Use that vocabulary for promotion: "MCP/AGENTS/ACP tell the agent what it
   should do; Hazmat tells the OS what it can do."

Remote providers such as Runloop, Daytona, E2B, and Vercel remain good future
adapter targets, but only after the remote runner model is proved. They are
also useful comparison anchors because they already expose network policy,
credential brokering, snapshots, and devbox lifecycle primitives.

## What Counts As A Contract

Hazmat should use "contract" narrowly:

| Contract layer | Examples | Enforcement reality | Hazmat fit |
| --- | --- | --- | --- |
| Agent instruction contract | `AGENTS.md`, repo rules, harness settings | Parsed by agents; overridden by user prompts or model behavior | Good promotion and onboarding target, not a boundary |
| Tool risk contract | MCP `ToolAnnotations`, app/tool descriptors | Hints unless trusted and bound to client policy | Good input to warnings and MCP recipes |
| Runtime authority contract | Hazmat session contract, Docker `sbx` policy, Vercel/Runloop network policies | Enforced by OS, VM, proxy, or control plane | Hazmat's home turf |
| Remote worker contract | Hazmat remote launch envelope, cloud sandbox job specs | Needs signing, replay defense, worker admission, cleanup proof | Future work only |

The category gap is that many ecosystems stop at instructions or tool hints.
Hazmat should keep saying that a real contract has an admission point and an
enforcement mechanism.

## Alignment Matrix

| Solution | What it contributes | Alignment with Hazmat | Integration or promotion path |
| --- | --- | --- | --- |
| [Anthropic `sandbox-runtime`](https://github.com/anthropic-experimental/sandbox-runtime) | Local OS-level wrapper for arbitrary processes, MCP servers, filesystem/network limits, macOS Seatbelt and Linux bubblewrap | Very high concept fit. Same local-process problem, but same-user and research-preview. Hazmat differs through dedicated macOS user, rollback, pf/DNS hardening, credential deny floor, and TLA-governed setup boundaries. | Publish a "SRT vs Hazmat" contract mapping. Add an MCP-server recipe showing how Hazmat contains stdio MCP servers and agent harnesses together. |
| [nono](https://nono.sh/) | Kernel-level local sandboxing language around Landlock/Seatbelt, snapshots, secret isolation, capability sets | High fit as a peer and competitor. Useful vocabulary around capability sets and signed policy. Claims need independent validation before any dependency. | Cross-reference as adjacent work. A comparison post can position Hazmat as macOS-first, dedicated-user, rollback-aware, and formally governed. |
| [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) | Local microVM agent sandboxes, private Docker daemon, network and credential proxy, org policy | Already aligned with Hazmat's Tier 3 route. Docker handles daemon-heavy workflows better than Tier 2 native containment. | Promote Hazmat as the decision layer: use native Hazmat by default, route Docker-heavy repos to Docker Sandboxes, refuse shared host-daemon holes. |
| [MCP](https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations) | Tool schema, annotations such as `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`, plus authorization guidance | Strong contract vocabulary, weak enforcement by itself. The spec explicitly treats annotations as hints unless trusted. | Build "MCP under Hazmat" recipes. Later: `hazmat mcp audit` can classify local stdio MCP servers and suggest read/write/network grants. |
| [AGENTS.md](https://agents.md/) | Cross-agent Markdown instructions for setup, tests, conventions, security considerations | Good adoption surface, no enforcement. It is a standard place to tell agents how to use Hazmat. | Add a copyable `AGENTS.md` security section: run autonomous commands through `hazmat`, use `hazmat explain`, list required integrations. |
| [ACP](https://www.jetbrains.com/acp/) | IDE-to-agent protocol for local, remote, and in-house coding agents | Good future frontend surface. ACP can connect editors to an agent process; Hazmat can contain the process. | Prototype `hazmat acp -- <agent-server>` or a recipe for running ACP-compatible local agents under `hazmat exec`. Promote to JetBrains/Zed ACP ecosystem once stable. |
| [A2A](https://a2a-protocol.org/) | Agent-to-agent discovery, task, auth, and communication protocol | Useful but less immediate. A2A governs inter-agent messages, not local filesystem/network authority. | Watch. Hazmat can later advertise "run local A2A agents in a contained worker," but should not chase this first. |
| [Runloop Devboxes](https://docs.runloop.ai/docs/devboxes/overview) | API-managed devboxes, network policies, snapshots, agent gateways, MCP Hub, browser/computer add-ons | High remote-provider fit. Strong vocabulary for credentials and network policy. | Future remote backend candidate after worker admission and credential-handle model. Near-term: use as comparison for Hazmat's local/private story. |
| [Daytona Sandboxes](https://www.daytona.io/docs/en/sandboxes/) | Dedicated kernel/filesystem/network stack, snapshots, SDKs, network limits, open-source/customer-managed posture | High remote-provider fit, especially if self-host/customer-managed compute matters. | Future adapter candidate. Use Go SDK/API shape as a reference when designing provider-neutral backend gaps. |
| [E2B](https://e2b.dev/docs/sandbox/internet-access) | Mature agent sandboxes, Git/files/commands, network allow/deny lists, public URL controls, many harness examples | Medium-high fit. Simple, mature API, but default internet/public URL posture differs from Hazmat's conservative defaults. | Future adapter candidate. Map Hazmat `network=none/default` and credential policy into E2B network config before launch. |
| [Vercel Sandbox](https://vercel.com/docs/vercel-sandbox/reference/readme) | Firecracker microVMs, short-lived Linux sandboxes, runtime-updatable firewall, credential brokering | High fit for web and preview workflows, less for long local dev sessions. | Future backend for web-app tasks. Good source for language around dynamic egress lock-down and credential brokering. |
| [OpenHands](https://docs.openhands.dev/usage/runtimes/overview) | Agent platform with Docker, process, and remote sandbox providers | Useful as a compatibility target, not a policy primitive. | Add a recipe or compatibility note for running OpenHands process/local components under Hazmat where practical. |
| [Coder Agents](https://coder.com/docs/ai-coder/agents) | Self-hosted agent control plane and workspaces with strict network boundaries | Enterprise workspace fit, not a local macOS replacement. | Position Hazmat as the local/private workstation counterpart. Possible future article: Coder for governed workspaces, Hazmat for governed laptops. |
| [OpenAI Codex sandbox/approvals](https://developers.openai.com/codex/agent-approvals-security#sandbox-and-approvals) | Clear split between technical sandbox and approval policy; cloud runs setup with network, agent phase offline by default | Very good explanatory fit. Mirrors Hazmat's claim that approval policy is not the boundary. | Use this framing in docs and outreach. Hazmat can be the stronger local boundary around Codex CLI/Desktop sessions on macOS. |

## Recommended Sequence

### Wave 1: publish the contract vocabulary

Add one public-facing doc or blog post that explains:

- `AGENTS.md` is an instruction contract.
- MCP annotations are tool-risk hints.
- Approval prompts are workflow gates.
- Hazmat's session contract is an authority contract, because it compiles to
  OS/runtime enforcement.

This is the cleanest promotion angle because it does not depend on new runtime
code. It also gives maintainers and list curators a crisp reason to include
Hazmat: it bridges the agent-contract ecosystem to a local host boundary.

### Wave 2: turn MCP and AGENTS.md into integration surfaces

Low-risk docs and CLI-adjacent work:

- Add a `docs/recipes/mcp-servers.md` recipe covering stdio MCP servers,
  filesystem grants, network mode, and credential caveats.
- Add a copyable `AGENTS.md` section for repos that want agents to run through
  Hazmat.
- Add a "contract examples" page showing `hazmat explain` output beside the
  equivalent mental model for MCP, Docker Sandboxes, and Vercel/Runloop-style
  network policies.

These are community-friendly and do not widen the trust boundary.

### Wave 3: expose a stable contract artifact

If the JSON contract is intended to be consumed outside the CLI, document the
versioned schema and invariants explicitly. The current package split direction
already supports this: `sessioncontract` is redaction-safe and `containment`
owns backend-neutral authority.

Do not let the JSON become authority directly. Keep the same rule as the remote
envelope plan: DTO bytes must parse through constructors before any runtime
sees them.

### Wave 4: adapters only after the proof boundary is ready

Provider adapters for Runloop, Daytona, E2B, or Vercel should wait for the
remote runner model: signed envelope, replay defense, worker identity, path
mapping, credential handles, cleanup proof, and telemetry classification.

ACP is a better near-term adapter candidate than cloud backends because it can
still be local: an ACP-compatible agent server can run under `hazmat exec`
without changing the remote trust model.

## Promotion Lanes

1. **Awesome lists and security maps.** Continue the
   [`awesome-agent-runtime-security`](https://github.com/bureado/awesome-agent-runtime-security)
   lane, but use the shorter positioning: "macOS-native local runtime
   containment with session contracts, dedicated user isolation, Seatbelt, pf,
   rollback, and TLA+ governance."
2. **MCP community.** Pitch Hazmat as the missing local containment layer for
   stdio MCP servers, especially filesystem and developer-tool servers.
3. **AGENTS.md community.** Provide a tiny "secure autonomous mode" snippet
   that any repo can paste.
4. **Docker Sandboxes ecosystem.** Promote the complement, not a replacement:
   Hazmat chooses when native containment is enough and when Docker Sandboxes
   are the right Tier 3 backend.
5. **ACP ecosystem.** Once there is a local recipe, open an issue or PR in ACP
   docs/examples showing `hazmat exec -- <agent-server>`.

## What Not To Do

- Do not call MCP annotations enforceable unless the server is trusted and the
  client binds them to policy.
- Do not open an arbitrary Hazmat plugin model to chase runtime integrations.
  The public roadmap already has a harness-adapter RFC for a safer shape.
- Do not make cloud sandbox adapters before remote credential handles and
  worker admission are modeled.
- Do not weaken the shared-host Docker daemon rule for promotional reasons.
- Do not market Hazmat as a universal cloud sandbox. The differentiated story is
  local/private macOS containment plus a portable authority-contract model.

## Concrete Follow-Ups

Filed follow-up beads:

1. `sandboxing-57l3` - Document Hazmat's public session-contract vocabulary
   and schema boundaries.
2. `sandboxing-jo8a` - Add an MCP server containment recipe.
3. `sandboxing-fud2` - Add an AGENTS.md Hazmat snippet for repo maintainers.
4. `sandboxing-bz9f` - Create an ACP local-agent recipe or spike.

Cloud provider adapters remain deferred behind the remote-envelope and
credential-handle proof work rather than becoming a new implementation issue
from this research note.

## Source Notes

- Docker Sandboxes describe microVM sandboxes with per-sandbox Docker daemon,
  filesystem, network, credential proxy, and org policy:
  <https://docs.docker.com/ai/sandboxes/> and
  <https://docs.docker.com/ai/sandboxes/security/>.
- Anthropic `sandbox-runtime` describes an OS-level wrapper for arbitrary
  processes and MCP servers:
  <https://github.com/anthropic-experimental/sandbox-runtime>.
- MCP `ToolAnnotations` are explicitly hints, not trusted enforcement:
  <https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations>.
- AGENTS.md is an open Markdown instruction format, now used across many
  coding agents:
  <https://agents.md/>.
- Runloop, Daytona, E2B, and Vercel document network policy, snapshots,
  credential brokering, or sandbox lifecycle features relevant to a future
  remote backend:
  <https://docs.runloop.ai/docs/devboxes/overview>,
  <https://www.daytona.io/docs/en/sandboxes/>,
  <https://e2b.dev/docs/sandbox/internet-access>,
  <https://vercel.com/docs/vercel-sandbox/reference/readme>, and
  <https://vercel.com/docs/vercel-sandbox/concepts/firewall>.
- OpenAI Codex security docs explicitly separate sandbox mode from approval
  policy:
  <https://developers.openai.com/codex/agent-approvals-security#sandbox-and-approvals>.
