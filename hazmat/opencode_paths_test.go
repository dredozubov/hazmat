package hazmat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCodeBinaryCandidatesPreferCurrentPath(t *testing.T) {
	want := []string{
		agentHome + openCodeCurrentBinRel,
		agentHome + openCodeLegacyBinRel,
	}
	if got := openCodeBinaryCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("openCodeBinaryCandidates() = %v, want %v", got, want)
	}
}

func TestFindInstalledOpenCodeBinaryWithPrefersCurrentPath(t *testing.T) {
	got, ok := findInstalledOpenCodeBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+openCodeCurrentBinRel {
			return "", nil
		}
		if args[len(args)-1] == agentHome+openCodeLegacyBinRel {
			return "", nil
		}
		return "", errors.New("unexpected path")
	})
	if !ok {
		t.Fatal("expected an installed OpenCode binary")
	}
	if got != agentHome+openCodeCurrentBinRel {
		t.Fatalf("findInstalledOpenCodeBinaryWith() = %q, want %q", got, agentHome+openCodeCurrentBinRel)
	}
}

func TestFindInstalledOpenCodeBinaryWithFallsBackToLegacyPath(t *testing.T) {
	got, ok := findInstalledOpenCodeBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+openCodeLegacyBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected legacy OpenCode binary to be detected")
	}
	if got != agentHome+openCodeLegacyBinRel {
		t.Fatalf("findInstalledOpenCodeBinaryWith() = %q, want %q", got, agentHome+openCodeLegacyBinRel)
	}
}

func TestOpenCodeLaunchScriptChecksBothLocations(t *testing.T) {
	script := openCodeLaunchScript()
	for _, want := range []string{
		`"$HOME/.opencode/bin/opencode"`,
		`"$HOME/.local/bin/opencode"`,
		openCodeMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("openCodeLaunchScript() missing %q in %q", want, script)
		}
	}
}

func TestOpenCodeArchiveNameForPlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform openCodeHostPlatform
		want     string
	}{
		{
			name:     "darwin arm64",
			platform: openCodeHostPlatform{OS: "Darwin", Arch: "arm64"},
			want:     "opencode-darwin-arm64.zip",
		},
		{
			name:     "darwin x86_64 baseline",
			platform: openCodeHostPlatform{OS: "Darwin", Arch: "x86_64"},
			want:     "opencode-darwin-x64-baseline.zip",
		},
		{
			name:     "linux aarch64",
			platform: openCodeHostPlatform{OS: "Linux", Arch: "aarch64"},
			want:     "opencode-linux-arm64.tar.gz",
		},
		{
			name:     "linux amd64 baseline",
			platform: openCodeHostPlatform{OS: "Linux", Arch: "amd64"},
			want:     "opencode-linux-x64-baseline.tar.gz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := openCodeArchiveNameForPlatform(tt.platform)
			if err != nil {
				t.Fatalf("openCodeArchiveNameForPlatform: %v", err)
			}
			if got != tt.want {
				t.Fatalf("archive = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenCodeArchiveNameForPlatformRejectsUnsupported(t *testing.T) {
	_, err := openCodeArchiveNameForPlatform(openCodeHostPlatform{OS: "Darwin", Arch: "riscv64"})
	if err == nil || !strings.Contains(err.Error(), "unsupported OpenCode release platform") {
		t.Fatalf("err = %v, want unsupported platform", err)
	}
}

func TestOpenCodeArchiveAssetFromReleaseSelectsExpectedAsset(t *testing.T) {
	release := openCodeGitHubRelease{
		TagName: "v1.2.3",
		Assets: []openCodeGitHubAsset{
			{Name: "opencode-darwin-x64.zip"},
			{Name: "opencode-darwin-arm64.zip", BrowserDownloadURL: "https://example.test/opencode.zip", Digest: "sha256:" + strings.Repeat("b", 64)},
		},
	}
	asset, err := openCodeArchiveAssetFromRelease(release, "opencode-darwin-arm64.zip")
	if err != nil {
		t.Fatalf("openCodeArchiveAssetFromRelease: %v", err)
	}
	if asset.BrowserDownloadURL != "https://example.test/opencode.zip" {
		t.Fatalf("asset URL = %q", asset.BrowserDownloadURL)
	}
	sum, err := openCodeArchiveSHA256(asset)
	if err != nil {
		t.Fatalf("openCodeArchiveSHA256: %v", err)
	}
	if sum != strings.Repeat("b", 64) {
		t.Fatalf("sum = %q", sum)
	}
}

func TestOpenCodeArchiveSHA256RejectsMissingDigest(t *testing.T) {
	_, err := openCodeArchiveSHA256(openCodeGitHubAsset{Name: "opencode-darwin-arm64.zip"})
	if err == nil || !strings.Contains(err.Error(), "digest is missing") {
		t.Fatalf("err = %v, want missing digest", err)
	}
}
