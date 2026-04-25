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

type geminiImportKind string

const (
	geminiImportOAuthFile    geminiImportKind = "oauth-file"
	geminiImportAccountsFile geminiImportKind = "accounts-file"
	geminiImportSettingsFile geminiImportKind = "settings-file"
	geminiImportGeminiMD     geminiImportKind = "gemini-md"
	geminiImportGitIdentity  geminiImportKind = "git-identity"
)

type geminiImportEnv struct {
	hostHome  string
	agentHome string
}

type geminiImportItem struct {
	Category   string
	Name       string
	Kind       geminiImportKind
	Status     claudeImportStatus
	SourcePath string
	DestPath   string

	HostName  string
	HostEmail string
}

type geminiImportPlan struct {
	Items []geminiImportItem
	Skips []claudeImportSkippedEntry
}

type geminiImportApplyResult struct {
	Imported    []geminiImportItem
	Overwritten []geminiImportItem
	Skipped     []geminiImportItem
	Unchanged   []geminiImportItem
}

type geminiImportOptions struct {
	PromptBeforeImport bool
	ConflictPolicy     claudeConflictPolicy
	AllowNoopMessage   bool
}

var errGeminiImportCancelled = errors.New("Gemini basics import cancelled")

func defaultGeminiImportEnv() (geminiImportEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return geminiImportEnv{}, fmt.Errorf("determine home directory: %w", err)
	}
	return geminiImportEnv{hostHome: home, agentHome: agentHome}, nil
}

func (e geminiImportEnv) hostGeminiDir() string {
	return filepath.Join(e.hostHome, ".gemini")
}

func (e geminiImportEnv) hostOAuthFile() string {
	return filepath.Join(e.hostGeminiDir(), "oauth_creds.json")
}

func (e geminiImportEnv) storedOAuthFile() string {
	return geminiOAuthStorePathForHome(e.hostHome)
}

func (e geminiImportEnv) hostAccountsFile() string {
	return filepath.Join(e.hostGeminiDir(), "google_accounts.json")
}

func (e geminiImportEnv) storedAccountsFile() string {
	return geminiAccountsStorePathForHome(e.hostHome)
}

func (e geminiImportEnv) hostSettingsFile() string {
	return filepath.Join(e.hostGeminiDir(), "settings.json")
}

func (e geminiImportEnv) hostGeminiMDFile() string {
	return filepath.Join(e.hostGeminiDir(), "GEMINI.md")
}

func (e geminiImportEnv) hostGitConfigPath() string {
	return filepath.Join(e.hostHome, ".gitconfig")
}

func (e geminiImportEnv) agentGeminiDir() string {
	return filepath.Join(e.agentHome, ".gemini")
}

func (e geminiImportEnv) agentOAuthFile() string {
	return filepath.Join(e.agentGeminiDir(), "oauth_creds.json")
}

func (e geminiImportEnv) agentAccountsFile() string {
	return filepath.Join(e.agentGeminiDir(), "google_accounts.json")
}

func (e geminiImportEnv) agentSettingsFile() string {
	return filepath.Join(e.agentGeminiDir(), "settings.json")
}

func (e geminiImportEnv) agentGeminiMDFile() string {
	return filepath.Join(e.agentGeminiDir(), "GEMINI.md")
}

func (e geminiImportEnv) agentGitConfigPath() string {
	return filepath.Join(e.agentHome, ".gitconfig")
}

