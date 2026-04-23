# Recipes

Recipes are practical starting points for real Hazmat workflows.

They are intentionally lighter-weight than core docs:

- focused on one harness + one stack
- explicit about integrations and caveats
- easy for community contributors to improve

Recipes are a **Community** support surface. They are useful, but they are not a substitute for the core trust model docs.

## Starter Recipes

- [Claude + Next.js](claude-nextjs.md)
- [Codex + uv](codex-uv.md)
- [OpenCode + Go](opencode-go.md)
- [Gemini + TLA+](gemini-tla.md)

## What a Good Recipe Includes

- which harness to use
- which integrations to activate
- whether the workflow is native-only, Docker-capable, or better suited for Tier 4
- what extra read or write scope is typical
- known caveats and setup friction

If you want to add a new recipe, keep it concrete. "Rust project" is weaker than "Codex + cargo workspace with native containment."
