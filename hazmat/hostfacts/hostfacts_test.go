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

func TestNewHostFactsNormalizesAndCopies(t *testing.T) {
	input := Facts{
		Platform: Platform{GOOS: " linux ", GOARCH: " arm64 "},
		Harnesses: map[string]HarnessStatus{
			" codex ": {Installed: true},
		},
		IntegrationMarkers: []IntegrationMarker{{Name: " go ", Source: "go.mod"}},
	}

	got, err := New(input)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	input.Harnesses[" codex "] = HarnessStatus{}
	input.IntegrationMarkers[0] = IntegrationMarker{Name: "node"}

	dto := got.DTO()
	if dto.Platform.GOOS != "linux" || dto.Platform.GOARCH != "arm64" {
		t.Fatalf("Platform = %+v", dto.Platform)
	}
	if !dto.Harnesses["codex"].Installed {
		t.Fatalf("Harnesses = %+v", dto.Harnesses)
	}
	if dto.IntegrationMarkers[0].Name != "go" || dto.IntegrationMarkers[0].Source != "go.mod" {
		t.Fatalf("IntegrationMarkers = %+v", dto.IntegrationMarkers)
	}

	dto.Harnesses["codex"] = HarnessStatus{}
	if fresh := got.DTO(); !fresh.Harnesses["codex"].Installed {
		t.Fatal("DTO returned storage aliasing authority")
	}
}

func TestNewHostFactsRejectsInvalidIdentifier(t *testing.T) {
	_, err := New(Facts{
		Harnesses: map[string]HarnessStatus{
			"bad key": {Installed: true},
		},
	})
	if err == nil {
		t.Fatal("expected invalid harness key to be rejected")
	}
}
