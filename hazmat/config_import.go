package main

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

type claudeImportKind string

const (
	claudeImportPortablePath   claudeImportKind = "portable-path"
	claudeImportCredentialFile claudeImportKind = "credential-file"
	claudeImportGitIdentity    claudeImportKind = "git-identity"
	claudeImportStateMerge     claudeImportKind = "state-merge"
)

type claudeImportStatus string

const (
	claudeImportNew       claudeImportStatus = "new"
	claudeImportConflict  claudeImportStatus = "conflict"
	claudeImportUnchanged claudeImportStatus = "unchanged"
	claudeImportOverwrite claudeImportStatus = "overwrite"
	claudeImportSkip      claudeImportStatus = "skip"
)

type claudeConflictPolicy string

const (
	claudeConflictPrompt    claudeConflictPolicy = "prompt"
	claudeConflictOverwrite claudeConflictPolicy = "overwrite"
	claudeConflictSkip      claudeConflictPolicy = "skip"
	claudeConflictFail      claudeConflictPolicy = "fail"
)

type claudeImportEnv struct {
	hostHome  string
	agentHome string
}

type claudeImportItem struct {
	Category   string
	Name       string
	Kind       claudeImportKind
	Status     claudeImportStatus
	SourcePath string
	DestPath   string
	Reason     string

	HostName  string
	HostEmail string

	HostJSON map[string]json.RawMessage
}

type claudeImportSkippedEntry struct {
	Category string
	Name     string
	Path     string
	Reason   string
}

type claudeImportPlan struct {
	Items []claudeImportItem
	Skips []claudeImportSkippedEntry
}

type claudeImportApplyResult struct {
	Imported    []claudeImportItem
	Overwritten []claudeImportItem
	Skipped     []claudeImportItem
	Unchanged   []claudeImportItem
}

type claudeImportOptions struct {
	PromptBeforeImport bool
	ConflictPolicy     claudeConflictPolicy
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

func (e claudeImportEnv) hostClaudeStatePath() string {
	return filepath.Join(e.hostHome, ".claude.json")
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
and skills.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newConfigImportClaudeCmd())
	cmd.AddCommand(newConfigImportCodexCmd())
	cmd.AddCommand(newConfigImportOpenCodeCmd())
	return cmd
}

