package cli

import "github.com/spf13/cobra"

type PersistentFlags struct {
	Verbose *bool
	DryRun  *bool
	YesAll  *bool
}

type RootConfig struct {
	Version string
	Flags   PersistentFlags

	DefaultRun func(*cobra.Command, []string) error
	Completion func(*cobra.Command) *cobra.Command
	AddDebug   func(*cobra.Command)

	Setup     []*cobra.Command
	Run       []*cobra.Command
	Snapshots []*cobra.Command
	Workspace []*cobra.Command
	Hidden    []*cobra.Command
}

func NewRootCommand(config RootConfig) *cobra.Command {
	root := &cobra.Command{
		Use:           "hazmat",
		Short:         "Hazmat — AI agent containment for macOS",
		Version:       config.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          config.DefaultRun,
	}

	if config.Flags.Verbose != nil {
		root.PersistentFlags().BoolVarP(config.Flags.Verbose, "verbose", "v", false,
			"Print each command before executing")
	}
	if config.Flags.DryRun != nil {
		root.PersistentFlags().BoolVarP(config.Flags.DryRun, "dry-run", "n", false,
			"Print all commands without executing (implies --verbose)")
	}
	if config.Flags.YesAll != nil {
		root.PersistentFlags().BoolVarP(config.Flags.YesAll, "yes", "y", false,
			"Answer yes to all prompts (for non-interactive / scripted use)")
	}

	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup:"},
		&cobra.Group{ID: "run", Title: "Run agents:"},
		&cobra.Group{ID: "snap", Title: "Snapshots:"},
		&cobra.Group{ID: "ws", Title: "Workspace:"},
	)
	root.AddCommand(grouped("setup", config.Setup)...)
	root.AddCommand(grouped("run", config.Run)...)
	root.AddCommand(grouped("snap", config.Snapshots)...)
	root.AddCommand(grouped("ws", config.Workspace)...)
	root.AddCommand(config.Hidden...)

	if config.Completion != nil {
		root.AddCommand(config.Completion(root))
	}
	if config.AddDebug != nil {
		config.AddDebug(root)
	}
	root.SetHelpCommandGroupID("ws")
	return root
}

func grouped(groupID string, commands []*cobra.Command) []*cobra.Command {
	out := make([]*cobra.Command, 0, len(commands))
	for _, command := range commands {
		if command == nil {
			continue
		}
		command.GroupID = groupID
		out = append(out, command)
	}
	return out
}
