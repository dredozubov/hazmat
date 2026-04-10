package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

// Runner routes every side-effecting operation through a single chokepoint.
// Read-only operations (sudoOutput, asAgentOutput, os.Stat, os.ReadFile, …)
// bypass the Runner entirely — they have no side effects and nothing to surface.
//
// Three modes:
//
//	default  — silent execution, descriptions via ui.Ok/SkipDone/WarnMsg
//	Verbose  — prints each command in dim style before executing
//	DryRun   — prints everything, executes nothing (implies Verbose)
type Runner struct {
	Verbose bool
	DryRun  bool
	ui      *UI
}

// NewRunner creates a Runner.  DryRun implies Verbose.
func NewRunner(ui *UI, verbose, dryRun bool) *Runner {
	return &Runner{Verbose: verbose || dryRun, DryRun: dryRun, ui: ui}
}

var faint = color.New(color.Faint)

func (r *Runner) showCmd(shell string) {
	if r.Verbose {
		faint.Printf("    $ %s\n", shell)
	}
}

// showReason prints a dim one-liner explaining why sudo is needed.
// Called before every privileged operation so the user is never surprised.
func (r *Runner) showReason(reason string) {
	if reason != "" {
		faint.Printf("    → %s\n", reason)
	}
}

// ── Privileged commands ───────────────────────────────────────────────────────

// Sudo shows why, then optionally executes: sudo <args>.
// The reason string explains to the user why root access is needed.
func (r *Runner) Sudo(reason string, args ...string) error {
	r.showReason(reason)
	r.showCmd("sudo " + strings.Join(shellQuote(args), " "))
	if r.DryRun {
		return nil
	}
	return sudo(args...)
}

