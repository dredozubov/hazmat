package hazmat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type importKind string

const (
	importPortablePath   importKind = "portable-path"
	importCredentialFile importKind = "credential-file"
	importGitIdentity    importKind = "git-identity"
	importStateMerge     importKind = "state-merge"
	// importAuthFile is the shared import kind for harnesses that store
	// sign-in in a single auth.json (Codex, OpenCode) rather than Claude's
	// split credential-file + state-merge.
	importAuthFile importKind = "auth-file"
)

type importStatus string

const (
	importNew       importStatus = "new"
	importConflict  importStatus = "conflict"
	importUnchanged importStatus = "unchanged"
	importOverwrite importStatus = "overwrite"
	importSkip      importStatus = "skip"
)

type importConflictPolicy string

const (
	importConflictPrompt    importConflictPolicy = "prompt"
	importConflictOverwrite importConflictPolicy = "overwrite"
	importConflictSkip      importConflictPolicy = "skip"
	importConflictFail      importConflictPolicy = "fail"
)

type claudeImportEnv struct {
	hostHome  string
	agentHome string
}

type importItem struct {
	Category   string
	Name       string
	Kind       importKind
	Status     importStatus
	SourcePath string
	DestPath   string
	Reason     string

	HostName  string
	HostEmail string

	HostJSON map[string]json.RawMessage
}

type importSkippedEntry struct {
	Category string
	Name     string
	Path     string
	Reason   string
}

type importPlan struct {
	Items []importItem
	Skips []importSkippedEntry
}

type importApplyResult struct {
	Imported    []importItem
	Overwritten []importItem
	Skipped     []importItem
	Unchanged   []importItem
}

type importOptions struct {
	PromptBeforeImport bool
	ConflictPolicy     importConflictPolicy
	AllowNoopMessage   bool
}

var (
	errClaudeImportCancelled = errors.New("Claude basics import cancelled")
	claudePortableAuthKeys   = []string{
		"oauthAccount",
		"userID",
		"hasAvailableSubscription",
		"customApiKeyResponses",
		"claudeCodeFirstTokenDate",
	}
	claudePortablePreferenceKeys = []string{
		"hasCompletedOnboarding",
		"lastOnboardingVersion",
		"theme",
		"tui",
		"showSpinnerTree",
		"shiftEnterKeyBindingInstalled",
		"deepLinkTerminal",
	}
	claudePortableStateKeys    = append(append([]string{}, claudePortableAuthKeys...), claudePortablePreferenceKeys...)
	claudePortableSettingsKeys = []string{
		"theme",
		"tui",
	}
)

func defaultClaudeImportEnv() (claudeImportEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeImportEnv{}, fmt.Errorf("determine home directory: %w", err)
	}
	return claudeImportEnv{hostHome: home, agentHome: agentHome}, nil
}

func (e claudeImportEnv) hostClaudeDir() string {
	return filepath.Join(e.hostHome, ".claude")
}

func (e claudeImportEnv) hostCommandsDir() string {
	return filepath.Join(e.hostClaudeDir(), "commands")
}

func (e claudeImportEnv) hostSkillsDir() string {
	return filepath.Join(e.hostClaudeDir(), "skills")
}

func (e claudeImportEnv) hostCredentialFile() string {
	return filepath.Join(e.hostClaudeDir(), ".credentials.json")
}

func (e claudeImportEnv) storedCredentialFile() string {
	return claudeCredentialStorePathForHome(e.hostHome)
}

func (e claudeImportEnv) hostClaudeStatePath() string {
	return filepath.Join(e.hostHome, ".claude.json")
}

func (e claudeImportEnv) storedClaudeStatePath() string {
	return claudeStateStorePathForHome(e.hostHome)
}

func (e claudeImportEnv) hostGitConfigPath() string {
	return filepath.Join(e.hostHome, ".gitconfig")
}

func (e claudeImportEnv) agentClaudeDir() string {
	return filepath.Join(e.agentHome, ".claude")
}

func (e claudeImportEnv) agentCommandsDir() string {
	return filepath.Join(e.agentClaudeDir(), "commands")
}

func (e claudeImportEnv) agentSkillsDir() string {
	return filepath.Join(e.agentClaudeDir(), "skills")
}

func (e claudeImportEnv) agentCredentialFile() string {
	return filepath.Join(e.agentClaudeDir(), ".credentials.json")
}

