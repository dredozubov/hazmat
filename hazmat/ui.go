package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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

type UIChoice struct {
	Key         string
	Label       string
	Description string
}

var (
	cGreen       = color.New(color.FgGreen)
	cRed         = color.New(color.FgRed)
	cYellow      = color.New(color.FgYellow)
	cBlue        = color.New(color.FgBlue, color.Bold)
	cBold        = color.New(color.Bold)
	cDim         = color.New(color.Faint)
	uiIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
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
		cRed.Println("  Hazmat is NOT fully operational. Fix failures before running an agent autonomously.")
	case u.Warn > 0:
		cYellow.Println("  Hazmat is operational with warnings. Review warnings before running an agent autonomously.")
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
	cDim.Printf("    ─ %s %s (%d lines):\n", verb, path, len(lines))
	const maxPreview = 30
	for i, line := range lines {
		if i == maxPreview {
			cDim.Printf("    │ … (%d more lines)\n", len(lines)-maxPreview)
			break
		}
		cDim.Printf("    │ %s\n", line)
	}
}

// IsInteractive returns true when the UI should prompt the user: not in
// dry-run, not in --yes mode, and stdin is a real terminal.
func (u *UI) IsInteractive() bool {
	return !u.DryRun && !u.YesAll && uiIsTerminal()
}

// Ask prints a [y/N] prompt and reads one line.
// In dry-run mode, prints the prompt and assumes yes so previewed output
// includes commands that belong to optional steps.
// Returns false immediately if stdin is not a terminal (non-interactive run).
// Reads byte-by-byte to avoid buffering ahead of interactive subprocesses
// (e.g. sudo passwd) that will also read from stdin.
func (u *UI) Ask(prompt string) bool {
	if u.DryRun {
		cDim.Printf("    [dry-run] Would ask: %s [y/N]  → assuming yes for preview\n", prompt)
		return true
	}
	if u.YesAll {
		cBold.Printf("  %s [y/N] ", prompt)
		fmt.Println("y  (--yes)")
		return true
	}
	if !uiIsTerminal() {
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

func (u *UI) Choose(prompt string, choices []UIChoice, defaultKey string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no choices provided")
	}
	if defaultKey == "" {
		defaultKey = choices[0].Key
	}
	valid := make(map[string]struct{}, len(choices))
	for i, choice := range choices {
		valid[choice.Key] = struct{}{}
		label := fmt.Sprintf("%d) %s", i+1, choice.Label)
		if choice.Key == defaultKey {
			label += " [default]"
		}
		fmt.Printf("  %s\n", label)
		if choice.Description != "" {
			cDim.Printf("     %s\n", choice.Description)
		}
	}
	if u.DryRun {
		cDim.Printf("    [dry-run] Would ask: %s → assuming %s for preview\n", prompt, defaultKey)
		return defaultKey, nil
	}
	if u.YesAll || !uiIsTerminal() {
		cBold.Printf("  %s ", prompt)
		fmt.Printf("%s\n", defaultKey)
		return defaultKey, nil
	}

	cBold.Printf("  %s ", prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	selection := strings.TrimSpace(strings.ToLower(input))
	if selection == "" {
		return defaultKey, nil
	}
	if idx, err := strconv.Atoi(selection); err == nil {
		if idx >= 1 && idx <= len(choices) {
			return choices[idx-1].Key, nil
		}
	}
	if _, ok := valid[selection]; ok {
		return selection, nil
	}
	return "", fmt.Errorf("invalid choice %q", strings.TrimSpace(input))
}

func (u *UI) ChooseMany(prompt string, choices []UIChoice, defaultKeys []string) ([]string, error) {
	if len(choices) == 0 {
		return nil, fmt.Errorf("no choices provided")
	}
	if len(defaultKeys) == 0 {
		for _, choice := range choices {
			defaultKeys = append(defaultKeys, choice.Key)
		}
	}
	defaultKeys = dedupeStrings(defaultKeys)

	choiceByKey := make(map[string]UIChoice, len(choices))
	choiceByIndex := make(map[string]string, len(choices))
	defaultSet := make(map[string]struct{}, len(defaultKeys))
	for _, key := range defaultKeys {
		defaultSet[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}

	for i, choice := range choices {
		choiceByKey[strings.ToLower(choice.Key)] = choice
		choiceByIndex[strconv.Itoa(i+1)] = choice.Key

		label := fmt.Sprintf("%d) %s", i+1, choice.Label)
		if _, ok := defaultSet[strings.ToLower(choice.Key)]; ok {
			label += " [default]"
		}
		fmt.Printf("  %s\n", label)
		if choice.Description != "" {
			cDim.Printf("     %s\n", choice.Description)
		}
	}

	defaultLabel := strings.Join(defaultKeys, ", ")
	if len(defaultKeys) == len(choices) {
		defaultLabel = "all"
	}
	if u.DryRun {
		cDim.Printf("    [dry-run] Would ask: %s → assuming %s for preview\n", prompt, defaultLabel)
		return append([]string(nil), defaultKeys...), nil
	}
	if u.YesAll {
		cBold.Printf("  %s ", prompt)
		fmt.Printf("%s\n", defaultLabel)
		return append([]string(nil), defaultKeys...), nil
	}
	if !uiIsTerminal() {
		return nil, fmt.Errorf("non-interactive input")
	}

	cBold.Printf("  %s ", prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	selection := strings.TrimSpace(strings.ToLower(input))
	if selection == "" {
		return append([]string(nil), defaultKeys...), nil
	}
	if selection == "0" || selection == "none" {
		return nil, nil
	}

	var selected []string
	selectedSet := make(map[string]struct{})
	for _, token := range strings.Split(selection, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		key, ok := choiceByIndex[token]
		if !ok {
			choice, exists := choiceByKey[token]
			if !exists {
				return nil, fmt.Errorf("invalid choice %q", token)
			}
			key = choice.Key
		}
		if _, dup := selectedSet[key]; dup {
			continue
		}
		selected = append(selected, key)
		selectedSet[key] = struct{}{}
	}
	return selected, nil
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
	fmt.Println("    cd your-project && hazmat shell")
	fmt.Println("    hazmat bootstrap claude|codex|opencode")
	fmt.Println()
}
