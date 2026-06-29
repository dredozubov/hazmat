// hazmat-launch is the narrow privileged helper that bridges the dr→agent
// privilege gap for Hazmat-owned agent operations.
//
// It is the sole binary covered by the NOPASSWD sudoers rule:
//
//	dr ALL = (agent) NOPASSWD: /usr/local/libexec/hazmat-launch
//	dr ALL = (agent) NOPASSWD: sha256:<digest> /Users/dr/.local/libexec/hazmat-launch
//
// Unlike granting NOPASSWD to /usr/bin/sandbox-exec directly, this helper:
//   - Accepts only a policy file path (no inline policies)
//   - Validates the policy file is at /private/tmp/hazmat-<pid>.sb
//   - Validates the policy file is owned by the invoking user (SUDO_UID),
//     not by root or agent, to prevent pre-planted policy substitution.
//     The explicit --hazmat-current-user mode instead validates ownership by
//     the current uid for same-user Seatbelt launch; the sudo bridge remains
//     the default.
//   - Validates the policy file has mode 0644
//   - Validates the policy contains (deny default)
//
// Instead of exec'ing sandbox-exec, the Darwin sandbox backend calls
// sandbox_init() directly via cgo and then execs the target command. This
// eliminates the sandbox-exec process from the chain, giving:
//   - Correct signal forwarding (the process IS the target)
//   - Proper PTY handling for interactive TUI applications
//   - One fewer process in the sudo → sandbox → target chain
//
// Usage:
//
//	sudo -u agent <hazmat-launch> <policy-file> <cmd> [args...]
//	sudo -u agent <hazmat-launch> exec <cmd> [args...]

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// policyFilePattern enforces that the policy file lives under /private/tmp
// and follows the hazmat-<pid>.sb naming convention.  The numeric PID
// component prevents traversal attacks (/private/tmp/../../etc/...).
var policyFilePattern = regexp.MustCompile(`^/private/tmp/hazmat-\d+\.sb$`)

// denyDefaultMarker is the literal string that every legitimate generated
// session policy starts with. Policies that lack it (e.g. hand-written
// "(version 1)(allow default)" files) are rejected.
const denyDefaultMarker = "(deny default)"

const metadataJSONArg = "--hazmat-metadata-json"
const launchProfileArg = "--hazmat-launch-profile"
const currentUserArg = "--hazmat-current-user"
const directExecArg = "--hazmat-direct-exec"
const workingDirArg = "--hazmat-working-dir"
const envArg = "--hazmat-env"
const sessionTempArg = "--hazmat-session-temp"

var sessionTempRoot = "/Users/agent/.cache/hazmat/tmp"
var sessionTempLeafPattern = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

var profileStderr io.Writer = os.Stderr

type launchProfileSpan struct {
	label    string
	duration time.Duration
}

type launchProfile struct {
	enabled bool
	spans   []launchProfileSpan
}

type launchModeArgs struct {
	MetadataJSON string
	DirectExec   bool
	WorkingDir   string
	SessionTemp  string
	EnvPairs     []string
	CmdArgs      []string
}

type helperModeArgs struct {
	Profile     bool
	CurrentUser bool
	Args        []string
}

func launchProfileRequested(args []string) bool {
	return len(args) > 0 && args[0] == launchProfileArg
}

func stripLaunchProfileArg(args []string) []string {
	if launchProfileRequested(args) {
		return args[1:]
	}
	return args
}

func parseHelperModeArgs(args []string) helperModeArgs {
	var parsed helperModeArgs
	for len(args) > 0 {
		switch args[0] {
		case launchProfileArg:
			parsed.Profile = true
			args = args[1:]
		case currentUserArg:
			parsed.CurrentUser = true
			args = args[1:]
		default:
			parsed.Args = args
			return parsed
		}
	}
	return parsed
}

func (p *launchProfile) Record(label string, start time.Time) {
	if p == nil || !p.enabled {
		return
	}
	duration := time.Since(start)
	if duration < 0 {
		duration = 0
	}
	p.spans = append(p.spans, launchProfileSpan{label: label, duration: duration})
}

