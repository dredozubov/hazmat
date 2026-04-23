# Repo-local Git Hooks

Hazmat's repo-local Git hook flow is intentionally stricter than plain Git
hooks, Husky, or `pre-commit install`.

That extra machinery is there for one reason: in Hazmat's threat model, a
contained agent can edit tracked repo files today and arrange for those files to
execute later on the host during your next `git commit` or `git push`. Normal
Git hook UX does not treat that as a special trust boundary. Hazmat does.

## Why This Feels More Complex

Plain Git hooks are simple because Git assumes the repo and the user are in the
same trust domain. Hazmat does not make that assumption. The repo may have been
edited by a contained agent that did **not** have host execution permission, so
"just copy files into `.git/hooks`" or "just run Husky install" crosses a real
boundary.

If you do not care about that boundary, ordinary Git hook tools are simpler.
Hazmat's hook layer is justified only when you want:

- repo-declared hook intent
- explicit host approval
- immutable approved snapshots
- refusal on drift or reroute instead of silent execution

## Attack Vectors We Considered

### 1. `core.hooksPath` reroute

This is the main one.

A contained agent with write access to `.git/config` can set:

```bash
git config core.hooksPath .agent-hooks/
```

If the host later runs `git commit`, Git will execute hook code from that new
path. "Re-run the approved hook script if its contents changed" does **not**
defend against this. Git never reaches the approved dispatcher in the first
place.

Hazmat's response:

- pins a managed local `core.hooksPath`
- validates that path before approved execution
- installs fallback `.git/hooks/*` dispatchers that refuse instead of acting as
  an alternate execution path
- refuses silent replacement when another local `core.hooksPath` owner already
  exists

### 2. Post-approval mutation of live repo hook code

Without snapshots, approval would mean "I approve whatever bytes happen to be in
the repo later." A contained agent could get a harmless hook approved, then
change the hook afterwards and wait for future host Git operations.

Hazmat's response:

- copies approved hook content into host-owned storage outside the repo
- records approval keyed by canonical repo path plus bundle hash
- executes only the approved snapshot bytes, not the live repo copy
- refuses when the live repo bundle hash drifts from the approved hash

### 3. Smuggling behavior through helper/config files outside the approved hook

Even if the hook entrypoint itself is snapshotted, it can still shell out to
other live repo files or read mutable config from the repo. That would move the
real behavior outside the approved boundary.

Hazmat's response:

- the manifest now allows explicit auxiliary `files:` under `.hazmat/hooks/`
- those files are hashed, approved, copied into the immutable snapshot, and
  available to the approved hook at runtime

This is why Hazmat's own gitleaks config lives at
`.hazmat/hooks/gitleaks.toml` rather than at the repo root.

### 4. Path escape and symlink tricks

An attacker should not be able to declare a hook file like `../somewhere` or
smuggle content in through a symlink that resolves outside `.hazmat/hooks/`.

Hazmat's response:

- manifest paths must be relative to `.hazmat/hooks/`
- absolute paths and `..` escapes are rejected
- top-level symlinks are rejected
- symlinked parent escapes are rejected
- declared paths must resolve to regular files

### 5. Conflicting hook ownership

Another tool may already own `core.hooksPath`. If Hazmat silently overwrote it,
that would be both confusing and unsafe.

Hazmat's response:

- detects an existing local `core.hooksPath` owner
- refuses automatic replacement
- requires an explicit `hazmat hooks install --replace`

### 6. Drift or tampering in the managed hook layout

If the managed hook directory or fallback `.git/hooks/` directory contains
unexpected entries, the install/validation story is no longer clean.

Hazmat's response:

- validates the managed and fallback layouts
- treats unexpected entries as invalid state
- refuses rather than trying to merge with unknown files

### 7. Host-state leftovers after uninstall or rollback

If approval records, snapshots, or dispatchers remain after uninstall, the
system becomes hard to reason about and can accidentally preserve old trust.

Hazmat's response:

- `hazmat hooks uninstall` removes approval, snapshot, wrapper, and dispatcher
  state
- `hazmat rollback` sweeps the same repo-local hook state

### 8. Accidental policy widening

Approving a hook should not implicitly widen future Hazmat session network or
filesystem policy.

Hazmat's response:

- hook approval is a separate host-side execution approval
- it does not modify future Hazmat session access policy

## What Hazmat Actually Promises

Hazmat does **not** promise that approved hooks are safe. It promises something
narrower:

- unapproved hook content should not become host-executed through the managed
  path
- repo drift should cause refusal, not silent execution
- rerouting Git away from the managed path should cause refusal, not silent
  execution
- uninstall and rollback should remove host-owned hook state cleanly

That is why the TLA+ model is named `MC_GitHookApproval`, not "safe hook code."

## What Is Still Out Of Scope

Some limits are deliberate:

- Approved hooks still run as the host user. If you approve malicious hook
  content, Hazmat does not sandbox it.
- `requires:` is review metadata, not a binary allowlist or execution policy.
- Hazmat's formal boundary is about Hazmat-managed entrypoints. It does not
  claim to control arbitrary direct use of a foreign `git` binary outside that
  managed path.
- V1 scope is intentionally narrow: repo-local only, and only `pre-commit`,
  `commit-msg`, and `pre-push`.

## Why Hazmat Does Not Just Reuse Husky Or Plain `.git/hooks`

Those tools are fine when the repo and the host are treated as the same trust
domain. Hazmat is solving a different problem.

The moment you allow contained agents to edit repo files, Git hooks become a
future host-execution surface. That makes "easy" hook install flows unsafe by
default:

- raw `.git/hooks` copy is too manual and too easy to drift
- lifecycle auto-install is too implicit
- global hooks and template directories are too persistent
- live repo helper/config files weaken "approved-content-only" execution

So yes, this is more complex than plain Git hooks. That complexity exists to
keep a repo-authored future host execution path legible, reviewable, and
refusable.