// SudoVisible runs a sudo command with stdout/stderr visible to the user
// but no stdin (not interactive). Use for commands that print progress
// but don't need input, like installers.
func (r *Runner) SudoVisible(reason string, args ...string) error {
	r.showReason(reason)
	r.showCmd("sudo " + strings.Join(shellQuote(args), " "))
	if r.DryRun {
		return nil
	}
	cmd := newSudoCommand(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Interactive shows the command; in dry-run annotates it as interactive and
// skips execution so the user is never prompted during a preview run.
func (r *Runner) Interactive(reason, name string, args ...string) error {
	r.showReason(reason)
	shell := name + " " + strings.Join(args, " ")
	if r.DryRun {
		faint.Printf("    $ %s  ← interactive, skipped in dry-run\n", shell)
		return nil
	}
	r.showCmd(shell)
	return runInteractive(name, args...)
}

// AsAgent shows why, then optionally executes an agent-user maintenance
// command through Hazmat's helper-backed path.
func (r *Runner) AsAgent(reason string, args ...string) error {
	r.showReason(reason)
	r.showCmd("sudo " + strings.Join(shellQuote(agentCommandArgs(args...)), " "))
	if r.DryRun {
		return nil
	}
	return asAgentQuiet(args...)
}

// AsAgentVisible runs an agent-user maintenance command with stdout/stderr
// attached to the terminal.
func (r *Runner) AsAgentVisible(reason string, args ...string) error {
	r.showReason(reason)
	r.showCmd("sudo " + strings.Join(shellQuote(agentCommandArgs(args...)), " "))
	if r.DryRun {
		return nil
	}
	cmd := newAgentCommand(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ── Filesystem writes ─────────────────────────────────────────────────────────
// Content is always shown in verbose/dry-run.  Security-sensitive paths
// (/etc/sudoers.d, /etc/pf.conf, /etc/hosts, LaunchDaemons) must be auditable.

// SudoWriteFile creates or overwrites path as root via sudo tee.
func (r *Runner) SudoWriteFile(reason, path, content string) error {
	r.showReason(reason)
	if r.Verbose && r.ui != nil {
		r.ui.ShowFileOp("Write", path, content)
	}
	if r.DryRun {
		return nil
	}
	return sudoWriteFile(path, content)
}

// SudoAppendFile appends content to a root-owned file via sudo tee -a.
func (r *Runner) SudoAppendFile(reason, path, content string) error {
	r.showReason(reason)
	if r.Verbose && r.ui != nil {
		r.ui.ShowFileOp("Append to", path, content)
	}
	if r.DryRun {
		return nil
	}
	return sudoAppendFile(path, content)
}

// UserWriteFile creates or overwrites a user-owned file without sudo.
func (r *Runner) UserWriteFile(path, content string) error {
	if r.Verbose && r.ui != nil {
		r.ui.ShowFileOp("Write", path, content)
	}
	if r.DryRun {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// UserAppendFile appends content to a user-owned file (e.g. ~/.zshrc).
// No sudo required; opens, writes, and closes atomically.
func (r *Runner) UserAppendFile(path, content string) error {
	if r.Verbose && r.ui != nil {
		r.ui.ShowFileOp("Append to", path, content)
	}
	if r.DryRun {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprint(f, content)
	return err
}

// ── Filesystem structure ──────────────────────────────────────────────────────

// Chmod shows and optionally changes permissions on a user-owned path.
func (r *Runner) Chmod(path string, mode os.FileMode) error {
	r.showCmd(fmt.Sprintf("chmod %04o %s", mode, path))
	if r.DryRun {
		return nil
	}
	return os.Chmod(path, mode)
}

// Symlink shows and optionally creates a symbolic link.
func (r *Runner) Symlink(target, link string) error {
	r.showCmd(fmt.Sprintf("ln -s %s %s", target, link))
	if r.DryRun {
		return nil
	}
	return os.Symlink(target, link)
}

// MkdirAll shows and optionally creates a user-owned directory tree.
func (r *Runner) MkdirAll(path string, mode os.FileMode) error {
	r.showCmd(fmt.Sprintf("mkdir -p %s", path))
	if r.DryRun {
		return nil
	}
	return os.MkdirAll(path, mode)
}

// ── Firewall / daemon ─────────────────────────────────────────────────────────

// PfctlLoad shows and optionally reloads /etc/pf.conf.
// Captures stderr so syntax errors are never silently swallowed.
func (r *Runner) PfctlLoad(reason string) error {
	r.showReason(reason)
	r.showCmd("sudo pfctl -f /etc/pf.conf")
	if r.DryRun {
		return nil
	}
	return pfctlLoadRules()
}

// LaunchctlBootstrap shows and optionally bootstraps a system LaunchDaemon.
func (r *Runner) LaunchctlBootstrap(reason, plist string) error {
	r.showReason(reason)
	r.showCmd(fmt.Sprintf("sudo launchctl bootstrap system %s", plist))
	if r.DryRun {
		return nil
	}
	return launchctlBootstrap(plist)
}

// ── Read-only sudo helpers ────────────────────────────────────────────────────
// These are for idempotency checks — reads that bypass normal write routing.
// In dry-run they return an empty string so the step is treated as "not done"
// and the corresponding commands are shown in the preview output.

// SudoOutput runs a sudo read in non-dry-run mode.
// In dry-run it skips the call and returns ("", errDryRun) so the caller's
// idempotency check fails and the associated commands appear in preview output.
func (r *Runner) SudoOutput(args ...string) (string, error) {
	if r.DryRun {
		return "", fmt.Errorf("dry-run: skipped")
	}
	return sudoOutput(args...)
}

// AgentOutput runs an agent-user read in non-dry-run mode.
// Same dry-run semantics as SudoOutput.
func (r *Runner) AgentOutput(args ...string) (string, error) {
	if r.DryRun {
		return "", fmt.Errorf("dry-run: skipped")
	}
	return asAgentOutput(args...)
}

// ── Display helper ────────────────────────────────────────────────────────────

// shellQuote returns args with minimal shell quoting for display purposes only.
// Not used for actual execution — exec.Command receives unquoted args directly.
func shellQuote(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\"'`$\\{}()|&;<>") {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			out[i] = a
		}
	}
	return out
}
