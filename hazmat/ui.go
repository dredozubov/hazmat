package hazmat

import (
	"bufio"
	"encoding/json"
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
	// RepairExecution records the command-mode policy used when building
	// diagnostic repair plans.
	RepairExecution diagnosticRepairExecutionRequest
	// Quick records whether diagnostic live network probes were skipped.
	Quick bool
	// JSON suppresses terminal rendering and emits a machine-readable
	// diagnostic report from Summary.
	JSON bool
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
	Severity   uiFindingSeverity
	Step       string
	Message    string
	Definition diagnosticFindingDefinition
	Typed      bool
	Details    []string
}

type uiRecommendation struct {
	Key        string
	Severity   uiFindingSeverity
	Step       string
	Definition diagnosticFindingDefinition
	Title      string
	Action     string
	Details    []string
}

type uiDiagnosticReport struct {
	FormatVersion   int                          `json:"format_version"`
	Kind            string                       `json:"kind"`
	Quick           bool                         `json:"quick"`
	Totals          uiDiagnosticTotals           `json:"totals"`
	Findings        []uiDiagnosticFinding        `json:"findings"`
	Recommendations []uiDiagnosticRecommendation `json:"recommendations"`
	RepairPlan      diagnosticRepairPlan         `json:"repair_plan"`
}

type uiDiagnosticTotals struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Warn int `json:"warn"`
	Skip int `json:"skip"`
}

type uiDiagnosticResource struct {
	ID           string `json:"id,omitempty"`
	Owner        string `json:"owner,omitempty"`
	DesiredState string `json:"desired_state,omitempty"`
}

type uiDiagnosticFinding struct {
	Severity         string                `json:"severity"`
	Step             string                `json:"step,omitempty"`
	Message          string                `json:"message"`
	Typed            bool                  `json:"typed"`
	ID               string                `json:"id,omitempty"`
	Resource         *uiDiagnosticResource `json:"resource,omitempty"`
	Title            string                `json:"title,omitempty"`
	Repairability    string                `json:"repairability,omitempty"`
	Action           string                `json:"action,omitempty"`
	RepairAction     string                `json:"repair_action,omitempty"`
	RepairReceipt    string                `json:"repair_receipt,omitempty"`
	Verification     string                `json:"verification,omitempty"`
	SecurityImpact   string                `json:"security_impact,omitempty"`
	RollbackBoundary string                `json:"rollback_boundary,omitempty"`
	Details          []string              `json:"details,omitempty"`
}

type uiDiagnosticRecommendation struct {
	Key              string   `json:"key"`
	Severity         string   `json:"severity"`
	Step             string   `json:"step,omitempty"`
	FindingID        string   `json:"finding_id"`
	ResourceID       string   `json:"resource_id"`
	Repairability    string   `json:"repairability"`
	Title            string   `json:"title"`
	Action           string   `json:"action"`
	RepairAction     string   `json:"repair_action,omitempty"`
	RepairReceipt    string   `json:"repair_receipt,omitempty"`
	Verification     string   `json:"verification,omitempty"`
	RollbackBoundary string   `json:"rollback_boundary,omitempty"`
	Details          []string `json:"details,omitempty"`
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
	if u.JSON {
		return
	}
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
	if u.JSON {
		return
	}
	cGreen.Print("  ✓ ")
	fmt.Println(msg)
}

func (u *UI) TestFail(msg string) {
	u.Fail++
	u.recordFinding(uiFindingFailure, msg)
	if u.JSON {
		return
	}
	cRed.Print("  ✗ ")
	fmt.Println(msg)
}

func (u *UI) TestFailWithAction(msg, action string) {
	u.TestFail(msg)
}

func (u *UI) TestFailFinding(def diagnosticFindingDefinition, msg string, details ...string) {
	u.Fail++
	u.recordTypedFinding(uiFindingFailure, def, msg, details...)
	if u.JSON {
		return
	}
	cRed.Print("  ✗ ")
	fmt.Println(msg)
}

func (u *UI) TestWarn(msg string) {
	u.Warn++
	u.recordFinding(uiFindingWarning, msg)
	if u.JSON {
		return
	}
	cYellow.Print("  ! ")
	fmt.Println(msg)
}

func (u *UI) TestWarnWithAction(msg, action string) {
	u.TestWarn(msg)
}

func (u *UI) TestWarnFinding(def diagnosticFindingDefinition, msg string, details ...string) {
	u.Warn++
	u.recordTypedFinding(uiFindingWarning, def, msg, details...)
	if u.JSON {
		return
	}
	cYellow.Print("  ! ")
	fmt.Println(msg)
}