func (p *launchProfile) Done() {
	if p == nil || !p.enabled || len(p.spans) == 0 {
		return
	}
	fmt.Fprintln(profileStderr, "hazmat-launch: helper profile:")
	for _, span := range p.spans {
		fmt.Fprintf(profileStderr, "  %s: %.3fs\n", span.label, span.duration.Seconds())
	}
}

func main() {
	mode := parseHelperModeArgs(os.Args[1:])
	profile := &launchProfile{enabled: mode.Profile}
	args := mode.Args
	if len(args) < 1 {
		dieUsage()
	}
	if args[0] == "run-agent" {
		runAgentCommand(args[1:], profile)
		return
	}

	start := time.Now()
	if err := closeInheritedFDs(); err != nil {
		die("hazmat-launch: close inherited fds: %v", err)
	}
	profile.Record("close inherited fds", start)

	if args[0] == "exec" {
		if len(args) < 2 {
			dieUsage()
		}
		execCommand(args[1:], profile)
		return
	}

	if len(args) < 2 {
		dieUsage()
	}

	policyMode := policyOwnerSudo
	if mode.CurrentUser {
		policyMode = policyOwnerCurrentUser
	}
	runLaunchMode(args[0], args[1:], profile, policyMode)
}

func runLaunchMode(policyFile string, cmdArgs []string, profile *launchProfile, policyMode policyOwnerMode) {
	start := time.Now()
	launchArgs, err := parseLaunchModeArgs(cmdArgs)
	profile.Record("parse launch args", start)
	if err != nil {
		die("hazmat-launch: %v", err)
	}

	start = time.Now()
	policy, err := validateAndReadPolicy(policyFile, policyMode)
	profile.Record("validate and read policy", start)
	if err != nil {
		die("hazmat-launch: %v", err)
	}

	if launchArgs.SessionTemp != "" {
		start = time.Now()
		if err := prepareSessionTempDir(launchArgs.SessionTemp); err != nil {
			die("hazmat-launch: %v", err)
		}
		profile.Record("prepare session temp", start)
	}

	// Apply the seatbelt sandbox to this process. After sandbox_init(),
	// the sandbox is active and all subsequent operations (including exec)
	// are subject to the policy.
	start = time.Now()
	if err := sandboxInit(policy); err != nil {
		die("hazmat-launch: %v", err)
	}
	profile.Record("sandbox_init", start)
	if launchArgs.MetadataJSON != "" {
		start = time.Now()
		fmt.Fprintln(os.Stderr, launchArgs.MetadataJSON)
		profile.Record("write metadata json", start)
	}

	if launchArgs.DirectExec {
		execDirectCommand(launchArgs, profile)
		return
	}
	execCommand(launchArgs.CmdArgs, profile)
}

func parseLaunchModeArgs(args []string) (launchModeArgs, error) {
	if len(args) == 0 {
		return launchModeArgs{}, fmt.Errorf("missing command")
	}
	var parsed launchModeArgs
	for len(args) > 0 {
		switch args[0] {
		case metadataJSONArg:
			if len(args) < 3 {
				return launchModeArgs{}, fmt.Errorf("%s requires a JSON payload and command", metadataJSONArg)
			}
			if args[1] == "" {
				return launchModeArgs{}, fmt.Errorf("%s payload is empty", metadataJSONArg)
			}
			parsed.MetadataJSON = args[1]
			args = args[2:]
		case directExecArg:
			parsed.DirectExec = true
			args = args[1:]
		case workingDirArg:
			if len(args) < 2 || args[1] == "" {
				return launchModeArgs{}, fmt.Errorf("%s requires a path", workingDirArg)
			}
			parsed.WorkingDir = args[1]
			args = args[2:]
		case envArg:
			if len(args) < 2 || args[1] == "" {
				return launchModeArgs{}, fmt.Errorf("%s requires KEY=VALUE", envArg)
			}
			parsed.EnvPairs = append(parsed.EnvPairs, args[1])
			args = args[2:]
		case sessionTempArg:
			if len(args) < 2 || args[1] == "" {
				return launchModeArgs{}, fmt.Errorf("%s requires a path", sessionTempArg)
			}
			parsed.SessionTemp = args[1]
			args = args[2:]
		case "--":
			parsed.CmdArgs = args[1:]
			args = nil
		default:
			parsed.CmdArgs = args
			args = nil
		}
	}
	if len(parsed.CmdArgs) == 0 {
		return launchModeArgs{}, fmt.Errorf("missing command")
	}
	if parsed.DirectExec && parsed.WorkingDir == "" {
		return launchModeArgs{}, fmt.Errorf("%s requires %s", directExecArg, workingDirArg)
	}
	return parsed, nil
}

