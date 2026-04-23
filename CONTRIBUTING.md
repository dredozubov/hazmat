# Contributing to Hazmat

Hazmat is early-stage, but the contribution model is already explicit: community-editable data and compatibility knowledge should grow quickly; the enforcement boundary stays under tighter maintainer review.

Start with [docs/community.md](docs/community.md). It defines the support tiers, ownership model, and where contributions are easiest to land safely.

## Best Places to Help

These are the highest-leverage community surfaces right now:

1. **Compatibility reports.** Real macOS version + harness + stack reports are extremely useful. Use the compatibility issue template and [docs/compatibility.md](docs/compatibility.md).
2. **Recipes.** Practical setup guides such as Claude + Next.js or Codex + uv are cheap to contribute and useful immediately. See [docs/recipes/README.md](docs/recipes/README.md).
3. **Integrations.** Built-in manifests are intentionally bounded. If you want to add or improve one, start with [docs/integration-author-kit.md](docs/integration-author-kit.md).
4. **Docs and UX.** Confusing prompts, unclear contract output, missing troubleshooting steps, and rough onboarding edges are all good contributions.
5. **Research and evidence.** Incident writeups, CVE tracking, reproducible repros, and comparative safety analysis all strengthen the project.

## What Needs Deeper Review

These areas are not off-limits, but they are not good first contributions:

- seatbelt policy structure
- `pf` firewall behavior
- setup / rollback ordering
- credential delivery and capability brokering
- behavior governed by [tla/VERIFIED.md](tla/VERIFIED.md)

Linux is important, but it is not the default volunteer onramp. It touches the trust boundary, setup/rollback resources, and the proof story.

## Reporting Issues

Use the GitHub issue templates for:

- bugs and regressions
- compatibility reports
- integration requests
- harness requests
- docs / UX problems

Include your macOS version (`sw_vers`) and Hazmat version (`hazmat --version`) whenever possible.

## Security Reports

If you find a containment bypass, credential leak, sandbox escape, or other security issue, do **not** open a public GitHub issue. Use the private reporting path in [SECURITY.md](SECURITY.md).

## Building

```bash
make all           # builds hazmat + hazmat-launch (requires Xcode CLI tools for cgo)
make test          # unit tests
make e2e E2E_ACK=1 # full lifecycle test (needs sudo, modifies system)
make e2e-vm        # same test inside an isolated Lume VM (no system changes)
```

## Testing

```bash
make test                    # unit tests
hazmat check                 # integration: verify setup
hazmat check --full          # include live network probes
./scripts/e2e.sh             # full lifecycle: init → containment → snapshot → restore → rollback
```

TLA+ model checking (requires Java + [tla2tools.jar](https://github.com/tlaplus/tlaplus/releases)):

```bash
cd tla
bash check_suite.sh
```

## Pull requests

- Unit tests must pass
- `tla/VERIFIED.md` is the authoritative proof-scope document. If you change verified behavior, update the spec first and re-run TLC.
- Follow the commit convention: `<area>: <what changed>` (areas: `cloud`, `ux`, `privilege`, `docker`, `docs`, `test`)
- One logical change per commit
- If you change trust-boundary behavior, explain the rationale in the PR description and update the relevant docs.

## Community-Owned Surfaces

These are good starting points for outside contributors:

| Area | Why it matters |
|------|---------------|
| **Recipes** | Practical examples make Hazmat easier to adopt than abstract safety claims. |
| **Compatibility matrix** | Community reports turn "supported in theory" into "known to work on this host with these caveats." |
| **Integrations** | Bounded YAML manifests are a good scaling surface without opening arbitrary plugins. |
| **Docs / UX polish** | Clearer onboarding, better examples, and sharper troubleshooting reduce drop-off immediately. |
| **Evidence and incident tracking** | Hazmat's credibility comes from concrete threat models, not generic enthusiasm. |

## Security-relevant changes

If you change the seatbelt credential deny list, network policy, trust model, or containment boundaries — update `docs/design-assumptions.md` and add the rationale to your PR description.
