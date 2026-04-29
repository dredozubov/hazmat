# Explicit GitHub Capability Outside Repo Recommendations

Status: Implemented session-grant foundation
Date: 2026-04-02
Updated: 2026-04-29
Related issues: `sandboxing-e89`, `sandboxing-9lz`

## Position

Hazmat supports GitHub API access as a first-class session capability, not as a
generic environment passthrough and not as an integration side effect.

The trust boundary is:

- integrations are ergonomics overlays
- integrations do not widen external-service authority
- repo recommendations cannot request credentials
- GitHub API access is delegated only by host-owned configuration plus an
  explicit launch flag

## Current Implementation

Configure the host-owned token:

```bash
hazmat config github
GH_TOKEN=... hazmat config github --token-from-env
hazmat config github --clear
```

The token is stored at `~/.hazmat/secrets/github/token` through the credential
registry entry `github.api-token`. It is not written to `~/.hazmat/config.yaml`
and is not copied into `/Users/agent`.

Activate the capability per session:

```bash
hazmat claude --github
hazmat codex --github
hazmat opencode --github
hazmat gemini --github
hazmat shell --github
hazmat exec --github gh pr status
hazmat explain --github --json
```

When active in a native session, Hazmat injects only:

- `GH_TOKEN=<stored token>`

The session contract and `hazmat explain --json` show a redacted grant:

```text
Credential env grants: GH_TOKEN=<redacted> (github.api-token, host secret store)
```

Docker Sandbox sessions currently fail closed for `--github` because that
backend does not yet provide equivalent session env credential delivery.

## Goals

- Make `gh` usable inside Hazmat when the host user deliberately allows it
- Keep repo-controlled files incapable of requesting GitHub credentials
- Avoid a generic `session.env_passthrough` knob
- Keep the consent surface capability-specific and legible
- Fail closed when the chosen runtime cannot safely honor the capability

## Non-Goals

- General credential passthrough for arbitrary services
- Repo-declared credential capabilities
- Silent import of host `gh` state from `~/.config/gh`
- Permanent agent-user GitHub auth bootstrap in v1
- Docker Sandbox parity before that backend has an env-grant delivery contract

## Runtime Behavior

At session resolution time, Hazmat computes GitHub access separately from
integration resolution.

Resolution order:

1. explicit `--github` flag
2. otherwise disabled

If enabled and the host secret-store token is absent, session resolution fails
with a clear configuration hint:

```text
GitHub API token is not configured
Run: hazmat config github --token-from-env
```

If enabled for Docker Sandbox mode, session resolution fails with an explicit
unsupported-capability message. That is preferable to pretending GitHub access
is available when it is not.

## Repo Interaction Rules

Repo files may recommend workflows or integrations, but they cannot activate
GitHub credentials.

Specifically:

- `.hazmat/integrations.yaml` cannot request credential capabilities
- integration `env_passthrough` rejects `GH_TOKEN`, `GITHUB_TOKEN`, and other
  credential-shaped keys
- repo setup env selectors reject credential-shaped keys before they reach the
  session environment
- approval of a repo recommendation means "ergonomics overlay approval," not
  "credential delegation approval"

If maintainers want contributors to use GitHub access in Hazmat, they should
document the host-owned setup flow:

```text
Run `hazmat config github`, then launch the needed session with `--github`.
```

## Deferred Work

Host-owned project auto-grants remain a possible convenience layer, but the
current implementation intentionally starts with per-session activation only.
If added later, auto-grants must live in host-owned config, canonicalize the
project path, show up in session previews, and never be writable by the agent
or by repo content.

## Testing

Coverage should prove:

- `hazmat config github --token-from-env` stores the token under
  `~/.hazmat/secrets/github/token`
- `hazmat config github --clear` removes the host-store token
- explicit `--github` activation injects `GH_TOKEN` and records a redacted
  `github.api-token` grant
- missing token fails closed without partial session mutation
- Docker Sandbox mode fails closed for `--github`
- integration and repo setup env passthrough still reject `GH_TOKEN` and
  `GITHUB_TOKEN`
- repo-owned recommendations cannot activate GitHub access