func prepareSessionTempDir(path string) error {
	if err := validateSessionTempDir(path); err != nil {
		return err
	}
	root := filepath.Clean(sessionTempRoot)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create session temp root: %w", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create session temp dir: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set session temp mode: %w", err)
	}
	return nil
}

func validateSessionTempDir(path string) error {
	clean := filepath.Clean(path)
	if path != clean || !filepath.IsAbs(clean) {
		return fmt.Errorf("%s path %q is invalid", sessionTempArg, path)
	}
	root := filepath.Clean(sessionTempRoot)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(os.PathSeparator)) {
		return fmt.Errorf("%s path %q must be a direct child of %s", sessionTempArg, path, root)
	}
	if !sessionTempLeafPattern.MatchString(rel) {
		return fmt.Errorf("%s path %q has invalid generated name", sessionTempArg, path)
	}
	return nil
}

func execDirectCommand(args launchModeArgs, profile *launchProfile) {
	start := time.Now()
	os.Clearenv()
	for _, pair := range args.EnvPairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			die("hazmat-launch: invalid %s value %q", envArg, pair)
		}
		if err := os.Setenv(key, value); err != nil {
			die("hazmat-launch: set %s: %v", key, err)
		}
	}
	profile.Record("set direct env", start)

	start = time.Now()
	if err := os.Chdir(args.WorkingDir); err != nil {
		die("hazmat-launch: chdir %s: %v", args.WorkingDir, err)
	}
	profile.Record("chdir", start)

	execCommand(args.CmdArgs, profile)
}

func execCommand(cmdArgs []string, profile *launchProfile) {
	// Exec the target command. Since sandbox_init() was called in this
	// process, the sandbox is inherited by the exec'd program.
	// This is a direct exec (no fork) — signals, PTY, and exit codes
	// all work correctly.
	start := time.Now()
	bin, err := resolveExecPath(cmdArgs[0])
	profile.Record("resolve exec path", start)
	if err != nil {
		die("hazmat-launch: %v", err)
	}

	profile.Done()
	if err := syscall.Exec(bin, cmdArgs, os.Environ()); err != nil {
		die("hazmat-launch: exec %s: %v", bin, err)
	}
}

func dieUsage() {
	die("usage: hazmat-launch [--hazmat-launch-profile] [--hazmat-current-user] <policy-file> <cmd> [args...]\n       hazmat-launch [--hazmat-launch-profile] exec <cmd> [args...]")
}

// closeInheritedFDs drops every non-stdio descriptor before any helper logic
// runs. sandbox_init() cannot revoke access granted by an already-open handle.
//
// Enumerates actually-open fds via /dev/fd/ (always present on Darwin) instead
// of iterating from 3 to RLIMIT_NOFILE. RLIMIT_NOFILE can be RLIM_INFINITY
// (~9 quintillion on macOS when the parent shell has 'ulimit -n unlimited'),
// which would hang the loop forever.
func closeInheritedFDs() error {
	entries, err := readDirCloexec("/dev/fd")
	if err != nil {
		return fmt.Errorf("read /dev/fd: %w", err)
	}

	for _, entry := range entries {
		fd, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || fd < 3 {
			continue
		}
		if err := unix.Close(fd); err != nil && err != unix.EBADF {
			return fmt.Errorf("close fd %d: %w", fd, err)
		}
	}
	return nil
}

