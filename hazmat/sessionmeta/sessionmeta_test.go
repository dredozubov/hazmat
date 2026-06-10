package sessionmeta

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestParseNetworkMode(t *testing.T) {
	for _, raw := range []string{"", "default", "DEFAULT"} {
		got, err := ParseNetworkMode(raw)
		if err != nil {
			t.Fatalf("ParseNetworkMode(%q): %v", raw, err)
		}
		if got != NetworkDefault {
			t.Fatalf("ParseNetworkMode(%q) = %q, want default", raw, got)
		}
	}
	got, err := ParseNetworkMode("none")
	if err != nil {
		t.Fatalf("ParseNetworkMode(none): %v", err)
	}
	if got != NetworkNone {
		t.Fatalf("ParseNetworkMode(none) = %q, want none", got)
	}
	if _, err := ParseNetworkMode("off"); err == nil {
		t.Fatal("expected invalid network mode to be rejected")
	}
}

func TestBuildLaunchMetadataReportsNetworkNone(t *testing.T) {
	meta := BuildLaunchMetadata(LaunchMetadataInput{
		Target:      "codex",
		Mode:        ModeNative,
		ProjectDir:  "/tmp/myproject",
		NetworkMode: NetworkNone,
	})

	if meta.Kind != "hazmat.session" {
		t.Fatalf("Kind = %q, want hazmat.session", meta.Kind)
	}
	if meta.Mode != "native" || meta.ModeLabel != "Native containment" {
		t.Fatalf("mode fields = %q/%q, want native/Native containment", meta.Mode, meta.ModeLabel)
	}
	if meta.NetworkPolicy.Requested != "none" || meta.NetworkPolicy.Effective != "none" {
		t.Fatalf("NetworkPolicy = %+v, want requested/effective none", meta.NetworkPolicy)
	}
	if !meta.NetworkPolicy.Enforced || !meta.NetworkPolicy.DenyAllEgress {
		t.Fatalf("NetworkPolicy = %+v, want enforced deny-all egress", meta.NetworkPolicy)
	}
	if !slices.Contains(meta.NetworkPolicy.Denied, "outbound-ipv4") ||
		!slices.Contains(meta.NetworkPolicy.Denied, "outbound-ipv6") ||
		!slices.Contains(meta.NetworkPolicy.Denied, "dns") {
		t.Fatalf("NetworkPolicy.Denied = %v, want IPv4, IPv6, and DNS", meta.NetworkPolicy.Denied)
	}
	if meta.NetworkPolicy.CleanupRequired {
		t.Fatalf("CleanupRequired = true, want false for native seatbelt network mode")
	}
}

func TestBuildNetworkPolicyMetadataForDockerSandbox(t *testing.T) {
	meta := BuildNetworkPolicyMetadata(NetworkNone, ModeDockerSandbox)

	if meta.Requested != "none" || meta.Effective != "sandbox-profile" {
		t.Fatalf("NetworkPolicy = %+v, want requested none and sandbox-profile effective", meta)
	}
	if meta.Enforcement != "docker-sandbox-network-profile" || !meta.CleanupRequired {
		t.Fatalf("NetworkPolicy = %+v, want docker sandbox enforcement and cleanup", meta)
	}
	if meta.DenyAllEgress || len(meta.Denied) != 0 {
		t.Fatalf("NetworkPolicy = %+v, docker sandbox metadata should defer details to sandbox profile", meta)
	}
}

func TestMarshalLaunchMetadataJSON(t *testing.T) {
	raw, err := MarshalLaunchMetadataJSON(LaunchMetadataInput{
		Target:      "shell",
		Mode:        ModeNative,
		ProjectDir:  "/tmp/project",
		NetworkMode: NetworkDefault,
	})
	if err != nil {
		t.Fatalf("MarshalLaunchMetadataJSON: %v", err)
	}

	var decoded LaunchMetadata
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("metadata JSON did not decode: %v\n%s", err, raw)
	}
	if decoded.FormatVersion != LaunchMetadataFormatVersion || decoded.Target != "shell" {
		t.Fatalf("decoded metadata = %+v", decoded)
	}
}

func TestBuildNetworkPolicyMetadataForAppleContainer(t *testing.T) {
	meta := BuildNetworkPolicyMetadata(NetworkDefault, ModeAppleContainer)

	if meta.Requested != "default" || meta.Effective != "default" {
		t.Fatalf("NetworkPolicy = %+v, want default requested and effective", meta)
	}
	if meta.Enforcement != "apple-container-vm-network" {
		t.Fatalf("NetworkPolicy = %+v, want apple-container-vm-network enforcement", meta)
	}
	if meta.DenyAllEgress || len(meta.Denied) != 0 {
		t.Fatalf("NetworkPolicy = %+v, apple container must not claim deny rules", meta)
	}
}

func TestAppleContainerModeLabels(t *testing.T) {
	if ModeAppleContainer.Label() != "Apple Container" {
		t.Fatalf("Label = %q", ModeAppleContainer.Label())
	}
	want := "default (outbound allowed, Apple Container VM network)"
	if got := NetworkContractLabel(NetworkDefault, ModeAppleContainer); got != want {
		t.Fatalf("NetworkContractLabel = %q, want %q", got, want)
	}
}
