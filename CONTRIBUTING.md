# Contributing to Hazmat

Hazmat is early-stage and actively evolving. The most valuable contributions right now are **feedback, bug reports, and UX improvements** — not just code.

## The easiest way to help

1. **Try `hazmat init` and tell us what happened.** Did a step fail? Was a prompt confusing? Did something take longer than expected? [Open an issue](https://github.com/dredozubov/hazmat/issues) with your macOS version and a copy of the output. Rough reports are fine — we'd rather hear about it than not.

2. **Run `hazmat claude` on a real project.** Did the agent break something? Did a snapshot save you? Was restore seamless or janky? Did the seatbelt block something it shouldn't have? Every real-world session is a test case.

3. **Read the docs and tell us what's missing.** If you had to guess what a command does, that's a UX bug. If a design decision isn't explained, that's a docs bug.

## Reporting issues

- **UX issues:** confusing prompts, unnecessary sudo, unexpected behavior, missing feedback
- **Containment issues:** something the agent shouldn't be able to do but can, credential leaks, policy gaps
- **Compatibility issues:** macOS version, shell, toolchain, VPN/proxy, specific npm/pip packages that break under `ignore-scripts`

Include your macOS version (`sw_vers`) and hazmat version (`hazmat --version`).

## Security reports

If you find a containment bypass — a way for the agent to read host credentials, escape the seatbelt, bypass the firewall, or escalate privileges — please report it via [GitHub Issues](https://github.com/dredozubov/hazmat/issues). We don't have a formal security disclosure process yet; just open an issue.

## Building

```bash
cd hazmat
make all           # builds hazmat + hazmat-launch (requires Xcode CLI tools for cgo)
make test          # unit tests
make e2e           # full lifecycle test (needs sudo, modifies system)
make e2e-vm        # same test inside an isolated Lume VM (no system changes)
```

## Testing

```bash
make test                    # unit tests + go vet
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

## High-impact areas

These are the areas where contributions would make the biggest difference:

| Area | Why it matters |
|------|---------------|
| **Linux port** | Most agent users are on Linux. Same architecture (user isolation + kernel sandbox + firewall), different primitives (namespaces, seccomp, nftables). This is the single biggest unlock for adoption. |
| **UX polish** | Every unnecessary prompt, confusing message, or unexplained sudo is a reason someone stops using hazmat. The bar is "it should feel like a normal CLI tool." |
| **Supply chain hardening** | npm `ignore-scripts` is a start. What about pip `setup.py`? Cargo `build.rs`? MCP server pinning? Each one closes a real attack class. |
| **Observable containment** | What did the agent change? What network connections did it make? Post-session audit logs would make hazmat feel like a debugger for agent behavior. |
| **Agent presets** | Named configurations for common tools (Codex, Aider, custom loops) that set the right flags, read-only dirs, and env vars. |

## Security-relevant changes

If you change the seatbelt credential deny list, network policy, trust model, or containment boundaries — update `docs/design-assumptions.md` and add the rationale to your PR description.
