package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// UI handles all terminal output for both setup and test commands.
// Setup methods (Step/Ok/SkipDone/WarnMsg/Fatal) do not touch counters.
// Test methods (TestPass/TestFail/TestWarn/TestSkip) increment counters.
type UI struct {
	stepNum int
	Pass    int
	Fail    int
	Warn    int
	Skip    int
	// DryRun causes Ask to skip the prompt and assume yes, so dry-run output
	// shows the commands that would run for optional steps.
	DryRun bool
	// YesAll causes Ask to assume yes without prompting (--yes / -y flag).
	// Unlike DryRun, commands are still executed.
	YesAll bool
}

var (
	cGreen  = color.New(color.FgGreen)
	cRed    = color.New(color.FgRed)
	cYellow = color.New(color.FgYellow)
	cBlue   = color.New(color.FgBlue, color.Bold)
	cBold   = color.New(color.Bold)
	cDim    = color.New(color.Faint)
)

func (u *UI) Step(label string) {
	u.stepNum++
	fmt.Println()
	cBlue.Printf("━━━ Step %d: %s ━━━\n", u.stepNum, label)
}

// ── Setup output (no counters) ────────────────────────────────────────────────

func (u *UI) Ok(msg string) {
	cGreen.Print("  ✓ ")
	fmt.Println(msg)
}

func (u *UI) SkipDone(msg string) {
	cYellow.Print("  → ")
	fmt.Printf("%s (already done)\n", msg)
}

func (u *UI) WarnMsg(msg string) {
	cYellow.Print("  ! ")
	fmt.Println(msg)
}

// Fatal prints an error and exits immediately.  Use for unrecoverable
// precondition failures (wrong OS, running as root) before the defer is set up.
func (u *UI) Fatal(msg string) {
	cRed.Fprintf(os.Stderr, "  ✗ %s\n", msg)
	os.Exit(1)
}

// ── Test output (increments counters) ────────────────────────────────────────

func (u *UI) TestPass(msg string) {
	u.Pass++
	cGreen.Print("  ✓ ")
	fmt.Println(msg)
}

func (u *UI) TestFail(msg string) {
	u.Fail++
	cRed.Print("  ✗ ")
	fmt.Println(msg)
}

func (u *UI) TestWarn(msg string) {
	u.Warn++
	cYellow.Print("  ! ")
	fmt.Println(msg)
}

func (u *UI) TestSkip(msg string) {
	u.Skip++
	cYellow.Print("  → ")
	fmt.Printf("%s (skipped)\n", msg)
}

// Summary prints the results table.  Returns true if there were any failures.
func (u *UI) Summary() bool {
	fmt.Println()
	cBold.Println("━━━ Results ━━━")
	fmt.Println()

	total := u.Pass + u.Fail + u.Warn + u.Skip
	fmt.Printf("  Total checks: %d\n", total)
	cGreen.Printf("  Pass:  %d\n", u.Pass)
	if u.Fail > 0 {
		cRed.Printf("  Fail:  %d\n", u.Fail)
	} else {
		fmt.Printf("  Fail:  %d\n", u.Fail)
	}
	if u.Warn > 0 {
		cYellow.Printf("  Warn:  %d\n", u.Warn)
	} else {
		fmt.Printf("  Warn:  %d\n", u.Warn)
	}
	fmt.Printf("  Skip:  %d\n", u.Skip)
	fmt.Println()

	switch {
	case u.Fail > 0:
		cRed.Println("  Hazmat is NOT fully operational. Fix failures before running Claude in auto mode.")
	case u.Warn > 0:
		cYellow.Println("  Hazmat is operational with warnings. Review warnings before running autonomously.")
	default:
		cGreen.Println("  All checks passed. Hazmat is ready.")
	}
	fmt.Println()
	return u.Fail > 0
}

// ShowFileOp prints the verb (Write / Append to), the path, and up to 30 lines
// of content in dim style.  Called by Runner before any file write in verbose
// or dry-run mode so users can audit what goes into system files.
func (u *UI) ShowFileOp(verb, path, content string) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	faint.Printf("    ─ %s %s (%d lines):\n", verb, path, len(lines))
	const maxPreview = 30
	for i, line := range lines {
		if i == maxPreview {
			faint.Printf("    │ … (%d more lines)\n", len(lines)-maxPreview)
			break
		}
		faint.Printf("    │ %s\n", line)
	}
}

// IsInteractive returns true when the UI should prompt the user: not in
// dry-run, not in --yes mode, and stdin is a real terminal.
func (u *UI) IsInteractive() bool {
	return !u.DryRun && !u.YesAll && term.IsTerminal(int(os.Stdin.Fd()))
}

// Ask prints a [y/N] prompt and reads one line.
// In dry-run mode, prints the prompt and assumes yes so previewed output
// includes commands that belong to optional steps.
// Returns false immediately if stdin is not a terminal (non-interactive run).
// Reads byte-by-byte to avoid buffering ahead of interactive subprocesses
// (e.g. sudo passwd) that will also read from stdin.
func (u *UI) Ask(prompt string) bool {
	if u.DryRun {
		faint.Printf("    [dry-run] Would ask: %s [y/N]  → assuming yes for preview\n", prompt)
		return true
	}
	if u.YesAll {
		cBold.Printf("  %s [y/N] ", prompt)
		fmt.Println("y  (--yes)")
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		u.WarnMsg(fmt.Sprintf("Non-interactive: skipping '%s'", prompt))
		return false
	}
	cBold.Printf("  %s [y/N] ", prompt)

	var sb strings.Builder
	b := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			sb.WriteByte(b[0])
		}
		if err != nil {
			break
		}
	}
	ans := strings.TrimSpace(sb.String())
	return ans == "y" || ans == "Y"
}

// Logo prints the Homer-in-hazmat / Claude-logo ANSI art header.
// Called after setup completes (reward, not gate).
func (u *UI) Logo() {
	fmt.Println()
	fmt.Println(homerHazmatArt)
	cRed.Print("       ☢  ")
	cBold.Print("H A Z M A T")
	cRed.Println("  ☢")
	fmt.Println()
}

// Banner prints the compact setup header (no art — keep first screen useful).
func (u *UI) Banner(currentUser string) {
	fmt.Println()
	cRed.Print("  ☢ ")
	cBold.Println("Hazmat — AI agent containment for macOS")
	fmt.Println()
	fmt.Println("  Creates a contained environment for AI agents: dedicated user,")
	fmt.Println("  filesystem sandbox, firewall, DNS blocklist, and snapshot backup.")
	fmt.Println()
	cDim.Println("  Preview first:  hazmat init --dry-run")
	fmt.Println()
	fmt.Println("  After setup:")
	fmt.Printf("    cd your-project && hazmat claude\n")
	fmt.Println()
}