func readDirCloexec(path string) ([]os.DirEntry, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap fd %d for %q", fd, path)
	}
	defer f.Close()

	return f.ReadDir(-1)
}

// resolveExecPath finds the absolute path for a command. syscall.Exec
// requires an absolute path — it doesn't search PATH.
func resolveExecPath(cmd string) (string, error) {
	if cmd[0] == '/' {
		return cmd, nil
	}
	if strings.ContainsRune(cmd, os.PathSeparator) {
		abs, err := filepath.Abs(cmd)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(abs); err == nil && info.Mode()&0o111 != 0 {
			return abs, nil
		}
		return "", fmt.Errorf("command not found: %s", cmd)
	}
	// Search PATH
	for _, dir := range splitPath(os.Getenv("PATH")) {
		full := dir + "/" + cmd
		if info, err := os.Stat(full); err == nil && info.Mode()&0o111 != 0 {
			return full, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", cmd)
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == ':' {
			if i > start {
				result = append(result, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		result = append(result, path[start:])
	}
	return result
}

type policyOwnerMode string

const (
	policyOwnerSudo        policyOwnerMode = "sudo"
	policyOwnerCurrentUser policyOwnerMode = "current-user"
)

// validateAndReadPolicy checks the policy file and returns its contents.
func validateAndReadPolicy(path string, ownerMode policyOwnerMode) (string, error) {
	// ── Path ──────────────────────────────────────────────────────────────────
	if !policyFilePattern.MatchString(path) {
		return "", fmt.Errorf("policy file must match /private/tmp/hazmat-<pid>.sb, got %q", path)
	}

	// ── Existence and metadata ────────────────────────────────────────────────
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", path, err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("cannot inspect policy file ownership")
	}
	ownerUID, ownerLabel, err := expectedPolicyOwnerUID(ownerMode)
	if err != nil {
		return "", err
	}
	if int(stat.Uid) != ownerUID {
		return "", fmt.Errorf("policy file owner uid %d does not match %s uid %d",
			stat.Uid, ownerLabel, ownerUID)
	}

	// ── Mode: must be exactly 0644 ────────────────────────────────────────────
	if perm := info.Mode().Perm(); perm != 0o644 {
		return "", fmt.Errorf("policy file has mode %04o, expected 0644", perm)
	}

	// ── Content ──────────────────────────────────────────────────────────────
	data, err := readFileCloexec(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}
	if !bytes.Contains(data, []byte(denyDefaultMarker)) {
		return "", fmt.Errorf("policy file does not contain %q — permissive policies are not allowed", denyDefaultMarker)
	}

	return string(data), nil
}

func expectedPolicyOwnerUID(ownerMode policyOwnerMode) (int, string, error) {
	switch ownerMode {
	case policyOwnerSudo:
		sudoUID, err := strconv.Atoi(os.Getenv("SUDO_UID"))
		if err != nil || sudoUID == 0 {
			return 0, "", fmt.Errorf("SUDO_UID not set or root — not a valid sudo invocation")
		}
		return sudoUID, "invoking user", nil
	case policyOwnerCurrentUser:
		uid := os.Getuid()
		if uid == 0 {
			return 0, "", fmt.Errorf("current-user policy owner uid is root — not a valid current-user invocation")
		}
		return uid, "current user", nil
	default:
		return 0, "", fmt.Errorf("unsupported policy owner mode %q", ownerMode)
	}
}

func readFileCloexec(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap fd %d for %q", fd, path)
	}
	defer f.Close()

	return io.ReadAll(f)
}

// validatePolicyFile checks the policy file without reading its full contents.
// Kept for backward compatibility with tests.
func validatePolicyFile(path string) error {
	_, err := validateAndReadPolicy(path, policyOwnerSudo)
	return err
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
