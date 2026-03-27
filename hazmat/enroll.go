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
  hazmat enroll                   # Interactive prompts
  hazmat enroll --api-key sk-ant-...  # Non-interactive API key`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runEnroll()
		},
	}
	return cmd
}

func runEnroll() error {
	ui := &UI{}
	if !ui.IsInteractive() {
		return fmt.Errorf("enroll requires an interactive terminal")
	}

	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user %q not found — run 'hazmat setup' first", agentUser)
	}

	fmt.Println()
	cBold.Println("  ┌──────────────────────────────────────────────┐")
	cBold.Println("  │  Agent Credential Setup                      │")
	cBold.Println("  └──────────────────────────────────────────────┘")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// ── Step 1: API key ─────────────────────────────────────────────────────
	cBold.Println("  1. Anthropic API Key")
	fmt.Println()

	currentKey, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "grep ANTHROPIC_API_KEY ~/.zshrc 2>/dev/null | tail -1")
	currentKey = strings.TrimSpace(currentKey)

	if currentKey != "" {
		cDim.Printf("     Current: %s\n", maskKey(currentKey))
		fmt.Print("     New API key (Enter to keep current, or paste new): ")
	} else {
		fmt.Print("     API key (sk-ant-...): ")
	}

	apiKey, _ := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()

	if len(apiKey) > 0 {
		key := strings.TrimSpace(string(apiKey))
		if key != "" {
			// Write the export line, replacing any existing one
			script := fmt.Sprintf(
				`sed -i '' '/^export ANTHROPIC_API_KEY=/d' ~/.zshrc 2>/dev/null; echo 'export ANTHROPIC_API_KEY="%s"' >> ~/.zshrc`,
				key)
			if _, err := sudoOutput("sudo", "-u", agentUser, "-i", "bash", "-c", script); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			cGreen.Println("     ✓ API key set")
		}
	} else if currentKey != "" {
		cDim.Println("     (kept existing)")
	} else {
		cYellow.Println("     ! Skipped — set later with: hazmat enroll")
	}
	fmt.Println()

	// ── Step 2: Git identity ────────────────────────────────────────────────
	cBold.Println("  2. Git Identity")
	fmt.Println()

	currentName, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.name 2>/dev/null")
	currentName = strings.TrimSpace(currentName)
	currentEmail, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.email 2>/dev/null")
	currentEmail = strings.TrimSpace(currentEmail)

	if currentName != "" {
		fmt.Printf("     Git name [%s]: ", currentName)
	} else {
		fmt.Print("     Git name: ")
	}
	gitName, _ := reader.ReadString('\n')
	gitName = strings.TrimSpace(gitName)
	if gitName == "" {
		gitName = currentName
	}

	if currentEmail != "" {
		fmt.Printf("     Git email [%s]: ", currentEmail)
	} else {
		fmt.Print("     Git email: ")
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
		cGreen.Printf("     ✓ Git identity: %s <%s>\n", gitName, gitEmail)
	} else {
		cYellow.Println("     ! Skipped — set later with: hazmat enroll")
	}
	fmt.Println()

	// ── Step 3: Git credential helper ───────────────────────────────────────
	cBold.Println("  3. Git Credential Helper")
	fmt.Println()
	fmt.Println("     SSH is blocked by the seatbelt profile. Use HTTPS with a")
	fmt.Println("     GitHub personal access token (scope: repo) instead.")
	fmt.Println()

	currentHelper, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global credential.helper 2>/dev/null")
	currentHelper = strings.TrimSpace(currentHelper)

	if currentHelper != "" {
		cGreen.Printf("     ✓ Already configured: %s\n", currentHelper)
	} else {
		fmt.Print("     Configure git credential store? [Y/n]: ")
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans == "" || ans == "y" || ans == "yes" {
			helper := "store --file " + agentHome + "/.config/git/credentials"
			if _, err := sudoOutput("sudo", "-u", agentUser, "-i",
				"bash", "-c", fmt.Sprintf("mkdir -p ~/.config/git && git config --global credential.helper %q", helper)); err != nil {
				return fmt.Errorf("set credential helper: %w", err)
			}
			cGreen.Println("     ✓ Credential helper configured")
			fmt.Println("       Git will prompt for your PAT on first push.")
		} else {
			cDim.Println("     (skipped)")
		}
	}

	fmt.Println()
	cGreen.Println("  ━━━ Enrollment complete ━━━")
	fmt.Println()
	fmt.Println("  Next: hazmat test        (verify everything)")
	fmt.Println("        hazmat claude      (start a session)")
	fmt.Println()
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
