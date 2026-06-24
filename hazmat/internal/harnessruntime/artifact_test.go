package harnessruntime

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const testAgentHome = "/Users/agent"

func TestInspectArtifactDetectsDrift(t *testing.T) {
	path := testAgentHome + "/.local/lib/node_modules/@qwen-code/qwen-code"
	status := InspectArtifact(
		fakeArtifactRead(map[string]ArtifactKind{
			path: ArtifactFile,
		}, nil),
		testAgentHome,
		DirArtifact(path, "Qwen Code npm package"),
	)
	if !status.Exists {
		t.Fatal("expected artifact to exist")
	}
	if !strings.Contains(status.Drift, "expected directory") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectArtifactChecksNpmPackageMetadata(t *testing.T) {
	path := testAgentHome + "/.local/lib/node_modules/@qwen-code/qwen-code"
	status := InspectArtifact(
		fakeArtifactRead(map[string]ArtifactKind{
			path: ArtifactDir,
		}, map[string]string{
			filepath.Join(path, "package.json"): `{"name":"left-pad"}`,
		}),
		testAgentHome,
		NpmPackageDirArtifact(path, "@qwen-code/qwen-code", "Qwen Code npm package"),
	)
	if !status.Exists {
		t.Fatal("expected package directory to exist")
	}
	if !strings.Contains(status.Drift, "expected npm package @qwen-code/qwen-code, got left-pad") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectArtifactRejectsDisallowedSymlink(t *testing.T) {
	path := testAgentHome + "/.local/bin/codex"
	status := InspectArtifact(
		fakeArtifactRead(map[string]ArtifactKind{
			path: ArtifactKind("symlink"),
		}, nil),
		testAgentHome,
		RegularFileArtifact(path, "Codex executable"),
	)
	if !status.Exists || !status.Symlink {
		t.Fatalf("artifact status = %#v", status)
	}
	if !strings.Contains(status.Drift, "symlink not allowed") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectArtifactAllowsDeclaredSymlink(t *testing.T) {
	path := testAgentHome + "/.local/bin/opencode"
	status := InspectArtifact(
		fakeArtifactRead(map[string]ArtifactKind{
			path: ArtifactKind("symlink"),
		}, nil),
		testAgentHome,
		SymlinkArtifact(path, "OpenCode PATH shim"),
	)
	if status.Drift != "" {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestValidateArtifactPathRejectsAgentHomeAndExternalPaths(t *testing.T) {
	for _, path := range []string{
		testAgentHome,
		"/Users/dr/.local/bin/codex",
		"/Users/agent2/.local/bin/codex",
	} {
		if err := ValidateArtifactPath(testAgentHome, path); err == nil {
			t.Fatalf("ValidateArtifactPath(%q) succeeded, want error", path)
		}
	}

	if err := ValidateArtifactPath(testAgentHome, testAgentHome+"/.local/bin/codex"); err != nil {
		t.Fatalf("ValidateArtifactPath(valid): %v", err)
	}
}

func TestRemoveArtifactUsesExactPlannedPath(t *testing.T) {
	path := testAgentHome + "/.local/lib/node_modules/@qwen-code/qwen-code"
	var gotReason string
	var gotArgs []string

	err := RemoveArtifact(func(reason string, args ...string) error {
		gotReason = reason
		gotArgs = append([]string(nil), args...)
		return nil
	}, testAgentHome, DirArtifact(path, "Qwen Code npm package"))
	if err != nil {
		t.Fatalf("RemoveArtifact: %v", err)
	}

	if gotReason != "remove Qwen Code npm package" {
		t.Fatalf("reason = %q", gotReason)
	}
	wantArgs := []string{"/bin/rm", "-rf", "--", path}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestRemoveArtifactReturnsRemovalError(t *testing.T) {
	removeErr := errors.New("remove failed")
	err := RemoveArtifact(func(string, ...string) error {
		return removeErr
	}, testAgentHome, FileArtifact(testAgentHome+"/.local/bin/codex", "Codex executable"))
	if !errors.Is(err, removeErr) {
		t.Fatalf("RemoveArtifact error = %v, want %v", err, removeErr)
	}
}

func fakeArtifactRead(paths map[string]ArtifactKind, versions map[string]string) CommandReader {
	if paths == nil {
		paths = map[string]ArtifactKind{}
	}
	if versions == nil {
		versions = map[string]string{}
	}
	return func(args ...string) (string, error) {
		if len(args) >= 3 && (args[0] == "test" || args[0] == "/usr/bin/test") {
			kind, ok := paths[args[2]]
			if !ok {
				return "", errors.New("missing")
			}
			switch args[1] {
			case "-e", "-x":
				return "", nil
			case "-f":
				if kind == ArtifactFile || kind == ArtifactKind("symlink") {
					return "", nil
				}
			case "-d":
				if kind == ArtifactDir {
					return "", nil
				}
			case "-L":
				if kind == ArtifactKind("symlink") {
					return "", nil
				}
				return "", errors.New("not symlink")
			}
			return "", errors.New("wrong kind")
		}
		if len(args) == 2 && args[0] == "/bin/cat" {
			if value, ok := versions[args[1]]; ok {
				return value, nil
			}
			return "", errors.New("missing")
		}
		return "", errors.New("unexpected command")
	}
}
