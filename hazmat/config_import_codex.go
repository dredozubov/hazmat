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

type codexImportKind string

const (
	codexImportAuthFile    codexImportKind = "auth-file"
	codexImportGitIdentity codexImportKind = "git-identity"
)

type codexImportEnv struct {
	hostHome  string
	agentHome string
}

type codexImportItem struct {
	Category   string
	Name       string
	Kind       codexImportKind
	Status     claudeImportStatus
	SourcePath string
	DestPath   string

	HostName  string
	HostEmail string
}

type codexImportPlan struct {
	Items []codexImportItem
	Skips []claudeImportSkippedEntry
}

type codexImportApplyResult struct {
	Imported    []codexImportItem
	Overwritten []codexImportItem
	Skipped     []codexImportItem
	Unchanged   []codexImportItem
}

type codexImportOptions struct {
	PromptBeforeImport bool
	ConflictPolicy     claudeConflictPolicy
	AllowNoopMessage   bool
}

var errCodexImportCancelled = errors.New("Codex basics import cancelled")

func defaultCodexImportEnv() (codexImportEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return codexImportEnv{}, fmt.Errorf("determine home directory: %w", err)
	}
	return codexImportEnv{hostHome: home, agentHome: agentHome}, nil
}

func (e codexImportEnv) hostCodexDir() string {
	return filepath.Join(e.hostHome, ".codex")
}

func (e codexImportEnv) hostAuthFile() string {
	return filepath.Join(e.hostCodexDir(), "auth.json")
}

func (e codexImportEnv) hostGitConfigPath() string {
	return filepath.Join(e.hostHome, ".gitconfig")
}

func (e codexImportEnv) agentCodexDir() string {
	return filepath.Join(e.agentHome, ".codex")
}

func (e codexImportEnv) agentAuthFile() string {
	return filepath.Join(e.agentCodexDir(), "auth.json")
}

func (e codexImportEnv) agentGitConfigPath() string {
	return filepath.Join(e.agentHome, ".gitconfig")
}

