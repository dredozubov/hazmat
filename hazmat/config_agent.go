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

func newConfigAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Configure API key and git identity for the agent user",
		Long: `Configure the agent user's credentials.

Sets up:
  1. Anthropic API key (copies from host env, or paste, or skip for /login)
  2. Git identity (name + email, pre-filled from host git config)
  3. Git credential helper (HTTPS with stored credentials)

Idempotent: existing values are shown and can be kept or overridden.

Examples:
  hazmat config agent`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigAgent(nil)
		},
	}
	return cmd
}

// runConfigAgent configures agent credentials. If ui is non-nil, uses its
// step counter (chained from init). If nil, creates a standalone UI.
func runConfigAgent(ui *UI) error {
	standalone := ui == nil
	if standalone {
		ui = &UI{}
	}
	if !ui.IsInteractive() {
		return fmt.Errorf("config agent requires an interactive terminal")
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

	// Check if the host user already has an API key we can copy.
	hostKey := os.Getenv("ANTHROPIC_API_KEY")

	if currentKey != "" {
		cDim.Printf("  Current: %s\n", maskKey(currentKey))
		fmt.Print("  New API key (Enter to keep, or paste new): ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()

		key := strings.TrimSpace(string(apiKey))
		if key != "" {
			if err := setAgentAPIKey(key); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			ui.Ok("API key updated")
		} else {
			ui.SkipDone("API key kept")
		}
	} else if hostKey != "" {
		// Offer to copy the host user's key.
		masked := hostKey
		if len(masked) > 15 {
			masked = masked[:11] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("  Found ANTHROPIC_API_KEY in your environment: %s\n", masked)
		if ui.Ask("Copy this key to the agent user?") {
			if err := setAgentAPIKey(hostKey); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			ui.Ok("API key copied from host environment")
		} else {
			fmt.Println("  You can set it later with 'hazmat config agent' or 'claude /login' inside the sandbox.")
			ui.SkipDone("API key skipped")
		}
	} else {
		fmt.Println("  Three options:")
		fmt.Println("    1) Paste an API key now (sk-ant-...)")
		fmt.Println("    2) Press Enter to skip — use 'claude /login' inside the sandbox later")
		fmt.Println()
		fmt.Print("  API key: ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()

		key := strings.TrimSpace(string(apiKey))
		if key != "" {
			if err := setAgentAPIKey(key); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			ui.Ok("API key set")
		} else {
			fmt.Println("  Run 'hazmat shell' then 'claude /login' to authenticate via browser.")
			ui.SkipDone("API key skipped — use /login later")
		}
	}

	// ── Git identity ────────────────────────────────────────────────────────
	ui.Step("Git identity")

	// Read the agent user's current git config.
	agentName, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.name 2>/dev/null")
	agentName = strings.TrimSpace(agentName)
	agentEmail, _ := sudoOutput("sudo", "-u", agentUser, "-i",
		"bash", "-c", "git config --global user.email 2>/dev/null")
	agentEmail = strings.TrimSpace(agentEmail)

	// Read the host user's git config as defaults.
	hostName, _ := execOutput("git", "config", "--global", "user.name")
	hostName = strings.TrimSpace(hostName)
	hostEmail, _ := execOutput("git", "config", "--global", "user.email")
	hostEmail = strings.TrimSpace(hostEmail)

	// Pick the best default: agent's existing config > host user's config.
	defaultName := agentName
	if defaultName == "" {
		defaultName = hostName
	}
	defaultEmail := agentEmail
	if defaultEmail == "" {
		defaultEmail = hostEmail
	}

	var gitName, gitEmail string
	if defaultName != "" || defaultEmail != "" {
		// We have defaults — offer to use them.
		source := "host git config"
		if agentName != "" {
			source = "agent"
		}
		fmt.Printf("  Name  [%s] (%s): ", defaultName, source)
		gitName, _ = reader.ReadString('\n')
		gitName = strings.TrimSpace(gitName)
		if gitName == "" {
			gitName = defaultName
		}

		source = "host git config"
		if agentEmail != "" {
			source = "agent"
		}
		fmt.Printf("  Email [%s] (%s): ", defaultEmail, source)
		gitEmail, _ = reader.ReadString('\n')
		gitEmail = strings.TrimSpace(gitEmail)
		if gitEmail == "" {
			gitEmail = defaultEmail
		}
	} else {
		fmt.Print("  Name: ")
		gitName, _ = reader.ReadString('\n')
		gitName = strings.TrimSpace(gitName)
		fmt.Print("  Email: ")
		gitEmail, _ = reader.ReadString('\n')
		gitEmail = strings.TrimSpace(gitEmail)
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
		ui.WarnMsg("Skipped — run 'hazmat config agent' later to set")
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

// setAgentAPIKey writes the API key to the agent user's .zshrc.
func setAgentAPIKey(key string) error {
	script := fmt.Sprintf(
		`sed -i '' '/^export ANTHROPIC_API_KEY=/d' ~/.zshrc 2>/dev/null; echo 'export ANTHROPIC_API_KEY="%s"' >> ~/.zshrc`,
		key)
	_, err := sudoOutput("sudo", "-u", agentUser, "-i", "bash", "-c", script)
	return err
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
