package hazmat

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
  1. Provider API keys used by installed harnesses (Anthropic / OpenAI /
     Gemini / OpenRouter). Stored in ~/.hazmat/secrets and injected only into
     explicitly allowed sessions
  2. Git identity (name + email, pre-filled from host git config)
  3. Removes legacy agent-home Git HTTPS credential helpers

Each prompt copies from your invoking-shell environment when the matching
env var is set, lets you paste a key, or accepts Enter to skip in favour
of a provider import or interactive sign-in path.

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

// harnessAPIKeySpec describes a provider API-key prompt. Eligibility comes
// from the credential registry's consumer harnesses, not from this table.
type harnessAPIKeySpec struct {
	CredentialID credentialID
	EnvVar       string // env var name injected into matching sessions
	DisplayName  string // "Anthropic", "OpenAI", "Gemini"
	KeyPrefix    string // mask hint — known prefix that identifies a real key (e.g. "sk-ant-"); empty = no prefix-based mask
	SkipHint     string // shown when the user chooses not to set the key
	NotFoundHint string // shown when neither current nor host env var is set, before the paste prompt
}

// harnessAPIKeyPrompts is the provider-prompt presentation table. Provider
// descriptors decide which harnesses can consume each key.
var harnessAPIKeyPrompts = []harnessAPIKeySpec{
	{
		CredentialID: credentialProviderAnthropicAPIKey,
		EnvVar:       "ANTHROPIC_API_KEY",
		DisplayName:  "Anthropic",
		KeyPrefix:    "sk-ant-",
		SkipHint:     "use provider auth inside Claude Code or another allowed harness",
		NotFoundHint: "Paste an API key now (sk-ant-...) or press Enter to skip provider API-key setup.",
	},
	{
		CredentialID: credentialProviderOpenAIAPIKey,
		EnvVar:       "OPENAI_API_KEY",
		DisplayName:  "OpenAI",
		KeyPrefix:    "sk-",
		SkipHint:     "use provider auth inside Codex or another allowed harness",
		NotFoundHint: "Paste an API key now (sk-...) or press Enter to skip provider API-key setup.",
	},
	{
		CredentialID: credentialProviderGeminiAPIKey,
		EnvVar:       "GEMINI_API_KEY",
		DisplayName:  "Gemini",
		KeyPrefix:    "",
		SkipHint:     "use provider auth inside Gemini or another allowed harness",
		NotFoundHint: "Paste an API key now (from https://aistudio.google.com/apikey) or press Enter to skip provider API-key setup.",
	},
	{
		CredentialID: credentialProviderOpenRouterAPIKey,
		EnvVar:       "OPENROUTER_API_KEY",
		DisplayName:  "OpenRouter",
		KeyPrefix:    "sk-or-",
		SkipHint:     "use provider auth inside an allowed harness",
		NotFoundHint: "Paste an API key now (sk-or-...) or press Enter to skip provider API-key setup.",
	},
}

// pendingAPIKeyUpdate captures a single env var to persist in Hazmat's
// host-owned secret store. Value == "" means "remove any legacy export".
type pendingAPIKeyUpdate struct {
	EnvVar string
	Value  string
}

type collectedAPIKeyUpdate struct {
	value   pendingAPIKeyUpdate
	present bool
}

