package hazmat

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type codexImportEnv struct {
	hostHome  string
	agentHome string
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

func (e codexImportEnv) storedAuthFile() string {
	return codexAuthStorePathForHome(e.hostHome)
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
	return newConfigImportHarnessCmd(
		"codex",
		"Import Codex basics into Hazmat-managed state",
		`Import a curated subset of your host Codex setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from ~/.codex/auth.json (covers ChatGPT subscription
    OAuth tokens AND OpenAI API keys; Codex stores both in this file;
    Hazmat stores the imported result in ~/.hazmat/secrets and materializes
    it only for Codex sessions)
  - git user.name and user.email

Hazmat does NOT import config.toml, MCP servers, prompts, rules, AGENTS.md,
session history, or runtime caches. Prompts/rules/AGENTS.md are mirrored
from your host into the agent environment automatically by the managed
harness asset sync at session launch.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		func(ui *UI, r *Runner, policy importConflictPolicy) error {
			env, err := defaultCodexImportEnv()
			if err != nil {
				return err
			}
			err = importCodexBasics(ui, r, env, importOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errCodexImportCancelled) {
				return nil
			}
			return err
		},
	)
}

func runCodexBasicsImport(ui *UI, r *Runner, env codexImportEnv, opts importOptions) error {
	return runBasicsImport(ui, r, opts, harnessImportSpec{
		label:        "Codex",
		cancelledErr: errCodexImportCancelled,
		scan:         func(r *Runner) (importPlan, error) { return scanCodexImportPlan(env, r) },
		applyItem:    func(item importItem, r *Runner) error { return applyCodexImportItem(item, env, r) },
		printPlan:    printCodexImportPlan,
		printResult:  printCodexImportResult,
	})
}

func scanCodexImportPlan(env codexImportEnv, r *Runner) (importPlan, error) {
	var plan importPlan

	if item, ok, err := scanCodexAuthFile(env, r); err != nil {
		return plan, err
	} else if ok {
		plan.Items = append(plan.Items, item)
	}

	if item, ok := scanCodexGitIdentity(env); ok {
		plan.Items = append(plan.Items, item)
	}

	sortImportItems(plan.Items)
	sortImportSkips(plan.Skips)

	return plan, nil
}

func scanCodexAuthFile(env codexImportEnv, r *Runner) (importItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostAuthFile())
	if err != nil {
		if os.IsNotExist(err) {
			return importItem{}, false, nil
		}
		return importItem{}, false, fmt.Errorf("read host Codex auth: %w", err)
	}

	status := importNew
	if storedRaw, ok, err := readHostStoredSecretFile(env.storedAuthFile()); err != nil {
		return importItem{}, false, fmt.Errorf("read stored Codex auth: %w", err)
	} else if ok {
		if bytes.Equal(hostRaw, storedRaw) {
			status = importUnchanged
		} else {
			status = importConflict
		}
	} else {
		agentRaw, ok, err := readAgentSecretFile(env.agentAuthFile())
		if err != nil {
			return importItem{}, false, fmt.Errorf("read agent Codex auth: %w", err)
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
		Name:       "Codex auth file",
		Kind:       importAuthFile,
		Status:     status,
		SourcePath: env.hostAuthFile(),
		DestPath:   env.storedAuthFile(),
	}, true, nil
}

func scanCodexGitIdentity(env codexImportEnv) (importItem, bool) {
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

func printCodexImportPlan(plan importPlan) {
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

	printImportPlannedActions(plan)

	fmt.Println()
	cDim.Println("  Hazmat keeps its own runtime settings, MCP wiring, and safety controls.")
	fmt.Println()
}

func printCodexImportResult(result importApplyResult) {
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

func applyCodexImportPlan(plan importPlan, env codexImportEnv, r *Runner) (importApplyResult, error) {
	return applyImportPlan(plan, r, func(item importItem, r *Runner) error {
		return applyCodexImportItem(item, env, r)
	})
}

func applyCodexImportItem(item importItem, env codexImportEnv, r *Runner) error {
	switch item.Kind {
	case importGitIdentity:
		return writeImportedGitIdentity(item, env.agentGitConfigPath())
	case importAuthFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host Codex auth file: %w", err)
		}
		if err := writeHostStoredSecretFile(item.DestPath, raw); err != nil {
			return fmt.Errorf("write stored Codex auth file: %w", err)
		}
		return removeAgentSecretFile(env.agentAuthFile())
	case importPortablePath, importCredentialFile, importStateMerge:
		// Codex sign-in is a single auth.json; it has no portable dirs, split
		// credential file, or Claude-style state merge.
	}
	return fmt.Errorf("unsupported import kind: %s", item.Kind)
}