func (e claudeImportEnv) agentClaudeStatePath() string {
	return filepath.Join(e.agentHome, ".claude.json")
}

func (e claudeImportEnv) agentGitConfigPath() string {
	return filepath.Join(e.agentHome, ".gitconfig")
}

func newConfigImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import portable basics from an existing agent setup",
		Long: `Import a curated subset of another agent setup into Hazmat.

Hazmat keeps its own runtime settings, hooks, and safety controls. Import is
limited to portable basics such as sign-in state, git identity, commands,
and skills. Durable auth is stored in ~/.hazmat/secrets and materialized only
for matching sessions.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newConfigImportClaudeCmd())
	cmd.AddCommand(newConfigImportCodexCmd())
	cmd.AddCommand(newConfigImportOpenCodeCmd())
	return cmd
}

func newConfigImportClaudeCmd() *cobra.Command {
	return newConfigImportHarnessCmd(
		"claude",
		"Import Claude basics into Hazmat-managed state",
		`Import a curated subset of your host Claude setup into Hazmat.

Hazmat imports only portable basics:
  - account, subscription, and first-run onboarding state from Claude's known
    auth stores, when present
    (stored in ~/.hazmat/secrets and materialized only for Claude sessions)
  - git user.name and user.email
  - ~/.claude/commands
  - ~/.claude/skills

Hazmat does NOT import settings.json wholesale, hooks, MCP configuration,
plugins, project-local .claude directories, session history, or runtime caches.
Claude sessions reconcile only narrow non-secret startup display preferences
needed to avoid repeated first-run style onboarding.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		func(ui *UI, r *Runner, policy importConflictPolicy) error {
			env, err := defaultClaudeImportEnv()
			if err != nil {
				return err
			}
			err = importClaudeBasics(ui, r, env, importOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errClaudeImportCancelled) {
				return nil
			}
			return err
		},
	)
}

func runClaudeBasicsImport(ui *UI, r *Runner, env claudeImportEnv, opts importOptions) error {
	return runBasicsImport(ui, r, opts, harnessImportSpec{
		label:        "Claude",
		cancelledErr: errClaudeImportCancelled,
		scan:         func(r *Runner) (importPlan, error) { return scanClaudeImportPlan(env, r) },
		applyItem:    func(item importItem, r *Runner) error { return applyClaudeImportItem(item, env, r) },
		printPlan:    printClaudeImportPlan,
		printResult:  printClaudeImportResult,
	})
}

func scanClaudeImportPlan(env claudeImportEnv, r *Runner) (importPlan, error) {
	var plan importPlan

	if item, ok, err := scanClaudeAuthState(env, r); err != nil {
		return plan, err
	} else if ok {
		plan.Items = append(plan.Items, item)
	}

	if item, ok, err := scanClaudeCredentialFile(env, r); err != nil {
		return plan, err
	} else if ok {
		plan.Items = append(plan.Items, item)
	}

	if item, ok := scanClaudeGitIdentity(env); ok {
		plan.Items = append(plan.Items, item)
	}

	commandItems, skips, err := scanPortableImportDir("command", env.hostCommandsDir(), env.agentCommandsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, commandItems...)
	plan.Skips = append(plan.Skips, skips...)

	skillItems, skips, err := scanPortableImportDir("skill", env.hostSkillsDir(), env.agentSkillsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, skillItems...)
	plan.Skips = append(plan.Skips, skips...)

	sortImportItems(plan.Items)
	sortImportSkips(plan.Skips)

	return plan, nil
}

