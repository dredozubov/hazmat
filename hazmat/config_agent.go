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

	// ── Collect all inputs first (no sudo needed) ──────────────────────────

	// ── 1. API key ──────────────────────────────────────────────────────────
	ui.Step("Anthropic API key")

	var currentKey string
	if data, err := os.ReadFile(agentHome + "/.zshrc"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "ANTHROPIC_API_KEY") {
				currentKey = strings.TrimSpace(line)
			}
		}
	}

	hostKey := os.Getenv("ANTHROPIC_API_KEY")
	var newAPIKey string // empty = no change

	if currentKey != "" {
		cDim.Printf("  Current: %s\n", maskKey(currentKey))
		fmt.Print("  New API key (Enter to keep, or paste new): ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		newAPIKey = strings.TrimSpace(string(apiKey))
		if newAPIKey == "" {
			ui.SkipDone("API key kept")
		}
	} else if hostKey != "" {
		masked := hostKey
		if len(masked) > 15 {
			masked = masked[:11] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("  Found ANTHROPIC_API_KEY in your environment: %s\n", masked)
		if ui.Ask("Copy this key to the agent user?") {
			newAPIKey = hostKey
		} else {
			fmt.Println("  Set it later with 'hazmat config agent' or type /login inside 'hazmat claude'.")
			ui.SkipDone("API key skipped")
		}
	} else {
		fmt.Println("    1) Paste an API key now (sk-ant-...)")
		fmt.Println("    2) Press Enter to skip — run 'hazmat claude' and type /login")
		fmt.Println()
		fmt.Print("  API key: ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		newAPIKey = strings.TrimSpace(string(apiKey))
		if newAPIKey == "" {
			fmt.Println("  Run 'hazmat claude' and type /login to authenticate via browser.")
			ui.SkipDone("API key skipped — use /login inside Claude")
		}
	}

	// ── 2. Git identity ─────────────────────────────────────────────────────
	ui.Step("Git identity")

	agentName := gitConfigValue(agentHome+"/.gitconfig", "name")
	agentEmail := gitConfigValue(agentHome+"/.gitconfig", "email")
	hostName, _ := execOutput("git", "config", "--global", "user.name")
	hostName = strings.TrimSpace(hostName)
	hostEmail, _ := execOutput("git", "config", "--global", "user.email")
	hostEmail = strings.TrimSpace(hostEmail)

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
		source := "from host git config"
		if agentName != "" {
			source = "current"
		}
		fmt.Printf("  Name  [%s] (%s, Enter to accept): ", defaultName, source)
		gitName, _ = reader.ReadString('\n')
		gitName = strings.TrimSpace(gitName)
		if gitName == "" {
			gitName = defaultName
		}

		source = "from host git config"
		if agentEmail != "" {
			source = "current"
		}
		fmt.Printf("  Email [%s] (%s, Enter to accept): ", defaultEmail, source)
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

	// ── 3. Git credential helper ────────────────────────────────────────────
	currentHelper := gitConfigValue(agentHome+"/.gitconfig", "helper")
	needHelper := currentHelper == ""

	// ── Apply all writes in one sudo invocation ─────────────────────────────
	// Build a single shell script with all changes, run once as agent user.
	// This way the user sees one sudo prompt with a clear explanation.

	var scriptParts []string

	if newAPIKey != "" {
		scriptParts = append(scriptParts,
			`sed -i '' '/^export ANTHROPIC_API_KEY=/d' ~/.zshrc 2>/dev/null`,
			fmt.Sprintf(`echo 'export ANTHROPIC_API_KEY="%s"' >> ~/.zshrc`, newAPIKey))
	}
	if gitName != "" {
		scriptParts = append(scriptParts, fmt.Sprintf(`git config --global user.name %q`, gitName))
	}
	if gitEmail != "" {
		scriptParts = append(scriptParts, fmt.Sprintf(`git config --global user.email %q`, gitEmail))
	}
	if needHelper {
		helper := "store --file " + agentHome + "/.config/git/credentials"
		scriptParts = append(scriptParts,
			`mkdir -p ~/.config/git`,
			fmt.Sprintf(`git config --global credential.helper %q`, helper))
	}

	if len(scriptParts) > 0 {
		fmt.Println()
		cDim.Println("  Writing to agent home requires sudo (your password, not root).")
		script := strings.Join(scriptParts, " && ")
		if _, err := sudoOutput("sudo", "-u", agentUser, "-i", "bash", "-c", script); err != nil {
			return fmt.Errorf("apply agent config: %w", err)
		}

		if newAPIKey != "" {
			ui.Ok("API key set")
		}
		if gitName != "" || gitEmail != "" {
			ui.Ok(fmt.Sprintf("Git identity: %s <%s>", gitName, gitEmail))
		}
		if needHelper {
			ui.Ok("Git credential helper configured")
		}
	} else {
		if gitName == "" && gitEmail == "" {
			ui.WarnMsg("Skipped — run 'hazmat config agent' later to set")
		}
	}

	if standalone {
		fmt.Println()
		fmt.Println("  Next: hazmat claude")
		fmt.Println()
	}
	return nil
}

// gitConfigValue reads a value from a git config file by searching for
// the key name. Simple parser — good enough for user.name, user.email,
// credential.helper.
func gitConfigValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" = ") {
			return strings.TrimPrefix(line, key+" = ")
		}
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
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
