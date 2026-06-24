package harnesses

import (
	"strings"
	"testing"
)

func TestBuiltinMetadataIsCompleteAndUnique(t *testing.T) {
	metadata := BuiltinMetadata()
	if len(metadata) != 8 {
		t.Fatalf("BuiltinMetadata length = %d, want 8", len(metadata))
	}

	seen := map[ID]bool{}
	for _, entry := range metadata {
		if entry.Spec.ID == "" {
			t.Fatalf("empty harness ID: %+v", entry)
		}
		if seen[entry.Spec.ID] {
			t.Fatalf("duplicate harness ID %q", entry.Spec.ID)
		}
		seen[entry.Spec.ID] = true
		if entry.Spec.DisplayName == "" {
			t.Fatalf("%s display name is empty", entry.Spec.ID)
		}
		if entry.Spec.StateVersion != "1" {
			t.Fatalf("%s state version = %q, want 1", entry.Spec.ID, entry.Spec.StateVersion)
		}
		if entry.LaunchCommand == "" || entry.BootstrapCommand == "" {
			t.Fatalf("%s commands incomplete: %+v", entry.Spec.ID, entry)
		}
	}
}

func TestImportPolicyDocumentsSupportedAndNoImportHarnesses(t *testing.T) {
	for _, id := range []ID{Claude, Codex, OpenCode} {
		metadata := MustMetadata(id)
		if !metadata.ImportPolicy.Supported {
			t.Fatalf("%s import policy = unsupported, want supported", id)
		}
		if strings.Contains(metadata.ImportPolicy.Boundary, "no curated import") {
			t.Fatalf("%s supported boundary should not say no curated import: %q", id, metadata.ImportPolicy.Boundary)
		}
	}

	for _, id := range []ID{Antigravity, Hermes, Qwen, CursorAgent, Pi} {
		metadata := MustMetadata(id)
		if metadata.ImportPolicy.Supported {
			t.Fatalf("%s import policy = supported, want unsupported", id)
		}
		if !strings.Contains(metadata.ImportPolicy.Boundary, "no curated import") {
			t.Fatalf("%s unsupported boundary = %q, want no curated import", id, metadata.ImportPolicy.Boundary)
		}
		if !strings.Contains(metadata.ImportPolicy.Boundary, "contained-only") ||
			!strings.Contains(metadata.ImportPolicy.Boundary, "not synced") {
			t.Fatalf("%s unsupported boundary = %q, want contained-only/not synced", id, metadata.ImportPolicy.Boundary)
		}
	}
}

func TestBuiltinMetadataReturnsCopy(t *testing.T) {
	metadata := BuiltinMetadata()
	metadata[0].Spec.DisplayName = "mutated"

	if got := MustSpec(Claude).DisplayName; got != "Claude Code" {
		t.Fatalf("MustSpec(Claude).DisplayName = %q, want Claude Code", got)
	}
}
