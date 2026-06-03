package hookruntime

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type GitWrapperRunner func(projectDir string, args []string) error
type GitDispatchRunner func(projectDir, hookName string, args []string) error
type GitFallbackRefusal func(projectDir, hookName string) error

func NewGitHookWrapperCommand(run GitWrapperRunner) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:    "_git-hook-wrapper",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, args []string) {
			exitOnError(run(projectDir, args))
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func NewGitHookDispatchCommand(run GitDispatchRunner) *cobra.Command {
	var projectDir string
	var hookName string
	cmd := &cobra.Command{
		Use:    "_git-hook-dispatch",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, args []string) {
			exitOnError(run(projectDir, hookName, args))
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	cmd.Flags().StringVar(&hookName, "hook", "", "Hook type")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("hook")
	return cmd
}

func NewGitHookFallbackCommand(refusal GitFallbackRefusal) *cobra.Command {
	var projectDir string
	var hookName string
	cmd := &cobra.Command{
		Use:    "_git-hook-fallback",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, _ []string) {
			exitOnError(refusal(projectDir, hookName))
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	cmd.Flags().StringVar(&hookName, "hook", "", "Hook type")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("hook")
	return cmd
}

func exitOnError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