func scanClaudeAuthState(env claudeImportEnv, r *Runner) (importItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostClaudeStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return importItem{}, false, nil
		}
		return importItem{}, false, fmt.Errorf("read host Claude state: %w", err)
	}

	hostState, err := selectClaudePortableStateKeys(hostRaw)
	if err != nil {
		return importItem{}, false, fmt.Errorf("parse host Claude state: %w", err)
	}
	if len(hostState) == 0 {
		return importItem{}, false, nil
	}

	status := importNew
	if storedState, ok, err := readJSONMapStoreFile(env.storedClaudeStatePath()); err != nil {
		return importItem{}, false, fmt.Errorf("read stored Claude state: %w", err)
	} else if ok {
		if jsonSubsetEqual(hostState, storedState) {
			status = importUnchanged
		} else {
			status = importConflict
		}
	} else {
		agentState, ok, err := readClaudeStateKeysFromAgent(env.agentClaudeStatePath())
		if err != nil {
			return importItem{}, false, fmt.Errorf("read agent Claude state: %w", err)
		}
		if ok {
			if jsonSubsetEqual(hostState, agentState) {
				status = importNew
			} else {
				status = importConflict
			}
		}
	}

	return importItem{
		Category:   "sign-in",
		Name:       "Claude account state",
		Kind:       importStateMerge,
		Status:     status,
		SourcePath: env.hostClaudeStatePath(),
		DestPath:   env.storedClaudeStatePath(),
		HostJSON:   hostState,
	}, true, nil
}

func scanClaudeCredentialFile(env claudeImportEnv, r *Runner) (importItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostCredentialFile())
	if err != nil {
		if os.IsNotExist(err) {
			return importItem{}, false, nil
		}
		return importItem{}, false, fmt.Errorf("read host Claude credentials: %w", err)
	}

	status := importNew
	if storedRaw, ok, err := readHostStoredSecretFile(env.storedCredentialFile()); err != nil {
		return importItem{}, false, fmt.Errorf("read stored Claude credentials: %w", err)
	} else if ok {
		if bytes.Equal(hostRaw, storedRaw) {
			status = importUnchanged
		} else {
			status = importConflict
		}
	} else {
		agentRaw, ok, err := readAgentSecretFile(env.agentCredentialFile())
		if err != nil {
			return importItem{}, false, fmt.Errorf("read agent Claude credentials: %w", err)
		}
		if ok {
			if bytes.Equal(hostRaw, agentRaw) {
				status = importNew
			} else {
				status = importConflict
			}
		}
	}

	return importItem{
		Category:   "sign-in",
		Name:       "Claude credential file",
		Kind:       importCredentialFile,
		Status:     status,
		SourcePath: env.hostCredentialFile(),
		DestPath:   env.storedCredentialFile(),
	}, true, nil
}

func scanClaudeGitIdentity(env claudeImportEnv) (importItem, bool) {
	hostName := gitConfigValue(env.hostGitConfigPath(), "name")
	hostEmail := gitConfigValue(env.hostGitConfigPath(), "email")
	if hostName == "" && hostEmail == "" {
		return importItem{}, false
	}

	agentName := gitConfigValue(env.agentGitConfigPath(), "name")
	agentEmail := gitConfigValue(env.agentGitConfigPath(), "email")

	status := importNew
	sameName := hostName == "" || hostName == agentName
	sameEmail := hostEmail == "" || hostEmail == agentEmail
	conflictingName := hostName != "" && agentName != "" && hostName != agentName
	conflictingEmail := hostEmail != "" && agentEmail != "" && hostEmail != agentEmail

	switch {
	case sameName && sameEmail && (agentName != "" || agentEmail != ""):
		status = importUnchanged
	case conflictingName || conflictingEmail:
		status = importConflict
	}

	return importItem{
		Category:   "git identity",
		Name:       "git identity",
		Kind:       importGitIdentity,
		Status:     status,
		SourcePath: env.hostGitConfigPath(),
		DestPath:   env.agentGitConfigPath(),
		HostName:   hostName,
		HostEmail:  hostEmail,
	}, true
}

func scanPortableImportDir(category, hostDir, agentDir string) ([]importItem, []importSkippedEntry, error) {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s import directory: %w", hostDir, err)
	}

	var items []importItem
	var skips []importSkippedEntry

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		source := filepath.Join(hostDir, name)
		resolved, info, err := resolvePortableSource(source)
		if err != nil {
			skips = append(skips, importSkippedEntry{
				Category: category,
				Name:     name,
				Path:     source,
				Reason:   err.Error(),
			})
			continue
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			skips = append(skips, importSkippedEntry{
				Category: category,
				Name:     name,
				Path:     source,
				Reason:   fmt.Sprintf("unsupported source type %s", info.Mode().String()),
			})
			continue
		}

		dest := filepath.Join(agentDir, name)
		status := importNew
		equal, err := portablePathEqual(resolved, dest)
		switch {
		case err == nil && equal:
			status = importUnchanged
		case err == nil:
			if _, statErr := os.Lstat(dest); statErr == nil || os.IsPermission(statErr) {
				status = importConflict
			}
		case os.IsNotExist(err):
			status = importNew
		case os.IsPermission(err):
			status = importConflict
		default:
			return nil, nil, fmt.Errorf("compare %s import %s: %w", category, name, err)
		}

		items = append(items, importItem{
			Category:   category,
			Name:       name,
			Kind:       importPortablePath,
			Status:     status,
			SourcePath: resolved,
			DestPath:   dest,
		})
	}

	return items, skips, nil
}

