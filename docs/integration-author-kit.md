# Integration Author Kit

Integrations are Hazmat's first community-scalable surface on purpose.

They are bounded, auditable, and intentionally weaker than a plugin system. An integration can reduce friction for a stack. It cannot widen write scope, bypass the credential deny list, or change network policy.

If you want to propose a built-in integration or tighten an existing one, this is the contract to work from.

If you are still deciding how a normal user discovers the integration path, read
[docs/integration-contributor-flow.md](integration-contributor-flow.md) first.
This page focuses on the manifest contract after someone decides to create or
review an integration.

## What Integrations Can and Cannot Do

### Allowed

- add read-only toolchain or cache paths
- add snapshot excludes
- pass through a small allowlisted set of environment selectors
- expose warnings and common commands

### Not Allowed

- widen write scope
- expose credentials
- inject arbitrary runtime flags
- change firewall behavior
- execute hooks or inline code from the manifest

For the user-facing trust model, see [docs/integrations.md](integrations.md).

## Manifest Shape

The current manifest schema is intentionally small:

- `integration`
- `detect`
- `session`
- `backup`
- `warnings`
- `commands`

Unknown fields are rejected at load time. That is deliberate. Typos should fail closed.

The example manifest lives at [docs/examples/integration-template.yaml](examples/integration-template.yaml).

## Why Ownership Metadata Is Not in the Manifest

Support tier and ownership information matter, but they are not part of the execution contract.

Hazmat keeps the manifest schema focused on execution-relevant behavior. Support tier, community ownership, and roadmap state live in docs such as [docs/community.md](community.md), [docs/public-roadmap.md](public-roadmap.md), and [docs/compatibility.md](compatibility.md), not in the runtime manifest.

That keeps the schema tight and easier to audit.

## Author Checklist

Before sending a PR, make sure the proposed integration:

1. has a clear stack boundary and problem statement
2. uses only read-only paths
3. uses only safe env passthrough keys
4. has conservative snapshot excludes
5. includes useful warnings where the workflow has sharp edges
6. includes realistic build / test / lint commands
7. does not rely on hidden host state outside the documented contract

## Required Validation

At minimum, a built-in integration PR should include:

- manifest schema validity
- path validation safety
- at least one focused test or fixture update when behavior changes
- docs update when the integration changes user-visible behavior

The repo already checks built-in manifests at load time. The example template is also covered by a test so the author kit does not drift away from the real parser contract.

## Review Expectations

Maintainers will usually ask:

- is this a good built-in integration, or should it stay a recipe first?
- are the read-only paths truly safe?
- is there a simpler integration shape with fewer assumptions?
- are the warnings honest about caveats?
- is this still path-based, or is it starting to look like a policy escape?

## Good PR Shape

A strong integration PR usually contains:

- one manifest change
- one or two tests
- one doc change
- one compatibility or recipe note if the stack is user-facing

Keep it small. If the integration needs a design essay to justify why it is safe, it may not belong in the bounded manifest system.
