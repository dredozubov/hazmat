package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	codexLatestReleaseAPI     = "https://api.github.com/repos/openai/codex/releases/latest"
	codexLatestInstallerURL   = "https://github.com/openai/codex/releases/latest/download/install.sh"
	codexInstallerAssetName   = "install.sh"
	codexBinRel               = "/.local/bin/codex"
	codexMissingHelp          = "Error: Codex not installed for agent user. Run: hazmat bootstrap codex"
	codexStateDirRel          = "/.codex"
	codexGitHubAPIAccept      = "application/vnd.github+json"
	codexGitHubRequestTimeout = 15 * time.Second
)

type codexGitHubRelease struct {
	TagName string             `json:"tag_name"`
	Assets  []codexGitHubAsset `json:"assets"`
}

type codexGitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func findInstalledCodexBinary() (string, bool) {
	return findInstalledCodexBinaryWith(asAgentOutput)
}

func findInstalledCodexBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + codexBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func codexLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + codexBinRel + `" || ` +
		`{ echo "` + codexMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + codexBinRel + `" "$@"`
}

func fetchLatestCodexRelease() (codexGitHubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, codexLatestReleaseAPI, nil)
	if err != nil {
		return codexGitHubRelease{}, fmt.Errorf("build Codex release request: %w", err)
	}
	req.Header.Set("Accept", codexGitHubAPIAccept)
	req.Header.Set("User-Agent", "hazmat/"+version)

	client := &http.Client{Timeout: codexGitHubRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return codexGitHubRelease{}, fmt.Errorf("fetch latest Codex release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return codexGitHubRelease{}, fmt.Errorf("fetch latest Codex release: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var release codexGitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return codexGitHubRelease{}, fmt.Errorf("decode latest Codex release: %w", err)
	}
	return release, nil
}

func codexInstallerAssetFromRelease(release codexGitHubRelease) (codexGitHubAsset, error) {
	for _, asset := range release.Assets {
		if asset.Name == codexInstallerAssetName {
			return asset, nil
		}
	}
	return codexGitHubAsset{}, fmt.Errorf("latest Codex release does not publish %s", codexInstallerAssetName)
}

func codexInstallerSHA256(asset codexGitHubAsset) (string, error) {
	digest := strings.TrimSpace(asset.Digest)
	if digest == "" {
		return "", fmt.Errorf("latest Codex installer digest is missing")
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("latest Codex installer digest has unexpected format %q", digest)
	}
	sum := strings.TrimPrefix(digest, "sha256:")
	if len(sum) != 64 {
		return "", fmt.Errorf("latest Codex installer digest has unexpected length %d", len(sum))
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("latest Codex installer digest is not valid hex: %w", err)
	}
	return sum, nil
}

func codexInstallerReleaseArg(release codexGitHubRelease) (string, error) {
	tagName := strings.TrimSpace(release.TagName)
	if tagName == "" {
		return "", fmt.Errorf("latest Codex release tag is missing")
	}
	return tagName, nil
}

func resolveLatestCodexInstaller() (string, string, string, error) {
	release, err := fetchLatestCodexRelease()
	if err != nil {
		return "", "", "", err
	}
	asset, err := codexInstallerAssetFromRelease(release)
	if err != nil {
		return "", "", "", err
	}
	sum, err := codexInstallerSHA256(asset)
	if err != nil {
		return "", "", "", err
	}
	releaseArg, err := codexInstallerReleaseArg(release)
	if err != nil {
		return "", "", "", err
	}
	if asset.BrowserDownloadURL == "" {
		return "", "", "", fmt.Errorf("latest Codex installer URL is missing")
	}
	return asset.BrowserDownloadURL, sum, releaseArg, nil
}

func codexInstallScript(installerURL, installerSHA256, installerRelease string) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
installer=$(mktemp "${TMPDIR:-/tmp}/codex-install.XXXXXX")
cleanup() { rm -f "$installer"; }
trap cleanup EXIT
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$installer"
actual=$(shasum -a 256 "$installer" | awk '{print $1}')
expected=%q
if [[ "$actual" != "$expected" ]]; then
  echo "Codex installer checksum mismatch: expected $expected, got $actual" >&2
  exit 1
fi
export PATH="$HOME/.local/bin:$PATH"
export CODEX_INSTALL_DIR="$HOME/.local/bin"
sh "$installer" --release %q
test -x "$HOME%s"
`, installerURL, installerSHA256, installerRelease, codexBinRel)
}

func newBootstrapCodexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "codex",
		Short: "Install or update Codex for the agent user",
		Long: `Install or update Codex for the agent user.

Hazmat uses the official Codex installer, verifies the published installer
digest from the latest GitHub release, and installs Codex into ~/.local/bin
for the agent user. Codex keeps its own auth and runtime state under ~/.codex.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return codexHarness.Bootstrap(ui, r)
		},
	}
}

func runCodexBootstrap(ui *UI, r *Runner) error {
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := requireAgentUser(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "Codex",
		TempPattern:   "hazmat-codex-bootstrap-*.sh",
		InstallReason: "download, verify, and install or update Codex as agent user",
		BuildScript: func(dryRun bool) (string, error) {
			installerURL := codexLatestInstallerURL
			installerSHA256 := strings.Repeat("0", 64)
			installerRelease := "latest"
			if !dryRun {
				var err error
				installerURL, installerSHA256, installerRelease, err = resolveLatestCodexInstaller()
				if err != nil {
					return "", err
				}
			}
			return codexInstallScript(installerURL, installerSHA256, installerRelease), nil
		},
		FindExisting: findInstalledCodexBinaryWith,
	}); err != nil {
		return err
	}

	ui.Step("Create Codex state directory")
	stateDir := agentHome + codexStateDirRel
	if err := agentEnsureSharedDir(stateDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", stateDir, err)
	}
	ui.Ok(fmt.Sprintf("Prepared %s", stateDir))

	ui.Step("Create shared agents skills directory")
	agentsDir := agentHome + "/.agents"
	if err := agentEnsureSharedDir(agentsDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", agentsDir, err)
	}
	ui.Ok(fmt.Sprintf("Prepared %s", agentsDir))

	return nil
}
