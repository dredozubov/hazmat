package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type stackMatrixManifest struct {
	SchemaVersion int                       `yaml:"schema_version"`
	Repos         []stackMatrixManifestRepo `yaml:"repos"`
}

type stackMatrixManifestRepo struct {
	ID                  string   `yaml:"id"`
	Wave                int      `yaml:"wave"`
	Track               string   `yaml:"track"`
	DefaultCheck        string   `yaml:"default_check"`
	Repo                string   `yaml:"repo"`
	Ref                 string   `yaml:"ref"`
	Stack               string   `yaml:"stack"`
	ProjectSubdir       string   `yaml:"project_subdir"`
	ExpectedSuggestions []string `yaml:"expected_suggestions"`
	Activate            []string `yaml:"activate"`
	RequiredFormulas    []string `yaml:"required_formulas"`
	SmokeCommands       []string `yaml:"smoke_commands"`
	Notes               string   `yaml:"notes"`
}

func TestStackMatrixManifestIsWellFormed(t *testing.T) {
	path := filepath.Join("..", "testdata", "stack-matrix", "repos.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var manifest stackMatrixManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", manifest.SchemaVersion)
	}
	if len(manifest.Repos) == 0 {
		t.Fatal("manifest contains no repos")
	}

	seenIDs := make(map[string]struct{}, len(manifest.Repos))
	for _, repo := range manifest.Repos {
		if repo.ID == "" {
			t.Fatal("repo id must not be empty")
		}
		if _, dup := seenIDs[repo.ID]; dup {
			t.Fatalf("duplicate repo id %q", repo.ID)
		}
		seenIDs[repo.ID] = struct{}{}

		if repo.Wave < 1 {
			t.Fatalf("%s: wave = %d, want >= 1", repo.ID, repo.Wave)
		}
		switch repo.Track {
		case "required", "informational":
		default:
			t.Fatalf("%s: track = %q, want required or informational", repo.ID, repo.Track)
		}
		switch repo.DefaultCheck {
		case "detect", "smoke":
		default:
			t.Fatalf("%s: default_check = %q, want detect or smoke", repo.ID, repo.DefaultCheck)
		}
		if repo.Repo == "" {
			t.Fatalf("%s: repo must not be empty", repo.ID)
		}
		if len(repo.Ref) != 40 {
			t.Fatalf("%s: ref = %q, want 40-char git SHA", repo.ID, repo.Ref)
		}
		if repo.Stack == "" {
			t.Fatalf("%s: stack must not be empty", repo.ID)
		}
		if repo.ProjectSubdir == "" {
			t.Fatalf("%s: project_subdir must not be empty", repo.ID)
		}
		if len(repo.Activate) == 0 {
			t.Fatalf("%s: activate must not be empty", repo.ID)
		}
		if len(repo.RequiredFormulas) == 0 {
			t.Fatalf("%s: required_formulas must not be empty", repo.ID)
		}
		if repo.Track == "required" && len(repo.SmokeCommands) == 0 {
			t.Fatalf("%s: required repo must include smoke_commands", repo.ID)
		}
		if repo.Notes == "" {
			t.Fatalf("%s: notes must not be empty", repo.ID)
		}
	}
}
