# Claude + Next.js

Use this recipe when you want Claude Code in native containment for a Node / Next.js repo and you do **not** need Hazmat to control the host Docker daemon.

## Recommended Setup

```bash
hazmat claude --integration node
```

If this is a repo you revisit often:

```bash
hazmat config set integrations.pin "~/workspace/my-next-app:node"
```

## Best Fit

- app code lives in the project directory
- `node_modules/`, `.next/`, and similar build output can stay inside the repo
- local services are either not needed or are managed outside Hazmat

## Caveats

- This is usually a **native only** workflow. If the project depends on host Docker control, do not punch that hole through native containment.
- If the repo uses a shared host daemon or a devcontainer workflow that requires Docker control, move the Docker part outside Hazmat or use Tier 4 instead.
- For extra read-only docs or design refs, add `-R` explicitly rather than widening scope globally.

## Why This Recipe Exists

Next.js is a common case where people do not need the full Docker story to get value. Native containment plus the `node` integration is the simplest safe default.