func newCollectedAPIKeyUpdate(envVar, value string) collectedAPIKeyUpdate {
	return collectedAPIKeyUpdate{
		value: pendingAPIKeyUpdate{
			EnvVar: envVar,
			Value:  value,
		},
		present: true,
	}
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

	// ── 1. Provider API keys (one prompt per relevant provider) ────────────
	var apiKeyUpdates []pendingAPIKeyUpdate
	apiKeyPrompts := providerAPIKeyPromptsForHarnesses(installedManagedHarnessIDs())
	if len(apiKeyPrompts) == 0 {
		// No harnesses installed yet — preserve historical UX by showing the
		// Anthropic prompt so users discover the path before they bootstrap.
		apiKeyPrompts = []harnessAPIKeySpec{harnessAPIKeyPrompts[0]}
	}
	for _, spec := range apiKeyPrompts {
		update, err := collectHarnessAPIKey(ui, spec)
		if err != nil {
			return err
		}
		if update.present {
			apiKeyUpdates = append(apiKeyUpdates, update.value)
		}
	}

	// ── 2. Git identity ─────────────────────────────────────────────────────
	ui.Step("Git identity")

	agentName := gitConfigValue(gitHTTPSAgentGitConfigPath, "name")
	agentEmail := gitConfigValue(gitHTTPSAgentGitConfigPath, "email")
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

	// ── 3. Git HTTPS helper cleanup ────────────────────────────────────────
	legacyGitHTTPSHelper := hasLegacyGitHTTPSCredentialHelper(gitHTTPSAgentGitConfigPath)

	// ── Apply writes ────────────────────────────────────────────────────────
	// Read each file, modify in memory, write to temp, sudo install with
	// correct ownership. No login shell needed.

	cleanupEnvVars := removableLegacyAPIKeyEnvVars(apiKeyUpdates)
	needsWrite := len(apiKeyUpdates) > 0 || gitName != "" || gitEmail != "" || legacyGitHTTPSHelper || hasLegacyAPIKeyExports(cleanupEnvVars)

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

		// .gitconfig: update name/email and remove legacy Git HTTPS helper.
		if gitName != "" || gitEmail != "" || legacyGitHTTPSHelper {
			if err := updateAgentFile(
				gitHTTPSAgentGitConfigPath,
				func(content string) string {
					cfg := parseINI(content)
					if gitName != "" {
						cfg = setINIValue(cfg, "user", "name", gitName)
					}
					if gitEmail != "" {
						cfg = setINIValue(cfg, "user", "email", gitEmail)
					}
					if legacyGitHTTPSHelper {
						cfg = removeINIValues(cfg, "credential", "helper", isLegacyGitHTTPSCredentialHelperValue)
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
			if legacyGitHTTPSHelper {
				ui.Ok("Legacy Git HTTPS credential helper removed; Hazmat now brokers HTTPS credentials per session")
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
// for a single harness API key. Returns a present pending update only when
// the user provides a new value.
func collectHarnessAPIKey(ui *UI, spec harnessAPIKeySpec) (collectedAPIKeyUpdate, error) {
	ui.Step(fmt.Sprintf("%s API key", spec.DisplayName))
	if consumers := providerAPIKeyPromptConsumerLabel(spec); consumers != "" {
		fmt.Printf("  Used by: %s\n", consumers)
	}

	currentValue, source, err := lookupConfiguredAPIKey(spec)
	if err != nil {
		return collectedAPIKeyUpdate{}, err
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
				return newCollectedAPIKeyUpdate(spec.EnvVar, currentValue), nil
			}
			ui.SkipDone("API key kept")
			return collectedAPIKeyUpdate{}, nil
		}
		return newCollectedAPIKeyUpdate(spec.EnvVar, newKey), nil
	case hostKey != "":
		fmt.Printf("  Found %s in your environment: %s\n", spec.EnvVar, maskHostKey(hostKey))
		if ui.Ask("Store this key for Hazmat sessions?") {
			return newCollectedAPIKeyUpdate(spec.EnvVar, hostKey), nil
		}
		fmt.Printf("  Set it later with 'hazmat config agent', or %s.\n", spec.SkipHint)
		ui.SkipDone("API key skipped")
		return collectedAPIKeyUpdate{}, nil
	default:
		fmt.Printf("  %s\n", spec.NotFoundHint)
		fmt.Println()
		fmt.Print("  API key: ")
		apiKey, _ := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		newKey := strings.TrimSpace(string(apiKey))
		if newKey == "" {
			ui.SkipDone(fmt.Sprintf("API key skipped — %s", spec.SkipHint))
			return collectedAPIKeyUpdate{}, nil
		}
		return newCollectedAPIKeyUpdate(spec.EnvVar, newKey), nil
	}
}

func installedManagedHarnessIDs() []HarnessID {
	installed := installedManagedHarnesses()
	ids := make([]HarnessID, 0, len(installed))
	for _, harness := range installed {
		ids = append(ids, harness.Spec.ID)
	}
	return ids
}

func providerAPIKeyPromptsForHarnesses(harnesses []HarnessID) []harnessAPIKeySpec {
	installed := make(map[HarnessID]struct{}, len(harnesses))
	for _, harness := range harnesses {
		if harness != "" {
			installed[harness] = struct{}{}
		}
	}
	if len(installed) == 0 {
		return nil
	}

	var prompts []harnessAPIKeySpec
	for _, spec := range harnessAPIKeyPrompts {
		descriptor, ok := findCredentialDescriptor(spec.CredentialID)
		if !ok || descriptor.Kind != credentialKindProviderAPIKey {
			continue
		}
		for _, consumer := range descriptor.ConsumerHarnessIDs() {
			if _, ok := installed[consumer]; ok {
				prompts = append(prompts, spec)
				break
			}
		}
	}
	return prompts
}

func providerAPIKeyPromptConsumerLabel(spec harnessAPIKeySpec) string {
	descriptor, ok := findCredentialDescriptor(spec.CredentialID)
	if !ok {
		return ""
	}
	consumers := descriptor.ConsumerHarnessIDs()
	if len(consumers) == 0 {
		return ""
	}
	labels := make([]string, 0, len(consumers))
	for _, consumer := range consumers {
		labels = append(labels, harnessDisplayNameForPrompt(consumer))
	}
	return joinPromptLabels(labels)
}

func harnessDisplayNameForPrompt(id HarnessID) string {
	if id == HarnessHermes {
		return "Hermes"
	}
	if harness, ok := managedHarnessByID(id); ok && strings.TrimSpace(harness.Spec.DisplayName) != "" {
		return harness.Spec.DisplayName
	}
	return string(id)
}

func joinPromptLabels(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " and " + labels[1]
	default:
		return strings.Join(labels[:len(labels)-1], ", ") + ", and " + labels[len(labels)-1]
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

func removeINIValues(sections []iniSection, section, key string, shouldRemove func(string) bool) []iniSection {
	for i, s := range sections {
		if s.name != section {
			continue
		}
		filtered := s.lines[:0]
		for _, line := range s.lines {
			trimmed := strings.TrimSpace(line)
			value, ok := parseINIKeyValue(trimmed, key)
			if ok && shouldRemove(value) {
				continue
			}
			filtered = append(filtered, line)
		}
		sections[i].lines = filtered
	}
	return sections
}

func parseINIKeyValue(line, key string) (string, bool) {
	if strings.HasPrefix(line, key+" =") {
		return strings.TrimSpace(strings.TrimPrefix(line, key+" =")), true
	}
	if strings.HasPrefix(line, key+"=") {
		return strings.TrimSpace(strings.TrimPrefix(line, key+"=")), true
	}
	return "", false
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
		if value, ok := parseINIKeyValue(strings.TrimSpace(line), key); ok {
			return value
		}
	}
	return ""
}

func hasLegacyGitHTTPSCredentialHelper(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		value, ok := parseINIKeyValue(strings.TrimSpace(line), "helper")
		if ok && isLegacyGitHTTPSCredentialHelperValue(value) {
			return true
		}
	}
	return false
}

func isLegacyGitHTTPSCredentialHelperValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.Contains(trimmed, "store") && strings.Contains(trimmed, gitHTTPSAgentCredentialsPath)
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
