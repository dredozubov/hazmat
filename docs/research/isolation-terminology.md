# Isolation System Terminology Research

Date: 2026-04-02
Context: Deciding how to name the distinction between hazmat's "packs" (passive
session config) and the proposed `--github` flag (authority delegation to an
external service). Research covers 15 security/isolation systems.

## Systems Surveyed

### Docker

- **Passive config:** volumes, configs, environment — data plumbing.
- **Authority:** capabilities (`--cap-add CAP_NET_ADMIN`), secrets (mounted
  read-only at `/run/secrets/`), `--privileged`.
- Docker uses "capability" for the Linux kernel concept and "secret" for
  credential delivery. The word "permission" is rarely used.

### Flatpak

- No separate passive config layer. Everything goes through **permissions**.
- **Static permissions** declared at build time in the app manifest:
  `--share=network`, `--socket=pulseaudio`, `--filesystem=home:ro`,
  `--device=dri`.
- **Portals** (`org.freedesktop.portal.*`) mediate dynamic access at runtime
  through UI interaction (file chooser, print dialog).
- **Overrides** let users tighten or loosen static permissions post-install
  via `flatpak override`.
- Fundamental split: static permissions (build-time holes) vs portals
  (runtime, user-mediated).

### Snap

- No separate passive config layer. All resource access flows through
  **interfaces**.
- **Interface** = the abstract capability (camera, network, desktop).
- **Plug** = a snap's declared need for a resource.
- **Slot** = a snap's (or the system's) provision of a resource.
- **Connection** = the link between a plug and slot.
- Key distinction is **auto-connected** (implicitly trusted, e.g. `network`)
  vs **manually-connected** (explicitly granted by the user).

### macOS App Sandbox

- No separate passive config layer.
- **Entitlements** = key-value pairs baked into the app's code signature.
  Boolean flags in a `.entitlements` plist file. Examples:
  `com.apple.security.network.client`,
  `com.apple.security.files.user-selected.read-only`,
  `com.apple.security.device.camera`.
- **Capabilities** = the Xcode UI concept. Toggling a capability in Xcode's
  Signing & Capabilities tab updates the entitlements file.
- Apple uses "capability" for what you toggle and "entitlement" for the
  signed key-value pair.

### Android

- No separate passive config layer.
- **Permissions** declared in `AndroidManifest.xml`. Protection levels define
  the taxonomy:
  - **Normal** = low-risk, granted automatically at install (`INTERNET`,
    `SET_WALLPAPER`).
  - **Dangerous** = runtime permissions, user-prompted (`CAMERA`, `LOCATION`,
    `CONTACTS`).
  - **Signature** = granted only if same signing certificate.
  - **Special** = particularly powerful, managed in system Settings.
- Key innovation: the **install-time vs runtime** split. The word "permission"
  covers everything.

### iOS / Xcode

- Same model as macOS: **capability** (UI toggle) -> **entitlement** (signed
  key-value) -> **provisioning profile** (authorization envelope).
- Three-layer hierarchy: capability = intent, entitlement = signed grant,
  provisioning profile = delivery mechanism.

### Firejail

- **Profiles** = per-application configuration files (`.profile`). Located in
  `/etc/firejail/` or `~/.config/firejail/`. A profile is a set of
  directives.
- Passive isolation: `private-bin`, `private-etc`, `private-tmp` (namespace
  isolation for specific filesystem areas).
- Authority: `caps` / `caps.drop` / `caps.keep` (Linux capabilities),
  `whitelist` / `blacklist` (path access), `seccomp` (syscall filtering),
  `net` / `dns` (network).
- Profiles are the organizing unit, containing both passive and authority
  directives.

### systemd

- **Passive / filesystem isolation:** `ProtectSystem=`, `ProtectHome=`,
  `PrivateTmp=`, `PrivateDevices=`, `ReadOnlyPaths=`, `BindReadOnlyPaths=`.
- **Privilege control:** `CapabilityBoundingSet=`, `AmbientCapabilities=`,
  `SystemCallFilter=`, `RestrictAddressFamilies=`, `NoNewPrivileges=`.
- Implicit distinction between `Protect*` / `Private*` (safe filesystem
  defaults) and `Capability*` / `Restrict*` (authority control).
- `systemd-analyze security <unit>` scores a unit's hardening level.

### Linux Capabilities

- The kernel primitive that Docker, systemd, and Kubernetes wrap.
- **Capability** = a distinct unit of privilege formerly bundled into root.
  Named `CAP_*`: `CAP_NET_ADMIN`, `CAP_SYS_ADMIN`, `CAP_DAC_OVERRIDE`, etc.
- Five per-thread sets: **effective** (active), **permitted** (ceiling for
  effective), **inheritable** (preserved across exec), **ambient** (transfers
  to unprivileged children), **bounding** (absolute ceiling).
- Purely about authority. No passive config concept — that is left to
  namespaces, cgroups, seccomp.

### AWS IAM

- **Policy** = JSON document defining permissions (Effect + Action + Resource
  + Condition). Seven types: identity-based, resource-based, permissions
  boundaries, SCPs, RCPs, ACLs, session policies.