func newConfigImportCodexCmd() *cobra.Command {
	var overwrite bool
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   "codex",
		Short: "Import Codex basics into the agent environment",
		Long: `Import a curated subset of your host Codex setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from ~/.codex/auth.json (covers ChatGPT subscription
    OAuth tokens AND OpenAI API keys; Codex stores both in this file)
  - git user.name and user.email

Hazmat does NOT import config.toml, MCP servers, prompts, rules, AGENTS.md,
session history, or runtime caches. Prompts/rules/AGENTS.md are mirrored
from your host into the agent environment automatically by the managed
harness asset sync at session launch.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if overwrite && skipExisting {
				return fmt.Errorf("choose either --overwrite or --skip-existing, not both")
			}

			env, err := defaultCodexImportEnv()
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

			err = codexHarness.ImportBasics(ui, r, env, codexImportOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errCodexImportCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite conflicting imported items")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip conflicting imported items")

	return cmd
}

func runCodexBasicsImport(ui *UI, r *Runner, env codexImportEnv, opts codexImportOptions) error {
	ui.Step("Import Codex basics")

	plan, err := scanCodexImportPlan(env, r)
	if err != nil {
		return err
	}

	if !plan.hasFoundBasics() {
		ui.SkipDone("No Codex basics found to import")
		return nil
	}

	if opts.AllowNoopMessage && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Codex basics already match the current import scope")
		return nil
	}

	if opts.PromptBeforeImport && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone("Codex basics already imported")
		return nil
	}

	if opts.PromptBeforeImport && !ui.Ask("Import Codex basics from your host setup?") {
		ui.SkipDone("Codex basics import skipped")
		return nil
	}

	printCodexImportPlan(plan)

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
				return errCodexImportCancelled
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

	result, err := applyCodexImportPlan(plan, env, r)
	if err != nil {
		return err
	}
	printCodexImportResult(result)
	return nil
}

func scanCodexImportPlan(env codexImportEnv, r *Runner) (codexImportPlan, error) {
	var plan codexImportPlan

	if item, ok, err := scanCodexAuthFile(env, r); err != nil {
		return plan, err
	} else if ok {
		plan.Items = append(plan.Items, item)
	}

	if item, ok := scanCodexGitIdentity(env); ok {
		plan.Items = append(plan.Items, item)
	}

	sortCodexImportItems(plan.Items)
	sortImportSkips(plan.Skips)

	return plan, nil
}

func scanCodexAuthFile(env codexImportEnv, r *Runner) (codexImportItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostAuthFile())
	if err != nil {
		if os.IsNotExist(err) {
			return codexImportItem{}, false, nil
		}
		return codexImportItem{}, false, fmt.Errorf("read host Codex auth: %w", err)
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
			return codexImportItem{}, false, fmt.Errorf("stat agent Codex auth: %w", statErr)
		}
	default:
		return codexImportItem{}, false, fmt.Errorf("read agent Codex auth: %w", err)
	}

	// Self-heal: if content matches but the file isn't owned by the agent
	// (e.g. a pre-fix import wrote it as the host user), re-import so the
	// agent-write path restores correct ownership. Otherwise codex can't
	// open the file and dies with errSecNoSuchKeychain inside the sandbox.
	if status == claudeImportUnchanged && !agentOwnsFile(env.agentAuthFile()) {
		status = claudeImportNew
	}

	return codexImportItem{
		Category:   "sign-in",
		Name:       "Codex auth file",
		Kind:       codexImportAuthFile,
		Status:     status,
		SourcePath: env.hostAuthFile(),
		DestPath:   env.agentAuthFile(),
	}, true, nil
}

func scanCodexGitIdentity(env codexImportEnv) (codexImportItem, bool) {
	hostName := gitConfigValue(env.hostGitConfigPath(), "name")
	hostEmail := gitConfigValue(env.hostGitConfigPath(), "email")
	if hostName == "" && hostEmail == "" {
		return codexImportItem{}, false
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

	return codexImportItem{
		Category:   "git identity",
		Name:       "git identity",
		Kind:       codexImportGitIdentity,
		Status:     status,
		SourcePath: env.hostGitConfigPath(),
		DestPath:   env.agentGitConfigPath(),
		HostName:   hostName,
		HostEmail:  hostEmail,
	}, true
}

func (p codexImportPlan) hasFoundBasics() bool {
	return len(p.Items) > 0
}

func (p codexImportPlan) hasActionableChanges() bool {
	for _, item := range p.Items {
		if item.Status == claudeImportNew || item.Status == claudeImportConflict {
			return true
		}
	}
	return false
}

func (p codexImportPlan) conflictCount() int {
	count := 0
	for _, item := range p.Items {
		if item.Status == claudeImportConflict {
			count++
		}
	}
	return count
}

func (p *codexImportPlan) resolveConflicts(policy claudeConflictPolicy) error {
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
		return fmt.Errorf("conflicting Codex basics already exist in the agent environment: %s\nRe-run with --overwrite or --skip-existing.", strings.Join(names, ", "))
	default:
		return fmt.Errorf("unknown conflict policy: %s", policy)
	}

	return nil
}

func printCodexImportPlan(plan codexImportPlan) {
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

func printCodexImportResult(result codexImportApplyResult) {
	for _, item := range result.Imported {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ Codex auth imported")
		case "git identity":
			cGreen.Printf("  ✓ Git identity: %s <%s>\n", item.HostName, item.HostEmail)
		}
	}
	for _, item := range result.Overwritten {
		switch item.Category {
		case "sign-in":
			cGreen.Println("  ✓ Codex auth refreshed")
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

func (p codexImportPlan) hasCategory(category string) bool {
	for _, item := range p.Items {
		if item.Category == category {
			return true
		}
	}
	return false
}

func (p codexImportPlan) firstItem(category string) (codexImportItem, bool) {
	for _, item := range p.Items {
		if item.Category == category {
			return item, true
		}
	}
	return codexImportItem{}, false
}

func (p codexImportPlan) countStatus(status claudeImportStatus) int {
	count := 0
	for _, item := range p.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func applyCodexImportPlan(plan codexImportPlan, env codexImportEnv, r *Runner) (codexImportApplyResult, error) {
	var result codexImportApplyResult

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

		if err := applyCodexImportItem(item, env, r); err != nil {
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

func applyCodexImportItem(item codexImportItem, env codexImportEnv, r *Runner) error {
	switch item.Kind {
	case codexImportGitIdentity:
		return writeImportedGitIdentity(item.toClaudeImportItem(), env.agentGitConfigPath())
	case codexImportAuthFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host Codex auth file: %w", err)
		}
		return writeMaybePrivilegedFile(item.DestPath, raw, 0o600, agentUser+":staff", r)
	default:
		return fmt.Errorf("unsupported import kind: %s", item.Kind)
	}
}

func (i codexImportItem) toClaudeImportItem() claudeImportItem {
	return claudeImportItem{
		Category:  i.Category,
		Name:      i.Name,
		HostName:  i.HostName,
		HostEmail: i.HostEmail,
	}
}

func sortCodexImportItems(items []codexImportItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
}
