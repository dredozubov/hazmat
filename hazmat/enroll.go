package main

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newEnrollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Set API key and git credentials for the agent user",
		Long: `Configure the agent user's credentials in one step. This replaces the
manual process of switching to the agent shell and running multiple commands.

Sets up:
  1. Anthropic API key (or Claude.ai login token)
  2. Git identity (name + email)
  3. Git credential helper (HTTPS with stored credentials)

This command is idempotent: values already set are shown and can be kept or
overridden. Run it again any time to update credentials.

Examples:
  hazmat enroll                   # Interactive prompts`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runEnroll(nil)
		},
	}
	return cmd
}

// runEnroll configures agent credentials. If ui is non-nil, uses its step
// counter (chained from setup). If nil, creates a standalone UI.
func runEnroll(ui *UI) error {
	standalone := ui == nil
	if standalone {
		ui = &UI{}
	}
	if !ui.IsInteractive() {
		return fmt.Errorf("enroll requires an interactive terminal")
	}

	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user %q not found — run 'hazmat init' first", agentUser)
	}

	reader := bufio.NewReader(os.Stdin)

	// ── API key ─────────────────────────────────────────────────────────────
	ui.Step("Anthropic API key")

	currentKey, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "grep ANTHROPIC_API_KEY ~/.zshrc 2>/dev/null | tail -1")
	currentKey = strings.TrimSpace(currentKey)

	if currentKey != "" {
		cDim.Printf("  Current: %s\n", maskKey(currentKey))
		fmt.Print("  New API key (Enter to keep, or paste new): ")
	} else {
		fmt.Print("  API key (sk-ant-...): ")
	}

	apiKey, _ := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()

	if len(apiKey) > 0 {
		key := strings.TrimSpace(string(apiKey))
		if key != "" {
			script := fmt.Sprintf(
				`sed -i '' '/^export ANTHROPIC_API_KEY=/d' ~/.zshrc 2>/dev/null; echo 'export ANTHROPIC_API_KEY="%s"' >> ~/.zshrc`,
				key)
			if _, err := sudoOutput("sudo", "-u", agentUser, "-i", "bash", "-c", script); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			ui.Ok("API key set")
		}
	} else if currentKey != "" {
		ui.SkipDone("API key kept")
	} else {
		ui.WarnMsg("Skipped — run 'hazmat enroll' later to set")
	}

	// ── Git identity ────────────────────────────────────────────────────────
	ui.Step("Git identity")

	currentName, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.name 2>/dev/null")
	currentName = strings.TrimSpace(currentName)
	currentEmail, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.email 2>/dev/null")
	currentEmail = strings.TrimSpace(currentEmail)

	if currentName != "" {
		fmt.Printf("  Name [%s]: ", currentName)
	} else {
		fmt.Print("  Name: ")
	}
	gitName, _ := reader.ReadString('\n')
	gitName = strings.TrimSpace(gitName)
	if gitName == "" {
		gitName = currentName
	}

	if currentEmail != "" {
		fmt.Printf("  Email [%s]: ", currentEmail)
	} else {
		fmt.Print("  Email: ")
	}
	gitEmail, _ := reader.ReadString('\n')
	gitEmail = strings.TrimSpace(gitEmail)
	if gitEmail == "" {
		gitEmail = currentEmail
	}

	if gitName != "" {
		if _, err := sudoOutput("sudo", "-u", agentUser, "-i",
			"bash", "-c", fmt.Sprintf("git config --global user.name %q", gitName)); err != nil {
			return fmt.Errorf("set git name: %w", err)
		}
	}
	if gitEmail != "" {
		if _, err := sudoOutput("sudo", "-u", agentUser, "-i",
			"bash", "-c", fmt.Sprintf("git config --global user.email %q", gitEmail)); err != nil {
			return fmt.Errorf("set git email: %w", err)
		}
	}

	if gitName != "" || gitEmail != "" {
		ui.Ok(fmt.Sprintf("Git identity: %s <%s>", gitName, gitEmail))
	} else {
		ui.WarnMsg("Skipped — run 'hazmat enroll' later to set")
	}

	// ── Git credential helper ───────────────────────────────────────────────
	ui.Step("Git credential helper (SSH is blocked — use HTTPS)")

	currentHelper, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global credential.helper 2>/dev/null")
	currentHelper = strings.TrimSpace(currentHelper)

	if currentHelper != "" {
		ui.SkipDone(fmt.Sprintf("credential.helper = %s", currentHelper))
	} else {
		helper := "store --file " + agentHome + "/.config/git/credentials"
		if _, err := sudoOutput("sudo", "-u", agentUser, "-i",
			"bash", "-c", fmt.Sprintf("mkdir -p ~/.config/git && git config --global credential.helper %q", helper)); err != nil {
			return fmt.Errorf("set credential helper: %w", err)
		}
		ui.Ok("Credential helper configured (git will prompt for PAT on first push)")
	}

	if standalone {
		fmt.Println()
		fmt.Println("  Next: hazmat claude")
		fmt.Println()
	}
	return nil
}

// maskKey shows "export ANTHROPIC_API_KEY=sk-ant-...xxxx" with most of the
// key masked for display.
func maskKey(line string) string {
	if i := strings.Index(line, "sk-ant-"); i >= 0 {
		key := line[i:]
		key = strings.Trim(key, "\"' ")
		if len(key) > 15 {
			return key[:11] + "..." + key[len(key)-4:]
		}
		return key[:11] + "..."
	}
	return "(set)"
}