func newConfigImportClaudeCmd() *cobra.Command {
	var overwrite bool
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Import Claude basics into the agent environment",
		Long: `Import a curated subset of your host Claude setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from Claude's known auth stores, when present
  - git user.name and user.email
  - ~/.claude/commands
  - ~/.claude/skills

Hazmat does NOT import settings.json, hooks, MCP configuration, plugins,
project-local .claude directories, session history, or runtime caches.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if overwrite && skipExisting {
				return fmt.Errorf("choose either --overwrite or --skip-existing, not both")
			}

			env, err := defaultClaudeImportEnv()
			if err != nil {
				return err
			}

			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)

			policy := claudeConflictPrompt
			switch {
			case overwrite:
				policy = claudeConflictOverwrite
			case skipExisting:
				policy = claudeConflictSkip
			case !ui.IsInteractive():
				policy = claudeConflictFail
			}

			err = claudeCodeHarness.ImportBasics(ui, r, env, claudeImportOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errClaudeImportCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite conflicting imported items")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip conflicting imported items")

	return cmd
}

func runClaudeBasicsImport(ui *UI, r *Runner, env claudeImportEnv, opts claudeImportOptions) error {
	ui.Step("Import Claude basics")

	plan, err := scanClaudeImportPlan(env, r)
	if err != nil {
		return err
	}

	if !plan.hasFoundBasics() {
		ui.SkipDone("No Claude basics found to import")
		return nil
	}

	if opts.AllowNoopMessage && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Claude basics already match the current import scope")
		return nil
	}

	if opts.PromptBeforeImport && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Claude basics already imported")
		return nil
	}

	if opts.PromptBeforeImport && !ui.Ask("Import Claude basics from your host setup?") {
		ui.SkipDone("Claude basics import skipped")
		return nil
	}

	printClaudeImportPlan(plan)

	if !plan.hasActionableChanges() {
		ui.SkipDone("Nothing to import")
		return nil
	}

	policy := opts.ConflictPolicy
	if flagDryRun && policy == claudeConflictPrompt {
		policy = claudeConflictFail
	}
	if plan.conflictCount() > 0 && policy == claudeConflictPrompt {
		selected, err := promptImportConflictPolicy()
		if err != nil {
			return err
		}
		policy = selected
	}

	if flagDryRun {
		if plan.conflictCount() > 0 && (policy == claudeConflictPrompt || policy == claudeConflictFail) {
			cDim.Println("  Re-run with --overwrite or --skip-existing to choose a conflict policy.")
			fmt.Println()
			return nil
		}
		if err := plan.resolveConflicts(policy); err != nil {
			return err
		}
		return nil
	}

	if err := plan.resolveConflicts(policy); err != nil {
		return err
	}

	result, err := applyClaudeImportPlan(plan, env, r)
	if err != nil {
		return err
	}
	printClaudeImportResult(result)
	return nil
}

func scanClaudeImportPlan(env claudeImportEnv, r *Runner) (claudeImportPlan, error) {
	var plan claudeImportPlan

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

func scanClaudeAuthState(env claudeImportEnv, r *Runner) (claudeImportItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostClaudeStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return claudeImportItem{}, false, nil
		}
		return claudeImportItem{}, false, fmt.Errorf("read host Claude state: %w", err)
	}

	hostState, err := selectClaudeAuthKeys(hostRaw)
	if err != nil {
		return claudeImportItem{}, false, fmt.Errorf("parse host Claude state: %w", err)
	}
	if len(hostState) == 0 {
		return claudeImportItem{}, false, nil
	}

	agentRaw, err := readMaybePrivilegedFile(env.agentClaudeStatePath(), r)
	status := claudeImportNew
	switch {
	case err == nil && len(agentRaw) > 0:
		agentState, err := selectClaudeAuthKeys(agentRaw)
		if err != nil {
			return claudeImportItem{}, false, fmt.Errorf("parse agent Claude state: %w", err)
		}
		switch {
		case jsonSubsetEqual(hostState, agentState):
			status = claudeImportUnchanged
		case len(agentState) > 0:
			status = claudeImportConflict
		}
	case os.IsNotExist(err):
		status = claudeImportNew
	case errors.Is(err, fs.ErrPermission):
		if _, statErr := os.Stat(env.agentClaudeStatePath()); statErr == nil {
			status = claudeImportConflict
		} else if !os.IsNotExist(statErr) {
			return claudeImportItem{}, false, fmt.Errorf("stat agent Claude state: %w", statErr)
		}
	case err != nil:
		return claudeImportItem{}, false, fmt.Errorf("read agent Claude state: %w", err)
	}

	return claudeImportItem{
		Category:   "sign-in",
		Name:       "Claude account state",
		Kind:       claudeImportStateMerge,
		Status:     status,
		SourcePath: env.hostClaudeStatePath(),
		DestPath:   env.agentClaudeStatePath(),
		HostJSON:   hostState,
	}, true, nil
}

func scanClaudeCredentialFile(env claudeImportEnv, r *Runner) (claudeImportItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostCredentialFile())
	if err != nil {
		if os.IsNotExist(err) {
			return claudeImportItem{}, false, nil
		}
		return claudeImportItem{}, false, fmt.Errorf("read host Claude credentials: %w", err)
	}

	agentRaw, err := readMaybePrivilegedFile(env.agentCredentialFile(), r)
	status := claudeImportNew
	switch {
	case err == nil && bytes.Equal(hostRaw, agentRaw):
		status = claudeImportUnchanged
	case err == nil:
		status = claudeImportConflict
	case os.IsNotExist(err):
		status = claudeImportNew
	case errors.Is(err, fs.ErrPermission):
		if _, statErr := os.Stat(env.agentCredentialFile()); statErr == nil {
			status = claudeImportConflict
		} else if !os.IsNotExist(statErr) {
			return claudeImportItem{}, false, fmt.Errorf("stat agent Claude credentials: %w", statErr)
		}
	default:
		return claudeImportItem{}, false, fmt.Errorf("read agent Claude credentials: %w", err)
	}

	return claudeImportItem{
		Category:   "sign-in",
		Name:       "Claude credential file",
		Kind:       claudeImportCredentialFile,
		Status:     status,
		SourcePath: env.hostCredentialFile(),
		DestPath:   env.agentCredentialFile(),
	}, true, nil
}

func scanClaudeGitIdentity(env claudeImportEnv) (claudeImportItem, bool) {
	hostName := gitConfigValue(env.hostGitConfigPath(), "name")
	hostEmail := gitConfigValue(env.hostGitConfigPath(), "email")
	if hostName == "" && hostEmail == "" {
		return claudeImportItem{}, false
	}

	agentName := gitConfigValue(env.agentGitConfigPath(), "name")
	agentEmail := gitConfigValue(env.agentGitConfigPath(), "email")

	status := claudeImportNew
	sameName := hostName == "" || hostName == agentName
	sameEmail := hostEmail == "" || hostEmail == agentEmail
	conflictingName := hostName != "" && agentName != "" && hostName != agentName
	conflictingEmail := hostEmail != "" && agentEmail != "" && hostEmail != agentEmail

	switch {
	case sameName && sameEmail && (agentName != "" || agentEmail != ""):
		status = claudeImportUnchanged
	case conflictingName || conflictingEmail:
		status = claudeImportConflict
	}

	return claudeImportItem{
		Category:   "git identity",
		Name:       "git identity",
		Kind:       claudeImportGitIdentity,
		Status:     status,
		SourcePath: env.hostGitConfigPath(),
		DestPath:   env.agentGitConfigPath(),
		HostName:   hostName,
		HostEmail:  hostEmail,
	}, true
}

func scanPortableImportDir(category, hostDir, agentDir string) ([]claudeImportItem, []claudeImportSkippedEntry, error) {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s import directory: %w", hostDir, err)
	}

	var items []claudeImportItem
	var skips []claudeImportSkippedEntry

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		source := filepath.Join(hostDir, name)
		resolved, info, err := resolvePortableSource(source)
		if err != nil {
			skips = append(skips, claudeImportSkippedEntry{
				Category: category,
				Name:     name,
				Path:     source,
				Reason:   err.Error(),
			})
			continue
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			skips = append(skips, claudeImportSkippedEntry{
				Category: category,
				Name:     name,
				Path:     source,
				Reason:   fmt.Sprintf("unsupported source type %s", info.Mode().String()),
			})
			continue
		}

		dest := filepath.Join(agentDir, name)
		status := claudeImportNew
		equal, err := portablePathEqual(resolved, dest)
		switch {
		case err == nil && equal:
			status = claudeImportUnchanged
		case err == nil:
			if _, statErr := os.Lstat(dest); statErr == nil {
				status = claudeImportConflict
			}
		case os.IsNotExist(err):
			status = claudeImportNew
		default:
			return nil, nil, fmt.Errorf("compare %s import %s: %w", category, name, err)
		}

		items = append(items, claudeImportItem{
			Category:   category,
			Name:       name,
			Kind:       claudeImportPortablePath,
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
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	selected := make(map[string]json.RawMessage)
	for _, key := range claudePortableAuthKeys {
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

func (p claudeImportPlan) hasFoundBasics() bool {
	return len(p.Items) > 0
}

func (p claudeImportPlan) hasActionableChanges() bool {
	for _, item := range p.Items {
		if item.Status == claudeImportNew || item.Status == claudeImportConflict {
			return true
		}
	}
	return false
}

func (p claudeImportPlan) conflictCount() int {
	count := 0
	for _, item := range p.Items {
		if item.Status == claudeImportConflict {
			count++
		}
	}
	return count
}

func (p *claudeImportPlan) resolveConflicts(policy claudeConflictPolicy) error {
	if p.conflictCount() == 0 {
		return nil
	}

	switch policy {
	case claudeConflictOverwrite:
		for i := range p.Items {
			if p.Items[i].Status == claudeImportConflict {
				p.Items[i].Status = claudeImportOverwrite
			}
		}
	case claudeConflictSkip:
		for i := range p.Items {
			if p.Items[i].Status == claudeImportConflict {
				p.Items[i].Status = claudeImportSkip
			}
		}
	case claudeConflictFail, claudeConflictPrompt:
		var names []string
		for _, item := range p.Items {
			if item.Status == claudeImportConflict {
				names = append(names, fmt.Sprintf("%s: %s", item.Category, item.Name))
			}
		}
		return fmt.Errorf("conflicting Claude basics already exist in the agent environment: %s\nRe-run with --overwrite or --skip-existing.", strings.Join(names, ", "))
	default:
		return fmt.Errorf("unknown conflict policy: %s", policy)
	}

	return nil
}

func promptImportConflictPolicy() (claudeConflictPolicy, error) {
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
			return claudeConflictPrompt, err
		}
		switch strings.TrimSpace(line) {
		case "1":
			return claudeConflictOverwrite, nil
		case "2":
			return claudeConflictSkip, nil
		case "", "3":
			return claudeConflictPrompt, errClaudeImportCancelled
		}
	}
}

func printClaudeImportPlan(plan claudeImportPlan) {
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

	fmt.Println()
	cBold.Println("  Planned Actions")
	fmt.Println()
	fmt.Printf("    New:          %d\n", plan.countStatus(claudeImportNew))
	fmt.Printf("    Conflicts:    %d\n", plan.countStatus(claudeImportConflict))
	fmt.Printf("    Unchanged:    %d\n", plan.countStatus(claudeImportUnchanged))
	if len(plan.Skips) > 0 {
		fmt.Printf("    Skipped:      %d\n", len(plan.Skips))
	}

	if plan.conflictCount() > 0 {
		fmt.Println()
		cBold.Println("  Conflicts")
		fmt.Println()
		for _, item := range plan.Items {
			if item.Status == claudeImportConflict {
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

	fmt.Println()
	cDim.Println("  Hazmat keeps its own settings, hooks, MCP config, plugins, and safety controls.")
	fmt.Println()
}

func printClaudeImportResult(result claudeImportApplyResult) {
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

func countResultCategory(items []claudeImportItem, category string) int {
	count := 0
	for _, item := range items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p claudeImportPlan) hasCategory(category string) bool {
	for _, item := range p.Items {
		if item.Category == category {
			return true
		}
	}
	return false
}

func (p claudeImportPlan) firstItem(category string) (claudeImportItem, bool) {
	for _, item := range p.Items {
		if item.Category == category {
			return item, true
		}
	}
	return claudeImportItem{}, false
}

func (p claudeImportPlan) countCategory(category string) int {
	count := 0
	for _, item := range p.Items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p claudeImportPlan) countStatus(status claudeImportStatus) int {
	count := 0
	for _, item := range p.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func applyClaudeImportPlan(plan claudeImportPlan, env claudeImportEnv, r *Runner) (claudeImportApplyResult, error) {
	var result claudeImportApplyResult

	for _, item := range plan.Items {
		switch item.Status {
		case claudeImportUnchanged:
			result.Unchanged = append(result.Unchanged, item)
			continue
		case claudeImportSkip:
			result.Skipped = append(result.Skipped, item)
			continue
		default:
			// New, conflict, and overwrite statuses all flow to apply below.
		}

		if err := applyClaudeImportItem(item, env, r); err != nil {
			return result, err
		}

		if item.Status == claudeImportOverwrite {
			result.Overwritten = append(result.Overwritten, item)
		} else {
			result.Imported = append(result.Imported, item)
		}
	}

	return result, nil
}

func applyClaudeImportItem(item claudeImportItem, env claudeImportEnv, r *Runner) error {
	switch item.Kind {
	case claudeImportPortablePath:
		if err := os.RemoveAll(item.DestPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s %s: %w", item.Category, item.Name, err)
		}
		if err := copyPortablePath(item.SourcePath, item.DestPath); err != nil {
			return fmt.Errorf("copy %s %s: %w", item.Category, item.Name, err)
		}
		return nil
	case claudeImportGitIdentity:
		return writeImportedGitIdentity(item, env.agentGitConfigPath())
	case claudeImportCredentialFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host credential file: %w", err)
		}
		return writeMaybePrivilegedFile(item.DestPath, raw, 0o600, agentUser+":staff", r)
	case claudeImportStateMerge:
		return writeImportedClaudeState(item, env.agentClaudeStatePath(), r)
	default:
		return fmt.Errorf("unsupported import kind: %s", item.Kind)
	}
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

func writeImportedGitIdentity(item claudeImportItem, path string) error {
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

func writeImportedClaudeState(item claudeImportItem, path string, r *Runner) error {
	currentRaw, err := readMaybePrivilegedFile(path, r)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	current := map[string]json.RawMessage{}
	if len(currentRaw) > 0 {
		if err := json.Unmarshal(currentRaw, &current); err != nil {
			return fmt.Errorf("parse existing agent Claude state: %w", err)
		}
	}
	for key, value := range item.HostJSON {
		current[key] = value
	}

	merged, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	merged = append(merged, '\n')

	return writeMaybePrivilegedFile(path, merged, 0o600, agentUser+":staff", r)
}

func readMaybePrivilegedFile(path string, r *Runner) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		return raw, nil
	}
	if os.IsNotExist(err) {
		return nil, err
	}
	if !errors.Is(err, fs.ErrPermission) || r == nil {
		return nil, err
	}
	if r.DryRun {
		return nil, fs.ErrPermission
	}
	if path == agentHome || isWithinDir(agentHome, path) {
		out, agentErr := asAgentCombinedOutput("cat", path)
		if agentErr != nil {
			return nil, agentErr
		}
		return []byte(out), nil
	}
	out, sudoErr := r.SudoOutput("cat", path)
	if sudoErr != nil {
		return nil, sudoErr
	}
	return []byte(out), nil
}

func writeMaybePrivilegedFile(path string, raw []byte, mode os.FileMode, owner string, r *Runner) error {
	// Files inside agent home must end up owned by the agent — even when
	// the host can write directly via a dev-group setgid'd parent dir,
	// because a host-owned 0600 credential is unreadable by the agent.
	// Route through the agent write path so ownership matches `owner`.
	if path == agentHome || isWithinDir(agentHome, path) {
		if r != nil && r.DryRun {
			return nil
		}
		if err := agentMkdirAll(filepath.Dir(path)); err != nil {
			return err
		}
		if strings.Contains(owner, sharedGroup) {
			return agentWriteSharedFile(path, raw, mode)
		}
		return agentWriteFile(path, raw, mode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil && !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if err := os.WriteFile(path, raw, mode); err == nil {
		return os.Chmod(path, mode)
	} else if !errors.Is(err, fs.ErrPermission) {
		return err
	}

	if r == nil {
		return fmt.Errorf("write %s: permission denied", path)
	}
	if err := r.SudoWriteFile("write imported Claude file", path, string(raw)); err != nil {
		return err
	}
	if owner != "" {
		if err := r.Sudo("set imported Claude file owner", "chown", owner, path); err != nil {
			return err
		}
	}
	return r.Sudo("set imported Claude file permissions", "chmod", fmt.Sprintf("%04o", mode.Perm()), path)
}

func sortImportItems(items []claudeImportItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
}

func sortImportSkips(skips []claudeImportSkippedEntry) {
	sort.Slice(skips, func(i, j int) bool {
		if skips[i].Category != skips[j].Category {
			return skips[i].Category < skips[j].Category
		}
		return skips[i].Name < skips[j].Name
	})
}
