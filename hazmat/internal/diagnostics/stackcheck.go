package diagnostics

import (
	"encoding/json"
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
type StackcheckInitCheck func() error

type StackcheckCommandConfig struct {
	DefaultManifestPath  func() string
	DefaultWorkspaceRoot func() string
	DefaultTrack         string
	RequireInitialized   StackcheckInitCheck
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
	defaultTrack := cfg.DefaultTrack
	if defaultTrack == "" {
		defaultTrack = stackMatrixTrackRequired
	}
	opts := StackcheckOptions{
		ManifestPath:  defaultString(cfg.DefaultManifestPath, defaultStackMatrixManifestPath),
		WorkspaceRoot: defaultString(cfg.DefaultWorkspaceRoot, defaultStackcheckWorkspaceRoot),
		Track:         defaultTrack,
	}
	cmd := &cobra.Command{
		Use:    mode,
		Short:  fmt.Sprintf("Run stack-matrix %s checks", mode),
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.ManifestPath == "" {
				opts.ManifestPath = defaultString(cfg.DefaultManifestPath, defaultStackMatrixManifestPath)
			}
			if opts.WorkspaceRoot == "" {
				opts.WorkspaceRoot = defaultString(cfg.DefaultWorkspaceRoot, defaultStackcheckWorkspaceRoot)
			}
			run := cfg.Run
			if run == nil {
				run = func(mode string, opts StackcheckOptions, stdout io.Writer, stderr io.Writer) error {
					return runStackCheckForCommand(mode, opts, stdout, stderr, cfg.RequireInitialized)
				}
			}
			return run(mode, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&opts.ManifestPath, "manifest", opts.ManifestPath,
		"Path to the repo corpus manifest")
	cmd.Flags().StringVar(&opts.WorkspaceRoot, "workspace-root", opts.WorkspaceRoot,
		"Directory where pinned repo checkouts are stored")
	cmd.Flags().StringVar(&opts.Track, "track", defaultTrack,
		`Repo track to run: "required", "informational", or "all"`)
	cmd.Flags().IntVar(&opts.Wave, "wave", 0,
		"Only run repos from a specific wave (0 means all waves)")
	cmd.Flags().StringArrayVar(&opts.IDs, "id", nil,
		"Run only the named repo id(s) from the manifest")
	cmd.Flags().BoolVar(&opts.UpstreamHead, "upstream-head", false,
		"Resolve each repo to its current upstream HEAD instead of the pinned manifest SHA")
	return cmd
}

func runStackCheckForCommand(mode string, opts StackcheckOptions, stdout io.Writer, stderr io.Writer, requireInitialized StackcheckInitCheck) error {
	results, err := runStackCheck(mode, opts, requireInitialized)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(results); err != nil {
		return err
	}

	fmt.Fprint(stderr, summarizeStackcheckResults(results))
	if failed := stackcheckFailureCount(results); failed > 0 {
		return fmt.Errorf("%d stackcheck repo(s) failed", failed)
	}
	return nil
}

func defaultString(fn func() string, fallback func() string) string {
	if fn == nil {
		if fallback == nil {
			return ""
		}
		return fallback()
	}
	return fn()
}
