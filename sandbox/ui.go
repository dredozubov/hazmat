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
		cRed.Println("  Sandbox is NOT fully operational. Fix failures before running Claude in auto mode.")
	case u.Warn > 0:
		cYellow.Println("  Sandbox is operational with warnings. Review warnings before running autonomously.")
	default:
		cGreen.Println("  All checks passed. Sandbox is ready.")
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

// Banner prints the setup welcome box.
func (u *UI) Banner(currentUser string) {
	fmt.Println()
	cBold.Println("  ┌──────────────────────────────────────────────────┐")
	cBold.Println("  │  Option A: Dedicated agent user + soft blocklist │")
	cBold.Println("  └──────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("  This tool will:")
	fmt.Printf("    1. Create a hidden '%s' macOS user\n", agentUser)
	fmt.Printf("    2. Create a '%s' group (only %s + %s)\n", sharedGroup, currentUser, agentUser)
	fmt.Printf("    3. Set up a shared workspace at %s\n", sharedWorkspace)
	fmt.Println("    4. Harden known macOS isolation gaps")
	fmt.Println("    5. Install sandbox-aware shell wrappers and toolchain env")
	fmt.Printf("    6. Configure passwordless sudo (%s → %s)\n", currentUser, agentUser)
	fmt.Println("    7. Install a pf port blocklist (SMTP, IRC, FTP, Tor, etc.)")
	fmt.Println("    8. Add a DNS domain blocklist (tunnel/paste/fileshare services)")
	fmt.Println("    9. Persist firewall rules across reboots")
	fmt.Println()
	fmt.Println("  You'll need to manually install afterward:")
	fmt.Println("    • Claude Code (as the agent user)")
	fmt.Println("    • A GitHub Personal Access Token for HTTPS git auth")
	fmt.Println("    • LuLu network monitor (optional, recommended)")
	fmt.Println()
}

// DoneBox prints the post-setup manual steps.
func (u *UI) DoneBox(currentUser string) {
	fmt.Println()
	cGreen.Println("━━━ Setup complete ━━━")
	fmt.Println()
	fmt.Println("  Remaining manual steps:")
	fmt.Println()
	cBold.Printf("  1. Install Claude Code as the agent user:\n")
	fmt.Printf("     sudo -u %s -i\n", agentUser)
	fmt.Println("     curl -fsSL https://claude.ai/install.sh | bash")
	fmt.Println()
	cBold.Println("  2. Set your Anthropic API key:")
	fmt.Println(`     echo 'export ANTHROPIC_API_KEY="sk-ant-..."' >> ~/.zshrc`)
	fmt.Println()
	cBold.Println("  3. Configure git HTTPS authentication:")
	fmt.Println("     git config --global credential.helper 'store --file ~/.config/git/credentials'")
	fmt.Println("     # Create a GitHub Personal Access Token (scope: repo)")
	fmt.Println("     # github.com → Settings → Developer settings → Personal access tokens")
	fmt.Println("     # git will prompt for it on the first push; it is then stored.")
	fmt.Println("     #")
	fmt.Println("     # Note: git-over-SSH is blocked by the seatbelt profile.")
	fmt.Println()
	cBold.Println("  4. Configure git:")
	fmt.Println(`     git config --global user.name "Your Name"`)
	fmt.Println(`     git config --global user.email "you@example.com"`)
	fmt.Println()
	cBold.Println("  5. Use the generated wrappers from your normal shell:")
	fmt.Println("     cd ~/workspace-shared/my-project")
	fmt.Println("     claude-sandbox")
	fmt.Println("     agent-shell")
	fmt.Println("     agent-exec make test")
	fmt.Println("     agent-exec npx vitest")
	fmt.Println("     agent-exec uvx ruff check .")
	fmt.Println()
	cYellow.Println("  Note: wrappers are in ~/.local/bin — reload your shell first:")
	fmt.Println("     source ~/.zshrc   (or open a new terminal)")
	fmt.Println()
	cBold.Println("  6. Install LuLu (optional, recommended):")
	fmt.Println("     https://objective-see.org/products/lulu.html")
	fmt.Println()
	cBold.Println("  Agent-shell workflow:")
	fmt.Println("     cd ~/workspace-shared/my-project")
	fmt.Println("     agent-shell")
	fmt.Println("     claude")
	fmt.Println("     make test")
	fmt.Println("     npx vitest")
	fmt.Println("     uvx ruff check .")
	fmt.Println()
	fmt.Println("  To uninstall, see setup-option-a.md § Uninstall / Rollback")
	fmt.Println()
}