func resolvePortableSource(path string) (string, fs.FileInfo, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, err
	}
	return resolved, info, nil
}

func portablePathEqual(src, dst string) (bool, error) {
	srcResolved, srcInfo, err := resolvePortableSource(src)
	if err != nil {
		return false, err
	}

	dstResolved, dstInfo, err := resolvePortableSource(dst)
	if err != nil {
		return false, err
	}

	if srcInfo.IsDir() != dstInfo.IsDir() {
		return false, nil
	}
	if !srcInfo.IsDir() {
		srcRaw, err := os.ReadFile(srcResolved)
		if err != nil {
			return false, err
		}
		dstRaw, err := os.ReadFile(dstResolved)
		if err != nil {
			return false, err
		}
		return bytes.Equal(srcRaw, dstRaw), nil
	}

	srcEntries, err := os.ReadDir(srcResolved)
	if err != nil {
		return false, err
	}
	dstEntries, err := os.ReadDir(dstResolved)
	if err != nil {
		return false, err
	}
	if len(srcEntries) != len(dstEntries) {
		return false, nil
	}

	dstByName := make(map[string]fs.DirEntry, len(dstEntries))
	for _, entry := range dstEntries {
		dstByName[entry.Name()] = entry
	}

	for _, entry := range srcEntries {
		if _, ok := dstByName[entry.Name()]; !ok {
			return false, nil
		}
		equal, err := portablePathEqual(filepath.Join(srcResolved, entry.Name()), filepath.Join(dstResolved, entry.Name()))
		if err != nil {
			return false, err
		}
		if !equal {
			return false, nil
		}
	}
	return true, nil
}

func selectClaudeAuthKeys(raw []byte) (map[string]json.RawMessage, error) {
	return selectClaudeJSONKeys(raw, claudePortableAuthKeys)
}

func selectClaudePortableStateKeys(raw []byte) (map[string]json.RawMessage, error) {
	return selectClaudeJSONKeys(raw, claudePortableStateKeys)
}

func selectClaudePortableSettingsKeys(raw []byte) (map[string]json.RawMessage, error) {
	return selectClaudeJSONKeys(raw, claudePortableSettingsKeys)
}

func selectClaudeJSONKeys(raw []byte, keys []string) (map[string]json.RawMessage, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	selected := make(map[string]json.RawMessage)
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			selected[key] = value
		}
	}
	return selected, nil
}

func jsonSubsetEqual(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for key, rawA := range a {
		rawB, ok := b[key]
		if !ok {
			return false
		}
		if !jsonRawEqual(rawA, rawB) {
			return false
		}
	}
	return true
}

func jsonRawEqual(a, b json.RawMessage) bool {
	var av any
	var bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return bytes.Equal(a, b)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(av, bv)
}

func (p importPlan) hasFoundBasics() bool {
	return len(p.Items) > 0
}

func (p importPlan) hasActionableChanges() bool {
	for _, item := range p.Items {
		if item.Status == importNew || item.Status == importConflict {
			return true
		}
	}
	return false
}

func (p importPlan) conflictCount() int {
	count := 0
	for _, item := range p.Items {
		if item.Status == importConflict {
			count++
		}
	}
	return count
}

func (p *importPlan) resolveConflicts(policy importConflictPolicy) error {
	if p.conflictCount() == 0 {
		return nil
	}

	switch policy {
	case importConflictOverwrite:
		for i := range p.Items {
			if p.Items[i].Status == importConflict {
				p.Items[i].Status = importOverwrite
			}
		}
	case importConflictSkip:
		for i := range p.Items {
			if p.Items[i].Status == importConflict {
				p.Items[i].Status = importSkip
			}
		}
	case importConflictFail, importConflictPrompt:
		var names []string
		for _, item := range p.Items {
			if item.Status == importConflict {
				names = append(names, fmt.Sprintf("%s: %s", item.Category, item.Name))
			}
		}
		return fmt.Errorf("conflicting Claude basics already exist in the agent environment: %s\nRe-run with --overwrite or --skip-existing.", strings.Join(names, ", "))
	default:
		return fmt.Errorf("unknown conflict policy: %s", policy)
	}

	return nil
}

