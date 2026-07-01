# Session Contract Vocabulary

**Status:** public vocabulary for docs, CLI copy, and integration work
**Related code:** `hazmat/sessioncontract`, `hazmat/sessionplanner`,
`hazmat/containment`, `hazmat/sessionbackend`

Hazmat uses "contract" narrowly. A useful contract has an admission point and
an enforcement mechanism. Agent instructions, tool hints, and JSON previews are
useful, but they are not the boundary by themselves. The boundary is created
when Hazmat validates authority-bearing input, compiles it into a backend
artifact, and launches the agent through the runtime that enforces it.

This vocabulary should be used consistently in product docs, CLI output, issue
descriptions, and outreach.

## Contract Layers

| Term | Examples | What it can do | What it cannot do |
| --- | --- | --- | --- |
| Instruction contract | `AGENTS.md`, harness settings, repo conventions | Tell the agent what the project expects. | Enforce filesystem, network, process, or credential boundaries. |
| Tool-risk hint | MCP tool annotations, integration warnings | Help a user or planner classify risk. | Become trusted policy unless a trusted client binds it to policy. |
| Session contract preview | `sessioncontract.Plan`, `hazmat explain --json`, planner DTOs | Describe the resolved session in a redaction-safe, stable shape. | Act as launch authority or mutate host state. |
| Runtime authority contract | `containment.Contract` | Represent the backend-neutral authority Hazmat is willing to enforce. | Launch a process without a backend compiler and runner. |
| Prepared backend artifact | `sessionbackend.PreparedLaunch` plus one typed artifact | Pair a backend plan with exactly one launch artifact after gap checks. | Substitute for runtime enforcement once execution begins. |
| Executed boundary | macOS Seatbelt, Docker Sandbox, future native or remote runners | Enforce the selected contract through the OS, runtime, or control plane. | Prove that advisory hints were truthful. |

The short positioning line is:

> Agent instructions and tool annotations describe intent. Hazmat turns a
> validated session contract into a host-enforced boundary.

## DTOs Are Descriptions

`sessioncontract` is intentionally data-only. It builds redaction-safe request
and plan shapes for previews and frontends. It must not prompt, mutate files,
render SBPL, call Docker, or launch a process.

That means JSON emitted by `hazmat explain --json` is descriptive. It is safe to
read, store, diff, and show to a user, but it must not be treated as authority
because it has already crossed a serialization boundary. If a future command
accepts JSON input, it must parse that data through constructors that rebuild
typed path grants, credential floors, backend plans, and prepared artifacts.

The same rule applies to remote launch envelopes: wire bytes are not authority.
They become actionable only after admission checks, signature or integrity
checks, replay defense, worker identity checks, path mapping, credential handle
validation, and cleanup obligations are satisfied.

## Proxy Evidence Is Not Authority

`proxyruntime.Event` records what a proxy observed and which narrow proxy policy
decision it made. It is audit evidence. It can describe a downstream identity,
attach shape, normalized operation, allow/deny decision, reason, and redaction
markers, but it does not enforce filesystem, network, process, or credential
boundaries by itself.

Proxy policy DTOs are similarly narrow. They can allow or deny by downstream
identity, MCP tool name, HTTP route, or local session-token presence. They do
not replace `containment.Contract`, `sessionbackend.PreparedLaunch`, or the
executed backend boundary. Use "proxy evidence", "proxy policy decision", or
"proxy mediation" for this layer; do not call it runtime authority.

## Authority-Bearing Objects

These objects carry authority inside the process:

- `sessionPlanAuthority` is the CLI-side bridge from resolved flags and config
  to both public DTOs and backend input. It stores normalized, defensive-copy
  data so later caller mutation cannot change the built plan.
- `containment.Contract` is the backend-neutral authority model. It validates
  project access, read-only and read-write grants, agent-home mode, temp scope,
  network mode, service grants, and the structural credential-deny floor.
- `containment.CredentialFloor` is intentionally non-omittable. Backend
  compilers can distinguish a real deny floor from a caller-supplied empty
  slice, and path grants that overlap credential denies are rejected.
- `sessionbackend.PreparedLaunch` is the closed backend artifact wrapper. It is
  constructed only through `NewPreparedLaunch`, validates artifact/backend
  compatibility, and requires every capability gap to be explicitly accepted.

These objects are still not enforcement by themselves. They are the inputs and
artifacts that a runner uses to create the executed boundary.

## Public Field Classification

Use this classification when explaining `hazmat explain --json`,
`sessioncontract.Plan`, and future UI surfaces.

