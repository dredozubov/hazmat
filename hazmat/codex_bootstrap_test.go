package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFindInstalledCodexBinaryWith(t *testing.T) {
	got, ok := findInstalledCodexBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+codexBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected Codex binary to be detected")
	}
	if got != agentHome+codexBinRel {
		t.Fatalf("findInstalledCodexBinaryWith() = %q, want %q", got, agentHome+codexBinRel)
	}
}

func TestCodexLaunchScriptChecksInstalledPath(t *testing.T) {
	script := codexLaunchScript()
	for _, want := range []string{
		`"$HOME/.local/bin/codex"`,
		codexMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("codexLaunchScript() missing %q in %q", want, script)
		}
	}
}

func TestCodexInstallScriptInstallsLatestIntoAgentPrefix(t *testing.T) {
	script := codexInstallScript(
		"https://example.com/install.sh",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"rust-v0.125.0",
	)
	for _, want := range []string{
		`curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail "https://example.com/install.sh" -o "$installer"`,
		`expected="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`,
		`export CODEX_INSTALL_DIR="$HOME/.local/bin"`,
		`sh "$installer" --release "rust-v0.125.0"`,
		`test -x "$HOME/.local/bin/codex"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("codexInstallScript() missing %q in %q", want, script)
		}
	}
}

func TestCodexInstallerReleaseArg(t *testing.T) {
	got, err := codexInstallerReleaseArg(codexGitHubRelease{TagName: "rust-v0.125.0"})
	if err != nil {
		t.Fatalf("codexInstallerReleaseArg() error = %v", err)
	}
	if got != "rust-v0.125.0" {
		t.Fatalf("codexInstallerReleaseArg() = %q", got)
	}
}

func TestCodexInstallerAssetFromRelease(t *testing.T) {
	asset, err := codexInstallerAssetFromRelease(codexGitHubRelease{
		Assets: []codexGitHubAsset{
			{Name: "other"},
			{
				Name:               codexInstallerAssetName,
				BrowserDownloadURL: "https://example.com/install.sh",
				Digest:             "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
		},
	})
	if err != nil {
		t.Fatalf("codexInstallerAssetFromRelease() error = %v", err)
	}
	if asset.Name != codexInstallerAssetName {
		t.Fatalf("codexInstallerAssetFromRelease() name = %q", asset.Name)
	}
}

func TestCodexInstallerSHA256(t *testing.T) {
	got, err := codexInstallerSHA256(codexGitHubAsset{
		Name:   codexInstallerAssetName,
		Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("codexInstallerSHA256() error = %v", err)
	}
	want := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got != want {
		t.Fatalf("codexInstallerSHA256() = %q, want %q", got, want)
	}
}
