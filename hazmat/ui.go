package hazmat

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
	stepNum   int
	Pass      int
	Fail      int
	Warn      int
	Skip      int
	stepLabel string
	findings  []uiFinding
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

type uiFindingSeverity uint8

const (
	uiFindingFailure uiFindingSeverity = iota
	uiFindingWarning
)

type uiFinding struct {
	Severity uiFindingSeverity
	Step     string
	Message  string
	Action   string
}

type uiRecommendation struct {
	Key      string
	Severity uiFindingSeverity
	Step     string
	Title    string
	Action   string
	Details  []string
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
	u.stepLabel = label
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
	u.TestFailWithAction(msg, "")
}

func (u *UI) TestFailWithAction(msg, action string) {
	u.Fail++
	u.recordFinding(uiFindingFailure, msg, action)
	cRed.Print("  ✗ ")
	fmt.Println(msg)
}

func (u *UI) TestWarn(msg string) {
	u.TestWarnWithAction(msg, "")
}

func (u *UI) TestWarnWithAction(msg, action string) {
	u.Warn++
	u.recordFinding(uiFindingWarning, msg, action)
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

	u.printRecommendations()

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

func (u *UI) recordFinding(severity uiFindingSeverity, msg, action string) {
	u.findings = append(u.findings, uiFinding{
		Severity: severity,
		Step:     u.stepLabel,
		Message:  msg,
		Action:   strings.TrimSpace(action),
	})
}

func (u *UI) printRecommendations() {
	recommendations := u.recommendations()
	if len(recommendations) == 0 {
		return
	}

	cBold.Println("━━━ Recommended next actions ━━━")
	fmt.Println()
	for i, rec := range recommendations {
		colorForSeverity(rec.Severity).Printf("  %d. [%s] %s\n", i+1, rec.Severity.Label(), rec.Title)
		if rec.Step != "" {
			cDim.Printf("     Check: %s\n", rec.Step)
		}
		fmt.Printf("     Action: %s\n", rec.Action)
		for _, detail := range rec.Details {
			cDim.Printf("     Affected: %s\n", detail)
		}
	}
	fmt.Println()
	cDim.Println("  After applying fixes, rerun: hazmat check --full")
	fmt.Println()
}

func (u *UI) recommendations() []uiRecommendation {
	recsByKey := make(map[string]int)
	var recs []uiRecommendation
	for _, finding := range u.findings {
		rec := recommendationForFinding(finding)
		if rec.Action == "" {
			continue
		}
		if idx, ok := recsByKey[rec.Key]; ok {
			recs[idx].Severity = highestSeverity(recs[idx].Severity, rec.Severity)
			recs[idx].Details = appendUnique(recs[idx].Details, rec.Details...)
			continue
		}
		recsByKey[rec.Key] = len(recs)
		recs = append(recs, rec)
	}
	return recs
}

func recommendationForFinding(f uiFinding) uiRecommendation {
	msg := strings.TrimSpace(f.Message)
	action := strings.TrimSpace(f.Action)
	key := "finding:" + msg
	title := titleForFinding(f)
	details := []string{msg}

	if action == "" {
		key, title, action, details = inferRecommendation(f)
	}

	return uiRecommendation{
		Key:      key,
		Severity: f.Severity,
		Step:     f.Step,
		Title:    title,
		Action:   action,
		Details:  details,
	}
}

func inferRecommendation(f uiFinding) (key, title, action string, details []string) {
	msg := strings.TrimSpace(f.Message)
	lower := strings.ToLower(msg)
	details = []string{msg}

	if path, ok := claudeProjectPermissionPath(msg); ok {
		return "claude-project-permissions",
			"Repair Claude project resume/export permissions",
			"Run `sudo chmod 2770 <path>` for each affected Claude project directory, then rerun `hazmat check`.",
			[]string{path}
	}

	if strings.Contains(lower, "no ssh key found") || strings.Contains(lower, "no id_ed25519.pub") {
		return "agent-ssh-key",
			"Create an agent SSH key if Git SSH access is needed",
			"Create an ed25519 SSH key for the agent user and register the public key with the remote Git provider.",
			details
	}

	if command, ok := explicitCommandAction(msg); ok {
		return "command:" + command,
			titleForFinding(f),
			"Run `" + command + "`.",
			details
	}

	switch {
	case strings.Contains(lower, "new file group is gid"):
		return "workspace-setgid",
			"Repair workspace group inheritance",
			fmt.Sprintf("Run `hazmat init` to restore %s-group setup, then verify project directories are group-owned by `%s` and have the setgid bit.", sharedGroup, sharedGroup),
			details
	case strings.Contains(lower, "can read agent's .zshrc"):
		return "agent-home-permissions",
			"Restrict agent home permissions",
			fmt.Sprintf("Run `sudo chmod 700 %s` after verifying no intentional shared files depend on broader agent-home access.", agentHome),
			details
	case strings.Contains(lower, "docker socket permissions"):
		return "docker-socket-permissions",
			"Restrict the Docker socket",
			"Set the Docker socket to owner-only access (mode 0700) or disable Docker socket exposure before autonomous agent runs.",
			details
	case strings.Contains(lower, "pf anchor") || strings.Contains(lower, "/etc/pf.conf"):
		return "pf-firewall",
			"Restore and reload the packet-filter rules",
			"Run `hazmat init` to restore Hazmat-managed pf configuration, then validate with `hazmat check --full`.",
			details
	case strings.Contains(lower, "dns blocklist") || strings.Contains(lower, "resolved to a real ip"):
		return "dns-blocklist",
			"Restore the DNS blocklist",
			"Run `hazmat init` and enable the DNS blocklist, then rerun `hazmat check`.",
			details
	case strings.Contains(lower, "umask 007"):
		return "agent-umask",
			"Restore the agent umask",
			"Run `hazmat init` to restore the managed agent shell block, or add `umask 007` to the agent shell profile.",
			details
	case strings.Contains(lower, "needs-repair") && strings.Contains(lower, "credential"):
		return "credential-repair",
			"Repair credential store drift",
			"Use the detail printed below the credential finding to migrate or remove stale credential material, then rerun `hazmat check`.",
			details
	case strings.Contains(lower, "adapter-required") && strings.Contains(lower, "credential"):
		return "credential-adapter-required",
			"Avoid credentials without a backend adapter",
			"Do not rely on this credential path until Hazmat has a backend adapter for it, or use a supported credential backend.",
			details
	case strings.Contains(lower, "anthropic_api_key not configured"):
		return "anthropic-api-key",
			"Configure Claude authentication for Hazmat sessions",
			"Run `hazmat config agent` to store an Anthropic API key, or launch `hazmat claude` and complete `/login`.",
			details
	case strings.Contains(lower, "git identity not fully configured"):
		return "agent-git-identity",
			"Configure the agent Git identity",
			"Set `user.name` and `user.email` for the agent user's global Git config.",
			details
	case strings.Contains(lower, "not found in agent path"):
		return "agent-tool-path",
			"Install or expose the missing project tool",
			"Install the missing tool for the agent user, or update the active integration so its toolchain is readable and on the agent PATH.",
			details
	case strings.Contains(lower, "golangci-lint: not accessible"):
		return "golangci-lint-access",
			"Expose golangci-lint to the agent user",
			"Install `golangci-lint` via Homebrew or repair Homebrew permissions so the agent user can execute it.",
			details
	case strings.Contains(lower, "no toolchain path resolved"):
		return "integration-toolchain",
			"Resolve the active integration toolchain",
			"Install the integration toolchain, fix its host permissions, or remove/pin integrations in `.hazmat/integrations.yaml`.",
			details
	case strings.Contains(lower, "tla2tools.jar"):
		return "tla2tools-jar",
			"Expose tla2tools.jar to Hazmat",
			"Set `TLA2TOOLS_JAR` to a readable tla2tools.jar path or place it at the default `~/workspace/tla2tools.jar` location.",
			details
	case strings.Contains(lower, "not group-accessible") || strings.Contains(lower, "workspace acl"):
		return "workspace-access",
			"Repair workspace group access",
			"Run `hazmat init` to restore dev-group membership and workspace ACL defaults, then rerun `hazmat check`.",
			details
	default:
		return "review:" + msg,
			titleForFinding(f),
			"Repair this finding and rerun `hazmat check` to confirm the result.",
			details
	}
}

func titleForFinding(f uiFinding) string {
	msg := strings.TrimSpace(f.Message)
	if idx := strings.Index(msg, " — "); idx > 0 {
		msg = msg[:idx]
	}
	if f.Step == "" {
		return msg
	}
	return fmt.Sprintf("%s: %s", f.Step, msg)
}

func explicitCommandAction(msg string) (string, bool) {
	for _, marker := range []string{"fix with:", "try:", "run:"} {
		idx := strings.Index(strings.ToLower(msg), marker)
		if idx < 0 {
			continue
		}
		command := strings.TrimSpace(msg[idx+len(marker):])
		command = strings.Trim(command, "`'\" ")
		if command != "" {
			return command, true
		}
	}
	return "", false
}

func claudeProjectPermissionPath(msg string) (string, bool) {
	if !strings.Contains(msg, "/.claude/projects/") || !strings.Contains(msg, "is not group-writable") {
		return "", false
	}
	path, _, found := strings.Cut(msg, " is not group-writable")
	if !found {
		return "", false
	}
	return strings.TrimSpace(path), true
}

func highestSeverity(a, b uiFindingSeverity) uiFindingSeverity {
	if a == uiFindingFailure || b == uiFindingFailure {
		return uiFindingFailure
	}
	return uiFindingWarning
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, ok := seen[value]; ok {
			continue
		}
		values = append(values, value)
		seen[value] = struct{}{}
	}
	return values
}

func (s uiFindingSeverity) Label() string {
	switch s {
	case uiFindingFailure:
		return "FAIL"
	case uiFindingWarning:
		return "WARN"
	default:
		return "ISSUE"
	}
}

func colorForSeverity(severity uiFindingSeverity) *color.Color {
	if severity == uiFindingFailure {
		return cRed
	}
	return cYellow
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
