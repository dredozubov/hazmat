# macOS Sandboxing Internals

Deep dive into the OS-level mechanisms available for sandboxing processes on macOS.

## sandbox-exec / Seatbelt (SBPL)

### What It Is

`sandbox-exec` is macOS's built-in command-line sandboxing tool, part of the "Seatbelt" framework. It uses kernel-level enforcement via the Mandatory Access Control Framework (MACF), with approximately 300 kernel hooks evaluating operations against policies.

### Current Status

- **Officially deprecated** since macOS Sierra (2016), but **still fully functional** as of macOS 26 Tahoe (confirmed through 26.4)
- Apple's own system software uses Seatbelt extensively (built-in profiles at `/System/Library/Sandbox/Profiles`)
- Apple has provided **no replacement** for sandboxing arbitrary CLI processes
- Production users: Claude Code, Codex CLI, Gemini CLI, Cursor, Chromium, Firefox, Swift Package Manager, Homebrew, Bazel, Nix, and numerous security tools

### Deprecation Analysis (Updated April 2026)

The `sandbox-exec(1)` man page has carried "DEPRECATED" since macOS 10.13.6 High Sierra (~2017). The `sandbox_init(3)` C API has carried `__deprecated` annotations since macOS 10.8 Mountain Lion (2012). **Nine years deprecated, zero removal signals, no functional degradation.**

**Why removal is unlikely in the near term:**

