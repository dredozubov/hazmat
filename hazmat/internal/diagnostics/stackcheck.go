package diagnostics

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

const (
	stackcheckModeDetect   = "detect"
	stackcheckModeContract = "contract"
	stackcheckModeSmoke    = "smoke"
)

type StackcheckOptions struct {
	ManifestPath  string
	WorkspaceRoot string
	Track         string
	Wave          int
	IDs           []string
	UpstreamHead  bool
}

type StackcheckRunner func(mode string, opts StackcheckOptions, stdout io.Writer, stderr io.Writer) error

type StackcheckCommandConfig struct {
	DefaultManifestPath  func() string
	DefaultWorkspaceRoot func() string
	DefaultTrack         string
	Run                  StackcheckRunner
}

func NewStackCheckCommand(cfg StackcheckCommandConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stackcheck",
		Short:  "Internal repo-matrix validation runner",
		Hidden: true,
	}
	cmd.AddCommand(
		newStackCheckRunCommand(stackcheckModeDetect, cfg),
		newStackCheckRunCommand(stackcheckModeContract, cfg),
		newStackCheckRunCommand(stackcheckModeSmoke, cfg),
	)
	return cmd
}

func newStackCheckRunCommand(mode string, cfg StackcheckCommandConfig) *cobra.Command {
	opts := StackcheckOptions{
		ManifestPath:  defaultString(cfg.DefaultManifestPath),
		WorkspaceRoot: defaultString(cfg.DefaultWorkspaceRoot),
		Track:         cfg.DefaultTrack,
	}
	cmd := &cobra.Command{
		Use:    mode,
		Short:  fmt.Sprintf("Run stack-matrix %s checks", mode),
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.ManifestPath == "" {
				opts.ManifestPath = defaultString(cfg.DefaultManifestPath)
			}
			if opts.WorkspaceRoot == "" {
				opts.WorkspaceRoot = defaultString(cfg.DefaultWorkspaceRoot)
			}
			if cfg.Run == nil {
				return fmt.Errorf("stackcheck runner is not configured")
			}
			return cfg.Run(mode, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&opts.ManifestPath, "manifest", opts.ManifestPath,
		"Path to the repo corpus manifest")
	cmd.Flags().StringVar(&opts.WorkspaceRoot, "workspace-root", opts.WorkspaceRoot,
		"Directory where pinned repo checkouts are stored")
	cmd.Flags().StringVar(&opts.Track, "track", cfg.DefaultTrack,
		`Repo track to run: "required", "informational", or "all"`)
	cmd.Flags().IntVar(&opts.Wave, "wave", 0,
		"Only run repos from a specific wave (0 means all waves)")
	cmd.Flags().StringArrayVar(&opts.IDs, "id", nil,
		"Run only the named repo id(s) from the manifest")
	cmd.Flags().BoolVar(&opts.UpstreamHead, "upstream-head", false,
		"Resolve each repo to its current upstream HEAD instead of the pinned manifest SHA")
	return cmd
}

func defaultString(fn func() string) string {
	if fn == nil {
		return ""
	}
	return fn()
}
