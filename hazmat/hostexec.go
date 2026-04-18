package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// hostexec.go resolves macOS system utilities to absolute paths.
//
// Hazmat is a security tool, so it cannot trust the controlling user's
// ambient PATH to resolve chmod, sudo, ls, pfctl, etc. A hostile early
// PATH entry could substitute a malicious binary that hazmat then invokes
// with elevated privilege. A benign case — the one reported as issue #7 —
// is Homebrew coreutils shadowing /bin/chmod with GNU chmod, which
// doesn't speak macOS's +a/-a ACL syntax.
//
// Every hazmat invocation of a macOS base-system utility resolves its
// binary through this file. The hostexec guard (scripts/check-hostexec.sh,
// run in CI) forbids bare exec.Command("chmod"/"ls"/"sudo"/...) anywhere
// outside this file.
//
// PATH sanitization is deliberately narrow rather than process-global.
// Several call sites are intentionally PATH-dependent and must stay that
// way: $EDITOR for config editing, docker lookup for sandbox mode, brew
// for stack detection. Process-wide os.Setenv("PATH", ...) would silently
// break those; typed absolute-path resolution at each call site makes the
// trust boundary visible and auditable.

// Absolute paths to macOS base-system utilities. Paths are stable across
// every supported macOS version and match /etc/sudoers' secure_path.
const (
	hostSudoPath      = "/usr/bin/sudo"
	hostChmodPath     = "/bin/chmod"
	hostChownPath     = "/usr/sbin/chown"
	hostLsPath        = "/bin/ls"
	hostDsclPath      = "/usr/bin/dscl"
	hostPfctlPath     = "/sbin/pfctl"
	hostLaunchctlPath = "/bin/launchctl"
	hostUnamePath     = "/usr/bin/uname"
	hostScriptPath    = "/usr/bin/script"
	hostDiffPath      = "/usr/bin/diff"
	hostTeePath       = "/usr/bin/tee"
)

// gitAllowlistCandidates is the fixed set of git binary paths hazmat will
// accept, in preference order. Homebrew installations win over Xcode
// Command Line Tools at /usr/bin/git because the Xcode shim routes to
// Apple-shipped Git, not to the user's Homebrew git. On macOS Sequoia,
// /usr/bin/git is Apple Git ~2.50 while Homebrew git is ~2.53 — features
// like protocol v2 defaults differ, so naive /usr/bin/git substitution
// would silently downgrade functionality.
var gitAllowlistCandidates = []string{
	"/opt/homebrew/bin/git", // Apple Silicon Homebrew
	"/usr/local/bin/git",    // Intel Homebrew
	"/usr/bin/git",          // Xcode Command Line Tools shim (fallback)
}

var (
	gitPathOnce  sync.Once
	gitPathValue string
	gitPathErr   error
)

// hostGitPath returns the absolute path to git from the fixed allowlist,
// cached on first use to keep tool resolution deterministic within a run.
// Returns an error only if none of the candidates exists or is executable,
// which on a supported hazmat host means Xcode Command Line Tools are
// missing — a pre-existing prerequisite.
func hostGitPath() (string, error) {
	gitPathOnce.Do(func() {
		for _, candidate := range gitAllowlistCandidates {
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if info.Mode()&0o111 == 0 {
				continue
			}
			gitPathValue = candidate
			return
		}
		gitPathErr = fmt.Errorf("no git found in allowlist %v", gitAllowlistCandidates)
	})
	return gitPathValue, gitPathErr
}

// hostGitCommand builds an *exec.Cmd for git using the allowlisted binary.
// Returns nil + error if no allowlisted git is available; callers must
// check the error before invoking Run/Output.
func hostGitCommand(args ...string) (*exec.Cmd, error) {
	path, err := hostGitPath()
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...), nil
}

// hostGitOutput runs an allowlisted git command and returns stdout with
// trailing whitespace trimmed. Mirrors the semantics of execOutput("git", ...)
// so callers migrate 1:1.
func hostGitOutput(args ...string) (string, error) {
	cmd, err := hostGitCommand(args...)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// hostGitCombinedOutput runs an allowlisted git command and returns the
// combined stdout+stderr. Mirrors exec.Command("git", ...).CombinedOutput().
func hostGitCombinedOutput(args ...string) ([]byte, error) {
	cmd, err := hostGitCommand(args...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}
