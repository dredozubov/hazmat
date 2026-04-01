package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type opencodeImportKind string

const (
	opencodeImportPortablePath opencodeImportKind = "portable-path"
	opencodeImportAuthFile     opencodeImportKind = "auth-file"
	opencodeImportGitIdentity  opencodeImportKind = "git-identity"
)

type opencodeImportEnv struct {
	hostHome  string
	agentHome string
}

type opencodeImportItem struct {
	Category   string
	Name       string
	Kind       opencodeImportKind
	Status     claudeImportStatus
	SourcePath string
	DestPath   string

	HostName  string
	HostEmail string
}

type opencodeImportPlan struct {
	Items []opencodeImportItem
	Skips []claudeImportSkippedEntry
}

type opencodeImportApplyResult struct {
	Imported    []opencodeImportItem
	Overwritten []opencodeImportItem
	Skipped     []opencodeImportItem
	Unchanged   []opencodeImportItem
}

type opencodeImportOptions struct {
	PromptBeforeImport bool
	ConflictPolicy     claudeConflictPolicy
	AllowNoopMessage   bool
}

var errOpenCodeImportCancelled = errors.New("OpenCode basics import cancelled")

func defaultOpenCodeImportEnv() (opencodeImportEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return opencodeImportEnv{}, fmt.Errorf("determine home directory: %w", err)
	}
	return opencodeImportEnv{hostHome: home, agentHome: agentHome}, nil
}

func (e opencodeImportEnv) hostOpenCodeDir() string {
	return filepath.Join(e.hostHome, ".config", "opencode")
}

func (e opencodeImportEnv) hostCommandsDir() string {
	return filepath.Join(e.hostOpenCodeDir(), "commands")
}

func (e opencodeImportEnv) hostAgentsDir() string {
	return filepath.Join(e.hostOpenCodeDir(), "agents")
}

func (e opencodeImportEnv) hostSkillsDir() string {
	return filepath.Join(e.hostOpenCodeDir(), "skills")
}

func (e opencodeImportEnv) hostConfigPath() string {
	return filepath.Join(e.hostOpenCodeDir(), "opencode.json")
}

func (e opencodeImportEnv) hostAuthFile() string {
	return filepath.Join(e.hostHome, ".local", "share", "opencode", "auth.json")
}

func (e opencodeImportEnv) hostGitConfigPath() string {
	return filepath.Join(e.hostHome, ".gitconfig")
}

func (e opencodeImportEnv) agentOpenCodeDir() string {
	return filepath.Join(e.agentHome, ".config", "opencode")
}

func (e opencodeImportEnv) agentCommandsDir() string {
	return filepath.Join(e.agentOpenCodeDir(), "commands")
}

func (e opencodeImportEnv) agentAgentsDir() string {
	return filepath.Join(e.agentOpenCodeDir(), "agents")
}

func (e opencodeImportEnv) agentSkillsDir() string {
	return filepath.Join(e.agentOpenCodeDir(), "skills")
}

func (e opencodeImportEnv) agentAuthFile() string {
	return filepath.Join(e.agentHome, ".local", "share", "opencode", "auth.json")
}

func (e opencodeImportEnv) agentGitConfigPath() string {
	return filepath.Join(e.agentHome, ".gitconfig")
}

