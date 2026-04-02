# Explicit GitHub Capability Outside Packs

Status: Proposed
Date: 2026-04-02
Related issue: `sandboxing-e89`

## Position

Hazmat should support GitHub API access as a **first-class session
capability**, not as a stack pack and not as a generic host-env passthrough.

The design goal is to preserve the current pack invariant:

- packs are ergonomics overlays
- packs do not widen external-service authority
- repo recommendations cannot request credentials

GitHub access is still useful enough to deserve a supported path. The right
model is a host-owned, explicit capability that the user enables per session
or pins per project outside repo control.

## Goals

- Make `gh` usable inside Hazmat when the host user deliberately allows it
- Keep `.hazmat/packs.yaml` incapable of requesting credentials
- Avoid a generic `session.env_passthrough` or similar footgun
- Keep the consent surface capability-specific and legible
- Fail closed when the chosen runtime cannot safely honor the capability

## Non-Goals

- General credential passthrough for arbitrary services
- Repo-declared credential capabilities
- Silent import of host `gh` state from `~/.config/gh`
- Permanent agent-user GitHub auth bootstrap in v1

## Options Considered

### 1. Generic env passthrough knob

Example: `hazmat config set session.env_passthrough GH_TOKEN`

Reject. This reintroduces the same design problem under a different name. The
moment Hazmat supports arbitrary host-to-session env passthrough, the product
loses the distinction between passive session configuration and delegated
external authority.

### 2. GitHub-specific session flag only

Example: `hazmat claude --github`

Viable, but incomplete. It is safe and explicit, but repetitive for projects
where GitHub access is routinely needed.

### 3. GitHub-specific session flag plus host-owned project grants

Example:

- `hazmat claude --github`
- `hazmat config github allow ~/workspace/hazmat`

Recommend this. It preserves explicit consent, matches Hazmat's existing
project-scoped ergonomics patterns, and keeps repo-controlled files out of the
credential path.

## Recommended User Interface

### Per-session activation

All session entrypoints gain a dedicated flag:

- `hazmat claude --github`
- `hazmat codex --github`
- `hazmat opencode --github`
- `hazmat shell --github`
- `hazmat exec --github gh pr status`

Semantics:

- `--github` means "delegate GitHub API access to this session"
- v1 source is fixed to host env key `GH_TOKEN`
- if `GH_TOKEN` is unset, explicit `--github` fails with a clear error

Suggested error:

```text
hazmat: GitHub access requested, but GH_TOKEN is not set in the invoker environment
hazmat: set a fine-grained PAT in GH_TOKEN or omit --github
```

### Persistent host-owned project grants

Add a dedicated config subcommand:

```bash
hazmat config github allow ~/workspace/hazmat
hazmat config github deny ~/workspace/hazmat
hazmat config github list
```

Semantics:

- `allow` canonicalizes the project path and records that GitHub access should
  auto-activate for sessions launched in that project
- `deny` removes the project grant
- `list` shows configured project grants
- grants are host-owned config, not repo content, and are never writable by
  the agent user

For an auto-granted project:

- if `GH_TOKEN` is present, GitHub access activates automatically
- if `GH_TOKEN` is absent, Hazmat prints a note and continues without GitHub
  access

Suggested note:

```text
hazmat: note: project allows GitHub access, but GH_TOKEN is not set; continuing without it
```

## Config Shape

Add a dedicated top-level section to `~/.hazmat/config.yaml`:

```yaml
github:
  grants:
    - project: /Users/dr/workspace/hazmat
      source: env:GH_TOKEN
```

Rationale:

- `github` is clearer than hiding this under `session` or `packs`
- `grants` reflects that this is a capability decision, not a credential store
- `source` is explicit and future-compatible without opening a generic
  passthrough surface

Hazmat should continue to store **no GitHub secret material** in config. The
token still comes from the invoking user's environment at launch time.

## Runtime Behavior

At session resolution time, Hazmat computes GitHub access separately from
packs.

Resolution order:

1. explicit `--github` flag
2. host-owned project grant from `hazmat config github allow`
3. otherwise disabled

When enabled and `GH_TOKEN` is present, Hazmat injects:

- `GH_TOKEN=<value>`

No other GitHub env keys are part of v1.

Hazmat should print a strong warning at session start:

```text
hazmat: warning: GitHub API access enabled for this session via GH_TOKEN
hazmat: subprocesses and MCP servers inherit this token unless stripped
```

This warning is intentionally stronger than pack notices because this is not a
passive selector. It is delegated authority to an external service.

## Repo Interaction Rules

`.hazmat/packs.yaml` must remain unable to activate GitHub access.

Specifically:

- there is no `github` pack
- repo-recommended packs cannot request credential capabilities
- pack approval continues to mean "ergonomics overlay approval," not
  "credential delegation approval"

If maintainers want contributors to use GitHub access in Hazmat, they should
document it in README or onboarding docs:

```text
For PR workflows inside Hazmat, export GH_TOKEN and run hazmat claude --github.
```

This keeps repo intent advisory and keeps authority with the host user.

## Implementation Guidance

### Config and command surface

- Add `GitHubConfig` to `HazmatConfig`
- Add `hazmat config github` with `allow`, `deny`, and `list` subcommands
- Extend all session commands with `--github`
- Show grants in `hazmat config`

### Session resolution

- Add dedicated GitHub fields to `sessionConfig`
- Resolve GitHub capability outside `applyPacks`
- Inject `GH_TOKEN` in the general session env builder only after explicit
  capability resolution

### Pack cleanup

- Remove `GH_TOKEN` from `safeEnvKeys`
- Remove the built-in `github` pack
- Remove pack-specific credential tracking added only for this experiment
- Restore pack docs and threat-model language to the original invariant

### Backend parity

The capability must not be silently dropped for `--sandbox`.

If Docker Sandbox support cannot yet inject the token with equivalent
semantics, Hazmat should fail closed with an explicit message such as:

```text
hazmat: --github is not supported with --sandbox yet
```

That is preferable to pretending GitHub access is available when it is not.

## Testing

Add tests for:

- `config github allow/deny` canonicalization and persistence
- explicit `--github` activation with and without `GH_TOKEN`
- project grant activation with and without `GH_TOKEN`
- session-start warnings for active GitHub access
- `.hazmat/packs.yaml` remaining unable to activate GitHub access
- `--sandbox` fail-closed behavior if parity is not yet implemented

## Migration Plan

1. Remove the experimental `github` pack and `GH_TOKEN` from pack allowlists
2. Add the dedicated GitHub capability surface
3. Update docs to distinguish:
   - packs = friction reduction
   - capabilities like GitHub = explicit external authority delegation
4. If needed later, consider a separate long-term path for agent-user GitHub
   bootstrap/import, but keep it distinct from session delegation

## Recommendation

Ship **Option 3**:

- `--github` for explicit one-off sessions
- `hazmat config github allow <project>` for host-owned convenience
- no repo-driven credential activation
- no generic env passthrough feature

This gives Hazmat the usability win for `gh` without collapsing the pack trust
model.
