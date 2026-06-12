package hazmat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	openCodeLatestReleaseAPI     = "https://api.github.com/repos/anomalyco/opencode/releases/latest"
	openCodeLatestArchiveBaseURL = "https://github.com/anomalyco/opencode/releases/latest/download/"
	openCodeCurrentBinRel        = "/.opencode/bin/opencode"
	openCodeLegacyBinRel         = "/.local/bin/opencode"
	openCodeMissingHelp          = "Error: OpenCode not installed for agent user. Run: hazmat bootstrap opencode"
	openCodeGitHubAPIAccept      = "application/vnd.github+json"
	openCodeGitHubRequestTimeout = 15 * time.Second
)

const agentOpenCodeConfigJSON = `{
  "$schema": "https://opencode.ai/config.json",
  "autoupdate": false
}
`

type openCodeGitHubRelease struct {
	TagName string                `json:"tag_name"`
	Assets  []openCodeGitHubAsset `json:"assets"`
}

type openCodeGitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type openCodeHostPlatform struct {
	OS   string
	Arch string
}

var detectOpenCodeHostPlatform = defaultDetectOpenCodeHostPlatform

func openCodeBinaryCandidates() []string {
	return []string{
		agentHome + openCodeCurrentBinRel,
		agentHome + openCodeLegacyBinRel,
	}
}

func findInstalledOpenCodeBinary() (string, bool) {
	return findInstalledOpenCodeBinaryWith(asAgentOutput)
}

func findInstalledOpenCodeBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	for _, path := range openCodeBinaryCandidates() {
		if _, err := read("test", "-x", path); err == nil {
			return path, true
		}
	}
	return "", false
}

func probeOpenCodeHarness(read func(args ...string) (string, error)) harnessProbe {
	return probeHarnessBinary(read, findInstalledOpenCodeBinaryWith, "--version")
}

func openCodeHarnessManagedCodeArtifacts() []harnessManagedArtifact {
	return []harnessManagedArtifact{
		harnessFileArtifact(agentHome+openCodeCurrentBinRel, "OpenCode executable"),
		harnessSymlinkArtifact(agentHome+openCodeLegacyBinRel, "OpenCode PATH shim"),
	}
}

func openCodeLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`opencode_bin=""; ` +
		`for candidate in "$HOME` + openCodeCurrentBinRel + `" "$HOME` + openCodeLegacyBinRel + `"; do ` +
		`if [ -x "$candidate" ]; then opencode_bin="$candidate"; break; fi; ` +
		`done; ` +
		`if [ -z "$opencode_bin" ]; then echo "` + openCodeMissingHelp + `" >&2; exit 1; fi; ` +
		`exec "$opencode_bin" "$@"`
}

func defaultDetectOpenCodeHostPlatform() (openCodeHostPlatform, error) {
	osName, err := commandStdout(hostUnamePath, "-s")
	if err != nil {
		return openCodeHostPlatform{}, fmt.Errorf("detect OpenCode host OS: %w", err)
	}
	arch, err := commandStdout(hostUnamePath, "-m")
	if err != nil {
		return openCodeHostPlatform{}, fmt.Errorf("detect OpenCode host architecture: %w", err)
	}
	return openCodeHostPlatform{OS: osName, Arch: arch}, nil
}

func openCodeArchiveNameForPlatform(platform openCodeHostPlatform) (string, error) {
	osName := strings.ToLower(strings.TrimSpace(platform.OS))
	arch := strings.ToLower(strings.TrimSpace(platform.Arch))
	switch arch {
	case "aarch64":
		arch = "arm64"
	case "x86_64", "amd64":
		arch = "x64"
	}

	switch osName {
	case "darwin":
		switch arch {
		case "arm64":
			return "opencode-darwin-arm64.zip", nil
		case "x64":
			// Prefer the baseline archive for Intel Macs. It is compatible
			// with AVX2 and non-AVX2 machines and avoids another host probe.
			return "opencode-darwin-x64-baseline.zip", nil
		}
	case "linux":
		switch arch {
		case "arm64":
			return "opencode-linux-arm64.tar.gz", nil
		case "x64":
			return "opencode-linux-x64-baseline.tar.gz", nil
		}
	}
	return "", fmt.Errorf("unsupported OpenCode release platform %s/%s", platform.OS, platform.Arch)
}