1. **Apple's own apps depend on it.** Safari, Mail, Quick Look, and dozens of system daemons use Seatbelt profiles. Removing kernel enforcement would require Apple to rewrite its own security stack.
2. **Major third-party dependencies.** Chromium uses `sandbox_init_with_parameters()` by dynamically loading from `libsandbox.dylib`. Firefox, Swift Package Manager, Homebrew, Bazel, and Nix all depend on the same APIs.
3. **Every AI agent sandbox tool on macOS depends on it.** Claude Code's `/sandbox` command, Codex CLI, Gemini CLI (six built-in Seatbelt profiles), Cursor's agent sandbox, Agent Safehouse, nono, and Hazmat all use sandbox-exec or sandbox_init.
4. **macOS 26 Tahoe release notes are silent.** No mention of sandbox API changes or removals. ([Source](https://developer.apple.com/documentation/macos-release-notes/macos-26-release-notes))
5. **No WWDC session has discussed a replacement.** Apple Developer Forums thread 661939 ("How to build a replacement for sandbox-exec?") has no official Apple engineer response providing a path forward. ([Source](https://developer.apple.com/forums/thread/661939))

**App Sandbox is not a replacement.** The official recommendation is the App Sandbox (entitlements-based), but it cannot sandbox arbitrary CLI processes at runtime — it requires re-signing at build time. Mark Rowe's analysis states directly: "In practice, the App Sandbox is not a usable replacement for many large applications. Apple continues to make use of these lower-level APIs for sandboxing its first-party applications and helper tools." ([Source](https://bdash.net.nz/posts/sandboxing-on-macos/))

**Apple Containerization is not a replacement either.** The Containerization framework (WWDC 2025) runs Linux VMs via `Virtualization.framework`. It cannot sandbox macOS-native processes. See [vm-tools-comparison.md](vm-tools-comparison.md) for the full analysis.

**The real risk is silent behavioral change, not removal.** Apple is more likely to modify which SBPL operations are honored than to remove the API. Historical precedent: the launchd rewrite in macOS 10.10 broke Chromium's bootstrap sandbox without any deprecation notice. Hazmat's `hazmat check` validates sandbox behavior at runtime, and the architecture treats seatbelt as defense-in-depth — user isolation and pf firewall are the primary containment layers.

**Industry pattern:** Everyone uses it, everyone acknowledges the deprecation, nobody has a migration plan.

**References:**
- [Sandboxing on macOS — Mark Rowe](https://bdash.net.nz/posts/sandboxing-on-macos/) — Best technical analysis of the deprecation gap
- [Codex CLI issue #215](https://github.com/openai/codex/issues/215) — OpenAI acknowledged deprecation but kept sandbox-exec because "it still works very well"
- [HN: sandbox-exec Deprecation Discussion](https://news.ycombinator.com/item?id=44283454) — Community analysis including Apple insider perspective
- [Apple Developer Forums thread 661939](https://developer.apple.com/forums/thread/661939) — No official Apple response on replacement path
- [macOS 26 Release Notes](https://developer.apple.com/documentation/macos-release-notes/macos-26-release-notes) — Silent on sandbox API changes
- [Chromium Mac Sandbox V2 Design Doc](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/sandbox/mac/seatbelt_sandbox_design.md) — Documents Chromium's dependency on `sandbox_init_with_parameters()`

### What It Can Restrict

| Capability | SBPL Operations |
|---|---|
| Filesystem reads | `file-read*`, `file-read-data`, `file-read-metadata` |
| Filesystem writes | `file-write*`, `file-write-data`, `file-write-create` |
| Network (outbound) | `network-outbound`, `network*` |
| Network (inbound/bind) | `network-inbound`, `network-bind` |
| Process execution | `process-exec`, `process-fork` |
| IPC / Mach services | `mach-lookup`, `ipc-posix-sem` |
| IOKit / devices | `iokit-open` |
| PTY / terminal | `pseudo-tty`, `file-ioctl` |
| System info | `process-info*`, `sysctl-read` |

### SBPL Syntax

Profiles use a Scheme-like DSL:

```scheme
(version 1)
(deny default)                              ; deny-by-default

; Path filters
(allow file-read* (literal "/exact/path"))  ; exact match
(allow file-read* (subpath "/dir"))         ; directory and descendants
(allow file-read* (regex "^/pattern"))      ; regex match
(allow file-read* (param "HOME"))           ; dynamic parameter

; Running
; PROJECT_DIR=$(pwd) sandbox-exec -f profile.sb <command>
```

### Node.js Gotcha

Node.js crashes without `(allow file-ioctl)` because `tcsetattr()` requires it for terminal raw mode. This is the #1 issue when sandboxing Node.js with Seatbelt.

### Limitations

1. **No resource limits** — cannot restrict CPU, memory, or disk quotas
2. **No process namespace isolation** — sandboxed processes can see other system processes
3. **Network granularity is limited** — allow/deny all network, but no domain/IP filtering at kernel level
4. **GPU/Metal cannot be blocked** via SBPL
5. **Child process inheritance** — child processes inherit restrictions (good for security)

### Known CVEs

- CVE-2025-31191: Security-scoped bookmark sandbox escape
- CVE-2025-43358: Shortcuts sandbox bypass
- These affect App Sandbox more than raw `sandbox-exec` profiles

### Debugging

```bash
log stream --style compact --predicate 'sender=="Sandbox"'
```

## TCC (Transparency, Consent, and Control)

### Terminal Inheritance Problem

When you run Claude Code in Terminal.app, **TCC uses Terminal.app's permissions**, not the child process's. If Terminal has Full Disk Access, so does everything inside it.

### Mitigations

1. **Do NOT grant Terminal.app Full Disk Access** unless absolutely needed
2. Use a **separate terminal emulator** for running agents — give it minimal TCC permissions
3. Revoke with: `tccutil reset All com.apple.Terminal`
4. No CLI for granting TCC — only for revoking

### Configuration Profiles (PPPC)

MDM-style `.mobileconfig` profiles can pre-approve or deny TCC permissions per bundle ID. Create with **iMazing Profile Editor** (free). User-installed profiles can always be removed by the user.

## Hardened Runtime

Protects binary runtime integrity (code injection, DYLD hijacking). **Not useful for restricting Claude Code** — it's a build-time/signing-time property that cannot be applied externally to arbitrary processes.

## System Integrity Protection (SIP)

Protects system-owned files (`/System`, `/bin`, `/sbin`, `/usr` except `/usr/local`):
- Sanitizes `DYLD_LIBRARY_PATH` for system binaries
- Prevents code injection into protected executables
- Acts as a global always-on baseline

**Never disable SIP.** It provides a foundational safety net that overrides process-specific sandbox rules.

## Launch Constraints (macOS Ventura+)

Control who, how, and from where a process can be launched via AMFI (AppleMobileFileIntegrity). Three constraint types: self, parent, responsible process.

**Not useful for sandboxing agents** — designed to prevent exploitation of system processes, not to restrict user-initiated tools.

## macOS 15.1+ Code Signing Enforcement

macOS 15.1 blocks all unsigned code by default. Unsigned apps need quarantine attribute removal: `xattr -d com.apple.quarantine /path/to/app`. Not a reliable isolation mechanism since ad-hoc signing (`codesign -s -`) trivially bypasses it.
