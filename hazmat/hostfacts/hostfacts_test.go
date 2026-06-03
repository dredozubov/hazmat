package hostfacts

import "testing"

func TestFactsNormalizedCopiesMutableFields(t *testing.T) {
	input := Facts{
		Platform:    Platform{GOOS: "darwin", GOARCH: "arm64"},
		AgentHome:   "/Users/agent",
		InvokerHome: "/Users/dev",
		Harnesses: map[string]HarnessStatus{
			"codex": {Installed: true, Version: "1.2.3"},
		},
		KernelProbes: map[string]ProbeFact{
			"landlock": {Available: true, Source: "uname"},
		},
		PlatformProbes: map[string]ProbeFact{
			"seatbelt": {Available: true, Source: "sandbox-exec"},
		},
		IntegrationMarkers: []IntegrationMarker{{Name: "go", Source: "go.mod"}},
	}

	got := input.Normalized()
	input.Harnesses["codex"] = HarnessStatus{}
	input.KernelProbes["landlock"] = ProbeFact{}
	input.PlatformProbes["seatbelt"] = ProbeFact{}
	input.IntegrationMarkers[0] = IntegrationMarker{Name: "node", Source: "package.json"}

	if got.TargetGOOS() != "darwin" || got.Platform.GOARCH != "arm64" {
		t.Fatalf("platform = %+v", got.Platform)
	}
	if !got.Harnesses["codex"].Installed || got.Harnesses["codex"].Version != "1.2.3" {
		t.Fatalf("Harnesses aliased input: %+v", got.Harnesses)
	}
	if !got.KernelProbes["landlock"].Available || got.KernelProbes["landlock"].Source != "uname" {
		t.Fatalf("KernelProbes aliased input: %+v", got.KernelProbes)
	}
	if !got.PlatformProbes["seatbelt"].Available || got.PlatformProbes["seatbelt"].Source != "sandbox-exec" {
		t.Fatalf("PlatformProbes aliased input: %+v", got.PlatformProbes)
	}
	if got.IntegrationMarkers[0].Name != "go" || got.IntegrationMarkers[0].Source != "go.mod" {
		t.Fatalf("IntegrationMarkers aliased input: %+v", got.IntegrationMarkers)
	}
}

func TestForGOOSBuildsPlatformFact(t *testing.T) {
	got := ForGOOS("linux")
	if got.TargetGOOS() != "linux" {
		t.Fatalf("TargetGOOS = %q", got.TargetGOOS())
	}
}