func promptImportConflictPolicy() (importConflictPolicy, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Println("  Conflicting imported items already exist in the agent environment:")
		fmt.Println("    1) Overwrite existing items")
		fmt.Println("    2) Skip existing items")
		fmt.Println("    3) Cancel")
		fmt.Print("  Choice [3]: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return importConflictPrompt, err
		}
		switch strings.TrimSpace(line) {
		case "1":
			return importConflictOverwrite, nil
		case "2":
			return importConflictSkip, nil
		case "", "3":
			return importConflictPrompt, errImportPromptCancelled
		}
	}
}

func printClaudeImportPlan(plan importPlan) {
	fmt.Println()
	cBold.Println("  Found")
	fmt.Println()

	if plan.hasCategory("sign-in") {
		fmt.Printf("    Sign-in:      yes\n")
	}
	if item, ok := plan.firstItem("git identity"); ok {
		desc := formatGitIdentity(item.HostName, item.HostEmail)
		fmt.Printf("    Git identity: %s\n", desc)
	}
	fmt.Printf("    Commands:     %d\n", plan.countCategory("command"))
	fmt.Printf("    Skills:       %d\n", plan.countCategory("skill"))

	printImportPlannedActions(plan)

	fmt.Println()
	cDim.Println("  Hazmat keeps its own settings, hooks, MCP config, plugins, and safety controls.")
	fmt.Println()
}

// printImportPlannedActions renders the "Planned Actions", "Conflicts", and
// "Skipped" sections that are identical across every harness import plan. The
// per-harness printPlan functions render the harness-specific "Found" header
// and trailer around this shared body.
func printImportPlannedActions(plan importPlan) {
	fmt.Println()
	cBold.Println("  Planned Actions")
	fmt.Println()
	fmt.Printf("    New:          %d\n", plan.countStatus(importNew))
	fmt.Printf("    Conflicts:    %d\n", plan.countStatus(importConflict))
	fmt.Printf("    Unchanged:    %d\n", plan.countStatus(importUnchanged))
	if len(plan.Skips) > 0 {
		fmt.Printf("    Skipped:      %d\n", len(plan.Skips))
	}

	if plan.conflictCount() > 0 {
		fmt.Println()
		cBold.Println("  Conflicts")
		fmt.Println()
		for _, item := range plan.Items {
			if item.Status == importConflict {
				fmt.Printf("    %s: %s\n", item.Category, item.Name)
			}
		}
	}

	if len(plan.Skips) > 0 {
		fmt.Println()
		cBold.Println("  Skipped")
		fmt.Println()
		for _, skip := range plan.Skips {
			fmt.Printf("    %s: %s (%s)\n", skip.Category, skip.Name, skip.Reason)
		}
	}
}

