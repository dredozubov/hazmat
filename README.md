<p align="center">
  <a href="#"><img src="assets/hazmat-final.png" alt="Hazmat" width="400"></a>
</p>

<h1 align="center">Hazmat</h1>

<p align="center">
  <strong>Run AI coding agents with full autonomy on macOS without giving them your real account.</strong><br>
  User isolation + kernel sandbox + firewall + rollback
</p>

---

I built Hazmat because manual approval mode was the worst of both worlds. It broke agent flow, and it still was not a real security boundary.

If an agent gets prompt-injected, runs a poisoned dependency, or follows malicious repo instructions, the important question is not whether it asked politely. The important question is what it can reach.

Hazmat changes that blast radius. The agent runs as a dedicated macOS user inside OS-level containment, not as you.

## Start Here

If you want the shortest path to a first contained session:

```bash
brew install dredozubov/tap/hazmat
hazmat init --bootstrap-agent claude
cd your-project
hazmat claude
```

That gets you:

- a dedicated `agent` macOS user
- a per-session seatbelt policy
- firewall and DNS hardening
- automatic pre-session snapshot and restore
- a session contract that tells you exactly what the agent can touch

If you want to preview before changing anything, run `hazmat init --dry-run` or `hazmat explain`.

If you use Codex, OpenCode, or Gemini instead of Claude, start with [docs/harnesses.md](docs/harnesses.md).

## What a Session Looks Like

Every session starts with a contract. No hidden widening, no vague "secure mode" label.

```
hazmat: session
  Mode:                 Native containment
  Why this mode:        using native containment by default (Docker routing: none)
  Project (read-write): /Users/dr/workspace/my-app
  Integrations:         go
  Auto read-only:       /Users/dr/go/pkg/mod
  Read-only extensions: none
  Read-write extensions: none
  Service access:       none
  Pre-session snapshot: on
  Snapshot excludes:    vendor/
```

That contract is the product. You can inspect it with `hazmat explain` before launch, and you can tell at a glance what changed for the session.

## Why This Exists

`--dangerously-skip-permissions` is where the real productivity is. It is also where the real blast radius is.

The category keeps proving the point:

