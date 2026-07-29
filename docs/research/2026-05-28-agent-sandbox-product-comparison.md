# Agent Sandbox Product Comparison for External Orchestrators

**Date**: 2026-05-28
**Scope**: Online research into production-ready alternatives and complements to
Hazmat for running coding agents or agent-produced code safely. This is a
strategic input before deeper Codex desktop or Hazmat rework.

## Executive Summary

For production orchestration, it is probably better to use an external
execution provider now and keep Hazmat as a differentiated local/private
workstation sandbox program.

Recommended pilot order:

1. **Runloop Devboxes** for long-running coding-agent workspaces,
   snapshots, browser support, egress policy, compliance posture, and a vendor
   already focused on production coding agents.
2. **Daytona Sandboxes** for an agent-first sandbox API with
   dedicated-kernel isolation, snapshots, multiple SDKs, and an open-source or
   customer-managed-compute path.
3. **Vercel Sandbox** if the immediate workload is web-app, Node/Python,
   preview-server, or Vercel-native. It is a strong short-lived Firecracker
   microVM primitive, but less obviously a full persistent devbox product.
4. **E2B** as a mature, simple API for isolated Linux VMs, computer-use/browser
   use, templates, files, volumes, and agent integrations.

Do not bet production orchestration on the Codex desktop attach path yet. It is
still unproven for host-user GUI state separation. Use cloud or microVM
execution for production, and let Hazmat mature around local trust boundaries
that cloud providers do not own.

## Comparison Table

| Product | Product shape | Isolation boundary | Agent/workspace features | Network and secrets | Production fit now | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| Runloop Devboxes | API-managed virtual workstations for AI agents | Vendor says VM technology and microVM-level hardware isolation for tenants | Full devboxes, snapshots, suspend/resume, custom images, browser support, Python/TS SDKs | Egress policies, Credential Gateway, VPC, SOC2 Type II, HIPAA, GDPR per pricing FAQ | **High** | Strongest agent-specific production story found. Best first pilot if cost and vendor dependency are acceptable. Sources: https://docs.runloop.ai/docs/devboxes/overview, https://runloop.ai/pricing |
| Daytona Sandboxes | Open-source and hosted secure infrastructure for AI-generated code | Dedicated kernel, filesystem, network stack, vCPU/RAM/disk per sandbox | SDK/API/CLI, filesystem, process execution, snapshots, computer use, web terminal, SSH/VNC, customer-managed compute | Network limits, audit logs, billing by consumed sandbox resources | **High** | Good strategic fit if we value open-source posture and self-host/customer-managed compute. Validate auth, egress, durability, and pricing in a small pilot. Sources: https://www.daytona.io/docs/, https://www.daytona.io/docs/billing, https://www.daytona.io/docs/limits |
| E2B | Cloud sandboxes for agents and code execution | Fast secure Linux VM created on demand | Templates, filesystem, volumes, commands, Git integration, metrics, SSH, cloud browser/computer use, Codex/Claude/OpenCode integrations | Internet access controls, secured access, BYOC listed in docs | **High** | Mature and easy to integrate. Good for generic "agent needs a computer" workloads. Need to inspect security/compliance terms and whether isolation is strong enough for our tenant model. Sources: https://e2b.dev/docs, https://e2b.dev/docs/sandbox |
| Vercel Sandbox | Ephemeral Linux microVM primitive exposed through SDK/CLI | Firecracker microVM per sandbox | Run commands, copy files, clone repos, run tests, live preview ports, snapshots; Node/Python emphasis | SDK supports network policies; Vercel platform auth/observability | **Medium-high** | Strong for Vercel/web-app-heavy or needs many short isolated executions. Less ideal for long stateful agent work unless we build workspace persistence. Sources: https://vercel.com/docs/vercel-sandbox/, https://vercel.com/sandbox |
| Modal Sandboxes | Secure containers on Modal compute | Secure container per sandbox, not positioned as microVM in the docs reviewed | Arbitrary commands, images, files, readiness probes, tunnels, snapshots, volumes, GPUs | Modal secrets/env and networking controls; enterprise security features elsewhere | **Medium** | Very capable infra if we already want Modal, GPUs, or batch workloads. Less agent-specific and weaker boundary story than microVM vendors for hostile multi-tenant code. Source: https://modal.com/docs/guide/sandboxes |
| Docker Sandboxes | Docker Desktop/AI local sandboxes for common agents | Docs reference microVM isolation, workspace mounting, networking, security model | Supports Claude Code, Codex, Copilot, Cursor, Gemini, Kiro, OpenCode, shell, templates/kits | Security model includes credentials, policies, org governance | **Medium** | Good local eval and developer workflow option. Not a hosted production backend by itself, and Docker daemon/desktop trust still matters. Source: https://docs.docker.com/ai/sandboxes/ |
| Coder Workspaces / Coder Agents | Self-hosted enterprise workspaces for humans and agents | Depends on customer infrastructure and templates | Workspaces, templates, web/desktop IDEs, agent tasks, AI governance add-ons | Enterprise governance, audit logs, quotas, SSO, network-isolated infra | **Medium** | Strong for enterprise dev environments and governance, not a minimal execution API. Use when managing persistent dev workspaces in our own infra. Sources: https://coder.com/docs, https://coder.com/pricing |
| GitHub Copilot Cloud Agent | Managed coding agent inside GitHub workflow | Ephemeral GitHub Actions-powered development environment | Research, plan, branch changes, run tests/linters, PR workflow, setup steps, custom agents | GitHub Actions secrets/variables, firewall customization, self-hosted runners | **Medium** | Good if we want "delegate issue to agent and get a PR" rather than own the execution substrate. Poor fit when one layer must control arbitrary agents/models/tools. Sources: https://docs.github.com/en/copilot/concepts/agents/cloud-agent/about-cloud-agent, https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/customize-the-agent-environment |
| OpenAI Codex Cloud | Managed OpenAI coding-agent tasks | Isolated OpenAI-managed containers | Checks out repo, runs setup, applies internet settings, edits/runs checks, returns diff/PR path | Setup has network; agent phase offline by default; cloud secrets removed before agent phase | **Medium** | Useful as a product integration, not as an owned execution layer. Strong security defaults but less provider-neutral control. Sources: https://developers.openai.com/codex/agent-approvals-security#sandbox-and-approvals, https://developers.openai.com/codex/cloud/environments#how-codex-cloud-tasks-run |
| OpenHands | Open-source coding agent platform with local/cloud/enterprise modes | Default local runtime is Docker container sandbox | SDK, CLI, local GUI, cloud, enterprise, Docker sandbox, remote/process providers | Depends on deployment; cloud/enterprise adds org features | **Medium-low** | More of an agent product/framework than a hardened general execution substrate. Useful when reusing an agent UI/runtime. Sources: https://docs.openhands.dev/overview/introduction, https://docs.openhands.dev/openhands/usage/sandboxes/docker |
| Browserbase | Browser-agent infrastructure, not general coding sandbox | Isolated browser sessions | Browsers, search/fetch, identity, Stagehand, observability, recordings | Browser identity/proxy/model gateway plans | **Complement** | Strong companion for web/browser tasks. Not enough alone for repo builds, shell execution, or full coding-agent containment. Sources: https://docs.browserbase.com/welcome/what-is-browserbase, https://www.browserbase.com/pricing |
| Emerging microVM/local products: Podflare, InstaVM, Redan, LockedCode | Smaller/newer agent sandbox/security products | Generally claim microVM, dedicated kernel, or OS-level confinement | Fast fork/run_code, local dev, secret injection, policy, audit depending on product | Varies; Redan emphasizes network-layer secret injection; LockedCode emphasizes OS confinement/scanning | **Watchlist** | Worth tracking, but not first production dependency without pilots and security review. Sources: https://podflare.ai/, https://instavm.io/, https://redan.ai/, https://lockedcode.ai/ |