func (u *UI) TestSkip(msg string) {
	u.Skip++
	if u.JSON {
		return
	}
	cYellow.Print("  → ")
	fmt.Printf("%s (skipped)\n", msg)
}

// Summary prints the results table.  Returns true if there were any failures.
func (u *UI) Summary() bool {
	if u.JSON {
		u.printJSONReport()
		return u.Fail > 0
	}

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

func (u *UI) recordFinding(severity uiFindingSeverity, msg string) {
	u.findings = append(u.findings, uiFinding{
		Severity: severity,
		Step:     u.stepLabel,
		Message:  msg,
	})
}

func (u *UI) recordTypedFinding(severity uiFindingSeverity, def diagnosticFindingDefinition, msg string, details ...string) {
	if err := def.Validate(); err != nil {
		panic(err)
	}
	if len(details) == 0 {
		details = []string{msg}
	}
	u.findings = append(u.findings, uiFinding{
		Severity:   severity,
		Step:       u.stepLabel,
		Message:    msg,
		Definition: def,
		Typed:      true,
		Details:    append([]string(nil), details...),
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
		if !finding.Typed {
			continue
		}
		rec := recommendationForFinding(finding)
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
	return uiRecommendation{
		Key:        f.Definition.RecommendationKey(),
		Severity:   f.Severity,
		Step:       f.Step,
		Definition: f.Definition,
		Title:      f.Definition.Title,
		Action:     f.Definition.Action,
		Details:    append([]string(nil), f.Details...),
	}
}

func (u *UI) printJSONReport() {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(u.diagnosticReport()); err != nil {
		fmt.Fprintf(os.Stderr, "emit diagnostic report JSON: %v\n", err)
	}
}

func (u *UI) diagnosticReport() uiDiagnosticReport {
	recommendations := u.recommendations()
	return uiDiagnosticReport{
		FormatVersion:   1,
		Kind:            "hazmat.diagnostic_report",
		Quick:           u.Quick,
		Totals:          uiDiagnosticTotals{Pass: u.Pass, Fail: u.Fail, Warn: u.Warn, Skip: u.Skip},
		Findings:        diagnosticFindingJSONs(u.findings),
		Recommendations: diagnosticRecommendationJSONs(recommendations),
		RepairPlan:      planDiagnosticRepairs(u.findings, recommendations, u.RepairExecution),
	}
}

func diagnosticFindingJSONs(findings []uiFinding) []uiDiagnosticFinding {
	out := make([]uiDiagnosticFinding, 0, len(findings))
	for _, finding := range findings {
		item := uiDiagnosticFinding{
			Severity: finding.Severity.Label(),
			Step:     finding.Step,
			Message:  finding.Message,
			Typed:    finding.Typed,
		}
		if finding.Typed {
			def := finding.Definition
			item.ID = string(def.ID)
			item.Resource = diagnosticResourceJSON(def.Resource)
			item.Title = def.Title
			item.Repairability = string(def.Repairability)
			item.Action = def.Action
			item.RepairAction = string(def.RepairAction)
			item.RepairReceipt = string(def.RepairReceipt)
			item.Verification = string(def.Verification)
			item.SecurityImpact = def.SecurityImpact
			item.RollbackBoundary = def.RollbackBoundary
			item.Details = append([]string(nil), finding.Details...)
		}
		out = append(out, item)
	}
	return out
}

func diagnosticResourceJSON(id diagnosticResourceID) *uiDiagnosticResource {
	resource := diagnosticResourceDefinitions[id]
	return &uiDiagnosticResource{
		ID:           string(id),
		Owner:        resource.Owner,
		DesiredState: resource.DesiredState,
	}
}

func diagnosticRecommendationJSONs(recommendations []uiRecommendation) []uiDiagnosticRecommendation {
	out := make([]uiDiagnosticRecommendation, 0, len(recommendations))
	for _, rec := range recommendations {
		def := rec.Definition
		out = append(out, uiDiagnosticRecommendation{
			Key:              rec.Key,
			Severity:         rec.Severity.Label(),
			Step:             rec.Step,
			FindingID:        string(def.ID),
			ResourceID:       string(def.Resource),
			Repairability:    string(def.Repairability),
			Title:            rec.Title,
			Action:           rec.Action,
			RepairAction:     string(def.RepairAction),
			RepairReceipt:    string(def.RepairReceipt),
			Verification:     string(def.Verification),
			RollbackBoundary: def.RollbackBoundary,
			Details:          append([]string(nil), rec.Details...),
		})
	}
	return out
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
