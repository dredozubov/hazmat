package containment

import (
	"reflect"
	"testing"
)

func TestPersistentAgentHomeManifestIsValid(t *testing.T) {
	if err := ValidatePersistentAgentHomeManifest(); err != nil {
		t.Fatalf("ValidatePersistentAgentHomeManifest: %v", err)
	}
}

func TestPersistentAgentHomeManifestCoversHomeMoveAuditPaths(t *testing.T) {
	required := map[string]AgentHomeStateClass{
		".bashrc":                 AgentHomeStateShellConfig,
		".gitconfig":              AgentHomeStateGitConfig,
		".profile":                AgentHomeStateShellConfig,
		".zshrc":                  AgentHomeStateShellConfig,
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

func TestPersistentAgentHomeManifestProjectionMatchesCurrentPolicy(t *testing.T) {
	home := "/Users/agent"

	wantDirs := []string{
		"/Users/agent/.agents",
		"/Users/agent/.bun",
		"/Users/agent/.cache",
		"/Users/agent/.cargo",
		"/Users/agent/.claude",
		"/Users/agent/.codex",
		"/Users/agent/.config",
		"/Users/agent/.cursor",
		"/Users/agent/.deno",
		"/Users/agent/.gem",
		"/Users/agent/.gemini",
		"/Users/agent/.gradle",
		"/Users/agent/.hazmat",
		"/Users/agent/.ivy2",
		"/Users/agent/.local",
		"/Users/agent/.m2",
		"/Users/agent/.node-gyp",
		"/Users/agent/.npm",
		"/Users/agent/.opencode",
		"/Users/agent/.pub-cache",
		"/Users/agent/.qwen",
		"/Users/agent/.rustup",
		"/Users/agent/.sbt",
		"/Users/agent/.swiftpm",
		"/Users/agent/.terraform.d",
	}
	if got := AgentHomeWritableSubpaths(home); !reflect.DeepEqual(got, wantDirs) {
		t.Fatalf("AgentHomeWritableSubpaths = %#v, want %#v", got, wantDirs)
	}

	wantFiles := []string{
		"/Users/agent/.bash_profile",
		"/Users/agent/.bashrc",
		"/Users/agent/.gitconfig",
		"/Users/agent/.npmrc",
		"/Users/agent/.profile",
		"/Users/agent/.pypirc",
		"/Users/agent/.zprofile",
		"/Users/agent/.zshenv",
		"/Users/agent/.zshrc",
	}
	if got := AgentHomeWritableFiles(home); !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("AgentHomeWritableFiles = %#v, want %#v", got, wantFiles)
	}

	wantExec := []string{
		"/Users/agent/.bun/bin",
		"/Users/agent/.cargo/bin",
		"/Users/agent/.claude/hooks",
		"/Users/agent/.deno/bin",
		"/Users/agent/.gem",
		"/Users/agent/.local/bin",
		"/Users/agent/.local/lib",
		"/Users/agent/.opencode/bin",
		"/Users/agent/.pub-cache/bin",
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