## Orchestrator Recommendation

Use a provider abstraction now:

```text
orchestrated task
  -> execution provider interface
     -> Runloop adapter
     -> Daytona adapter
     -> E2B or Vercel adapter
     -> local Hazmat adapter for private/local mode
```

The first production adapter should not depend on the Codex desktop app. It
should create a clean cloud sandbox/devbox, hydrate a repo or task packet, run
the selected agent or tool loop, stream logs, collect diffs/artifacts, and tear
down or snapshot.

Pilot criteria:

- Can it run our real repo setup, tests, and agent loop without special casing?
- Can we restrict egress to approved hosts?
- Can secrets be delivered without exposing raw host credentials to the agent?
- Can we snapshot/fork/resume enough for long tasks and debugging?
- Can we retrieve a precise diff, logs, artifacts, and execution metadata?
- Can we run enough concurrent jobs without cold-start or billing surprises?
- Can we self-host or VPC-deploy later if customer data requires it?

## Hazmat Direction and Moat

External providers are likely better for production cloud execution today.
Hazmat should avoid becoming another commodity cloud sandbox wrapper. The
differentiated space is:

- **Local private execution**: strong boundaries when source cannot leave a
  developer machine or private workstation.
- **Native desktop/GUI containment**: macOS app state, keychain, TCC, temp
  sockets, browser-use, and GUI side effects are still poorly solved by generic
  Linux sandbox vendors.
- **Bring-your-own-agent enforcement**: consistent policy around Claude, Codex,
  Gemini, OpenCode, Cursor/Copilot-style tools, MCP servers, and local
  package-manager side effects.
- **Credential brokering**: deliver capabilities without raw secret exposure,
  with deny-by-default host-state import rules and auditable grants.
- **Formal policy evidence**: TLA-backed setup/rollback/seatbelt/permission
  repair invariants are unusual and useful for high-trust local execution.
- **Hybrid bridge**: the same orchestrated task can run in Runloop/Daytona in
  production and Hazmat locally for private, interactive, or desktop-specific
  workflows.

## Decision

For production orchestration, start with a Runloop vs Daytona vs E2B/Vercel
bake-off. Treat Hazmat as the local/private execution provider and security
research track. Delay heavy Codex desktop containment rework until we know
which production surfaces remain unsolved by external providers.
