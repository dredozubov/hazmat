package containment

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestPersistentAgentHomeManifestIsValid(t *testing.T) {
	if err := ValidatePersistentAgentHomeManifest(); err != nil {
		t.Fatalf("ValidatePersistentAgentHomeManifest: %v", err)
	}
}

func TestPersistentAgentHomeManifestCoversHomeMoveAuditPaths(t *testing.T) {
	required := map[string]AgentHomeStateClass{
		".bashrc":    AgentHomeStateShellConfig,
		".gitconfig": AgentHomeStateGitConfig,
		".profile":   AgentHomeStateShellConfig,
		".zshrc":     AgentHomeStateShellConfig,
		// Top-level harness state dirs — the curated guard that every built-in
		// harness keeps a manifest entry. Dropping one fails here even though the
		// projection test below is now derived from the manifest itself.
		".claude":                 AgentHomeStateHarnessState,
		".claude.json":            AgentHomeStateHarnessState,
		".codex":                  AgentHomeStateHarnessState,
		".opencode":               AgentHomeStateHarnessState,
		".gemini":                 AgentHomeStateHarnessState, // covers Antigravity (~/.gemini/antigravity-cli)
		".cursor":                 AgentHomeStateHarnessState,
		".claude/commands":        AgentHomeStateHarnessState,
		".claude/skills":          AgentHomeStateHarnessState,
		".claude/projects":        AgentHomeStateTranscript,
		".config":                 AgentHomeStateXDGConfig,
		".config/git":             AgentHomeStateGitConfig,
		".config/mcp":             AgentHomeStateHarnessState,
		".local/bin":              AgentHomeStateExecutable,
		".local/lib":              AgentHomeStateToolchainState,
		".local/share":            AgentHomeStateXDGData,
		".cache":                  AgentHomeStateXDGCache,
		".cargo":                  AgentHomeStateToolchainState,
		".npm":                    AgentHomeStateToolchainState,
		".node-gyp":               AgentHomeStateToolchainState,
		".gem":                    AgentHomeStateToolchainState,
		".pi":                     AgentHomeStateHarnessState,
		".qwen":                   AgentHomeStateHarnessState,
		".hazmat/hermes/projects": AgentHomeStateTranscript,
		".hazmat/hermes/projects/example/state.json": AgentHomeStateTranscript,
	}
	for rel, wantClass := range required {
		if !PersistentAgentHomeManifestCovers(rel) {
			t.Fatalf("PersistentAgentHomeManifestCovers(%q) = false, want true", rel)
		}
		if gotClass, ok := PersistentAgentHomePathClass(rel); !ok || gotClass != wantClass {
			t.Fatalf("PersistentAgentHomePathClass(%q) = (%q, %v), want (%q, true)", rel, gotClass, ok, wantClass)
		}
	}
}

// TestPersistentAgentHomeManifestProjectionMatchesCurrentPolicy verifies the
// projection logic (kind filter, home-join, sort) without hand-maintaining a
// parallel copy of the manifest. Expectations are re-derived from the public
// PersistentAgentHomeManifest() accessor, so adding a harness state dir is a
// one-line change to persistentAgentHomeManifest with no mirror list to sync.
// The curated guard that each harness keeps a state dir lives in
// TestPersistentAgentHomeManifestCoversHomeMoveAuditPaths.
func TestPersistentAgentHomeManifestProjectionMatchesCurrentPolicy(t *testing.T) {
	home := "/Users/agent"
	manifest := PersistentAgentHomeManifest()

	var wantDirs, wantFiles, wantExec []string
	for _, entry := range manifest {
		switch entry.Kind {
		case AgentHomeStateDir:
			wantDirs = append(wantDirs, filepath.Join(home, entry.RelPath))
		case AgentHomeStateFile:
			wantFiles = append(wantFiles, filepath.Join(home, entry.RelPath))
		default:
			t.Fatalf("manifest entry %q has unexpected kind %q", entry.RelPath, entry.Kind)
		}
		for _, rel := range entry.ExecutableRelPaths {
			wantExec = append(wantExec, filepath.Join(home, rel))
		}
	}
	sort.Strings(wantDirs)
	sort.Strings(wantFiles)
	sort.Strings(wantExec)

	if len(wantDirs) == 0 || len(wantFiles) == 0 || len(wantExec) == 0 {
		t.Fatalf("manifest projected an empty grant set: dirs=%d files=%d exec=%d", len(wantDirs), len(wantFiles), len(wantExec))
	}

	if got := AgentHomeWritableSubpaths(home); !reflect.DeepEqual(got, wantDirs) {
		t.Fatalf("AgentHomeWritableSubpaths = %#v, want %#v", got, wantDirs)
	}
	if got := AgentHomeWritableFiles(home); !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("AgentHomeWritableFiles = %#v, want %#v", got, wantFiles)
	}
	if got := AgentHomeExecutableSubpaths(home); !reflect.DeepEqual(got, wantExec) {
		t.Fatalf("AgentHomeExecutableSubpaths = %#v, want %#v", got, wantExec)
	}
}

func TestPersistentAgentHomeManifestReturnsDefensiveCopy(t *testing.T) {
	entries := PersistentAgentHomeManifest()
	if len(entries) == 0 {
		t.Fatal("PersistentAgentHomeManifest returned no entries")
	}
	entries[0].RelPath = "changed"
	entries[0].CoveredPaths = append(entries[0].CoveredPaths, AgentHomeCoveredPath{RelPath: "changed"})
	entries[0].ExecutableRelPaths = append(entries[0].ExecutableRelPaths, "changed")

	again := PersistentAgentHomeManifest()
	if again[0].RelPath == "changed" {
		t.Fatal("PersistentAgentHomeManifest returned mutable entry storage")
	}
	for _, covered := range again[0].CoveredPaths {
		if covered.RelPath == "changed" {
			t.Fatal("PersistentAgentHomeManifest returned mutable CoveredPaths storage")
		}
	}
	for _, rel := range again[0].ExecutableRelPaths {
		if rel == "changed" {
			t.Fatal("PersistentAgentHomeManifest returned mutable ExecutableRelPaths storage")
		}
	}
}
