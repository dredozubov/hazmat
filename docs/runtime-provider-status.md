# Runtime Provider Status

Hazmat reports runtime providers with one vocabulary. A provider row describes
one authority lane, not a broad platform family. The status controls what the
CLI, docs, compatibility rows, and release checklists may claim.

## Status Vocabulary

| Status | Executable | Meaning |
| --- | --- | --- |
| `supported` | yes | Provider may launch when admission succeeds and required setup is present. |
| `experimental` | yes | Provider may launch only behind explicit experimental controls and evidence gates. |
| `plan-only` | no | Provider can preview plans, structured gaps, and release blockers, but must not launch. |
| `setup-required` | no | Provider needs modeled persistent setup resources before admission can become executable. |
| `unsupported` | no | Provider is registered only to explain why this route is unavailable. |

## Provider Lanes

| Provider | Host platform | User mode | Backend | Status | Identity boundary | User-facing claim |
| --- | --- | --- | --- | --- | --- | --- |
| `macos-current-user` | `macos` | `current-user` | `darwin-native` | `plan-only` | `current-user` | macOS same-user contract sandboxing is a planned lane; no executable runner claim yet. |
| `macos-agent-user` | `macos` | `agent-user` | `darwin-native` | `supported` | `macos-agent-user` | macOS native containment with the configured Hazmat agent user. |
| `docker-sandbox` | `container` | `container-user` | `docker-sandbox` | `supported` | `container-user` | Docker Sandbox private-daemon workflows, not shared host Docker socket authority. |
| `apple-container` | `container` | `container-user` | `apple-container` | `experimental` | `container-user` | Apple Container launch is experimental and explicitly gated. |
| `linux-current-user` | `linux` | `current-user` | `linux-native` | `plan-only` | `current-user` | Linux current-user planning and gaps only; no executable native run-agent claim yet. |
| `linux-agent-user` | `linux` | `agent-user` | `linux-native` | `setup-required` | `linux-agent-user` | Linux multi-user setup/runtime is model-first and not available until setup resources land. |
| `remote-envelope` | `remote` | `remote-worker` | `remote-envelope` | `plan-only` | `remote-worker` | Remote launch envelope preview only; no worker admission or runner semantics yet. |
| `unsupported-native` | `unsupported` | `none` | `unsupported-native` | `unsupported` | `none` | Native launch is unavailable for this platform or route. |

## Gap Rules

Unsupported authority must be reported as structured gaps, not as fallback
behavior. Important gap IDs include:

| Gap | Applies to | Meaning |
| --- | --- | --- |
| `macos.current-user-runner-missing` | `macos-current-user` | macOS current-user native runner is not implemented or admitted. |
| `linux.native-launch-helper-missing` | `linux-current-user` | Native Linux runner is not enabled in the session pipeline or lacks VM smoke evidence. |
| `linux.runtime-not-linux` | `linux-current-user` | The inspected runtime is not Linux. |
| `linux.user-namespace-unavailable` | `linux-current-user` | Rootless current-user admission lacks user namespaces. |
| `linux.mount-namespace-unavailable` | `linux-current-user` | Mount namespace enforcement is unavailable. |
| `linux.network-namespace-unavailable` | `linux-current-user` | `network=none` cannot be enforced. |
| `linux.landlock-unavailable` | `linux-current-user` | Landlock policy cannot be applied. |
| `linux.seccomp-unavailable` | `linux-current-user` | Seccomp policy cannot be applied. |
| `linux.cgroup-v2-unavailable` | Linux lanes | Cgroup v2 resource controls are unavailable. |
| `linux.distro-unsupported` | Linux lanes | The inspected distro is outside the validated Linux matrix. |
| `linux.setup-required` | `linux-agent-user` | Persistent agent-user setup resources are missing or not modeled. |
| `linux.helper-strategy-unsupported` | Linux lanes | Requested helper strategy does not match the selected identity lane. |

Provider admission must not silently downgrade identity, helper strategy,
containment, network, credential, or Docker authority. Examples:

- `macos-agent-user` must not fall back to `macos-current-user`.
- `linux-agent-user` must not fall back to `linux-current-user`.
- `root-helper` must not fall back to `rootless-userns`.
- `network=none` must not degrade to advisory network policy.
- brokered credentials must not degrade to broad environment passthrough.
- Docker private-daemon authority must not degrade to the shared host socket.

## Documentation Rules

Use these phrases consistently:

- Use `supported` only when implementation, model evidence, tests, and release
  gates for that provider lane have passed.
- Use `experimental` for an executable lane that still requires explicit gates
  or caveats.
- Use `plan-only` for compiler/probe/facade work that cannot launch.
- Use `setup-required` when modeled persistent setup resources are required
  before the lane can execute.
- Use `unsupported` for unavailable routes and unsupported host/platform
  combinations.

Linux current-user status is tied to the
[Linux current-user VM smoke matrix](linux-current-user-vm-smoke-matrix.md).
Linux agent-user status is separate and remains setup-required until the
`MC_SetupRollback` model, setup resource graph, diagnostics, root-helper
runtime, and lifecycle VM smokes are complete.
Release promotion gates for both lanes are listed in the
[Linux release checklist](linux-release-checklist.md).

macOS native support currently means the `macos-agent-user` lane. The
`macos-current-user` lane is registered only to keep the identity split
explicit; do not describe it as executable until it has its own admission,
runner, and evidence gates.

Do not write broad "Linux support" or "macOS native support" claims when the
identity lane matters. Name the provider lane and status instead.
