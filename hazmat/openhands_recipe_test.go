package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOpenHandsRecipeOnlyBoundaryIsDocumentedAndNotRegistered(t *testing.T) {
	for _, harness := range managedHarnessRegistry {
		if strings.EqualFold(string(harness.Spec.ID), "openhands") {
			t.Fatalf("OpenHands registered as managed harness: %+v", harness.Spec)
		}
	}
	if cmd := findCommandByName(NewRootCommand(), "openhands"); cmd != nil {
		t.Fatalf("unexpected hazmat openhands command registered: use=%q", cmd.Use)
	}

	recipe := normalizeWhitespace(readProjectFile(t, "docs/recipes/openhands-recipe-only.md"))
	for _, want := range []string{
		"recipe-only support",
		"does not provide `hazmat openhands`",
		"does not import host `~/.openhands`",
		"does not expose the host Docker socket",
		"does not treat OpenHands process mode as the isolation layer",
		"future `hazmat openhands` should be a service harness",
	} {
		if !strings.Contains(recipe, want) {
			t.Fatalf("OpenHands recipe missing %q", want)
		}
	}

	recipesIndex := normalizeWhitespace(readProjectFile(t, "docs/recipes/README.md"))
	if !strings.Contains(recipesIndex, "(openhands-recipe-only.md)") {
		t.Fatal("recipes README does not link the OpenHands recipe")
	}
	harnessesDoc := normalizeWhitespace(readProjectFile(t, "docs/harnesses.md"))
	for _, want := range []string{
		"it is not a supported `hazmat <harness>` command today",
		"(recipes/openhands-recipe-only.md)",
	} {
		if !strings.Contains(harnessesDoc, want) {
			t.Fatalf("harnesses doc missing %q", want)
		}
	}
}

func findCommandByName(cmd *cobra.Command, name string) *cobra.Command {
	if cmd.Name() == name {
		return cmd
	}
	for _, child := range cmd.Commands() {
		if found := findCommandByName(child, name); found != nil {
			return found
		}
	}
	return nil
}

func readProjectFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
