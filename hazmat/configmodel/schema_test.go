package configmodel

import "testing"

func TestParseDockerMode(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want DockerMode
	}{
		{name: "none", raw: "none", want: DockerModeNone},
		{name: "sandbox", raw: "sandbox", want: DockerModeSandbox},
		{name: "auto", raw: "auto", want: DockerModeAuto},
		{name: "case and whitespace normalized", raw: " Sandbox ", want: DockerModeSandbox},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDockerMode(tt.raw)
			if err != nil {
				t.Fatalf("ParseDockerMode(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseDockerMode(%q) = %q, want %q", tt.raw, got, tt.want)
			}
			if !ValidDockerMode(got) {
				t.Fatalf("ValidDockerMode(%q) = false, want true", got)
			}
		})
	}
}

func TestParseDockerModeRejectsUnknownMode(t *testing.T) {
	if got, err := ParseDockerMode("host"); err == nil {
		t.Fatalf("ParseDockerMode(host) = %q, nil error; want error", got)
	}
	if ValidDockerMode(DockerMode("host")) {
		t.Fatalf("ValidDockerMode(host) = true, want false")
	}
}
