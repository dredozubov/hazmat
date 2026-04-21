#!/bin/bash
#
# Guard: hazmat must resolve macOS system utilities by absolute path.
#
# Bare exec.Command("chmod", ...) inherits the controlling user's PATH,
# which on a Homebrew-coreutils machine silently substitutes GNU chmod for
# /bin/chmod (issue #7) and on a hostile machine could substitute a
# malicious binary. All system-utility calls go through the hostexec.go
# path constants instead.
#
# This script fails CI if a bare exec.Command on a known system utility
# appears outside hazmat/hostexec.go.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Utilities whose bare-name invocation is forbidden anywhere in hazmat
# source. Extend this list as new absolute-path wrappers are added to
# hostexec.go — keeping the list in sync with hostexec.go is the point.
FORBIDDEN_UTILS='chmod|chown|ls|sudo|dscl|pfctl|launchctl|uname|script|diff|tee|git'

# Allowlist files where the absolute-path constants are defined.
# hostexec*.go holds the platform path tables and git allowlist resolver;
# the comments there intentionally describe the forbidden pattern.
ALLOWED_FILES='^hazmat/hostexec(_[a-z]+)?\.go:'

# grep for bare exec.Command("<util>", ...) and execOutput("<util>", ...).
# We intentionally scan both Go sources and test files: allowlisted paths
# belong everywhere system utilities are invoked.
PATTERN="(exec\.Command|execOutput|commandStdout)\\(\"($FORBIDDEN_UTILS)\""

matches=$(grep -rnE --include='*.go' "$PATTERN" hazmat/ 2>/dev/null \
    | grep -vE "$ALLOWED_FILES" \
    || true)

if [ -n "$matches" ]; then
    printf '\033[31mhostexec guard: forbidden bare system-utility invocation\033[0m\n\n'
    printf '%s\n' "$matches"
    printf '\nUse the absolute-path constants in hazmat/hostexec.go '
    printf '(hostChmodPath, hostSudoPath, etc.) or the hostGit* helpers.\n'
    exit 1
fi

printf '\033[32m✓\033[0m hostexec guard: no bare system-utility invocations\n'
