# Gemini + TLA+

Use this when you want Gemini in native containment for TLA+ / TLC work with the `tla-java` integration.

## Recommended Setup

```bash
hazmat gemini --integration tla-java
```

If the repo should always suggest this path:

```yaml
integrations:
  - tla-java
```

## Best Fit

- you are editing specs, configs, or model-checking inputs
- Java is available on the host in a read-only path Hazmat can expose
- `TLA2TOOLS_JAR` or `JAVA_HOME` are already set appropriately on the host

## Caveats

- This is a **works with caveats** path because Gemini setup is still a little more manual than the smoothest Claude path.
- TLC runtime expectations should be documented in the repo so contributors do not guess.
- If you need heavy local service orchestration around the model-checking workflow, describe it explicitly rather than treating it as implied.

## Read Next

- [docs/harnesses.md](../harnesses.md)
- [docs/integrations.md](../integrations.md)
- [tla/VERIFIED.md](../../tla/VERIFIED.md)
