//go:build darwin

package darwin

import (
	"strings"
	"testing"
)

func TestLaunchHelperMissingErrorSplitsFreshSetupFromDrift(t *testing.T) {
	err := launchHelperMissingError("/usr/local/libexec/hazmat-launch")
	text := err.Error()
	for _, want := range []string{
		"make install",
		"Fresh host: run hazmat init.",
		"Setup drift: run hazmat doctor --fix",
		"preview with hazmat doctor --dry-run",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("launchHelperMissingError() = %q, want %q", text, want)
		}
	}
	for _, stale := range []string{
		"Then re-run: hazmat init",
		"rerun hazmat init",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("launchHelperMissingError() contains stale init-loop advice %q:\n%s", stale, text)
		}
	}
}
