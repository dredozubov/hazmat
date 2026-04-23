# Community Model

Hazmat is open source, but not every part of it has the same risk profile.

The project can become community-scalable without turning the trust boundary into an everything-goes extension point. The rule is simple:

- community-editable data and compatibility knowledge should grow quickly
- containment and enforcement logic should stay under tighter maintainer review

If you are looking for where to contribute, start here before opening a PR.

## Support Tiers

Hazmat uses four support tiers for docs, recipes, integrations, and roadmap work.

| Tier | Meaning | Examples |
|------|---------|----------|
| **Verified Core** | Trust-boundary behavior with explicit proof scope or governance rules | setup / rollback ordering, seatbelt policy structure, backup safety, migration rules, launch fd isolation |
| **Supported** | Maintainer-reviewed features that ship as part of Hazmat and are expected to work for normal users | built-in harness paths, built-in integrations, native containment docs, CI-backed flows |
| **Community** | Repo-visible surfaces where outside contributors can add signal without weakening containment | recipes, compatibility reports, docs, integration proposals, incident repros, benchmark data |
| **Experimental** | Ideas or early paths that are useful to discuss, prototype, or document, but should not be mistaken for hardened defaults | proposed harness adapters, early recipe drafts, upcoming research threads |

These tiers are about expectation-setting, not prestige. A `Community` recipe can be extremely useful. A `Verified Core` change can still be wrong if it is not accompanied by the required analysis and tests.

## Ownership Model

### Maintainer-Owned

These areas stay under tighter review because they define or weaken the trust boundary:

- user isolation and account setup
- seatbelt / session policy structure
- `pf` firewall policy
- rollback and recovery behavior
- secret delivery and capability brokering
- anything governed by [tla/VERIFIED.md](../tla/VERIFIED.md)

### Community-Owned

These are the best places for wider contribution:

- integrations and integration proposals
- recipes
- compatibility reports
- harness bring-up notes and docs
- incident repros and evidence gathering
- UX and onboarding docs
- benchmark and drift data

## Where to Start

If you want a low-risk first contribution:

1. Add or improve a recipe in [docs/recipes/README.md](recipes/README.md).
2. File a compatibility report using the GitHub issue template and [docs/compatibility.md](compatibility.md).
3. Improve an integration proposal using [docs/integration-author-kit.md](integration-author-kit.md).
4. Tighten docs, examples, screenshots, or troubleshooting.

If you want to work closer to the core, read [docs/design-assumptions.md](design-assumptions.md), [docs/testing.md](testing.md), and [tla/VERIFIED.md](../tla/VERIFIED.md) first so the review conversation starts on concrete ground.

## GitHub Surfaces

The repo now uses structured issue templates for bugs, compatibility reports, integration requests, harness requests, and docs / UX problems.

The next GitHub-native step is enabling Discussions with these categories:

- **Recipes**
- **Compatibility reports**
- **RFCs**
- **Security research**

That manual repo-setting work is tracked separately so the docs and issue flow can land first.

## Public Roadmap

Hazmat uses `bd` internally for issue tracking. The public roadmap is a curated export from those issues so outside contributors can see real work instead of guessing.

- source config: [docs/public-roadmap.config.json](public-roadmap.config.json)
- generated page: [docs/public-roadmap.md](public-roadmap.md)
- exporter: `scripts/export-public-roadmap.sh`

The roadmap is intentionally curated. Not every internal bead belongs in the public output.

## Sponsor Lanes

The project already has a funding link, but the work benefits from clearer lanes.

| Lane | What funding helps with | Ownership model |
|------|--------------------------|-----------------|
| **Linux backend** | platform-specific setup/rollback modeling, disposable lifecycle testing, docs | maintainer-owned trust boundary |
| **Harness maintainers** | better auth/import/bootstrap coverage and harness UX | maintainer-reviewed supported surface |
| **Compatibility stewards** | matrix upkeep, regression triage, repro quality | community-owned |
| **Formal verification** | spec maintenance, new modeled areas, proof-boundary clarity | maintainer-owned |
| **Researcher bounties** | incident repros, CVE analysis, attack-surface writeups | community-owned with maintainer triage |

This does not mean funding buys architectural control. It means the project can explain what support pays for.
