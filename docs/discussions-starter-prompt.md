# Pinned Discussions Starter Prompt

Use this as the first pinned GitHub Discussions post after Discussions are
enabled for the repository.

## Title

What should Hazmat support well enough to prove on your machine?

## Body

```md
Hazmat's first community loop is evidence, not wishlists.

If you use local coding agents on macOS, share one concrete workflow you want
Hazmat to support well enough to prove:

- agent or harness: Claude, Codex, OpenCode, Gemini, Hermes, Qwen, Cursor Agent, or other
- stack: language, framework, package manager, database, or local service shape
- repo shape: Docker/no Docker, devcontainer, database needs, local services
- containment mode: native, Docker Sandbox, code-only fallback, Tier 4, or unknown
- current blocker: setup, auth, import, integration paths, network, credentials, rollback, docs
- evidence you can share: `hazmat explain`, recipe commands, compatibility report, sanitized transcript, or fixture repo
- caveats: macOS version, Hazmat version, harness version, known sharp edges

Useful outputs from this thread:

- a recipe under `docs/recipes/`
- a compatibility row in `docs/compatibility.md`
- an integration proposal
- a harness candidate note
- a docs/UX fix
- an incident-to-control writeup when the evidence is public and safe to discuss

Please do not post private vulnerabilities, secret values, credential material,
or containment bypass details here. Use `SECURITY.md` for private security
reports.

Reference docs:

- Community model: `docs/community.md`
- Compatibility program: `docs/compatibility.md`
- Recipes: `docs/recipes/README.md`
- Discussions category rules: `docs/discussions.md`
- Security reporting: `SECURITY.md`
```

## Maintainer Notes

- Pin this post globally or in the most general category GitHub allows.
- If GitHub requires a category, use `RFCs` or the closest general category.
- Reply with a maintainer summary when a workflow turns into a bead, recipe,
  compatibility row, or PR.
- Redirect private vulnerability details to `SECURITY.md` immediately.
- Do not promise support in the thread until evidence, owner, and boundary are
  clear.
