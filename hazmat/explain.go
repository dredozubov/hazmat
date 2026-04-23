package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	var target string
	var project string
	var readDirs []string
	var writeDirs []string
	var integrationNames []string
	var skipHarnessAssetsSync bool
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	var dockerModeValue string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Preview the session contract without launching an agent",
		Long: `Show what Hazmat would do for a session without launching the agent.

This prints the same session contract, planned host changes, and
mode explanation that a real launch would show, but stops before snapshots,
sandbox setup, permission repair, or process execution.

Examples:
  hazmat explain
  hazmat explain --json
  hazmat explain -C ~/workspace/my-project --integration node
  hazmat explain --for shell --docker=sandbox -C ~/workspace/docker-app
  hazmat explain --for opencode --docker=none -C ~/workspace/repo
  hazmat explain --for gemini --integration go -C ~/workspace/my-go-project`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, mode, err := resolveExplainSession(target, harnessSessionOpts{
				project:               project,
				readDirs:              readDirs,
				writeDirs:             writeDirs,
				integrations:          integrationNames,
				skipHarnessAssetsSync: skipHarnessAssetsSync,
				noBackup:              noBackup,
				useSandbox:            useSandbox,
				allowDocker:           allowDocker,
				dockerMode:            dockerModeValue,
				dockerModeExplicit:    cmd.Flags().Changed("docker"),
			})
			if err != nil {
				return err
			}

			if outputJSON {
				preview := buildExplainJSON(target, cfg, mode, noBackup)
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				return enc.Encode(preview)
			}

			printSessionContract(cfg, mode, noBackup)
			printSuggestedIntegrations(cfg.SuggestedIntegrations)
			printSessionMutationDetails(cfg.PlannedHostMutations)
			fmt.Fprint(cmd.ErrOrStderr(), renderIntegrationDetails(cfg.IntegrationDetails))
			return nil
		},
	}

	cmd.Flags().StringVar(&target, "for", "claude",
		"Preview target (claude, shell, exec, opencode, codex, gemini)")
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVarP(&writeDirs, "write", "W", nil,
		"Read-write directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&integrationNames, "integration", nil,
		"Activate a session integration (repeatable, e.g. --integration go)")
	cmd.Flags().BoolVar(&skipHarnessAssetsSync, "skip-harness-assets-sync", false,
		"Preview without managed harness prompt-asset sync")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Preview without a pre-session snapshot")
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeNone),
		"Docker routing: none (default), sandbox, or auto")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Preview Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Preview native containment even if Docker markers are present")
	cmd.Flags().BoolVar(&outputJSON, "json", false,
		"Emit a machine-readable JSON preview instead of human-oriented text")
	cmd.SetFlagErrorFunc(legacyIntegrationFlagError)
	_ = cmd.Flags().MarkDeprecated("sandbox", "use --docker=sandbox")
	_ = cmd.Flags().MarkDeprecated("ignore-docker", "use --docker=none")

	return cmd
}

func resolveExplainSession(target string, opts harnessSessionOpts) (sessionConfig, sessionMode, error) {
	switch target {
	case "claude", "shell", "exec", "opencode", "codex", "gemini":
	default:
		return sessionConfig{}, "", fmt.Errorf("unknown preview target %q (want claude, shell, exec, opencode, codex, or gemini)", target)
	}

	switch target {
	case "claude", "shell", "exec":
		prepared, err := resolvePreparedSession(target, opts, true)
		if err != nil {
			return sessionConfig{}, "", err
		}
		return prepared.Config, prepared.Mode, nil
	case "opencode", "codex", "gemini":
		prepared, err := resolvePreparedSession(target, opts, false)
		if err != nil {
			return sessionConfig{}, "", err
		}
		return prepared.Config, prepared.Mode, nil
	default:
		return sessionConfig{}, "", fmt.Errorf("unknown preview target %q", target)
	}
}
