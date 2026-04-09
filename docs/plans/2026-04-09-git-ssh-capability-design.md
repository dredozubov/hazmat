# Managed Per-Project Git SSH Capability

Status: In progress
Date: 2026-04-09
Related issue: `sandboxing-oyi`

## Position

Hazmat should support a narrow Git-over-SSH capability without reintroducing
ambient SSH authority into contained sessions.

The key distinction is:

- Hazmat should still reject host-user `SSH_AUTH_SOCK` passthrough
- Hazmat should still keep `~/.ssh` outside the session contract
- Hazmat may grant a dedicated, low-privilege, per-project Git SSH identity
  through an explicit host-owned capability path

This keeps the current threat-model position intact for general SSH while
allowing a practical Git transport escape hatch for deploy keys and other
repo-scoped credentials.

## Goals

- Enable Git fetch/push over SSH for selected projects
- Keep raw private-key material out of the contained session filesystem
- Keep the grant host-owned and per-project, not repo-controlled
- Make the capability legible in the session contract
- Fail closed when the runtime cannot safely honor the capability

## Non-Goals

- Arbitrary remote shell access
- Host-user SSH agent forwarding
- Reopening `/Users/agent/.ssh`
- Generic credential brokering for arbitrary external services
- Docker Sandbox parity in the first slice

## Recommended Model

### 1. Host-owned key directory plus project assignment

Hazmat should treat SSH setup as a project setting:

- ops or the user points Hazmat at a directory containing SSH keys
- the default key directory is `~/.ssh`
- the project config stores the selected private-key path and `known_hosts` path

That keeps the main UX close to how developers think about repos:
"this project uses SSH key X."

### 2. Ephemeral session runtime preparation

Before entering native containment, Hazmat prepares an agent-local runtime:

- create a temporary directory under `/Users/agent/.config/hazmat/`
- start a fresh `ssh-agent` owned by the agent user
- load the configured private key into that agent from the host side
- copy the configured `known_hosts` file into the runtime directory
- write a wrapper script that forces Git through the prepared socket and host
  key file

The private key remains in host-owned storage. The contained session gets only
the resulting authentication capability.

### 3. Narrow transport semantics

The first implementation targets Git transport only:

- expose the capability via `GIT_SSH_COMMAND`
- force `BatchMode=yes`, `IdentitiesOnly=yes`,
  `StrictHostKeyChecking=yes`, `ForwardAgent=no`,
  `ClearAllForwardings=yes`
- pin `UserKnownHostsFile`
- use the session-local `ssh-agent` socket via `IdentityAgent`

The wrapper should reject non-Git remote commands. Host allowlists are out of
scope for this slice; the remote-side scope of the selected key remains the
primary authority boundary.

## Security Notes

- This does not preserve the old "no SSH authority at all" property.
- It does preserve the more important property for the original concern:
  the coding agent never receives the raw human SSH key.
- Safety depends on the configured key being low-privilege and dedicated to
  the project.
- Hazmat should continue to treat host `SSH_AUTH_SOCK` passthrough as unsafe.

## Backend Scope

V1 should support native containment only.

If a session resolves to Docker Sandbox mode and the project has managed Git
SSH configured, Hazmat should fail closed with an explicit message rather than
silently dropping the capability.

## Implementation Slice

1. Discover keys from a chosen directory, defaulting to `~/.ssh`.
2. Add `hazmat config ssh set|show|test|clear|list-keys`.
3. Store the selected private-key path plus `known_hosts` path in config and surface it in the session contract.
4. Prepare the ephemeral Git SSH runtime before native session launch.
5. Inject `GIT_SSH_COMMAND` for the session.
6. Add focused unit coverage for discovery, config persistence, session resolution, and wrapper generation.
7. Update the key SSH docs so they describe the managed Git exception instead of an unconditional "SSH unsupported" claim.