func printClaudeImportResult(result importApplyResult) {
	importedCommands := countResultCategory(result.Imported, "command") + countResultCategory(result.Overwritten, "command")
	importedSkills := countResultCategory(result.Imported, "skill") + countResultCategory(result.Overwritten, "skill")

	for _, item := range result.Imported {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ Sign-in imported")
		case "git identity":
			cGreen.Printf("  ✓ Git identity: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	for _, item := range result.Overwritten {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ Sign-in refreshed")
		case "git identity":
			cGreen.Printf("  ✓ Git identity refreshed: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	if importedCommands > 0 {
		cGreen.Printf("  ✓ Commands copied: %d\n", importedCommands)
	}
	if importedSkills > 0 {
		cGreen.Printf("  ✓ Skills copied: %d\n", importedSkills)
	}
	if len(result.Skipped) > 0 {
		cYellow.Printf("  → Existing items kept: %d\n", len(result.Skipped))
	}
	if len(result.Unchanged) > 0 {
		cDim.Printf("  Unchanged: %d\n", len(result.Unchanged))
	}
	fmt.Println()
}

func formatGitIdentity(name, email string) string {
	switch {
	case name != "" && email != "":
		return fmt.Sprintf("%s <%s>", name, email)
	case name != "":
		return name
	case email != "":
		return email
	default:
		return "found"
	}
}

func countResultCategory(items []importItem, category string) int {
	count := 0
	for _, item := range items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p importPlan) hasCategory(category string) bool {
	for _, item := range p.Items {
		if item.Category == category {
			return true
		}
	}
	return false
}

func (p importPlan) firstItem(category string) (importItem, bool) {
	for _, item := range p.Items {
		if item.Category == category {
			return item, true
		}
	}
	return importItem{}, false
}

func (p importPlan) countCategory(category string) int {
	count := 0
	for _, item := range p.Items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p importPlan) countStatus(status importStatus) int {
	count := 0
	for _, item := range p.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func applyClaudeImportPlan(plan importPlan, env claudeImportEnv, r *Runner) (importApplyResult, error) {
	return applyImportPlan(plan, r, func(item importItem, r *Runner) error {
		return applyClaudeImportItem(item, env, r)
	})
}

func applyClaudeImportItem(item importItem, env claudeImportEnv, r *Runner) error {
	switch item.Kind {
	case importPortablePath:
		if err := os.RemoveAll(item.DestPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s %s: %w", item.Category, item.Name, err)
		}
		if err := copyPortablePath(item.SourcePath, item.DestPath); err != nil {
			return fmt.Errorf("copy %s %s: %w", item.Category, item.Name, err)
		}
		return nil
	case importGitIdentity:
		return writeImportedGitIdentity(item, env.agentGitConfigPath())
	case importCredentialFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host credential file: %w", err)
		}
		if err := writeHostStoredSecretFile(item.DestPath, raw); err != nil {
			return fmt.Errorf("write stored Claude credential file: %w", err)
		}
		return removeAgentSecretFile(env.agentCredentialFile())
	case importStateMerge:
		return writeImportedClaudeState(item, env.storedClaudeStatePath(), env.agentClaudeStatePath())
	case importAuthFile:
		// Claude stores sign-in as a split credential file + state merge, not a
		// single auth.json, so the shared auth-file kind never applies here.
	}
	return fmt.Errorf("unsupported import kind: %s", item.Kind)
}

func copyPortablePath(src, dst string) error {
	srcResolved, info, err := resolvePortableSource(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o2770); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o2770); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcResolved)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPortablePath(filepath.Join(srcResolved, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	raw, err := os.ReadFile(srcResolved)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o2770); err != nil {
		return err
	}
	mode := portableFileMode(info.Mode())
	if err := os.WriteFile(dst, raw, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func portableFileMode(mode os.FileMode) os.FileMode {
	perms := mode.Perm()
	perms |= (perms & 0o700) >> 3
	if perms&0o444 == 0 {
		perms |= 0o440
	}
	if perms&0o200 != 0 {
		perms |= 0o020
	}
	return perms & 0o777
}

func writeImportedGitIdentity(item importItem, path string) error {
	current, _ := os.ReadFile(path)
	cfg := parseINI(string(current))
	if item.HostName != "" {
		cfg = setINIValue(cfg, "user", "name", item.HostName)
	}
	if item.HostEmail != "" {
		cfg = setINIValue(cfg, "user", "email", item.HostEmail)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderINI(cfg)), 0o660)
}

func writeImportedClaudeState(item importItem, storePath, legacyPath string) error {
	current, ok, err := readJSONMapStoreFile(storePath)
	if err != nil {
		return err
	}
	if !ok {
		current = map[string]json.RawMessage{}
		if legacyState, ok, err := readClaudeStateKeysFromAgent(legacyPath); err != nil {
			return err
		} else if ok {
			for key, value := range legacyState {
				current[key] = value
			}
		}
	}
	for key, value := range item.HostJSON {
		current[key] = value
	}
	if err := writeJSONMapStoreFile(storePath, current); err != nil {
		return err
	}
	return removeClaudeStateKeysFromAgent(legacyPath)
}

func sortImportItems(items []importItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
}

func sortImportSkips(skips []importSkippedEntry) {
	sort.Slice(skips, func(i, j int) bool {
		if skips[i].Category != skips[j].Category {
			return skips[i].Category < skips[j].Category
		}
		return skips[i].Name < skips[j].Name
	})
}
