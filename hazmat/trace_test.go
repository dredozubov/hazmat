//go:build hazmat_debug

package hazmat

import (
	"testing"
)

func TestTraceHarnessSpecsCoverManagedHarnesses(t *testing.T) {
	got := make(map[HarnessID]bool)
	for _, spec := range supportedTraceHarnessSpecs() {
		got[HarnessID(spec.ID)] = true
		if spec.CommandName == "" {
			t.Fatalf("%s CommandName is empty", spec.ID)
		}
		if len(spec.ProcessFilters) == 0 {
			t.Fatalf("%s ProcessFilters is empty", spec.ID)
		}
		if len(spec.SampleArgs) == 0 {
			t.Fatalf("%s SampleArgs is empty", spec.ID)
		}
		if spec.Explain == nil {
			t.Fatalf("%s Explain is nil", spec.ID)
		}
	}
	for _, managed := range managedHarnessRegistry {
		if !got[managed.Spec.ID] {
			t.Fatalf("managed harness %q is missing from trace harness specs", managed.Spec.ID)
		}
	}
}