func fetchLatestOpenCodeRelease() (openCodeGitHubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, openCodeLatestReleaseAPI, nil)
	if err != nil {
		return openCodeGitHubRelease{}, fmt.Errorf("build OpenCode release request: %w", err)
	}
	req.Header.Set("Accept", openCodeGitHubAPIAccept)
	req.Header.Set("User-Agent", "hazmat/"+version)

	client := &http.Client{Timeout: openCodeGitHubRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return openCodeGitHubRelease{}, fmt.Errorf("fetch latest OpenCode release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return openCodeGitHubRelease{}, fmt.Errorf("fetch latest OpenCode release: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var release openCodeGitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return openCodeGitHubRelease{}, fmt.Errorf("decode latest OpenCode release: %w", err)
	}
	return release, nil
}

func openCodeArchiveAssetFromRelease(release openCodeGitHubRelease, assetName string) (openCodeGitHubAsset, error) {
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset, nil
		}
	}
	return openCodeGitHubAsset{}, fmt.Errorf("latest OpenCode release does not publish %s", assetName)
}

func openCodeArchiveSHA256(asset openCodeGitHubAsset) (string, error) {
	return githubAssetSHA256("latest OpenCode archive", asset.Digest)
}

func openCodeReleaseTag(release openCodeGitHubRelease) (string, error) {
	tagName := strings.TrimSpace(release.TagName)
	if tagName == "" {
		return "", fmt.Errorf("latest OpenCode release tag is missing")
	}
	return tagName, nil
}

func resolveLatestOpenCodeArchive() (string, string, string, string, error) {
	platform, err := detectOpenCodeHostPlatform()
	if err != nil {
		return "", "", "", "", err
	}
	assetName, err := openCodeArchiveNameForPlatform(platform)
	if err != nil {
		return "", "", "", "", err
	}
	release, err := fetchLatestOpenCodeRelease()
	if err != nil {
		return "", "", "", "", err
	}
	asset, err := openCodeArchiveAssetFromRelease(release, assetName)
	if err != nil {
		return "", "", "", "", err
	}
	sum, err := openCodeArchiveSHA256(asset)
	if err != nil {
		return "", "", "", "", err
	}
	releaseTag, err := openCodeReleaseTag(release)
	if err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
		return "", "", "", "", fmt.Errorf("latest OpenCode archive URL is missing")
	}
	return asset.BrowserDownloadURL, sum, releaseTag, assetName, nil
}

func dryRunOpenCodeArchive() (string, string, string, string) {
	assetName := "opencode-darwin-arm64.zip"
	if platform, err := detectOpenCodeHostPlatform(); err == nil {
		if name, err := openCodeArchiveNameForPlatform(platform); err == nil {
			assetName = name
		}
	}
	return openCodeLatestArchiveBaseURL + assetName, strings.Repeat("0", 64), "latest", assetName
}

func openCodeInstallScript(archiveURL, archiveSHA256, releaseTag, assetName string) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
archive=$(mktemp "${TMPDIR:-/tmp}/opencode-archive.XXXXXX")
extract_dir=$(mktemp -d "${TMPDIR:-/tmp}/opencode-extract.XXXXXX")
cleanup() { rm -f "$archive"; rm -rf "$extract_dir"; }
trap cleanup EXIT
release=%q
asset=%q
echo "Installing OpenCode $release from $asset" >&2
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$archive"
actual=$(shasum -a 256 "$archive" | awk '{print $1}')
expected=%q
if [[ "$actual" != "$expected" ]]; then
  echo "OpenCode archive checksum mismatch: expected $expected, got $actual" >&2
  exit 1
fi
case "$asset" in
  *.zip)
    unzip -q "$archive" -d "$extract_dir"
    ;;
  *.tar.gz)
    tar -xzf "$archive" -C "$extract_dir"
    ;;
  *)
    echo "Unsupported OpenCode archive asset: $asset" >&2
    exit 1
    ;;
esac
if [ ! -f "$extract_dir/opencode" ]; then
  echo "OpenCode archive did not contain an opencode binary" >&2
  exit 1
fi
install -d -m 0700 "$HOME/.opencode/bin" "$HOME/.local/bin"
install -m 0755 "$extract_dir/opencode" "$HOME%s"
if [ -x "$HOME%s" ] && [ ! -e "$HOME%s" ] && [ ! -L "$HOME%s" ]; then
  ln -s "$HOME%s" "$HOME%s"
