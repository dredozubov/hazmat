# Open Design Containment Provider Maintainer Alignment

**Date:** 2026-06-13
**Bead:** `sandboxing-vv4n`
**Status:** maintainer alignment draft, not an implementation proposal

## Purpose

This draft is the package to take to Open Design maintainers before proposing
containment-provider code. The request is agreement on product shape,
responsibility split, OS expectations, testing burden, and distribution model.

The goal is not to make Open Design depend on one specific containment tool. It
is to define a provider-neutral boundary where Open Design can ask for a
contained agent session and receive honest launch/result metadata, while the
provider owns platform enforcement.

## Proposed User Surface

Open Design should expose containment as an execution choice for agent runs,
not as a project plugin or arbitrary shell wrapper. A user should be able to
select a containment provider, inspect the planned authority, and run the same
agent workflow with raw model/tool streams preserved.

Candidate surface:

```text
open-design run --containment <provider> --agent <agent-id> --project <path>
open-design explain --containment <provider> --agent <agent-id> --project <path>
open-design doctor containment --provider <provider>
```

The exact command names can change. The important UX requirements are:

- `explain` is side-effect-free and does not require privileged probes.
- `doctor` defaults to dry-run and reports the concrete fix path, not a vague
  follow-up command.
- `run` fails closed when the provider cannot enforce the requested contract.
- Raw stdout/stderr remain owned by the selected agent harness.
- Provider diagnostics use structured result fields, sidecars, or normal
  non-raw status output.

## Responsibility Split

### Open Design Owns

- agent identity and adapter registry;
- prompt, attachment, model, and protocol request construction;
- UI affordances for selecting, explaining, and approving containment;
- mapping user intent into a provider-neutral containment request;
- cancellation, display, and result handling at the product layer;
- user documentation for what containment mode means in Open Design;
- compatibility tests with fake providers and representative agents.

### The Provider Owns

- OS-specific launch planning and enforcement;
- persistent setup and rollback for provider-owned host resources;
- credential-deny validation and explicit credential delivery descriptors;
- network policy enforcement or honest unsupported-capability gaps;
- metadata emission after containment is active;
- cleanup and crash recovery for provider-owned state;
- provider-specific diagnostics, repair plans, and security documentation.

### Shared Contract

The contract between Open Design and a provider should be structured data, not
rendered shell snippets. It should include:

- project root and requested read/write/read-only roots;
- requested working directory;
- agent home/profile policy;
- temp/state policy;
- environment and credential descriptors, with secret bytes excluded;
- network mode;
- process and service expectations;
- output mode, including raw-stream requirements;
- result sidecar location or provider result channel;
- accepted capability gaps, if any.

Open Design should not let repos or user manifests define new containment
policy. A project may request a known mode; the product and provider decide
whether that request is safe and supported.

## Darwin-Native First Package

The first alignment package should target a Darwin-native provider because that
is the smallest path to an executable local containment story for common macOS
agent workflows.

Expected provider behavior:

- compile the shared request into a native macOS policy;
- launch through a narrow helper that closes inherited file descriptors before
  policy setup;
- emit metadata only after containment is active;
- preserve raw agent stdout/stderr;
- deny broad home, SSH, provider token, and credential cache access by default;
- report setup, helper, policy, credential, and network failures as typed
  diagnostics.

Open Design should treat Darwin-native as one provider backend, not as a
special hardcoded agent-launch path. That keeps later Linux-native and remote
providers convergent.

## Linux-Native Convergence

Linux-native support should converge on the same request/result contract before
it becomes user-facing. The Linux provider may enforce the contract through
namespaces, bind mounts, Landlock, seccomp, cgroups, and a root or unprivileged
helper strategy. It does not need exact macOS syscall parity.

Release alignment should require:

- plan-only mode before launch support;
- explicit helper strategy, with no silent fallback;
- structured capability gaps for missing user namespace, mount namespace,
  network namespace, Landlock, seccomp, cgroup v2, and setup resources;
- metadata sidecar parity with Darwin-native;
- raw stdout/stderr preservation;
- distro probe API and fixtures;
- VM/manual smoke evidence before production support claims.

Open Design should be able to render Linux as `plan-only`, `experimental`, or
`supported` from provider metadata without inventing separate UI semantics.

## Docker Sandbox Caveats

Docker Sandbox style execution is valuable, but it is not equivalent to native
host containment. The alignment package should keep these caveats explicit:

