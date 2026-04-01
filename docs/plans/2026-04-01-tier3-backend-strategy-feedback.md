# Tier 3 Backend Strategy Feedback

Status: Proposed
Date: 2026-04-01
Related issue: `sandboxing-01r`

## Position

Hazmat should be opinionated about **security properties**, not about a single
vendor. The product should enforce the conditions that make Docker-capable
agent sessions safe, then choose the best backend that can actually satisfy
those conditions.

That said, Hazmat should not relax the standard to "anything that sort of looks
like private Docker." A backend should only be accepted if Hazmat can validate
the relevant containment properties from outside the agent's control.

## Required Properties

For Docker-capable sessions, the minimum acceptable backend properties are:

1. **Private daemon**
   The agent cannot reach a Docker daemon it does not exclusively own.

2. **Policy before launch**
   Network restrictions are applied before the agent process starts, not as a
   manual step after launch.

3. **Enforcement outside the agent**
   The enforcement mechanism is not something the agent can trivially rewrite
   from inside the backend once it has code execution.

4. **Mount control**
   Hazmat must preserve its existing filesystem model:
   project directory read-write, `-R` directories read-only, unrelated host
   paths absent.

These properties matter more than the brand name of the backend.

## Why Docker Sandboxes Are Still the Right v1

Docker Sandboxes are the best v1 backend because they offer the cleanest path
to the properties above:

- private per-sandbox Docker daemon
- isolated per-sandbox runtime
- backend-managed policy surface Hazmat can inspect and drive
- straightforward operator UX

This does not mean Docker Sandboxes are the only possible safe backend. It
means they are the only backend Hazmat can currently ship with enough
confidence and enough product simplicity.

## Why Colima or Podman Are Not Equivalent by Default

A backend does not qualify merely because it uses a separate VM or separate
Docker daemon.

For example, "Colima + iptables" is only equivalent if Hazmat can prove all of
the following:

- the daemon is project- or session-private
- the network policy is applied before launch
- the policy cannot be disabled or rewritten by the agent from inside the
  runtime
- the mount model remains under Hazmat's control

If those guarantees are missing or only partially enforceable, the backend is
not equivalent for Hazmat's threat model.

## Product Direction

Hazmat should ship with Docker Sandboxes as the **only** supported Tier 3
backend in v1, but the architecture should be backend-oriented from day one.
The abstraction layer should not be deferred until after auto-routing ships.

The correct message is:

- Hazmat is adamant about the security properties.
- Docker Sandboxes are the only backend that currently meets the bar well
  enough to ship.
- Additional backends are welcome only if they satisfy the same properties
  without hidden caveats.

## Implementation Guidance

The backend interface should be defined in terms of what Hazmat must prove, not
which vendor command happens to be used.

At minimum, each backend should support:

- `Doctor()`
  Verifies backend identity, version floor, control-plane reachability, and the
  private-daemon property.

- `ValidatePolicyProfile()`
  Proves the requested policy can be enforced by the backend before launch.

- `PrepareProject()`
  Creates or validates the isolated per-project runtime.

- `ResetProject()`
  Destroys backend-managed state for a project safely.

Before a backend is accepted, it should also declare capabilities that Hazmat
can rely on, for example:

- `PrivateDaemon`
- `ExternalPolicyControl`
- `ROMounts`
- `EphemeralSessionLayer`

If a backend cannot honestly satisfy those capabilities, Hazmat should reject
it.

## Recommendation

The long-term stance should be:

**Property-first, vendor-pragmatic, standard-preserving.**

In practical terms:

- keep Docker Sandboxes as the v1 backend
- keep the security bar fixed
- build the abstraction layer early enough that Hazmat does not accidentally
  become "Docker Desktop-only" by architectural inertia

This gives Hazmat the widest credible long-term product position without
lowering the security standard that makes Tier 3 worth having.