fi
test -x "$HOME%s" || test -x "$HOME%s"
`, releaseTag, assetName, archiveURL, archiveSHA256, openCodeCurrentBinRel,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel,
		openCodeLegacyBinRel,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel)
}

func ensureOpenCodePathShim(ui *UI, r *Runner) error {
	ui.Step("Ensure OpenCode is on agent PATH")

	installedPath, ok := findInstalledOpenCodeBinaryWith(r.AgentOutput)
	if !ok {
		if !r.DryRun {
			return fmt.Errorf("OpenCode binary not found after install")
		}
		installedPath = agentHome + openCodeCurrentBinRel
	}

	shimPath := agentHome + openCodeLegacyBinRel
	if installedPath == shimPath {
		ui.SkipDone(shimPath + " already present")
		return nil
	}

	shimDir := agentHome + "/.local/bin"
	if r.DryRun {
		ui.Ok(fmt.Sprintf("Would link %s -> %s", shimPath, installedPath))
		return nil
	}
	if err := agentEnsureSharedDir(shimDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", shimDir, err)
	}
	if _, err := r.AgentOutput("test", "-L", shimPath); err == nil {
		if err := r.AsAgent("refresh OpenCode PATH shim", "ln", "-sfn", installedPath, shimPath); err != nil {
			return fmt.Errorf("refresh OpenCode PATH shim: %w", err)
		}
		ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
		return nil
	}
	if _, err := r.AgentOutput("test", "-e", shimPath); err == nil {
		ui.SkipDone(shimPath + " already present (not overwritten)")
		return nil
	}
	if err := r.AsAgent("link OpenCode into agent PATH", "ln", "-s", installedPath, shimPath); err != nil {
		return fmt.Errorf("link OpenCode PATH shim: %w", err)
	}
	ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
	return nil
}

func newBootstrapOpenCodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "opencode",
		Short: "Install or update OpenCode for the agent user and write a minimal config",
		Long: `Install or update OpenCode for the agent user and write a minimal global config.

Hazmat writes only a small agent-owned opencode.json with autoupdate disabled.
Runtime behavior, provider settings, commands, agents, skills, and auth can be
managed separately via OpenCode itself or 'hazmat config import opencode'.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			harness, _ := managedHarnessByID(HarnessOpenCode)
			return runManagedHarnessUpdate(harness)
		},
	}
}

func runOpenCodeBootstrap(ui *UI, r *Runner) error {
	if err := verifyAgentUserForBootstrap(ui, r); err != nil {
		return err
	}

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "OpenCode",
		TempPattern:   "hazmat-opencode-bootstrap-*.sh",
		InstallReason: "download, verify, and install or update OpenCode as agent user",
		BuildScript: func(dryRun bool) (string, error) {
			archiveURL, archiveSHA256, releaseTag, assetName := dryRunOpenCodeArchive()
			if !dryRun {
				var err error
				archiveURL, archiveSHA256, releaseTag, assetName, err = resolveLatestOpenCodeArchive()
				if err != nil {
					return "", err
				}
			}
			return openCodeInstallScript(archiveURL, archiveSHA256, releaseTag, assetName), nil
		},
		FindExisting: findInstalledOpenCodeBinaryWith,
	}); err != nil {
		return err
	}

	if err := ensureOpenCodePathShim(ui, r); err != nil {
		return err
	}

	ui.Step("Write agent OpenCode config")
	configDir := agentHome + "/.config/opencode"
	configPath := configDir + "/opencode.json"
	if r.DryRun {
		ui.Ok(fmt.Sprintf("Would prepare %s", configDir))
		ui.Ok(fmt.Sprintf("Would write %s if missing", configPath))
	} else {
		if err := agentEnsureSharedDir(configDir, 0o2770); err != nil {
			return fmt.Errorf("ensure %s: %w", configDir, err)
		}
		if _, err := r.AgentOutput("test", "-f", configPath); err == nil {
			ui.SkipDone(configPath + " already present (not overwritten)")
		} else {
			if err := agentWriteSharedFile(configPath, []byte(agentOpenCodeConfigJSON), 0o660); err != nil {
				return fmt.Errorf("write OpenCode config: %w", err)
			}
			ui.Ok(fmt.Sprintf("Wrote %s (0660)", configPath))
		}
	}

	ui.Step("Create OpenCode data directory")
	dataDir := agentHome + "/.local/share/opencode"
	if r.DryRun {
		ui.Ok(fmt.Sprintf("Would prepare %s", dataDir))
	} else {
		if err := agentEnsureSharedDir(dataDir, 0o2770); err != nil {
			return fmt.Errorf("ensure %s: %w", dataDir, err)
		}
		ui.Ok(fmt.Sprintf("Prepared %s", dataDir))
	}

	return nil
}
