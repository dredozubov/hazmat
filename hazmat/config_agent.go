package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Configure API keys and git identity for the agent user",
		Long: `Configure the agent user's credentials.

Sets up:
  1. API keys for installed harnesses (Anthropic / OpenAI / Gemini —
     only prompts for harnesses that are actually installed)
     Stored in ~/.hazmat/secrets and injected only into matching sessions
  2. Git identity (name + email, pre-filled from host git config)
  3. Git credential helper (HTTPS with stored credentials)

Each prompt copies from your invoking-shell environment when the matching
env var is set, lets you paste a key, or accepts Enter to skip in favour
of a per-harness import or interactive sign-in path.

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

// harnessAPIKeySpec maps a managed harness to the env var its CLI reads for
// API-key auth, plus presentation hints.
type harnessAPIKeySpec struct {
	Harness      HarnessID
	EnvVar       string // env var name injected into matching sessions
	DisplayName  string // "Anthropic", "OpenAI", "Gemini"
	KeyPrefix    string // mask hint — known prefix that identifies a real key (e.g. "sk-ant-"); empty = no prefix-based mask
	SkipHint     string // shown when the user chooses not to set the key
	NotFoundHint string // shown when neither current nor host env var is set, before the paste prompt
}

// harnessAPIKeyPrompts is the table of API-key prompts that runConfigAgent
// iterates. A harness is only prompted if it's currently installed.
var harnessAPIKeyPrompts = []harnessAPIKeySpec{
	{
		Harness:      HarnessClaude,
		EnvVar:       "ANTHROPIC_API_KEY",
		DisplayName:  "Anthropic",
		KeyPrefix:    "sk-ant-",
		SkipHint:     "import host Claude basics or run 'hazmat claude' and type /login",
		NotFoundHint: "Paste an API key now (sk-ant-...) or press Enter to skip and use /login inside 'hazmat claude'.",
	},
	{
		Harness:      HarnessCodex,
		EnvVar:       "OPENAI_API_KEY",
		DisplayName:  "OpenAI",
		KeyPrefix:    "sk-",
		SkipHint:     "import host Codex basics or sign in inside 'hazmat codex' (option 2 — Device Code)",
		NotFoundHint: "Paste an API key now (sk-...) or press Enter to skip and sign in inside 'hazmat codex'.",
	},
	{
		Harness:      HarnessGemini,
		EnvVar:       "GEMINI_API_KEY",
		DisplayName:  "Gemini",
		KeyPrefix:    "",
		SkipHint:     "import host Gemini basics or sign in with Google inside 'hazmat gemini'",
		NotFoundHint: "Paste an API key now (from https://aistudio.google.com/apikey) or press Enter to skip and sign in inside 'hazmat gemini'.",
	},
}

// pendingAPIKeyUpdate captures a single env var to persist in Hazmat's
// host-owned secret store. Value == "" means "remove any legacy export".
type pendingAPIKeyUpdate struct {
	EnvVar string
	Value  string
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

	if _, err := requireAgentUser(); err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	// ── Collect all inputs first (no sudo needed) ──────────────────────────

	// ── 1. API keys (one prompt per installed harness) ─────────────────────
	var apiKeyUpdates []pendingAPIKeyUpdate
	promptedAny := false
	for _, spec := range harnessAPIKeyPrompts {
		if !isManagedHarnessInstalled(spec.Harness) {
			continue
		}
		promptedAny = true
		update, err := collectHarnessAPIKey(ui, spec)
		if err != nil {
			return err
		}
		if update != nil {
			apiKeyUpdates = append(apiKeyUpdates, *update)
		}
	}
	if !promptedAny {
		// No harnesses installed yet — preserve historical UX by showing the
		// claude prompt anyway so users discover the path before they bootstrap.
		update, err := collectHarnessAPIKey(ui, harnessAPIKeyPrompts[0])
		if err != nil {
			return err
		}
		if update != nil {
			apiKeyUpdates = append(apiKeyUpdates, *update)
		}
	}

	// ── 2. Git identity ─────────────────────────────────────────────────────
	ui.Step("Git identity")

	agentName := gitConfigValue(agentHome+"/.gitconfig", "name")
	agentEmail := gitConfigValue(agentHome+"/.gitconfig", "email")
	hostName, _ := hostGitOutput("config", "--global", "user.name")
	hostEmail, _ := hostGitOutput("config", "--global", "user.email")

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

	cleanupEnvVars := removableLegacyAPIKeyEnvVars(apiKeyUpdates)
	needsWrite := len(apiKeyUpdates) > 0 || gitName != "" || gitEmail != "" || needHelper || hasLegacyAPIKeyExports(cleanupEnvVars)

	if needsWrite {
		if len(apiKeyUpdates) > 0 {
			for _, upd := range apiKeyUpdates {
				if err := storeHostAPIKey(harnessAPIKeyPromptByEnvVar(upd.EnvVar), upd.Value); err != nil {
					return err
				}
				ui.Ok(fmt.Sprintf("%s stored in ~/.hazmat/secrets", upd.EnvVar))
			}
		}

		if hasLegacyAPIKeyExports(cleanupEnvVars) {
			if err := removeLegacyAPIKeyExports(cleanupEnvVars); err != nil {
				return fmt.Errorf("remove legacy API-key exports from %s: %w", agentZshrcPath, err)
			}
			ui.Ok(fmt.Sprintf("Legacy API-key exports removed from %s", agentZshrcPath))
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
						// credential-regression: allow legacy agent-side Git HTTPS store until sandboxing-6md1 brokers it.
						helper := "store --file " + agentHome + "/.config/git/credentials"
						cfg = setINIValue(cfg, "credential", "helper", helper)
					}
					return renderINI(cfg)
				},
				0o644,
			); err != nil {
				return fmt.Errorf("set git config: %w", err)
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

// collectHarnessAPIKey runs the read-current / probe-host-env / prompt flow
// for a single harness API key. Returns a non-nil pending update only when
// the user provides a new value.
func collectHarnessAPIKey(ui *UI, spec harnessAPIKeySpec) (*pendingAPIKeyUpdate, error) {
	ui.Step(fmt.Sprintf("%s API key", spec.DisplayName))

	currentValue, source, err := lookupConfiguredAPIKey(spec)
	if err != nil {
		return nil, err
	}
	hostKey := os.Getenv(spec.EnvVar)

	switch {
	case currentValue != "":
		sourceLabel := "~/.hazmat/secrets"
		if source == configuredAPIKeySourceLegacy {
			sourceLabel = agentZshrcPath + " (legacy; Enter will migrate it)"
		}
		cDim.Printf("  Current: %s (%s)\n", maskKey(fmt.Sprintf(`export %s="%s"`, spec.EnvVar, currentValue), spec.KeyPrefix), sourceLabel)
		fmt.Print("  New API key (Enter to keep, or paste new): ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		newKey := strings.TrimSpace(string(apiKey))
		if newKey == "" {
			if source == configuredAPIKeySourceLegacy {
				ui.SkipDone("API key kept — migrating legacy export into ~/.hazmat/secrets")
				return &pendingAPIKeyUpdate{EnvVar: spec.EnvVar, Value: currentValue}, nil
			}
			ui.SkipDone("API key kept")
			return nil, nil
		}
		return &pendingAPIKeyUpdate{EnvVar: spec.EnvVar, Value: newKey}, nil
	case hostKey != "":
		fmt.Printf("  Found %s in your environment: %s\n", spec.EnvVar, maskHostKey(hostKey))
		if ui.Ask("Store this key for Hazmat sessions?") {
			return &pendingAPIKeyUpdate{EnvVar: spec.EnvVar, Value: hostKey}, nil
		}
		fmt.Printf("  Set it later with 'hazmat config agent', or %s.\n", spec.SkipHint)
		ui.SkipDone("API key skipped")
		return nil, nil
	default:
		fmt.Printf("  %s\n", spec.NotFoundHint)
		fmt.Println()
		fmt.Print("  API key: ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		newKey := strings.TrimSpace(string(apiKey))
		if newKey == "" {
			ui.SkipDone(fmt.Sprintf("API key skipped — %s", spec.SkipHint))
			return nil, nil
		}
		return &pendingAPIKeyUpdate{EnvVar: spec.EnvVar, Value: newKey}, nil
	}
}

// readZshrcEnvLine returns the trimmed line that exports the named env var
// from the agent zshrc, or empty string if the file or line is absent.
func readZshrcEnvLine(path, envVar string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := "export " + envVar + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// rewriteZshrcAPIKeys removes any existing `export <ENVVAR>=...` line for each
// pending update and appends non-empty replacement values at the end of the
// file. Order of other lines is preserved.
func rewriteZshrcAPIKeys(content string, updates []pendingAPIKeyUpdate) string {
	envVarsToReplace := make(map[string]string, len(updates))
	for _, upd := range updates {
		envVarsToReplace[upd.EnvVar] = upd.Value
	}

	var kept []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		drop := false
		for envVar := range envVarsToReplace {
			if strings.HasPrefix(trimmed, "export "+envVar+"=") {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}

	for _, upd := range updates {
		if upd.Value == "" {
			continue
		}
		kept = append(kept, fmt.Sprintf(`export %s="%s"`, upd.EnvVar, upd.Value))
	}

	return strings.Join(kept, "\n")
}

// updateAgentFile reads a file from agent home, applies a transform, and
// writes it back. Uses O_WRONLY|O_TRUNC on the existing file to preserve
// ownership (agent:dev). No sudo needed — the host user has group write
// access via the dev group (set up during hazmat init).
func updateAgentFile(path string, transform func(string) string, _ os.FileMode) error {
	current, _ := os.ReadFile(path)
	updated := transform(string(current))

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("open %s for writing: %w (run 'hazmat init' to fix permissions)", path, err)
	}
	defer f.Close()

	_, err = f.WriteString(updated)
	return err
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

// maskKey shows a masked form of an `export NAME="value"` zshrc line.
// If keyPrefix is non-empty, the mask anchors on that prefix (e.g. "sk-ant-")
// so the displayed prefix gives the user a hint of which key it is. If the
// prefix isn't found (or is empty), falls back to masking the value extracted
// after the first '=' / quote pair.
func maskKey(line, keyPrefix string) string {
	if keyPrefix != "" {
		if i := strings.Index(line, keyPrefix); i >= 0 {
			key := strings.Trim(line[i:], "\"' ")
			if len(key) > 15 {
				return key[:11] + "..." + key[len(key)-4:]
			}
			if len(key) > len(keyPrefix) {
				return key[:len(keyPrefix)] + "..."
			}
			return key
		}
	}
	// Generic fallback: mask the part after `="` if present.
	if i := strings.Index(line, "=\""); i >= 0 {
		return maskHostKey(strings.Trim(line[i+2:], "\"' "))
	}
	return "(set)"
}

// maskHostKey masks a raw key string (no surrounding `export NAME=` shell syntax).
// Used when we have only the value (e.g. from os.Getenv).
func maskHostKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	if len(key) > 15 {
		return key[:8] + "..." + key[len(key)-4:]
	}
	return key[:4] + "..." + key[len(key)-3:]
}
