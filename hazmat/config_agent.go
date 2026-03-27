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

	// ── Apply writes ────────────────────────────────────────────────────────
	// Read each file, modify in memory, write to temp, sudo install with
	// correct ownership. No login shell needed.

	needsWrite := newAPIKey != "" || gitName != "" || gitEmail != "" || needHelper

	if needsWrite {
		// .zshrc: update API key
		if newAPIKey != "" {
			if err := updateAgentFile(
				agentHome+"/.zshrc",
				func(content string) string {
					var lines []string
					for _, line := range strings.Split(content, "\n") {
						if !strings.HasPrefix(line, "export ANTHROPIC_API_KEY=") {
							lines = append(lines, line)
						}
					}
					lines = append(lines, fmt.Sprintf(`export ANTHROPIC_API_KEY="%s"`, newAPIKey))
					return strings.Join(lines, "\n")
				},
				0o600, // API key should not be world-readable
			); err != nil {
				return fmt.Errorf("set API key: %w", err)
			}
			ui.Ok("API key set")
		}

		// .gitconfig: update name, email, credential helper
		if gitName != "" || gitEmail != "" || needHelper {
			if err := updateAgentFile(
				agentHome+"/.gitconfig",
				func(content string) string {
					cfg := parseINI(content)
					if gitName != "" {
						cfg = setINIValue(cfg, "user", "name", gitName)
					}
					if gitEmail != "" {
						cfg = setINIValue(cfg, "user", "email", gitEmail)
					}
					if needHelper {
						helper := "store --file " + agentHome + "/.config/git/credentials"
						cfg = setINIValue(cfg, "credential", "helper", helper)
					}
					return renderINI(cfg)
				},
				0o644,
			); err != nil {
				return fmt.Errorf("set git config: %w", err)
			}

			// Ensure git credentials directory exists
			credDir := agentHome + "/.config/git"
			if _, err := os.Stat(credDir); os.IsNotExist(err) {
				sudo("mkdir", "-p", credDir)
				sudo("chown", agentUser, credDir)
			}

			if gitName != "" || gitEmail != "" {
				ui.Ok(fmt.Sprintf("Git identity: %s <%s>", gitName, gitEmail))
			}
			if needHelper {
				ui.Ok("Git credential helper configured")
			}
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

// updateAgentFile reads a file from agent home, applies a transform,
// writes to a temp file, then sudo installs it back with agent ownership.
func updateAgentFile(path string, transform func(string) string, mode os.FileMode) error {
	// Read current content (host can read via ACLs).
	current, _ := os.ReadFile(path)
	updated := transform(string(current))

	// Write to temp file.
	tmp, err := os.CreateTemp("", "hazmat-agent-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(updated); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// Install with correct ownership and mode.
	return sudo("install", "-o", agentUser, "-g", "staff",
		"-m", fmt.Sprintf("%04o", mode), tmpPath, path)
}

// ── Minimal INI parser for .gitconfig ───────────────────────────────────────

type iniSection struct {
	name  string
	lines []string // raw lines including key = value
}

func parseINI(content string) []iniSection {
	var sections []iniSection
	current := iniSection{name: ""} // preamble before any section

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sections = append(sections, current)
			current = iniSection{name: trimmed[1 : len(trimmed)-1]}
			continue
		}
		current.lines = append(current.lines, line)
	}
	sections = append(sections, current)
	return sections
}

func setINIValue(sections []iniSection, section, key, value string) []iniSection {
	// Find existing section and update or add the key.
	for i, s := range sections {
		if s.name == section {
			found := false
			for j, line := range s.lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, key+" =") || strings.HasPrefix(trimmed, key+"=") {
					sections[i].lines[j] = "\t" + key + " = " + value
					found = true
					break
				}
			}
			if !found {
				sections[i].lines = append(sections[i].lines, "\t"+key+" = "+value)
			}
			return sections
		}
	}
	// Section doesn't exist — create it.
	sections = append(sections, iniSection{
		name:  section,
		lines: []string{"\t" + key + " = " + value},
	})
	return sections
}

func renderINI(sections []iniSection) string {
	var b strings.Builder
	for _, s := range sections {
		if s.name != "" {
			fmt.Fprintf(&b, "[%s]\n", s.name)
		}
		for _, line := range s.lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
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
