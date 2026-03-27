# Contributing to Hazmat

## Setup

- macOS Ventura or later
- Go 1.21+
- Admin access (for integration/E2E tests)

```bash
cd hazmat
make all       # builds hazmat + hazmat-launch
```

## Testing

```bash
cd hazmat
make test                    # unit tests + go vet
./hazmat init check          # integration: verify setup
./hazmat init check --full   # integration: include live network probes
```

E2E tests run a full lifecycle (init, containment, snapshot, restore, rollback) and require sudo:

```bash
./scripts/e2e.sh
```

TLA+ model checking (requires Java + [tla2tools.jar](https://github.com/tlaplus/tlaplus/releases)):

```bash
cd tla
java -jar ~/workspace/tla2tools.jar -workers auto \
  -config MC_SetupRollback.cfg MC_SetupRollback.tla
```

## Pull requests

- Unit tests must pass
- TLA+ specs are checked in CI — if you change setup or rollback steps, update the spec first (see `tla/VERIFIED.md`)
- Follow the commit message convention: `<area>: <what changed>` (areas: `cloud`, `ux`, `privilege`, `docker`, `docs`, `rename`, `test`)
- One logical change per commit

## Security-relevant changes

If you change the seatbelt credential deny list, network policy, trust model, or containment boundaries — update `docs/design-assumptions.md`.

## Good places to start

- Linux port planning (different primitives: namespaces, seccomp, nftables — same architecture)
- Agent-specific presets for `hazmat exec`
- Improved rollback strategies
- Documentation fixes and examples
