# Seatbelt Profile Reference

## SBPL (Sandbox Profile Language) Quick Reference

### Syntax

```scheme
(version 1)                    ; required header
(deny default)                 ; deny-by-default (recommended)

; Operations
(allow file-read*)             ; all file reads
(allow file-write*)            ; all file writes
(allow file-read-data ...)     ; file content reads only
(allow file-read-metadata ...) ; file metadata only
(allow network*)               ; all network
(allow network-outbound ...)   ; outbound only
(allow process-exec ...)       ; execute binaries
(allow process-fork)           ; fork child processes
(allow mach-lookup ...)        ; Mach IPC
(allow pseudo-tty)             ; terminal support
(allow file-ioctl)             ; ioctl (needed by Node.js)

; Path filters
(literal "/exact/path")        ; exact match
(subpath "/dir")               ; directory and all descendants
(regex "^/pattern")            ; regex
(param "VAR_NAME")             ; dynamic parameter (from env)
```

### Running

```bash
# With parameters
VAR=value sandbox-exec -f profile.sb <command>

# Example
PROJECT_DIR=$(pwd) TMPDIR=$TMPDIR HOME=$HOME sandbox-exec -f profile.sb node app.js
```

## Ready-to-Use Profiles

### Profile 1: Restrictive — No Network, Project Dir Only

For maximum isolation. Agent can only read/write the project directory and system libraries.

```scheme
(version 1)
(deny default)

;; Process execution
(allow process-exec (literal "/usr/local/bin/node"))
(allow process-exec (subpath "/usr/bin"))
(allow process-exec (subpath "/opt/homebrew/bin"))
(allow process-fork)
(allow process-info* (target same-sandbox))

;; System libraries (Node.js needs these)
(allow file-read* (subpath "/usr/lib"))
(allow file-read* (subpath "/usr/share"))
(allow file-read* (subpath "/System/Library"))
(allow file-read* (subpath "/Library/Frameworks"))
(allow file-read* (subpath "/private/etc"))
(allow file-read* (literal "/dev/urandom"))
(allow file-read* (literal "/dev/null"))
(allow file-read* (subpath "/usr/local"))
(allow file-read* (subpath "/opt/homebrew"))

;; Project directory: read + write
(allow file-read* (subpath (param "PROJECT_DIR")))
(allow file-write* (subpath (param "PROJECT_DIR")))

;; Temp directories
(allow file-read* file-write* (subpath (param "TMPDIR")))
(allow file-read* file-write* (subpath "/private/tmp"))
(allow file-read* file-write* (subpath "/private/var/folders"))

;; Terminal support (Node.js crashes without this!)
(allow pseudo-tty)
(allow file-ioctl)
(allow file-read* file-write* (literal "/dev/ptmx"))
(allow file-read* file-write* (regex #"/dev/ttys[0-9]+"))

;; Mach services for basic operation
(allow mach-lookup (global-name "com.apple.system.logger"))
(allow mach-lookup (global-name "com.apple.CoreServices.coreservicesd"))
(allow mach-host*)

;; DENY network
(deny network*)

;; DENY sensitive directories
(deny file-read* (subpath (string-append (param "HOME") "/.ssh")))
(deny file-read* (subpath (string-append (param "HOME") "/.aws")))
(deny file-read* (subpath (string-append (param "HOME") "/.gnupg")))
(deny file-read* (subpath (string-append (param "HOME") "/Library/Keychains")))
(deny file-read* (subpath (string-append (param "HOME") "/.config/gh")))
```

Run:
```bash
PROJECT_DIR=$(pwd) TMPDIR=$TMPDIR HOME=$HOME \
  sandbox-exec -f restrictive.sb claude --dangerously-skip-permissions
```

### Profile 2: Moderate — API Access Allowed

Same as Profile 1 but with network access for the Anthropic API.

