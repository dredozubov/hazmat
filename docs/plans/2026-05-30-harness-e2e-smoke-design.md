# Harness E2E Smoke Design

Status: Implemented, expanded to all managed harnesses
Date: 2026-05-30

## Purpose

Hazmat already has Go unit coverage, TLA+ models, stack-matrix tests, and the
destructive lifecycle e2e. The missing surface is a one-command harness smoke
that runs the real launch plumbing without depending on live vendor CLIs,
network auth, browser OAuth, or a disposable VM.

The smoke should catch harness regressions that unit tests can miss:

- command parsing and forwarding through the real `hazmat <harness>` entrypoint
- pre-session mutation planning and application
- native launch setup and environment delivery
- temporary file-backed auth materialization and harvest cleanup for every
  file-backed harness auth surface
- failure modes introduced by upstream harness updates

## Shape

The implemented entrypoint is:

```bash
bash scripts/e2e-harness-smoke.sh
make e2e-harness-smoke
```

It is a prepared-host test, not a lifecycle test. It assumes `hazmat init` has
already created the agent user and helper, requires non-interactive `sudo -n`,
and takes the same shared host-side test lock as the other e2e scripts.

To avoid live harness dependencies, the script temporarily replaces each
agent-owned harness binary with a synthetic executable, runs the normal Hazmat
launch path, and restores every touched file on exit. The backup set includes
agent-owned harness binaries, agent runtime auth files, Hermes managed state,
host-owned harness auth stores, and provider env-secret files used by the smoke.

The script also exposes its policy surface:

```bash
bash scripts/e2e-harness-smoke.sh --list-harnesses
```

`TestHarnessSmokeCoversEveryManagedHarness` compares that list against
`managedHarnessRegistry`. Adding a future managed harness without updating the
synthetic e2e smoke is therefore a failing Go test, not a documentation-only
expectation.

## Cases

### Hermes Foreground Launch

The Hermes case installs a fake `/Users/agent/.local/bin/hermes`, runs:

```bash
hazmat hermes --no-backup -C <scratch-project> -- --version
```

The fake binary asserts `HERMES_HOME=$HOME/.hazmat/hermes` and prints a version
marker. The smoke verifies that marker and checks that the managed Hermes state
root exists. This covers the one-command foreground harness path without
requiring a real Hermes install. The fake binary also asserts all Hermes
provider env grants that Hazmat can transparently deliver.

### Claude Auth Harvest Guard

The Claude case seeds synthetic host-owned auth under
`~/.hazmat/secrets/claude/`, removes agent runtime auth residue, installs a fake
`/Users/agent/.local/bin/claude`, then runs:

```bash
hazmat claude --no-backup -C <scratch-project> -p "auth smoke"
```

The fake Claude asserts that Hazmat materialized the stored auth into the agent
home, then simulates an upstream logged-out rewrite by replacing
`/Users/agent/.claude/.credentials.json` with `{}` and stripping portable auth
keys from `/Users/agent/.claude.json`.

The expected behavior is host-store preservation: logged-out or empty runtime
credential files are non-harvestable and must not overwrite the durable
host-owned copy. Hazmat removes the runtime credential residue after the
session, while non-auth Claude state can remain in `/Users/agent/.claude.json`.

### Codex, OpenCode, and Gemini Harvest

The Codex, OpenCode, and Gemini cases seed their host-owned auth stores,
remove any agent runtime residue, and run:

```bash
hazmat codex --no-backup -C <scratch-project> exec "codex smoke"
hazmat opencode --no-backup -C <scratch-project> run "opencode smoke"
hazmat gemini --no-backup -C <scratch-project> -p "gemini smoke"
```

Each fake binary asserts cwd, forwarded argv, and expected materialized auth.
Codex and Gemini also assert provider env delivery. The fake binaries then
write updated runtime auth files; Hazmat must harvest those updates into
`~/.hazmat/secrets/<harness>/...` and remove the runtime files from the agent
home on session exit.

## Release Gate

The local pre-release gate is:

```bash
bash scripts/pre-release-local.sh
make pre-release-local
```

It runs `scripts/pre-push` first, then `scripts/e2e-harness-smoke.sh`.
`scripts/release.sh` calls this gate before changelog drafting, so local release
work stops before version/tag activity if any managed harness smoke fails.

## Boundaries

This smoke does not prove real Claude, Hermes, provider APIs, OAuth browser
flows, terminal UI behavior, or network reachability. Those remain in
`docs/manual-testing.md`. It also does not replace the TLA+ secret-store model:
the logged-out runtime-auth case is modeled as a `NoSecret` transition before
the Go harvest guard and smoke script rely on it.