- Docker-heavy projects may need a private daemon runtime rather than native
  containment.
- Native containment must not pass the host Docker socket through to an agent.
- Docker Sandbox mode has a different profile, mount, credential, history, and
  cleanup model.
- Provider env passthrough, Git SSH brokering, and host profile import may be
  unsupported until the provider declares equivalent semantics.
- Docker Sandbox egress defaults and policy controls must be represented
  honestly; do not describe advisory configuration as enforced containment.

Open Design UI should present Docker Sandbox as a distinct contained backend
with its own compatibility gaps, not as an invisible fallback from native mode.

## Testing Matrix

Open Design and the provider should agree on tests before code lands:

| Layer | Open Design Evidence | Provider Evidence |
| --- | --- | --- |
| Request construction | fake-provider golden requests for agent, prompt, attachments, cwd, and mode | schema validation rejects malformed requests |
| Explain/doctor | side-effect-free UI tests and no privileged probes in explain | dry-run repair plans name exact commands and mutations |
| Darwin-native run | integration against fake provider result stream | helper ordering, fd isolation, metadata-after-containment, path deny tests |
| Linux-native plan | plan-only rendering and capability-gap UI tests | compiler, distro probe, gap vocabulary, and golden spec tests |
| Docker Sandbox | backend selection and caveat rendering tests | private-daemon admission, mount/env/credential rejection tests |
| Raw streams | byte-for-byte stdout/stderr preservation with fake harness | helper diagnostics never leak into raw streams |
| Credentials | redaction and no secret bytes in requests/logs | registry-backed grants, deny-floor tests, crash cleanup |
| Cancellation | product cancel reaches provider and updates UI state | provider writes atomic result and cleans disposable state |
| Service agents | fake service lifecycle UI tests before first-class support | modeled session-scoped service lifecycle before host services |

Live privileged or helper-backed smokes should be opt-in manual gates. They
should never run as a surprise from `explain`, read-only diagnostics, or normal
unit tests.

## Distribution Options

The alignment discussion should choose one distribution model before
implementation:

| Option | Shape | Tradeoff |
| --- | --- | --- |
| External provider binary | Open Design shells out to a stable provider CLI/API | Clear ownership and release cadence; needs version negotiation and install UX |
| Optional provider package | Open Design ships adapter glue, provider ships helper/runtime | Better integrated UI; still keeps enforcement outside Open Design releases |
| Bundled provider | Open Design distributes a vetted provider build | Smoothest UX; highest maintenance and signing burden for Open Design |
| Provider protocol only | Open Design defines the contract, no bundled provider | Lowest coupling; users must install and trust a compatible provider |

For the first proposal, prefer an external provider binary or optional provider
package. Bundling should wait until signing, update, rollback, and support
ownership are explicit.

## Alignment Questions

Ask maintainers to answer these before any Open Design code proposal:

1. Should containment be exposed as a provider-selected execution mode, or as a
   narrower per-agent setting?
2. What command/UI names should distinguish side-effect-free explain, dry-run
   doctor, and effectful run?
3. What structured request fields are acceptable in Open Design's public or
   internal API?
4. Should Open Design support only reviewed provider integrations, or also
   user-configured provider binaries with a protocol/version check?
5. How should Open Design display provider capability gaps without implying
   weaker containment is good enough?
6. What is the minimum Darwin-native smoke evidence before maintainers would
   accept an implementation PR?
7. What Linux status labels should Open Design expose while Linux is plan-only
   or experimental?
8. Should Docker Sandbox be selectable beside native providers, or delegated
   entirely to provider policy?
9. Where should provider install/update guidance live, and who owns broken
   setup repair UX?
10. What CI jobs can Open Design reasonably own without privileged host setup?
11. What manual smoke checklist is acceptable for privileged helper-backed
   behavior?
12. What distribution and signing model is acceptable if a provider helper is
   needed?

## Non-Goals

- No Open Design code changes in this package.
- No generic repo-defined containment plugins.
- No dynamic harness or provider manifests that can add executable policy.
- No claim that Linux-native or Docker Sandbox semantics already equal
  Darwin-native support.
- No hidden fallback from failed native containment to a less isolated mode.
- No privileged probes from side-effect-free diagnostics.

## Next Step

Send this as a maintainer alignment draft. If maintainers agree on the surface,
create a narrower implementation RFC that specifies the request schema,
provider protocol, version negotiation, and fake-provider tests before touching
Open Design runtime code.
