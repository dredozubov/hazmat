package hazmat

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	var target string
	var flags sessionCommandFlags
	var backendValue string
	var imageValue string
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
  hazmat explain --github -C ~/workspace/my-project
  hazmat explain --for shell --docker=sandbox -C ~/workspace/docker-app
  hazmat explain --for opencode --docker=none -C ~/workspace/repo
  hazmat explain --for codex --network none -C ~/workspace/repo
  hazmat explain --for gemini --integration go -C ~/workspace/my-go-project
  hazmat explain --for hermes -C ~/workspace/repo
  hazmat explain --for qwen -C ~/workspace/repo
  hazmat explain --for cursor-agent -C ~/workspace/repo
  hazmat explain --backend=apple-container --image ghcr.io/example/hazmat-codex:latest --for codex`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessionOpts := flags.explainSessionOpts(cmd)

			switch backendValue {
			case "":
			case explainBackendAppleContainer:
				return runExplainAppleContainer(cmd, target, imageValue, sessionOpts, outputJSON)
			default:
				return fmt.Errorf("unknown preview backend %q (want apple-container)", backendValue)
			}
			if imageValue != "" {
				return fmt.Errorf("--image requires --backend=apple-container")
			}

			cfg, mode, err := resolveExplainSession(target, sessionOpts)
			if err != nil {
				return err
			}

			if outputJSON {
				preview := buildExplainJSON(target, cfg, mode, sessionOpts.noBackup)
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				return enc.Encode(preview)
			}

			printSessionContract(cfg, mode, sessionOpts.noBackup)
			fmt.Fprint(cmd.ErrOrStderr(), renderRepoSetupDetails(cfg.RepoSetup))
			printSessionMutationDetails(cfg.PlannedHostMutations)
			fmt.Fprint(cmd.ErrOrStderr(), renderIntegrationDetails(cfg.IntegrationDetails))
			return nil
		},
	}

	cmd.Flags().StringVar(&target, "for", "claude",
		"Preview target (claude, shell, exec, opencode, codex, gemini, hermes, qwen, cursor-agent)")
	bindExplainSessionFlags(cmd, &flags)
	cmd.Flags().StringVar(&backendValue, "backend", "",
		"Preview an alternate plan-only backend (apple-container)")
	cmd.Flags().StringVar(&imageValue, "image", "",
		"Explicit Linux image for --backend=apple-container previews")
	cmd.Flags().BoolVar(&outputJSON, "json", false,
		"Emit a machine-readable JSON preview instead of human-oriented text")

	return cmd
}

func resolveExplainSession(target string, opts harnessSessionOpts) (sessionConfig, sessionMode, error) {
	switch target {
	case "claude", "shell", "exec", "opencode", "codex", "gemini", "hermes", "qwen", "cursor-agent":
	default:
		return sessionConfig{}, "", fmt.Errorf("unknown preview target %q (want claude, shell, exec, opencode, codex, gemini, hermes, qwen, or cursor-agent)", target)
	}

	prepared, err := resolvePreparedSession(target, opts, true)
	if err != nil {
		return sessionConfig{}, "", err
	}
	return prepared.Config, prepared.Mode, nil
}
