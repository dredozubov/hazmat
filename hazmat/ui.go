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
	cOrange = color.New(color.FgHiYellow, color.Bold)
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
// Art is 55 chars wide × 33 lines, generated by art/gen_homer.py.
func (u *UI) Logo() {
	fmt.Println()
	fmt.Println(homerHazmatArt)
	cRed.Print("       ☢  "); cBold.Print("CLAUDE CODE SANDBOX"); cRed.Println("  ☢")
	fmt.Println()
}

// Banner prints the setup welcome box.
func (u *UI) Banner(currentUser string) {
	u.Logo()
	cBold.Println("  ┌──────────────────────────────────────────────────┐")
	cBold.Println("  │  Option A: Dedicated agent user + soft blocklist │")
	cBold.Println("  └──────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("  This tool will:")
	fmt.Printf("    1. Create a hidden '%s' macOS user\n", agentUser)
	fmt.Printf("    2. Create a '%s' group (only %s + %s)\n", sharedGroup, currentUser, agentUser)
	fmt.Printf("    3. Prepare %s for sandboxed project access\n", sharedWorkspace)
	fmt.Println("    4. Harden known macOS isolation gaps")
	fmt.Println("    5. Install hazmat-aware shell wrappers and toolchain env")
	fmt.Printf("    6. Configure passwordless sudo (%s → %s)\n", currentUser, agentUser)
	fmt.Println("    7. Install a pf port blocklist (SMTP, IRC, FTP, Tor, etc.)")
	fmt.Println("    8. Add a DNS domain blocklist (tunnel/paste/fileshare services)")
	fmt.Println("    9. Persist firewall rules across reboots")
	fmt.Println()
	fmt.Println("  After setup, run 'hazmat bootstrap' to install Claude Code and")
	fmt.Println("  write default settings for the agent user.  Then:")
	fmt.Println("    • Set ANTHROPIC_API_KEY for the agent user")
	fmt.Println("    • Configure git HTTPS credentials (SSH is blocked by the seatbelt)")
	fmt.Println("    • Install LuLu network monitor (optional, recommended)")
	fmt.Println()
}

// DoneBox prints the post-setup next steps.
func (u *UI) DoneBox(currentUser string) {
	fmt.Println()
	cGreen.Println("━━━ Setup complete ━━━")
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println()
	cBold.Println("  1. Bootstrap the agent user (installs Claude Code, settings, and hooks):")
	fmt.Println("     hazmat bootstrap")
	fmt.Println()
	cBold.Println("  2. Authenticate the agent user (choose one):")
	fmt.Printf("     sudo -u %s -i\n", agentUser)
	fmt.Println("     claude /login                                  # Claude.ai subscription")
	fmt.Println(`     echo 'export ANTHROPIC_API_KEY="sk-ant-..."' >> ~/.zshrc  # API key`)
	fmt.Println()
	cBold.Println("  3. Configure git HTTPS authentication for the agent user:")
	fmt.Printf("     sudo -u %s -i\n", agentUser)
	fmt.Println("     git config --global credential.helper 'store --file ~/.config/git/credentials'")
	fmt.Println("     # Create a GitHub PAT (scope: repo) and let git prompt for it on first push.")
	fmt.Println("     # Note: git-over-SSH is blocked by the seatbelt profile.")
	fmt.Println()
	cBold.Println("  4. Reload your shell, then test a session:")
	fmt.Println("     source ~/.zshrc   (or open a new terminal)")
	fmt.Println("     cd ~/workspace/my-project")
	fmt.Println("     hazmat claude")
	fmt.Println()
	cBold.Println("  5. Install LuLu for network monitoring (optional, recommended):")
	fmt.Println("     https://objective-see.org/products/lulu.html")
	fmt.Println()
	fmt.Println("  To uninstall, see setup-option-a.md § Uninstall / Rollback")
	fmt.Println()
}
