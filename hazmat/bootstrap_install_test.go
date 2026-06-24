package hazmat

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
		"bootstrap_antigravity.go",
		"bootstrap_qwen.go",
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
	script := openCodeInstallScript(
		"https://github.com/anomalyco/opencode/releases/download/v1.2.3/opencode-darwin-arm64.zip",
		strings.Repeat("a", 64),
		"v1.2.3",
		"opencode-darwin-arm64.zip",
	)
	for _, want := range []string{
		`release="v1.2.3"`,
		`asset="opencode-darwin-arm64.zip"`,
		`echo "Installing OpenCode $release from $asset" >&2`,
		`curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail "https://github.com/anomalyco/opencode/releases/download/v1.2.3/opencode-darwin-arm64.zip" -o "$archive"`,
		`actual=$(shasum -a 256 "$archive" | awk '{print $1}')`,
		`expected="` + strings.Repeat("a", 64) + `"`,
		`unzip -q "$archive" -d "$extract_dir"`,
		`install -m 0755 "$extract_dir/opencode" "$HOME/.opencode/bin/opencode"`,
		`ln -s "$HOME/.opencode/bin/opencode" "$HOME/.local/bin/opencode"`,
		`test -x "$HOME/.opencode/bin/opencode" || test -x "$HOME/.local/bin/opencode"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("openCodeInstallScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, "https://opencode.ai/install") {
		t.Fatalf("openCodeInstallScript() still runs the upstream live installer: %q", script)
	}
}

func TestAntigravityInstallScriptPinsAndVerifiesInstaller(t *testing.T) {
	script := antigravityInstallScript()
	for _, want := range []string{
		`curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail "https://antigravity.google/cli/install.sh" -o "$installer"`,
		`actual=$(shasum -a 256 "$installer" | awk '{print $1}')`,
		`expected="` + antigravityInstallerSHA256 + `"`,
		`bash "$installer"`,
		`test -x "$HOME/.local/bin/agy"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("antigravityInstallScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, "npm install") {
		t.Fatalf("antigravityInstallScript() should not require npm: %q", script)
	}
}

func TestQwenInstallScriptRefreshesLatestIntoAgentPrefix(t *testing.T) {
	script := qwenInstallScript()
	for _, want := range []string{
		`command -v node`,
		`major >= 20`,
		`mkdir -p "$HOME/.local/bin" "$HOME/.local/lib/node_modules"`,
		`export NPM_CONFIG_PREFIX="$HOME/.local"`,
		`npm install -g --silent "@qwen-code/qwen-code@latest"`,
		`test -x "$HOME/.local/bin/qwen"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("qwenInstallScript() missing %q in %q", want, script)
		}
	}
}