| Field group | Classification | How to describe it |
| --- | --- | --- |
| `routing_reason`, `suggested_integrations`, `integration_sources`, `integration_details`, `integration_warnings`, `session_notes` | Advisory | Planner explanations and recommendations. They help the user decide, but do not grant authority. |
| `repo_setup_summary`, `repo_setup_applied`, `repo_setup_pending` | Report or plan | Describes setup effects already recorded or still pending. Pending effects are not mutations until an approved repair/setup path applies them. |
| `planned_host_mutations` | Plan only | A redaction-safe description of possible host changes. It must not be described as already applied unless the execution path applied it and reported success. |
| `credential_env_grants`, `integration_env_keys`, `integration_registry_env_keys` | Redaction-safe descriptors | Names and IDs that describe credential or env delivery. They must not contain raw secret bytes, host secret-store paths, broker socket paths, or materialized credential files. |
| `project_dir`, `read_only_dirs`, `auto_read_only_dirs`, `user_read_only_dirs`, `read_write_extensions` | Authority input in DTO form | These are intended filesystem grants, but the JSON copy is not authority. Launch code must rebuild typed grants and reject deny-zone overlaps. |
| `network_policy`, `service_access`, `git_ssh_key` | Authority input in DTO form | Describes selected network and service shape. Runtime enforcement still depends on backend compilation and runner support. |
| `snapshot` | Runtime obligation | Describes backup behavior selected for launch. It is not a rollback proof by itself. |
| `session_home` | Experimental preview | Describes planned session-local HOME layout, activation readiness, blockers, phases, and durable bridge roots. It is not active unless the explicit activation path materializes it. |
| `backend`, `capability_gaps`, `lifecycle_artifacts` in backend plans | Backend gating metadata | Describes backend selection, unsupported features, and cleanup obligations. A gap blocks launch unless explicitly accepted where plan-only behavior is allowed. |
| `platform` reports | Host fact report | Describes inspected platform capability, not policy. |

## Check, Doctor, And Repair Wording

Diagnostics follow the same vocabulary:

- `hazmat check` reports findings. It should be read-only, non-interactive, and
  should never require a sudo password.
- A doctor dry run or repair plan is advisory until the user chooses the fix
  path. It should name the exact fix command or automatic repair path, not just
  send the user through a chain of generic commands.
- A doctor fix path is an execution path. It needs explicit consent, clear
  scope, idempotent repairs, and verification that the issue was actually
  fixed on the current machine.
- Optional harness integrations should stay optional in diagnostics unless the
  user selected that harness or the repo contract requires it.

In CLI and docs copy, prefer:

- "Hazmat will enforce these filesystem grants after launch."
- "This JSON previews the selected session contract."
- "This finding has an available fix."
- "This capability gap blocks launch."

Avoid:

- "The JSON enforces access."
- "MCP annotations are policy."
- "Approval prompts are the sandbox."
- "Doctor fixed the issue" unless verification passed.

## Testing Architecture

Contract vocabulary needs tests at each boundary, not only end-to-end smokes.

| Boundary | Tests that should exist |
| --- | --- |
| Public DTO construction | `sessioncontract.BuildPlan` and explain JSON tests should verify defensive copies, stable sorting, redaction-safe credential descriptors, and golden JSON shape. |
| CLI authority bridge | `sessionPlanAuthority` tests should verify that contract and backend inputs are normalized copies and cannot alias mutable config storage. |
| Runtime authority contract | `containment.Contract` tests should reject unconstructed or mutated credential floors, path grants overlapping deny paths, invalid agent-home modes, and unsupported network modes. |
| Backend artifact preparation | `sessionbackend.NewPreparedLaunch` tests should reject missing artifacts, backend/artifact mismatches, and unaccepted capability gaps. |
| Backend compilers | Darwin, Docker, Linux, Apple Container, and future remote compiler tests should reject missing credential floors and preserve deny floors in generated artifacts. |
| Diagnostics and repairs | Check/doctor tests should prove read-only checks do not prompt for sudo, dry-run output names the fix path, fix paths dispatch typed repairs, and post-fix verification catches repairs that did not change machine state. |
| Live validation | Sudo-adjacent smoke tests should be opt-in and named separately from default unit tests so agents ask before running them. |

The default test suite should exercise constructors and DTO invariants without
requiring privileged host state. Live setup, helper-backed checks, native
harness smokes, and push hooks that run those paths are sudo-adjacent and need
explicit user approval.
