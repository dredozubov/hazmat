# macOS Sandboxing Internals

Deep dive into the OS-level mechanisms available for sandboxing processes on macOS.

## sandbox-exec / Seatbelt (SBPL)

### What It Is

`sandbox-exec` is macOS's built-in command-line sandboxing tool, part of the "Seatbelt" framework. It uses kernel-level enforcement via the Mandatory Access Control Framework (MACF), with approximately 300 kernel hooks evaluating operations against policies.

### Current Status

- **Officially deprecated** since macOS Sierra (2016), but **still fully functional** as of macOS 15.4 (Sequoia)
- Apple's own system software uses Seatbelt extensively (built-in profiles at `/System/Library/Sandbox/Profiles`)
- Apple has provided **no replacement** for sandboxing arbitrary CLI processes
- Production users: Claude Code, Cursor, Gemini CLI, Chromium, Firefox, and numerous security tools

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
