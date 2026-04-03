package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	var target string
	var project string
	var readDirs []string
	var writeDirs []string
	var integrationNames []string
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	var dockerModeValue string

	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Preview the session contract without launching an agent",
		Long: `Show what Hazmat would do for a session without launching the agent.

This prints the same session contract and mode explanation that a real
launch would show, but stops before snapshots, sandbox setup, or process
execution.

Examples:
  hazmat explain
  hazmat explain -C ~/workspace/my-project --integration node
  hazmat explain --for shell --docker=sandbox -C ~/workspace/docker-app
  hazmat explain --for opencode --docker=none -C ~/workspace/repo`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, mode, err := resolveExplainSession(target, harnessSessionOpts{
				project:            project,
				readDirs:           readDirs,
				writeDirs:          writeDirs,
				integrations:       integrationNames,
				noBackup:           noBackup,
				useSandbox:         useSandbox,
				allowDocker:        allowDocker,
				dockerMode:         dockerModeValue,
				dockerModeExplicit: cmd.Flags().Changed("docker"),
			})
			if err != nil {
				return err
			}

			printSessionContract(cfg, mode, noBackup)
			fmt.Fprint(cmd.ErrOrStderr(), renderIntegrationDetails(cfg.IntegrationDetails))
			return nil
		},
	}

	cmd.Flags().StringVar(&target, "for", "claude",
		"Preview target (claude, shell, exec, opencode, codex)")
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVarP(&writeDirs, "write", "W", nil,
		"Read-write directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&integrationNames, "integration", nil,
		"Activate a session integration (repeatable, e.g. --integration go)")
	cmd.Flags().StringArrayVar(&integrationNames, "pack", nil,
		"Legacy alias for --integration")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Preview without a pre-session snapshot")
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeAuto),
		"Docker routing: auto, none, or sandbox")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Preview Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Preview native containment even if Docker markers are present")
	_ = cmd.Flags().MarkDeprecated("pack", "use --integration")
	_ = cmd.Flags().MarkHidden("pack")
	_ = cmd.Flags().MarkDeprecated("sandbox", "use --docker=sandbox")
	_ = cmd.Flags().MarkDeprecated("ignore-docker", "use --docker=none")

	return cmd
}

func resolveExplainSession(target string, opts harnessSessionOpts) (sessionConfig, sessionMode, error) {
	switch target {
	case "claude", "shell", "exec", "opencode", "codex":
	default:
		return sessionConfig{}, "", fmt.Errorf("unknown preview target %q (want claude, shell, exec, opencode, or codex)", target)
	}

	switch target {
	case "claude", "shell", "exec":
		prepared, err := resolvePreparedSession(target, opts, true)
		if err != nil {
			return sessionConfig{}, "", err
		}
		return prepared.Config, prepared.Mode, nil
	case "opencode", "codex":
		prepared, err := resolvePreparedSession(target, opts, false)
		if err != nil {
			return sessionConfig{}, "", err
		}
		return prepared.Config, prepared.Mode, nil
	default:
		return sessionConfig{}, "", fmt.Errorf("unknown preview target %q", target)
	}
}