```scheme
(version 1)
(deny default)

;; Process execution
(allow process-exec (literal "/usr/local/bin/node"))
(allow process-exec (subpath "/usr/bin"))
(allow process-exec (subpath "/opt/homebrew/bin"))
(allow process-fork)
(allow process-info* (target same-sandbox))

;; System libraries
(allow file-read* (subpath "/usr/lib"))
(allow file-read* (subpath "/usr/share"))
(allow file-read* (subpath "/System/Library"))
(allow file-read* (subpath "/Library/Frameworks"))
(allow file-read* (subpath "/private/etc"))
(allow file-read* (literal "/dev/urandom"))
(allow file-read* (literal "/dev/null"))
(allow file-read* (subpath "/usr/local"))
(allow file-read* (subpath "/opt/homebrew"))

;; Project directory
(allow file-read* (subpath (param "PROJECT_DIR")))
(allow file-write* (subpath (param "PROJECT_DIR")))

;; Claude config (needed for auth)
(allow file-read* (subpath (string-append (param "HOME") "/.claude")))
(allow file-write* (subpath (string-append (param "HOME") "/.claude")))

;; Temp
(allow file-read* file-write* (subpath (param "TMPDIR")))
(allow file-read* file-write* (subpath "/private/tmp"))
(allow file-read* file-write* (subpath "/private/var/folders"))

;; Terminal
(allow pseudo-tty)
(allow file-ioctl)
(allow file-read* file-write* (literal "/dev/ptmx"))
(allow file-read* file-write* (regex #"/dev/ttys[0-9]+"))

;; Mach services
(allow mach-lookup (global-name "com.apple.system.logger"))
(allow mach-lookup (global-name "com.apple.CoreServices.coreservicesd"))
(allow mach-lookup (global-name "com.apple.system.notification_center"))
(allow mach-host*)

;; Network: allow outbound (needed for API calls)
(allow network-outbound)
(allow network-inbound (local tcp "*:*"))  ; for local dev servers

;; DENY sensitive directories
(deny file-read* (subpath (string-append (param "HOME") "/.ssh")))
(deny file-read* (subpath (string-append (param "HOME") "/.aws")))
(deny file-read* (subpath (string-append (param "HOME") "/.gnupg")))
(deny file-read* (subpath (string-append (param "HOME") "/Library/Keychains")))
(deny file-read* (subpath (string-append (param "HOME") "/.config/gh")))
(deny file-read* (subpath (string-append (param "HOME") "/Library/Application Support/Google/Chrome")))
(deny file-read* (subpath (string-append (param "HOME") "/Library/Application Support/Firefox")))
```

### Profile 3: Read-Only Exploration

For when you want Claude to analyze code but not modify anything.

```scheme
(version 1)
(deny default)

;; Process execution
(allow process-exec (literal "/usr/local/bin/node"))
(allow process-exec (subpath "/usr/bin"))
(allow process-exec (subpath "/opt/homebrew/bin"))
(allow process-fork)
(allow process-info* (target same-sandbox))

;; System libraries
(allow file-read* (subpath "/usr/lib"))
(allow file-read* (subpath "/usr/share"))
(allow file-read* (subpath "/System/Library"))
(allow file-read* (subpath "/Library/Frameworks"))
(allow file-read* (subpath "/private/etc"))
(allow file-read* (literal "/dev/urandom"))
(allow file-read* (literal "/dev/null"))
(allow file-read* (subpath "/usr/local"))
(allow file-read* (subpath "/opt/homebrew"))

;; Project directory: READ ONLY
(allow file-read* (subpath (param "PROJECT_DIR")))
;; No file-write* for project dir!

;; Claude config (read only)
(allow file-read* (subpath (string-append (param "HOME") "/.claude")))

;; Temp (Claude needs some temp writes to function)
(allow file-read* file-write* (subpath (param "TMPDIR")))
(allow file-read* file-write* (subpath "/private/tmp"))
(allow file-read* file-write* (subpath "/private/var/folders"))

;; Terminal
(allow pseudo-tty)
(allow file-ioctl)
(allow file-read* file-write* (literal "/dev/ptmx"))
(allow file-read* file-write* (regex #"/dev/ttys[0-9]+"))

;; Mach
(allow mach-lookup (global-name "com.apple.system.logger"))
(allow mach-host*)

;; Network for API
(allow network-outbound)

;; DENY sensitive
(deny file-read* (subpath (string-append (param "HOME") "/.ssh")))
(deny file-read* (subpath (string-append (param "HOME") "/.aws")))
(deny file-read* (subpath (string-append (param "HOME") "/.gnupg")))
(deny file-read* (subpath (string-append (param "HOME") "/Library/Keychains")))
```

## Common Issues

### Node.js Crashes Silently

Almost always caused by missing `(allow file-ioctl)`. Node.js needs `tcsetattr()` for terminal raw mode.

### DNS Resolution Fails

If you deny `network*`, DNS also fails. You need to either:
- Allow all network: `(allow network*)`
- Or allow specific Mach lookups for DNS: `(allow mach-lookup (global-name "com.apple.mDNSResponder"))`

### Git Operations Fail

Git needs to read `~/.gitconfig` and `~/.config/git/`. Add:
```scheme
(allow file-read* (literal (string-append (param "HOME") "/.gitconfig")))
(allow file-read* (subpath (string-append (param "HOME") "/.config/git")))
```

### npm/pnpm Fail

Package managers need cache access:
```scheme
(allow file-read* file-write* (subpath (string-append (param "HOME") "/.npm")))
(allow file-read* file-write* (subpath (string-append (param "HOME") "/.pnpm-store")))
```

## Debugging Sandbox Violations

Watch the system log for sandbox denials:
```bash
log stream --style compact --predicate 'sender=="Sandbox"'
```

Each denial shows the operation, path, and profile rule that blocked it.
