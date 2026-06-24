package hazmat

import (
	"reflect"
	"testing"
)

func TestFormatInstalledHarnessNamesForStatusReadsStateVersion(t *testing.T) {
	installed := []ManagedHarness{
		{Spec: harnessMetadata(HarnessClaude).Spec},
		{Spec: harnessMetadata(HarnessCodex).Spec},
		{Spec: harnessMetadata(HarnessHermes).Spec},
	}
	state := HazmatState{
		Harnesses: map[HarnessID]HarnessState{
			HarnessClaude: {StateVersion: claudeHarnessStateVersion},
			HarnessCodex:  {StateVersion: "0"},
		},
	}

	got := formatInstalledHarnessNamesForStatus(installed, state)
	want := []string{
		"Claude Code",
		"Codex (state v0; want v1)",
		"Hermes (state missing)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatInstalledHarnessNamesForStatus() = %#v, want %#v", got, want)
	}
}
