package hazmat

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHarnessSmokeCoversEveryManagedHarness(t *testing.T) {
	cmd := exec.Command("bash", "../scripts/e2e-harness-smoke.sh", "--list-harnesses")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list harness smoke coverage: %v", err)
	}

	covered := map[HarnessID]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		covered[HarnessID(line)] = true
	}

	managed := map[HarnessID]bool{}
	for _, harness := range managedHarnessRegistry {
		id := harness.Spec.ID
		managed[id] = true
		if !covered[id] {
			t.Fatalf("managed harness %q is missing from scripts/e2e-harness-smoke.sh; future harnesses must add synthetic e2e smoke coverage before landing", id)
		}
	}
	for id := range covered {
		if !managed[id] {
			t.Fatalf("scripts/e2e-harness-smoke.sh declares unknown harness %q", id)
		}
	}
}