func newConfigImportGeminiCmd() *cobra.Command {
	var overwrite bool
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   "gemini",
		Short: "Import Gemini basics into Hazmat-managed state",
		Long: `Import a curated subset of your host Gemini setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from ~/.gemini/oauth_creds.json (OAuth refresh token —
    only present when the host stored creds in the file fallback rather
    than the macOS Keychain; modern installs default to Keychain;
    Hazmat stores imported file-based auth in ~/.hazmat/secrets and
    materializes it only for Gemini sessions)
  - account index from ~/.gemini/google_accounts.json
  - ~/.gemini/settings.json (model prefs, MCP servers — portable basics)
  - ~/.gemini/GEMINI.md (cross-project memory file)
  - git user.name and user.email

Hazmat does NOT import history/, the antigravity browser profile, or
runtime caches. Extensions and skills are mirrored automatically by the
managed harness asset sync at session launch.

If your host stores Gemini OAuth in macOS Keychain (the default), the
oauth_creds.json file won't exist on the host and that item will be
skipped — set GEMINI_API_KEY in the agent environment instead, or
re-auth inside 'hazmat gemini' via the device-code flow.

Use --dry-run to preview. If existing imported files differ, either
choose a policy interactively or pass --overwrite / --skip-existing
explicitly.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if overwrite && skipExisting {
				return fmt.Errorf("choose either --overwrite or --skip-existing, not both")
			}

			env, err := defaultGeminiImportEnv()
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

			err = geminiHarness.ImportBasics(ui, r, env, geminiImportOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errGeminiImportCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite conflicting imported items")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip conflicting imported items")

	return cmd
}

func runGeminiBasicsImport(ui *UI, r *Runner, env geminiImportEnv, opts geminiImportOptions) error {
	ui.Step("Import Gemini basics")

	plan, err := scanGeminiImportPlan(env, r)
	if err != nil {
		return err
	}

	if !plan.hasFoundBasics() {
		ui.SkipDone("No Gemini basics found to import")
		return nil
	}

	if opts.AllowNoopMessage && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Gemini basics already match the current import scope")
		return nil
	}

	if opts.PromptBeforeImport && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Gemini basics already imported")
		return nil
	}

	if opts.PromptBeforeImport && !ui.Ask("Import Gemini basics from your host setup?") {
		ui.SkipDone("Gemini basics import skipped")
		return nil
	}

	printGeminiImportPlan(plan)

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
				return errGeminiImportCancelled
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

	result, err := applyGeminiImportPlan(plan, env, r)
	if err != nil {
		return err
	}
	printGeminiImportResult(result)
	return nil
}

func scanGeminiImportPlan(env geminiImportEnv, r *Runner) (geminiImportPlan, error) {
	var plan geminiImportPlan

	scanStoredSecretFile := func(name string, kind geminiImportKind, hostPath, storePath, legacyPath string) error {
		hostRaw, err := os.ReadFile(hostPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("read host %s: %w", name, err)
		}

		status := claudeImportNew
		if storedRaw, ok, err := readHostStoredSecretFile(storePath); err != nil {
			return fmt.Errorf("read stored %s: %w", name, err)
		} else if ok {
			if bytes.Equal(hostRaw, storedRaw) {
				status = claudeImportUnchanged
			} else {
				status = claudeImportConflict
			}
		} else {
			legacyRaw, ok, err := readAgentSecretFile(legacyPath)
			if err != nil {
				return fmt.Errorf("read legacy %s: %w", name, err)
			}
			if ok {
				if bytes.Equal(hostRaw, legacyRaw) {
					status = claudeImportNew
				} else {
					status = claudeImportConflict
				}
			}
		}

		plan.Items = append(plan.Items, geminiImportItem{
			Category:   "sign-in",
			Name:       name,
			Kind:       kind,
			Status:     status,
			SourcePath: hostPath,
			DestPath:   storePath,
		})
		return nil
	}

	scanFile := func(category, name string, kind geminiImportKind, hostPath, agentPath string) error {
		hostRaw, err := os.ReadFile(hostPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("read host %s: %w", name, err)
		}
		agentRaw, err := readMaybePrivilegedFile(agentPath, r)
		status := claudeImportNew
		switch {
		case err == nil && bytes.Equal(hostRaw, agentRaw):
			status = claudeImportUnchanged
		case err == nil:
			status = claudeImportConflict
		case os.IsNotExist(err):
			status = claudeImportNew
		case errors.Is(err, fs.ErrPermission):
			if _, statErr := os.Stat(agentPath); statErr == nil {
				status = claudeImportConflict
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("stat agent %s: %w", name, statErr)
			}
		default:
			return fmt.Errorf("read agent %s: %w", name, err)
		}

		plan.Items = append(plan.Items, geminiImportItem{
			Category:   category,
			Name:       name,
			Kind:       kind,
			Status:     status,
			SourcePath: hostPath,
			DestPath:   agentPath,
		})
		return nil
	}

	if err := scanStoredSecretFile("Gemini OAuth credentials", geminiImportOAuthFile,
		env.hostOAuthFile(), env.storedOAuthFile(), env.agentOAuthFile()); err != nil {
		return plan, err
	}
	if err := scanStoredSecretFile("Gemini account index", geminiImportAccountsFile,
		env.hostAccountsFile(), env.storedAccountsFile(), env.agentAccountsFile()); err != nil {
		return plan, err
	}
	if err := scanFile("settings", "Gemini settings.json", geminiImportSettingsFile,
		env.hostSettingsFile(), env.agentSettingsFile()); err != nil {
		return plan, err
	}
	if err := scanFile("memory", "GEMINI.md (cross-project memory)", geminiImportGeminiMD,
		env.hostGeminiMDFile(), env.agentGeminiMDFile()); err != nil {
		return plan, err
	}

	if item, ok := scanGeminiGitIdentity(env); ok {
		plan.Items = append(plan.Items, item)
	}

	sortGeminiImportItems(plan.Items)
	sortImportSkips(plan.Skips)

	return plan, nil
}

func scanGeminiGitIdentity(env geminiImportEnv) (geminiImportItem, bool) {
	hostName := gitConfigValue(env.hostGitConfigPath(), "name")
	hostEmail := gitConfigValue(env.hostGitConfigPath(), "email")
	if hostName == "" && hostEmail == "" {
		return geminiImportItem{}, false
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

	return geminiImportItem{
		Category:   "git identity",
		Name:       "git identity",
		Kind:       geminiImportGitIdentity,
		Status:     status,
		SourcePath: env.hostGitConfigPath(),
		DestPath:   env.agentGitConfigPath(),
		HostName:   hostName,
		HostEmail:  hostEmail,
	}, true
}

func (p geminiImportPlan) hasFoundBasics() bool {
	return len(p.Items) > 0
}

func (p geminiImportPlan) hasActionableChanges() bool {
	for _, item := range p.Items {
		if item.Status == claudeImportNew || item.Status == claudeImportConflict {
			return true
		}
	}
	return false
}

func (p geminiImportPlan) conflictCount() int {
	count := 0
	for _, item := range p.Items {
		if item.Status == claudeImportConflict {
			count++
		}
	}
	return count
}

func (p *geminiImportPlan) resolveConflicts(policy claudeConflictPolicy) error {
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
		return fmt.Errorf("conflicting Gemini basics already exist in the agent environment: %s\nRe-run with --overwrite or --skip-existing.", strings.Join(names, ", "))
	default:
		return fmt.Errorf("unknown conflict policy: %s", policy)
	}

	return nil
}

func printGeminiImportPlan(plan geminiImportPlan) {
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
	if plan.hasCategory("settings") {
		fmt.Printf("    Settings:     yes\n")
	}
	if plan.hasCategory("memory") {
		fmt.Printf("    Memory:       yes\n")
	}

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
	cDim.Println("  Hazmat keeps its own runtime settings, MCP wiring, and safety controls.")
	fmt.Println()
}

func printGeminiImportResult(result geminiImportApplyResult) {
	for _, item := range result.Imported {
		switch item.Category {
		case "sign-in":
			cGreen.Printf("  ✓ %s imported\n", item.Name)
		case "settings":
			cGreen.Println("  ✓ Gemini settings imported")
		case "memory":
			cGreen.Println("  ✓ GEMINI.md imported")
		case "git identity":
			cGreen.Printf("  ✓ Git identity: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	for _, item := range result.Overwritten {
		switch item.Category {
		case "sign-in":
			cGreen.Printf("  ✓ %s refreshed\n", item.Name)
		case "settings":
			cGreen.Println("  ✓ Gemini settings refreshed")
		case "memory":
			cGreen.Println("  ✓ GEMINI.md refreshed")
		case "git identity":
			cGreen.Printf("  ✓ Git identity refreshed: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	if len(result.Skipped) > 0 {
		cYellow.Printf("  → Existing items kept: %d\n", len(result.Skipped))
	}
	if len(result.Unchanged) > 0 {
		cDim.Printf("  Unchanged: %d\n", len(result.Unchanged))
	}
	fmt.Println()
}

func (p geminiImportPlan) hasCategory(category string) bool {
	for _, item := range p.Items {
		if item.Category == category {
			return true
		}
	}
	return false
}

func (p geminiImportPlan) firstItem(category string) (geminiImportItem, bool) {
	for _, item := range p.Items {
		if item.Category == category {
			return item, true
		}
	}
	return geminiImportItem{}, false
}

func (p geminiImportPlan) countStatus(status claudeImportStatus) int {
	count := 0
	for _, item := range p.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func applyGeminiImportPlan(plan geminiImportPlan, env geminiImportEnv, r *Runner) (geminiImportApplyResult, error) {
	var result geminiImportApplyResult

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

		if err := applyGeminiImportItem(item, env, r); err != nil {
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

func applyGeminiImportItem(item geminiImportItem, env geminiImportEnv, r *Runner) error {
	switch item.Kind {
	case geminiImportGitIdentity:
		return writeImportedGitIdentity(item.toClaudeImportItem(), env.agentGitConfigPath())
	case geminiImportOAuthFile, geminiImportAccountsFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host %s: %w", item.Name, err)
		}
		if err := writeHostStoredSecretFile(item.DestPath, raw); err != nil {
			return fmt.Errorf("write stored %s: %w", item.Name, err)
		}
		legacyPath := env.agentOAuthFile()
		if item.Kind == geminiImportAccountsFile {
			legacyPath = env.agentAccountsFile()
		}
		return removeAgentSecretFile(legacyPath)
	case geminiImportSettingsFile, geminiImportGeminiMD:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host %s: %w", item.Name, err)
		}
		mode := os.FileMode(0o660)
		return writeMaybePrivilegedFile(item.DestPath, raw, mode, agentUser+":staff", r)
	default:
		return fmt.Errorf("unsupported import kind: %s", item.Kind)
	}
}

func (i geminiImportItem) toClaudeImportItem() claudeImportItem {
	return claudeImportItem{
		Category:  i.Category,
		Name:      i.Name,
		HostName:  i.HostName,
		HostEmail: i.HostEmail,
	}
}

func sortGeminiImportItems(items []geminiImportItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
}
