package main

import (
	"os"
	"strings"
	"testing"
)

func TestHarnessBootstrapsUseInstallOrUpdateHelper(t *testing.T) {
	for _, path := range []string{
		"bootstrap.go",
		"bootstrap_codex.go",
		"bootstrap_opencode.go",
		"bootstrap_gemini.go",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source := string(raw)
		if !strings.Contains(source, "runHarnessInstallOrUpdateStep(") {
			t.Fatalf("%s does not use runHarnessInstallOrUpdateStep", path)
		}
		if strings.Contains(source, "already installed") {
			t.Fatalf("%s contains stale installed-binary skip wording", path)
		}
	}
}

func TestClaudeInstallScriptRefreshesLatestIntoAgentPrefix(t *testing.T) {
	script := claudeInstallScript()
	for _, want := range []string{
		`curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail "https://claude.ai/install.sh" -o "$installer"`,
		`expected="` + claudeInstallerSHA256 + `"`,
		`bash "$installer" latest`,
		`test -x "$HOME/.local/bin/claude"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("claudeInstallScript() missing %q in %q", want, script)
		}
	}
}

func TestOpenCodeInstallScriptRefreshesLatestIntoAgentPrefix(t *testing.T) {
	script := openCodeInstallScript()
	for _, want := range []string{
		`curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail "https://opencode.ai/install" -o "$installer"`,
		`bash "$installer" --no-modify-path`,
		`ln -s "$HOME/.opencode/bin/opencode" "$HOME/.local/bin/opencode"`,
		`test -x "$HOME/.opencode/bin/opencode" || test -x "$HOME/.local/bin/opencode"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("openCodeInstallScript() missing %q in %q", want, script)
		}
	}
}

func TestGeminiInstallScriptRefreshesLatestIntoAgentPrefix(t *testing.T) {
	script := geminiInstallScript()
	for _, want := range []string{
		`mkdir -p "$HOME/.local/bin" "$HOME/.local/lib/node_modules"`,
		`export NPM_CONFIG_PREFIX="$HOME/.local"`,
		`npm install -g --silent "@google/gemini-cli@latest"`,
		`test -x "$HOME/.local/bin/gemini"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("geminiInstallScript() missing %q in %q", want, script)
		}
	}
}