func newConfigImportOpenCodeCmd() *cobra.Command {
	var overwrite bool
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "Import OpenCode basics into the agent environment",
		Long: `Import a curated subset of your host OpenCode setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from ~/.local/share/opencode/auth.json
  - git user.name and user.email
  - ~/.config/opencode/commands
  - ~/.config/opencode/agents
  - ~/.config/opencode/skills

Hazmat does NOT import opencode.json, plugins, tools, themes, modes, project-local
.opencode directories, or other runtime-specific state.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if overwrite && skipExisting {
				return fmt.Errorf("choose either --overwrite or --skip-existing, not both")
			}

			env, err := defaultOpenCodeImportEnv()
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

			err = openCodeHarness.ImportBasics(ui, r, env, opencodeImportOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errOpenCodeImportCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite conflicting imported items")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip conflicting imported items")

	return cmd
}

func runOpenCodeBasicsImport(ui *UI, r *Runner, env opencodeImportEnv, opts opencodeImportOptions) error {
	ui.Step("Import OpenCode basics")

	plan, err := scanOpenCodeImportPlan(env, r)
	if err != nil {
		return err
	}

	if !plan.hasFoundBasics() {
		ui.SkipDone("No OpenCode basics found to import")
		return nil
	}

	if opts.AllowNoopMessage && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("OpenCode basics already match the current import scope")
		return nil
	}

	if opts.PromptBeforeImport && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("OpenCode basics already imported")
		return nil
	}

	if opts.PromptBeforeImport && !ui.Ask("Import OpenCode basics from your host setup?") {
		ui.SkipDone("OpenCode basics import skipped")
		return nil
	}

	printOpenCodeImportPlan(plan)

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
			if errors.Is(err, errClaudeImportCancelled) {
				return errOpenCodeImportCancelled
			}
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

	result, err := applyOpenCodeImportPlan(plan, env, r)
	if err != nil {
		return err
	}
	printOpenCodeImportResult(result)
	return nil
}

func scanOpenCodeImportPlan(env opencodeImportEnv, r *Runner) (opencodeImportPlan, error) {
	var plan opencodeImportPlan

	if item, ok, err := scanOpenCodeAuthFile(env, r); err != nil {
		return plan, err
	} else if ok {
		plan.Items = append(plan.Items, item)
	}

	if item, ok := scanOpenCodeGitIdentity(env); ok {
		plan.Items = append(plan.Items, item)
	}

	commandItems, skips, err := scanPortableImportDir("command", env.hostCommandsDir(), env.agentCommandsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, toOpenCodePortableItems(commandItems)...)
	plan.Skips = append(plan.Skips, skips...)

	agentItems, skips, err := scanPortableImportDir("agent", env.hostAgentsDir(), env.agentAgentsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, toOpenCodePortableItems(agentItems)...)
	plan.Skips = append(plan.Skips, skips...)

	skillItems, skips, err := scanPortableImportDir("skill", env.hostSkillsDir(), env.agentSkillsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, toOpenCodePortableItems(skillItems)...)
	plan.Skips = append(plan.Skips, skips...)

	sortOpenCodeImportItems(plan.Items)
	sortImportSkips(plan.Skips)

	return plan, nil
}

func toOpenCodePortableItems(items []claudeImportItem) []opencodeImportItem {
	result := make([]opencodeImportItem, 0, len(items))
	for _, item := range items {
		result = append(result, opencodeImportItem{
			Category:   item.Category,
			Name:       item.Name,
			Kind:       opencodeImportPortablePath,
			Status:     item.Status,
			SourcePath: item.SourcePath,
			DestPath:   item.DestPath,
		})
	}
	return result
}

func scanOpenCodeAuthFile(env opencodeImportEnv, r *Runner) (opencodeImportItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostAuthFile())
	if err != nil {
		if os.IsNotExist(err) {
			return opencodeImportItem{}, false, nil
		}
		return opencodeImportItem{}, false, fmt.Errorf("read host OpenCode auth: %w", err)
	}

	agentRaw, err := readMaybePrivilegedFile(env.agentAuthFile(), r)
	status := claudeImportNew
	switch {
	case err == nil && bytes.Equal(hostRaw, agentRaw):
		status = claudeImportUnchanged
	case err == nil:
		status = claudeImportConflict
	case os.IsNotExist(err):
		status = claudeImportNew
	case errors.Is(err, fs.ErrPermission):
		if _, statErr := os.Stat(env.agentAuthFile()); statErr == nil {
			status = claudeImportConflict
		} else if !os.IsNotExist(statErr) {
			return opencodeImportItem{}, false, fmt.Errorf("stat agent OpenCode auth: %w", statErr)
		}
	default:
		return opencodeImportItem{}, false, fmt.Errorf("read agent OpenCode auth: %w", err)
	}

	return opencodeImportItem{
		Category:   "sign-in",
		Name:       "OpenCode auth file",
		Kind:       opencodeImportAuthFile,
		Status:     status,
		SourcePath: env.hostAuthFile(),
		DestPath:   env.agentAuthFile(),
	}, true, nil
}

func scanOpenCodeGitIdentity(env opencodeImportEnv) (opencodeImportItem, bool) {
	hostName := gitConfigValue(env.hostGitConfigPath(), "name")
	hostEmail := gitConfigValue(env.hostGitConfigPath(), "email")
	if hostName == "" && hostEmail == "" {
		return opencodeImportItem{}, false
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

	return opencodeImportItem{
		Category:   "git identity",
		Name:       "git identity",
		Kind:       opencodeImportGitIdentity,
		Status:     status,
		SourcePath: env.hostGitConfigPath(),
		DestPath:   env.agentGitConfigPath(),
		HostName:   hostName,
		HostEmail:  hostEmail,
	}, true
}

func (p opencodeImportPlan) hasFoundBasics() bool {
	return len(p.Items) > 0
}

func (p opencodeImportPlan) hasActionableChanges() bool {
	for _, item := range p.Items {
		if item.Status == claudeImportNew || item.Status == claudeImportConflict {
			return true
		}
	}
	return false
}

func (p opencodeImportPlan) conflictCount() int {
	count := 0
	for _, item := range p.Items {
		if item.Status == claudeImportConflict {
			count++
		}
	}
	return count
}

func (p *opencodeImportPlan) resolveConflicts(policy claudeConflictPolicy) error {
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
		return fmt.Errorf("conflicting OpenCode basics already exist in the agent environment: %s\nRe-run with --overwrite or --skip-existing.", strings.Join(names, ", "))
	default:
		return fmt.Errorf("unknown conflict policy: %s", policy)
	}

	return nil
}

func printOpenCodeImportPlan(plan opencodeImportPlan) {
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
	fmt.Printf("    Agents:       %d\n", plan.countCategory("agent"))
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
	cDim.Println("  Hazmat keeps its own runtime settings, plugin surface, and safety controls.")
	fmt.Println()
}

func printOpenCodeImportResult(result opencodeImportApplyResult) {
	importedCommands := countOpenCodeResultCategory(result.Imported, "command") + countOpenCodeResultCategory(result.Overwritten, "command")
	importedAgents := countOpenCodeResultCategory(result.Imported, "agent") + countOpenCodeResultCategory(result.Overwritten, "agent")
	importedSkills := countOpenCodeResultCategory(result.Imported, "skill") + countOpenCodeResultCategory(result.Overwritten, "skill")

	for _, item := range result.Imported {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ OpenCode auth imported")
		case "git identity":
			cGreen.Printf("  ✓ Git identity: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	for _, item := range result.Overwritten {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ OpenCode auth refreshed")
		case "git identity":
			cGreen.Printf("  ✓ Git identity refreshed: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	if importedCommands > 0 {
		cGreen.Printf("  ✓ Commands copied: %d\n", importedCommands)
	}
	if importedAgents > 0 {
		cGreen.Printf("  ✓ Agents copied: %d\n", importedAgents)
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

func countOpenCodeResultCategory(items []opencodeImportItem, category string) int {
	count := 0
	for _, item := range items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p opencodeImportPlan) hasCategory(category string) bool {
	for _, item := range p.Items {
		if item.Category == category {
			return true
		}
	}
	return false
}

func (p opencodeImportPlan) firstItem(category string) (opencodeImportItem, bool) {
	for _, item := range p.Items {
		if item.Category == category {
			return item, true
		}
	}
	return opencodeImportItem{}, false
}

func (p opencodeImportPlan) countCategory(category string) int {
	count := 0
	for _, item := range p.Items {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (p opencodeImportPlan) countStatus(status claudeImportStatus) int {
	count := 0
	for _, item := range p.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func applyOpenCodeImportPlan(plan opencodeImportPlan, env opencodeImportEnv, r *Runner) (opencodeImportApplyResult, error) {
	var result opencodeImportApplyResult

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

		if err := applyOpenCodeImportItem(item, env, r); err != nil {
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

func applyOpenCodeImportItem(item opencodeImportItem, env opencodeImportEnv, r *Runner) error {
	switch item.Kind {
	case opencodeImportPortablePath:
		if err := os.RemoveAll(item.DestPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s %s: %w", item.Category, item.Name, err)
		}
		if err := copyPortablePath(item.SourcePath, item.DestPath); err != nil {
			return fmt.Errorf("copy %s %s: %w", item.Category, item.Name, err)
		}
		return nil
	case opencodeImportGitIdentity:
		return writeImportedGitIdentity(item.toClaudeImportItem(), env.agentGitConfigPath())
	case opencodeImportAuthFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host OpenCode auth file: %w", err)
		}
		return writeMaybePrivilegedFile(item.DestPath, raw, 0o600, agentUser+":staff", r)
	default:
		return fmt.Errorf("unsupported import kind: %s", item.Kind)
	}
}

func (i opencodeImportItem) toClaudeImportItem() claudeImportItem {
	return claudeImportItem{
		Category:  i.Category,
		Name:      i.Name,
		HostName:  i.HostName,
		HostEmail: i.HostEmail,
	}
}

func sortOpenCodeImportItems(items []opencodeImportItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
}
