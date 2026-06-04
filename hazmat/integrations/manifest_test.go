package integrations

import (
	"slices"
	"strings"
	"testing"
)

func TestLoadSpecRejectsCredentialEnvKey(t *testing.T) {
	_, err := LoadSpec([]byte(`
integration:
  name: bad
  version: 1
session:
  env_passthrough:
    - OPENAI_API_KEY
`))
	if err == nil {
		t.Fatal("expected credential-shaped env key to be rejected")
	}
	if !strings.Contains(err.Error(), "credential/capability-shaped") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionForPlatformCopiesAndOverlays(t *testing.T) {
	session := Session{
		ReadDirs:       []string{"/common"},
		EnvPassthrough: []string{"GOPATH"},
		Platforms: map[string]PlatformSession{
			"linux": {
				ReadDirs:       []string{"/linux"},
				EnvPassthrough: []string{"GOROOT"},
			},
		},
	}

	got := SessionForPlatform(session, "linux")
	if !slices.Equal(got.ReadDirs, []string{"/common", "/linux"}) {
		t.Fatalf("ReadDirs = %v", got.ReadDirs)
	}
	if !slices.Equal(got.EnvPassthrough, []string{"GOPATH", "GOROOT"}) {
		t.Fatalf("EnvPassthrough = %v", got.EnvPassthrough)
	}
	got.ReadDirs[0] = "/mutated"
	if session.ReadDirs[0] != "/common" {
		t.Fatal("SessionForPlatform returned storage aliasing input")
	}
}

func TestNewAuthoritySpecNormalizesAndCopies(t *testing.T) {
	input := Spec{
		Meta: Meta{Name: " go ", Version: 1, Description: " Go tools "},
		Session: Session{
			ReadDirs:       []string{"/common"},
			EnvPassthrough: []string{" gopath "},
			Platforms: map[string]PlatformSession{
				" LINUX ": {
					ReadDirs:       []string{"/linux"},
					EnvPassthrough: []string{" goroot "},
				},
			},
		},
		Commands: map[string]string{"build": "go build ./..."},
	}

	authority, err := NewAuthoritySpec(input)
	if err != nil {
		t.Fatalf("NewAuthoritySpec: %v", err)
	}
	input.Session.ReadDirs[0] = "/mutated"
	input.Session.Platforms[" LINUX "] = PlatformSession{}
	input.Commands["build"] = "mutated"

	dto := authority.DTO()
	if dto.Meta.Name != "go" || dto.Meta.Description != "Go tools" {
		t.Fatalf("Meta = %+v", dto.Meta)
	}
	if !slices.Equal(dto.Session.EnvPassthrough, []string{"GOPATH"}) {
		t.Fatalf("EnvPassthrough = %v", dto.Session.EnvPassthrough)
	}
	if !slices.Equal(dto.Session.Platforms["linux"].EnvPassthrough, []string{"GOROOT"}) {
		t.Fatalf("linux env = %v", dto.Session.Platforms["linux"].EnvPassthrough)
	}
	if dto.Commands["build"] != "go build ./..." {
		t.Fatalf("Commands = %v", dto.Commands)
	}

	dto.Commands["build"] = "mutated"
	if fresh := authority.DTO(); fresh.Commands["build"] != "go build ./..." {
		t.Fatal("DTO returned storage aliasing authority")
	}
}

func TestMergeResolvedDedupesAndSurfacesRegistryKeys(t *testing.T) {
	items := []Resolved{
		{
			Spec: Spec{
				Meta: Meta{Name: "go", Version: 1},
				Session: Session{
					ReadDirs:       []string{"/declared"},
					EnvPassthrough: []string{"GOPROXY", "GOPATH"},
				},
				Backup:   Backup{Excludes: []string{"vendor/"}},
				Warnings: []string{"warning"},
			},
			AdditionalReadDirs: []string{"/runtime"},
			ResolvedEnv:        map[string]string{"GOROOT": "/go"},
		},
		{
			Spec: Spec{
				Meta:     Meta{Name: "node", Version: 1},
				Backup:   Backup{Excludes: []string{"vendor/", ".next/"}},
				Warnings: []string{"warning", "node warning"},
			},
			AdditionalReadDirs: []string{"/runtime", "/node"},
			AdditionalWarnings: []string{"runtime warning"},
		},
	}

	got, err := MergeResolved(items, MergeOptions{
		Platform: "darwin",
		ValidateReadDirs: func(spec Spec, platform string) ([]string, error) {
			return []string{"/declared"}, nil
		},
		Getenv: func(key string) string {
			switch key {
			case "GOPROXY":
				return "https://proxy.example"
			case "GOPATH":
				return "/gopath"
			default:
				return ""
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.ReadDirs, []string{"/declared", "/runtime", "/node"}) {
		t.Fatalf("ReadDirs = %v", got.ReadDirs)
	}
	if got.EnvPassthrough["GOPROXY"] != "https://proxy.example" ||
		got.EnvPassthrough["GOPATH"] != "/gopath" ||
		got.EnvPassthrough["GOROOT"] != "/go" {
		t.Fatalf("EnvPassthrough = %v", got.EnvPassthrough)
	}
	if !slices.Equal(got.RegistryKeys, []string{"GOPROXY"}) {
		t.Fatalf("RegistryKeys = %v", got.RegistryKeys)
	}
	if !slices.Equal(got.Excludes, []string{"vendor/", ".next/"}) {
		t.Fatalf("Excludes = %v", got.Excludes)
	}
	if !slices.Equal(got.Warnings, []string{"warning", "node warning", "runtime warning"}) {
		t.Fatalf("Warnings = %v", got.Warnings)
	}
}

func TestNewResolvedAuthorityRejectsDuplicateResolvedEnvAfterNormalization(t *testing.T) {
	_, err := NewResolvedAuthority(Resolved{
		Spec: Spec{Meta: Meta{Name: "go", Version: 1}},
		ResolvedEnv: map[string]string{
			" gopath ": "/one",
			"GOPATH":   "/two",
		},
	})
	if err == nil {
		t.Fatal("expected duplicate resolved env key to be rejected")
	}
}

func TestMergeResolvedRejectsInvalidPlatform(t *testing.T) {
	_, err := MergeResolved([]Resolved{
		{Spec: Spec{Meta: Meta{Name: "go", Version: 1}}},
	}, MergeOptions{Platform: "windows"})
	if err == nil {
		t.Fatal("expected unsupported platform to be rejected")
	}
}