- **Managed policies** = reusable, attachable permission bundles (closest to
  hazmat's packs as a convenience concept).
- **Permissions boundary** = "sets the maximum permissions that identity-based
  policies can grant." Effective permissions = intersection of policy and
  boundary. Structurally similar to hazmat's "packs cannot widen scope."
- **Roles** = delegation mechanism with trust policy (who can assume) and
  permission policies (what the assumer can do).
- IAM distinguishes "policy" (the document) from "permission" (the effective
  result).

### Tailscale ACLs / Grants

- **ACLs** (first generation) = network-layer only (IP, ports, protocols).
- **Grants** (second generation, recommended) = unified network + application
  layer access control.
  - **Network capabilities** (`ip`): `"tcp:443"`, `"udp:53"`.
  - **Application capabilities** (`app`):
    `"tailscale.com/cap/tailsql": [...]`.
- Key split: network capabilities (can you reach this host:port?) vs
  application capabilities (what can you do once connected?). The word
  "capability" here is closer to the capability-security sense than the Linux
  `CAP_*` sense.

### Deno

- **Permissions** = the universal term. "Deno is secure by default."
- **Allow flags:** `--allow-read` / `-R`, `--allow-write` / `-W`,
  `--allow-net` / `-N`, `--allow-env` / `-E`, `--allow-run`, `--allow-ffi`.
- **Deny flags:** `--deny-read`, `--deny-net`, etc. Deny takes precedence.
- **Permission sets** in `deno.json` = named bundles of grants, invoked with
  `-P=<set-name>`. Closest analogue to hazmat's packs.
- Scoped grants: `--allow-read=./data`, `--allow-net=api.example.com:443`.

### Podman

- Terminology is intentionally Docker-compatible: volumes, capabilities
  (`--cap-add`/`--cap-drop`), secrets, `--privileged`.
- **Secrets** can be `type=mount` (file at `/run/secrets/`) or `type=env`
  (environment variable) — cleaner distinction than Docker's swarm-only model.
- **Quadlet files** (`.container`, `.volume`) = declarative config that
  generates systemd units.

### NixOS

- **Passive config:** packages (`environment.systemPackages`), modules
  (composable config units), options (`programs.git.enable = true`).
- **Authority:** `security.wrappers` (setuid/capabilities for binaries),
  `networking.firewall.allowedTCPPorts`, `users.users.<name>.extraGroups`.
- Natural separation between `programs.*` / `environment.*` (passive) and
  `security.*` (authority), but both live in the same config language with
  no enforced boundary.

### Kubernetes

- **Passive config:** ConfigMaps (non-sensitive key-value data), volumes.
- **Authority:**
  - SecurityContext: `capabilities: add: / drop:`, `runAsNonRoot`,
    `readOnlyRootFilesystem`, `allowPrivilegeEscalation`.
  - **Secrets** = "specifically intended to hold confidential data." Distinct
    API object from ConfigMaps.
  - **Pod Security Standards** = three named profiles: Privileged, Baseline,
    Restricted. The Restricted profile is a ceiling.
  - **RBAC** = Roles (permission sets) bound to subjects via RoleBindings.
- Most layered terminology of any system surveyed.

## Cross-System Patterns

### 1. The two-layer split

Mobile/desktop sandboxes (Android, iOS, macOS, Flatpak, Snap) do NOT
separate passive config from authority grants — everything is a "permission"
or "entitlement." The split only emerges in server/infrastructure systems
(Docker, K8s, systemd, NixOS) where operational convenience demands
non-security config bundles.

### 2. Dominant terms for authority grants

| Term | Used by | Notes |
|------|---------|-------|
| **Permission** | Android, Flatpak, Deno, AWS | Most universally understood. Generic. |
| **Capability** | Linux, Docker, systemd, K8s | Discrete unit of privilege. Overloaded. |
| **Entitlement** | Apple (macOS + iOS) | Right baked into identity. Apple-specific. |
| **Grant** | Tailscale, AWS | Active voice, emphasizes delegation act. |
| **Interface** | Snap | Abstract resource with plug/slot metaphor. |

### 3. "Pack" has no collision

No surveyed system uses "pack" for anything security-related. Closest
analogues: Firejail profiles, Deno permission sets, K8s ConfigMaps, AWS
managed policies. The term is clean and unambiguous.

### 4. The "ceiling that can't be widened" concept

- AWS: permissions boundary
- Linux: bounding set
- K8s: Pod Security Standards (Restricted profile)
- Hazmat: "packs may not weaken the trust boundary"

Same structural role across systems.

### 5. Credential delivery is often separate from permissions

- Docker/Podman/K8s: **Secrets** as a distinct concept from capabilities
- AWS: Secrets Manager separate from IAM
- Relevant to hazmat: `GH_TOKEN` is credential delivery, not just a
  permission toggle.

## Candidates for Hazmat's Authority Concept

| Term | Pros | Cons |
|------|------|------|
| **Permission** | Universally understood (Android, Deno) | Hazmat doesn't have a permissions system; feels oversized for one flag. Risk of confusion with file permissions. |
| **Capability** | Precise in Linux/Tailscale sense | Heavily overloaded. Docker users think `CAP_NET_ADMIN`. macOS users think Xcode checkbox. Hazmat already uses Apple sandbox primitives. |
| **Entitlement** | Clean semantics ("this session is entitled to GitHub access") | Apple-specific connotation. Could create confusion with actual macOS entitlements since hazmat is macOS-only. |
| **Grant** | Active voice, emphasizes delegation. No overloading with systems hazmat touches. | Less widely recognized than "permission." |
| **Allow** | Deno's verb (`--allow-net`). Simple, direct. Already the verb in the design doc (`hazmat config github allow`). | Not a noun — no category name ("GitHub is an allow" doesn't work). |

### Observation

The design doc already uses `allow/deny` for config verbs, matching Deno's
pattern. If hazmat only ships `--github`, a category noun may not be needed —
"GitHub access" as a session flag is sufficient. If more service flags follow
(`--linear`, `--slack`), then **"grant"** or **"permission"** would be the
cleanest category name. "Grant" has the advantage of zero collision with
existing macOS/Linux terminology in hazmat's ecosystem.
