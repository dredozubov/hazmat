package applecontainer

import (
	"strings"
	"testing"

	applecontainerspec "hazmat/containment/applecontainer"
)

// Real transcripts from the 2026-06-10 host spike (sandboxing-ajmn) on
// apple/container 1.0.0, macOS 26.5.
const (
	spikeVersionJSON = `[{"appName":"container","buildType":"release","commit":"ee848e3ebfd7c73b04dd419683be54fb450b8779","version":"1.0.0"},{"appName":"container-apiserver","buildType":"release","commit":"ee848e3ebfd7c73b04dd419683be54fb450b8779","version":"container-apiserver version 1.0.0 (build: release, commit: ee848e3)"}]`
	spikeStatusJSON  = `{"apiServerAppName":"container-apiserver","apiServerBuild":"release","apiServerCommit":"ee848e3","apiServerVersion":"container-apiserver version 1.0.0","appRoot":"/Users/dr/Library/Application Support/com.apple.container/","installRoot":"/usr/local/","status":"running"}`
	// As the agent user the apiserver is unreachable (spike F1).
	spikeAgentStatus = `{"apiServerAppName":"","apiServerBuild":"","apiServerCommit":"","apiServerVersion":"","appRoot":"","installRoot":"","status":"not running"}`
)

func TestParseSystemVersionSpikeTranscript(t *testing.T) {
	version, supported := parseSystemVersion(spikeVersionJSON)
	if version != "1.0.0" || !supported {
		t.Fatalf("parseSystemVersion = %q, %v", version, supported)
	}
}

func TestParseSystemVersionRejectsUnsupported(t *testing.T) {
	for raw, wantSupported := range map[string]bool{
		`[{"appName":"container","version":"0.11.0"}]`:                false,
		`[{"appName":"container","version":"v2.1.0"}]`:                true,
		`[{"appName":"container-apiserver","version":"1.0.0 prose"}]`: false,
		`not json`: false,
		`[]`:       false,
	} {
		if _, supported := parseSystemVersion(raw); supported != wantSupported {
			t.Fatalf("parseSystemVersion(%q) supported = %v, want %v", raw, supported, wantSupported)
		}
	}
}

func TestParseSystemStatusSpikeTranscripts(t *testing.T) {
	if !parseSystemStatus(spikeStatusJSON) {
		t.Fatal("running status must be healthy")
	}
	if parseSystemStatus(spikeAgentStatus) {
		t.Fatal("agent-user 'not running' status must be unhealthy (spike F1)")
	}
	if parseSystemStatus("not json") {
		t.Fatal("unparseable status must be unhealthy")
	}
}

func TestParseMajorVersion(t *testing.T) {
	if got := parseMajorVersion("26.5\n"); got != 26 {
		t.Fatalf("parseMajorVersion = %d", got)
	}
	if got := parseMajorVersion("garbage"); got != 0 {
		t.Fatalf("parseMajorVersion(garbage) = %d", got)
	}
}

type fakeRunner struct {
	outputs map[string]string
}

func (f fakeRunner) Output(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	out, ok := f.outputs[key]
	if !ok {
		return "", &keyError{key}
	}
	return out, nil
}

type keyError struct{ key string }

func (e *keyError) Error() string { return "no transcript for " + e.key }

func TestProbeHostHealthy(t *testing.T) {
	if _, err := HostRunner().Output("/usr/bin/true"); err != nil {
		t.Skipf("host exec unavailable: %v", err)
	}
	runner := fakeRunner{outputs: map[string]string{
		"/usr/bin/sw_vers -productVersion":                "26.5\n",
		ApprovedCLIPath + " system version --format json": spikeVersionJSON,
		ApprovedCLIPath + " system status --format json":  spikeStatusJSON,
	}}
	report := ProbeHost(runner, "darwin", "arm64")
	if report.MacOSMajorVersion != 26 {
		t.Fatalf("MacOSMajorVersion = %d", report.MacOSMajorVersion)
	}
	// CLIPath depends on the real filesystem; the rest of the report only
	// populates when the CLI exists at the approved path.
	if report.CLIPath != "" {
		if report.CLIVersion != "1.0.0" || !report.CLIVersionSupported || !report.APIServerHealthy {
			t.Fatalf("report = %+v", report)
		}
	}
}

func TestRunRefusesPlanOnlySpecs(t *testing.T) {
	_, err := Run(applecontainerspec.LaunchSpec{Phase: applecontainerspec.PhasePlanOnly}, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("err = %v, want plan-only refusal", err)
	}
}

func TestRunRefusesSpecsWithCapabilityGaps(t *testing.T) {
	spec := applecontainerspec.LaunchSpec{
		Phase: applecontainerspec.PhaseExperimental,
		CapabilityGaps: []applecontainerspec.CapabilityGap{{
			Code:    "container-cli-missing",
			Message: "container CLI was not found at an approved absolute path",
		}},
	}
	_, err := Run(spec, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "admission failed") {
		t.Fatalf("err = %v, want admission refusal", err)
	}
	if !strings.Contains(err.Error(), "container-cli-missing") {
		t.Fatalf("err = %v, want gap listed", err)
	}
}

func TestGateError(t *testing.T) {
	err := GateError()
	for _, want := range []string{
		EnvExperimentalGate,
		"invoking macOS user",
		"Host account isolation is",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("GateError missing %q: %v", want, err)
		}
	}
}