- **Agents actively reason about escaping.** Ona showed Claude Code [bypassing its own denylist](https://ona.com/stories/how-claude-code-escapes-its-own-denylist-and-sandbox) via `/proc/self/root`, then trying to disable bubblewrap when that path was closed.
- **The CVEs are not hypothetical.** Hazmat tracks [16 Claude Code CVEs](docs/cve-audit.md), including [CVE-2025-59536](https://nvd.nist.gov/vuln/detail/CVE-2025-59536) and [CVE-2026-21852](https://nvd.nist.gov/vuln/detail/CVE-2026-21852).
- **Supply chain attacks are fast enough to beat human supervision.** The 2026 axios compromise delivered a RAT through a `postinstall` hook in about two seconds.

So the design goal is not "make the agent behave." The design goal is "make autonomous failure less catastrophic."

## What Hazmat Actually Does

```bash
hazmat claude
hazmat codex
hazmat opencode
hazmat exec ./my-agent-loop.sh
hazmat shell
```

| Layer | What it does |
|-------|--------------|
| **User isolation** | Runs the agent as a dedicated `agent` macOS user, so your real home directory is structurally out of reach |
| **Kernel sandbox** | Generates a per-session [seatbelt](https://developer.apple.com/documentation/security) policy with explicit read-write and read-only scope |
| **Credential deny** | Blocks common secret paths at the kernel level, including credential paths inside agent home |
| **Network firewall** | Uses `pf` to block common exfiltration and tunneling protocols |
| **DNS blocklist** | Redirects known tunnel, paste, and capture domains to localhost |
| **Supply chain hardening** | Applies conservative defaults such as npm `ignore-scripts=true` |
| **Snapshots and restore** | Takes a pre-session Kopia snapshot so you can diff or roll back |

## What Works Today

Current state, not aspirational state:

- **macOS native containment is the default path.** Hazmat ships release artifacts for `darwin/arm64` and `darwin/amd64`.
- **Four harnesses are supported in containment.** Claude Code, Codex, OpenCode, and Gemini. Details, tested versions, and auth flows live in [docs/harnesses.md](docs/harnesses.md).
- **Docker support is real, but selective.** Private-daemon Docker workflows can use Docker Sandbox mode. Shared host-daemon workflows stay code-only by default. See [docs/tier3-docker-sandboxes.md](docs/tier3-docker-sandboxes.md) and [docs/shared-daemon-projects.md](docs/shared-daemon-projects.md).
- **Integrations exist for common stacks.** Hazmat currently ships built-in integrations for Go, Node, Rust, Python, Java, TLA+, Terraform/OpenTofu, Beads, and more. See [docs/integrations.md](docs/integrations.md).
- **Repo-local Git hooks have a Hazmat-managed approval path.** Repos can declare `pre-commit`, `commit-msg`, and `pre-push` in `.hazmat/hooks/hooks.yaml`; approval, install, drift review, and uninstall flow through `hazmat hooks ...`.
- **Core behavior is tested and partially formally verified.** The exact proof boundary is explicit in [tla/VERIFIED.md](tla/VERIFIED.md). If something is not listed there, do not assume a proof exists.

## Limitations I Am Not Hiding

Hazmat is useful because the boundaries are concrete. That also means the limitations should be concrete.

- **macOS only today.** Linux is intentionally compile-only until setup and rollback resources are modeled and implemented. See [docs/testing.md](docs/testing.md).
- **This is not a total network allowlist.** HTTPS exfiltration to a brand-new domain is still not fully solved by Tier 2. See [docs/threat-matrix.md](docs/threat-matrix.md).
- **The DNS blocklist is exact-domain, not wildcard.** It is based on `/etc/hosts`, not a full DNS filtering stack. See [docs/design-assumptions.md](docs/design-assumptions.md).
- **Shared `/tmp` stays shared.** Hazmat does not pretend macOS temp space suddenly became private.
- **MCP env inheritance and `SSH_AUTH_SOCK` abuse are still category-wide problems.** Some of the hardest issues here are operational, not just architectural. They are called out directly in [docs/threat-matrix.md](docs/threat-matrix.md).
- **Docker Sandbox mode now covers every harness entrypoint.** `hazmat claude`, `hazmat codex`, `hazmat opencode`, `hazmat gemini`, `hazmat shell`, and `hazmat exec` can all route into private-daemon Docker Sandbox sessions when the repo needs it.

If you are dealing with hostile repos, long unattended runs, or shared-daemon Docker workflows, the honest answer may be Tier 4, not stretching Tier 2 past what it does well. Start with [docs/overview.md](docs/overview.md).

## Community Map

I want community help here, but I do not want to pretend every part of Hazmat is equally easy or equally safe to crowdsource.

### Best Places to Help

- **Integrations and stack coverage** - new manifests, detection fixes, better snapshot excludes, compatibility reports
- **Harness usability** - bootstrap friction, auth/import bugs, first-run UX, docs for real setups
- **Docs and onboarding** - quickstart clarity, explain-mode examples, screenshots, diagrams, troubleshooting
- **Research and evidence** - CVE tracking, incident writeups, comparative analysis, drift checks
- **Test matrix expansion** - real repo validation, macOS version coverage, harness regression repros

### Areas That Need Deeper Review

- **Seatbelt policy changes**
- **`pf` firewall behavior**
- **setup / rollback ordering**
- **credential delivery and capability brokering**
- **anything covered by the TLA+ governance rules**

If you want to contribute, [CONTRIBUTING.md](CONTRIBUTING.md) is the starting point. If you want to understand which claims are modeled versus just tested, read [tla/VERIFIED.md](tla/VERIFIED.md) and [docs/design-assumptions.md](docs/design-assumptions.md) first.

## Daily Use

```bash
# Claude Code
hazmat claude
hazmat claude -p "refactor the auth module"

# Other supported harnesses
hazmat codex
hazmat opencode
hazmat gemini

# Any command in containment
hazmat exec -- make test
hazmat exec -- /bin/zsh -lc 'uv run pytest -q'

# Interactive shell
hazmat shell

# Review and recovery
hazmat diff
hazmat snapshots
hazmat restore
```

You can expose more paths explicitly when you need them:

```bash
hazmat claude -R ~/reference-docs
hazmat claude -W ~/.venvs/my-app
hazmat config access add -C ~/workspace/my-app --read ~/reference-docs --write ~/.venvs/my-app
```

And if the repo needs integration hints:

```bash
hazmat integration list
hazmat integration show node
hazmat claude --integration node
hazmat config set integrations.pin "~/workspace/my-app:node,go"

# Repo-local Git hooks
hazmat hooks status
hazmat hooks install
hazmat hooks review
hazmat hooks uninstall
```

## Architecture In One Screen

```
  You (dr)                          Agent (agent)
  --------                          -------------
  ~/                                /Users/agent/
  ~/.ssh, ~/.aws  <- denied ->      ~/.claude/
  ~/workspace/    <- shared ->      ~/workspace/ (symlink)

  hazmat claude
       |
       |- snapshot project (Kopia)
       |- generate per-session seatbelt policy
       |- sudo -u agent hazmat-launch <policy>
       |    |- apply sandbox-exec
       |    `- exec harness
       |
       `- pf firewall already active
```

The important property is structural separation. The agent is not "forbidden from reading your SSH key while still running as you." It runs as a different user entirely.

## Read Next

| Doc | Why you would read it |
|-----|------------------------|
| [docs/usage.md](docs/usage.md) | Full user guide once you are past the first session |
| [docs/overview.md](docs/overview.md) | Which tier to use, and when |
| [docs/threat-matrix.md](docs/threat-matrix.md) | Risk-by-risk coverage and documented caveats |
| [docs/harnesses.md](docs/harnesses.md) | Harness setup matrix for Claude, Codex, OpenCode, Gemini |
| [docs/integrations.md](docs/integrations.md) | How integrations work, and what they are not allowed to do |
| [docs/integration-author-kit.md](docs/integration-author-kit.md) | How to propose integrations without turning them into policy escapes |
| [docs/community.md](docs/community.md) | Support tiers, ownership model, sponsor lanes, and contribution surfaces |
| [docs/public-roadmap.md](docs/public-roadmap.md) | Curated public roadmap exported from beads issues |
| [docs/compatibility.md](docs/compatibility.md) | Compatibility status meanings, matrix shape, and reporting flow |
| [docs/recipes/README.md](docs/recipes/README.md) | Community-expandable recipes for common harness + stack workflows |
| [docs/testing.md](docs/testing.md) | What is tested locally, in CI, and in destructive VM-backed flows |
| [docs/git-hooks.md](docs/git-hooks.md) | Why Hazmat's repo-local hook flow is stricter than plain Git hooks |
| [docs/manual-testing.md](docs/manual-testing.md) | Human-driven verification checklist (run before releases / after harness or seatbelt changes) |
| [docs/design-assumptions.md](docs/design-assumptions.md) | Non-obvious design decisions and known tradeoffs |
| [docs/cve-audit.md](docs/cve-audit.md) | How Hazmat maps against known Claude Code CVEs |
| [tla/VERIFIED.md](tla/VERIFIED.md) | Exact formal verification scope and governance rules |

## Background

[How I Made --dangerously-skip-permissions Safe in Claude Code](https://codeofchange.io/how-i-made-dangerously-skip-permissions-safe-in-claude-code/)

## Security

If you find a containment bypass, credential leak, sandbox escape, or other security issue, please use the private reporting path in [SECURITY.md](SECURITY.md).

## License

MIT

---

<sub>The Simpsons and all related characters are property of 20th Television and The Walt Disney Company. The Claude logo is property of Anthropic. We do not claim any rights to these properties. Their use here is purely for entertainment purposes.</sub>
