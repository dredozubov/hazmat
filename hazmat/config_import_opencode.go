package hazmat

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type opencodeImportEnv struct {
	hostHome  string
	agentHome string
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

func (e opencodeImportEnv) storedAuthFile() string {
	return openCodeAuthStorePathForHome(e.hostHome)
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
	return newConfigImportHarnessCmd(
		"opencode",
		"Import OpenCode basics into Hazmat-managed state",
		`Import a curated subset of your host OpenCode setup into Hazmat.

Hazmat imports only portable basics:
  - sign-in state from ~/.local/share/opencode/auth.json
    (stored in ~/.hazmat/secrets and materialized only for OpenCode sessions)
  - git user.name and user.email
  - ~/.config/opencode/commands
  - ~/.config/opencode/agents
  - ~/.config/opencode/skills

Hazmat does NOT import opencode.json, plugins, tools, themes, modes, project-local
.opencode directories, or other runtime-specific state.

Use --dry-run to preview. If existing imported files differ, either choose a
policy interactively or pass --overwrite / --skip-existing explicitly.`,
		func(ui *UI, r *Runner, policy importConflictPolicy) error {
			env, err := defaultOpenCodeImportEnv()
			if err != nil {
				return err
			}
			err = importOpenCodeBasics(ui, r, env, importOptions{
				PromptBeforeImport: false,
				ConflictPolicy:     policy,
				AllowNoopMessage:   true,
			})
			if errors.Is(err, errOpenCodeImportCancelled) {
				return nil
			}
			return err
		},
	)
}

func runOpenCodeBasicsImport(ui *UI, r *Runner, env opencodeImportEnv, opts importOptions) error {
	return runBasicsImport(ui, r, opts, harnessImportSpec{
		label:        "OpenCode",
		cancelledErr: errOpenCodeImportCancelled,
		scan:         func(r *Runner) (importPlan, error) { return scanOpenCodeImportPlan(env, r) },
		applyItem:    func(item importItem, r *Runner) error { return applyOpenCodeImportItem(item, env, r) },
		printPlan:    printOpenCodeImportPlan,
		printResult:  printOpenCodeImportResult,
	})
}

func scanOpenCodeImportPlan(env opencodeImportEnv, r *Runner) (importPlan, error) {
	var plan importPlan

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
	plan.Items = append(plan.Items, commandItems...)
	plan.Skips = append(plan.Skips, skips...)

	agentItems, skips, err := scanPortableImportDir("agent", env.hostAgentsDir(), env.agentAgentsDir())
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, agentItems...)
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

func scanOpenCodeAuthFile(env opencodeImportEnv, r *Runner) (importItem, bool, error) {
	hostRaw, err := os.ReadFile(env.hostAuthFile())
	if err != nil {
		if os.IsNotExist(err) {
			return importItem{}, false, nil
		}
		return importItem{}, false, fmt.Errorf("read host OpenCode auth: %w", err)
	}

	status := importNew
	if storedRaw, ok, err := readHostStoredSecretFile(env.storedAuthFile()); err != nil {
		return importItem{}, false, fmt.Errorf("read stored OpenCode auth: %w", err)
	} else if ok {
		if bytes.Equal(hostRaw, storedRaw) {
			status = importUnchanged
		} else {
			status = importConflict
		}
	} else {
		agentRaw, ok, err := readAgentSecretFile(env.agentAuthFile())
		if err != nil {
			return importItem{}, false, fmt.Errorf("read agent OpenCode auth: %w", err)
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
		Name:       "OpenCode auth file",
		Kind:       importAuthFile,
		Status:     status,
		SourcePath: env.hostAuthFile(),
		DestPath:   env.storedAuthFile(),
	}, true, nil
}

func scanOpenCodeGitIdentity(env opencodeImportEnv) (importItem, bool) {
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

func printOpenCodeImportPlan(plan importPlan) {
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

	printImportPlannedActions(plan)

	fmt.Println()
	cDim.Println("  Hazmat keeps its own runtime settings, plugin surface, and safety controls.")
	fmt.Println()
}

func printOpenCodeImportResult(result importApplyResult) {
	importedCommands := countResultCategory(result.Imported, "command") + countResultCategory(result.Overwritten, "command")
	importedAgents := countResultCategory(result.Imported, "agent") + countResultCategory(result.Overwritten, "agent")
	importedSkills := countResultCategory(result.Imported, "skill") + countResultCategory(result.Overwritten, "skill")

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

func applyOpenCodeImportPlan(plan importPlan, env opencodeImportEnv, r *Runner) (importApplyResult, error) {
	return applyImportPlan(plan, r, func(item importItem, r *Runner) error {
		return applyOpenCodeImportItem(item, env, r)
	})
}

func applyOpenCodeImportItem(item importItem, env opencodeImportEnv, r *Runner) error {
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
	case importAuthFile:
		raw, err := os.ReadFile(item.SourcePath)
		if err != nil {
			return fmt.Errorf("read host OpenCode auth file: %w", err)
		}
		if err := writeHostStoredSecretFile(item.DestPath, raw); err != nil {
			return fmt.Errorf("write stored OpenCode auth file: %w", err)
		}
		return removeAgentSecretFile(env.agentAuthFile())
	case importCredentialFile, importStateMerge:
		// OpenCode sign-in is a single auth.json; it has no split credential
		// file or Claude-style state merge.
	}
	return fmt.Errorf("unsupported import kind: %s", item.Kind)
}
