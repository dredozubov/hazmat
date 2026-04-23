# Linux Capability and Distro Extension Proposal

Status: Proposed
Date: 2026-04-23
Related issue: `sandboxing-ofgd`
Related Linux implementation issue: `sandboxing-pk5x`

## Position

Linux tool bootstrapping is not just "Homebrew, but with apt and dnf." If
Hazmat expands to Linux, distribution differences can quickly become the
product. That would be a bad trade unless Linux support has a narrow,
documented boundary and a contribution model that users can understand without
learning Hazmat internals.

Hazmat should therefore make an explicit product decision before implementing
Linux bootstrap support:

1. Keep Hazmat macOS-only and document why.
2. Support Linux containment, but make tool bootstrapping mostly user-managed.
3. Support Linux containment plus a small capability bootstrap system with a
   strict distro-extension model.

This proposal documents option 3 because it is the most ambitious viable
shape. It does not argue that option 3 is automatically worth doing.

## Why This Is Risky

macOS gives Hazmat one dominant host package manager story for developer tools:
Homebrew. Linux does not. Even common development setups vary across:

- Debian, Ubuntu, Fedora, RHEL, Arch, openSUSE, Alpine, NixOS, and immutable
  variants
- glibc versus musl
- system package manager availability
- sudo availability
- distro package freshness
- corporate mirrors and locked-down hosts
- different sandbox, firewall, service, and account-management primitives

If Hazmat tries to own all of that directly, Linux support will become a matrix
of distro-specific install scripts. That is hard to review, hard to test, and
hard to explain.

## Goals

- Keep capability recipes portable across Linux distributions.
- Make distro support mostly data-driven.
- Let users add or fix support for their distribution without writing Go code
  for common package-name differences.
- Keep distro profiles non-executable and easy to review.
- Prefer agent-owned tool state over mutating the host OS package database.
- Make unsupported or quirky distros fail with clear instructions instead of
  partial setup.

## Non-Goals

- Full Linux parity before the setup/rollback model in `sandboxing-pk5x`.
- Arbitrary shell hooks in distro profiles.
- Maintaining a complete package database for every Linux distribution.
- Making Nix, asdf, mise, devbox, or any other meta-tool the mandatory
  abstraction.
- Supporting every distro before supporting a small representative set well.

## Recommended Abstraction

Separate the model into three layers.

### 1. Capability Recipes

A capability is the user-facing thing Hazmat can provide:

- `node`
- `python-uv`
- `rust`
- `go`
- `java`
- `ruby-bundler`
- `bun`
- `deno`
- `terraform` or `opentofu`

Recipes should define:

- project markers that suggest the capability
- whether the capability is safe to install agent-locally
- preferred provider order
- expected install location
- verification commands
- safety defaults
- rollback behavior

Recipes should not encode distro package-manager commands directly.

### 2. Provider Backends

A provider is executable Hazmat code that knows how to perform a bounded class
of install or resolution action.

Initial providers could include:

- `upstream-archive`
- `ecosystem-installer`
- `apt`
- `dnf`
- `pacman`
- `zypper`
- `apk`
- `manual`

The provider owns command templates, non-interactive flags, timeout behavior,
and dry-run rendering. Distro profiles should only supply package names and
policy facts.

### 3. Distro Profiles

A distro profile is a small YAML file describing local facts:

```yaml
linux_distro:
  id: ubuntu
  id_like: [debian]
  versions: [">=22.04"]
  package_manager: apt
  libc: glibc

system_package_install: supported

package_tokens:
  base.ca_certs: ca-certificates
  base.curl: curl
  base.git: git
  c.build_tools: build-essential
  python.runtime: python3
  python.venv: python3-venv
  java.jdk_21: openjdk-21-jdk
```

Profiles may contain:

- distro identity and version constraints
- package manager selection
- libc family
- supported architectures
- abstract package-token mappings
- preferred providers
- warnings
- manual notes

Profiles must not contain:

- shell commands
- URLs that execute code
- credential paths
- arbitrary environment variables
- package-manager flags beyond a bounded schema

## Provider Order

For most capabilities, Hazmat should try providers in this order:

1. **Agent-local upstream artifacts**
   Install into `/home/agent/.local/share/hazmat/toolchains/...` or
   `/home/agent/.local/bin` with checksum or signature verification.

2. **Ecosystem installers**
   Use tools such as `rustup`, `uv`, or `corepack` only when their state stays
   under the agent account or a Hazmat-managed tool home.

3. **Distro packages**
   Use `apt`, `dnf`, `pacman`, `zypper`, or `apk` only when a capability is
   OS-coupled, upstream artifacts are unsuitable, or the user explicitly
   chooses system packages.

4. **Manual instructions**
   If Hazmat cannot install safely, print the exact missing capability and
   suggested command instead of guessing.

This keeps OS mutation as a fallback, not the default.

## Extension Workflow

A user adding distro support should have a short, documented loop:

```bash
hazmat linux inspect --json
hazmat distro scaffold > ~/.hazmat/linux/distros/mydistro.yaml
hazmat distro lint ~/.hazmat/linux/distros/mydistro.yaml
hazmat capability explain node
hazmat bootstrap tool node --dry-run
hazmat distro test ~/.hazmat/linux/distros/mydistro.yaml --capability node
```

For repository contributors, the workflow should be similarly explicit:

```bash
scripts/e2e-linux-distro.sh ubuntu:24.04 node
scripts/e2e-linux-distro.sh fedora:41 python-uv
scripts/e2e-linux-distro.sh alpine:3.20 go
```

Every new distro PR should include:

- an `/etc/os-release` fixture
- a distro profile YAML file
- dry-run expected plan fixtures
- at least one container smoke test when the distro has a usable image
- documentation of known gaps

Privileged setup, rollback, firewall, account, and service tests remain gated
by the broader Linux lifecycle work in `sandboxing-pk5x`.

## Documentation Shape

Add a dedicated guide such as `docs/linux-distro-support.md` if this proposal
is accepted. It should be written as a contribution recipe:

1. Run `hazmat linux inspect`.
2. Check whether `ID` or `ID_LIKE` is already covered.
3. Scaffold a distro profile.
4. Fill in package-token mappings.
5. Run `hazmat distro lint`.
6. Run dry-run plans for common capabilities.
7. Add fixtures.
8. Open a PR.

The guide should include copyable examples for:

- Debian / Ubuntu
- Fedora / RHEL
- Arch
- openSUSE
- Alpine
- unsupported or immutable systems

## Handling Weird Distros

Some Linux environments should not be forced into a package-manager model.

For Alpine, the `musl` libc boundary matters. Upstream Linux binaries often
assume glibc, so provider matching must include libc family and possibly
minimum glibc version.

For NixOS and Guix, system package mutation is conceptually wrong. Hazmat
should prefer agent-local providers or manual instructions rather than trying
to write Nix expressions.

For immutable systems such as Fedora Silverblue, CoreOS, or corporate locked
down hosts, system package install may be unavailable even when the package
manager exists.

Profiles should be able to say:

```yaml
system_package_install: unsupported
preferred_providers:
  - upstream-archive
  - agent-local
```

## Decision Gates

Do not proceed with broad Linux capability bootstrapping unless these gates are
met:

- A new distro can be added with profile data plus fixtures for common package
  mapping differences.
- Capability recipes stay portable and do not fork per distro.
- Distro profiles remain non-executable.
- `hazmat bootstrap tool <name> --dry-run` shows an understandable plan.
- Rollback is obvious for agent-local installs.
- System package mutation is opt-in or clearly previewed.
- Alpine, NixOS, and immutable systems have explicit unsupported or degraded
  paths instead of accidental partial support.

If these gates cannot be met, Hazmat should not grow first-party Linux tool
bootstrapping. It should either stay macOS-only or offer Linux containment with
user-managed tooling.

## Recommendation

Treat Linux expansion as a separate product decision, not an inevitable next
platform. The current Hazmat value proposition is strong because the macOS
boundary is specific and legible. Linux support only preserves that quality if
the model stays small:

- capabilities are portable
- providers execute bounded actions
- distro profiles map local facts
- unsupported environments fail clearly

If Linux distro support becomes a long tail of package-manager quirks, the
better decision is to not extend Hazmat to Linux.
