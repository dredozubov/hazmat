package configmodel

import (
	"strings"
	"testing"
)

func TestProjectSSHConfigNormalizedKeysCopiesHosts(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{
			Name:           "github",
			PrivateKeyPath: "/keys/github",
			Hosts:          []string{"github.com"},
		}},
	}

	got := cfg.NormalizedKeys()
	got[0].Hosts[0] = "mutated.example"
	if cfg.Keys[0].Hosts[0] != "github.com" {
		t.Fatalf("NormalizedKeys mutated source hosts: %v", cfg.Keys[0].Hosts)
	}
}

func TestValidateProjectSSHConfigRejectsOverlappingHosts(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "github", PrivateKeyPath: "/keys/github", Hosts: []string{"github.com"}},
			{Name: "github-work", PrivateKeyPath: "/keys/work", Hosts: []string{"*.com"}},
		},
	}

	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "both match host") {
		t.Fatalf("ValidateProjectSSHConfig overlap error = %v", err)
	}
}

func TestValidateProjectSSHProfileRefsRejectsInheritedOverlap(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "shared", Profile: "github"},
			{Name: "inline", PrivateKeyPath: "/keys/github", Hosts: []string{"github.com"}},
		},
	}
	profiles := map[string]SSHProfile{
		"github": {PrivateKeyPath: "/keys/shared", DefaultHosts: []string{"github.com"}},
	}

	err := ValidateProjectSSHProfileRefs(cfg, profiles)
	if err == nil || !strings.Contains(err.Error(), "after profile inheritance") {
		t.Fatalf("ValidateProjectSSHProfileRefs overlap error = %v", err)
	}
}

func TestDetectLegacyFlatSSHReportsMigrationSnippet(t *testing.T) {
	err := DetectLegacyFlatSSH("/tmp/project", ProjectSSHConfig{
		PrivateKeyPath: "/keys/github",
		KnownHostsPath: "/keys/known_hosts",
	})
	if err == nil {
		t.Fatal("DetectLegacyFlatSSH accepted legacy flat SSH config")
	}
	for _, want := range []string{"retired single-key SSH shape", "keys:", "hosts: [github.com]"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("DetectLegacyFlatSSH error = %q, missing %q", err, want)
		}
	}
}
