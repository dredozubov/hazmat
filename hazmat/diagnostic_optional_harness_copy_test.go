package hazmat

import (
	"strings"
	"testing"
)

func TestOptionalHarnessHealthMessagesUseLifecycleCommands(t *testing.T) {
	messages := []string{
		noManagedHarnessesInstalledMessage(),
		managedHarnessNotInstalledMessage("Claude Code", HarnessClaude),
		managedHarnessNotInstalledMessage("OpenCode", HarnessOpenCode),
		managedHarnessNotInstalledMessage("Codex", HarnessCodex),
	}

	for _, message := range messages {
		if !strings.Contains(message, "optional") {
			t.Fatalf("message = %q, want explicit optional classification", message)
		}
		if strings.Contains(message, "hazmat bootstrap") ||
			strings.Contains(message, "hazmat init") ||
			strings.Contains(message, "hazmat doctor") {
			t.Fatalf("message = %q, want no setup/repair command for optional harness guidance", message)
		}
		if !strings.Contains(message, "hazmat harness") {
			t.Fatalf("message = %q, want harness lifecycle command", message)
		}
	}
}
