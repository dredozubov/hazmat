package main

import (
	"fmt"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigGitHubCmd() *cobra.Command {
	var tokenFromEnv bool
	var clear bool
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Configure the explicit GitHub API session capability",
		Long: `Configure the host-owned GitHub API token used by explicit --github
session grants.

The token is stored in ~/.hazmat/secrets/github/token and is injected into
contained sessions as GH_TOKEN only when the launch command includes --github.
Repo recommendations and integrations cannot activate this capability.

Examples:
  GH_TOKEN=... hazmat config github --token-from-env
  hazmat config github
  hazmat config github --clear`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigGitHub(tokenFromEnv, clear)
		},
	}
	cmd.Flags().BoolVar(&tokenFromEnv, "token-from-env", false,
		"Read token from GH_TOKEN, falling back to GITHUB_TOKEN")
	cmd.Flags().BoolVar(&clear, "clear", false,
		"Remove the stored GitHub API token")
	return cmd
}

func runConfigGitHub(tokenFromEnv, clear bool) error {
	if tokenFromEnv && clear {
		return fmt.Errorf("--token-from-env and --clear cannot be used together")
	}
	if clear {
		if err := removeGitHubStoredToken(); err != nil {
			return err
		}
		fmt.Println()
		cGreen.Println("  GitHub API token cleared.")
		fmt.Println()
		return nil
	}

	var token string
	var source string
	if tokenFromEnv {
		source, token = githubTokenFromEnv()
		if token == "" {
			return fmt.Errorf("GH_TOKEN or GITHUB_TOKEN environment variable is not set")
		}
		fmt.Printf("  Found %s in your environment.\n", source)
	} else if term.IsTerminal(int(syscall.Stdin)) {
		fmt.Print("  GitHub token: ")
		secret, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return err
		}
		fmt.Println(" (set)")
		token = string(secret)
	} else {
		return fmt.Errorf("use --token-from-env or run interactively for GitHub token input")
	}

	if err := saveGitHubStoredToken(token); err != nil {
		return err
	}

	fmt.Println()
	cGreen.Println("  GitHub API capability configured.")
	fmt.Printf("    Credential: %s\n", filepath.Join("~", ".hazmat", "secrets", "github", "token"))
	cDim.Println("    Use: hazmat <command> --github")
	fmt.Println()
	return nil
}
