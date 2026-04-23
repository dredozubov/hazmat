# OpenCode + Go

Use this recipe for a Go project where OpenCode is your preferred harness and native containment is enough.

## Recommended Setup

```bash
hazmat config import opencode
hazmat opencode --integration go
```

Or for one-off commands:

```bash
hazmat exec --integration go -- go test ./...
```

## What the `go` Integration Helps With

- passes through a bounded set of Go environment selectors
- resolves the Go toolchain as read-only input
- excludes common generated or vendored output from snapshots

## Caveats

- Treat this as **works with caveats** until there are more public compatibility reports across macOS versions and toolchain layouts.
- If you rely on unusual `GOPATH` or private module routing, note that explicitly in a compatibility report so the matrix stays honest.

## Good Community Follow-up

If you use this recipe successfully, file a compatibility report with:

- macOS version
- Go version
- harness auth path
- any extra read/write scope you needed
