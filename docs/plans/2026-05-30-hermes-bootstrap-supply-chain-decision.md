# Hermes Bootstrap Supply-Chain Decision

Status: Accepted
Date: 2026-05-30
Related:
- `docs/plans/2026-05-30-hermes-harness-design.md`
- `sandboxing-lgd5.2`

## Decision

Phase 1 `hazmat bootstrap hermes` is detection/manual-guidance only. It must
not run the upstream Hermes installer, a `curl | sh` flow, `pipx install`, npm
`@latest`, or any other unpinned network installer automatically.

The bootstrap command may:

1. check whether a `hermes` binary is available to the agent account;
2. run `hermes --version`;
3. record Hermes harness state only after that version probe succeeds;
4. print manual installation guidance when the binary is missing or unhealthy.

Automated installation is deferred until a later design selects a pinned,
auditable source.

## Rationale

Hermes is an autonomous assistant runtime with skills, MCP, hooks, subprocesses,
memory, and optional gateway surfaces. Its installer therefore expands the
trusted computing base for a high-authority process tree. A bootstrap path that
silently fetches and executes a mutable remote script would make Hazmat's first
Hermes launch depend on upstream installer behavior that Hazmat has not pinned,
checksummed, or modeled.

The Phase 1 product requirement is that users are not left at a broken command:
`hazmat bootstrap hermes` exists, is idempotent, and tells the user exactly what
is missing. That does not require automated install. Detection/manual-guidance
keeps the harness path usable while preserving the security boundary.

This is stricter than the weakest existing bootstrap precedent in Hazmat. It is
closer to the Codex and Claude posture: verify before recording managed state,
and make the install source explicit before automated mutation.

## Future Automated Install Bar

A later Hermes bootstrap may automate installation only after the design records:

- source: a release artifact, package, or installer URL;
- pinning: exact version, digest, or signed release verification;
- update behavior: whether bootstrap upgrades, reinstalls, or refuses drift;
- install root: agent-owned managed tool path, not the host user's home;
- profile behavior: no host `~/.hermes` import or host shell-profile mutation;
- failure mode: no harness state is recorded unless `hermes --version` succeeds;
- rollback boundary: which installed artifacts survive ordinary rollback and
  what `--delete-user` removes.

Until that exists, `hazmat hermes` should fail with guidance when Hermes is not
installed, and `hazmat bootstrap hermes` should remain detection-first.

