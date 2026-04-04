package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	stackMatrixSchemaVersion       = 1
	stackMatrixTrackRequired       = "required"
	stackMatrixTrackInformational  = "informational"
	stackMatrixDefaultCheckDetect  = "detect"
	stackMatrixDefaultCheckSmoke   = "smoke"
	stackMatrixDefaultManifestPath = "testdata/stack-matrix/repos.yaml"
)

type stackMatrixManifest struct {
	SchemaVersion int               `yaml:"schema_version"`
	Repos         []stackMatrixRepo `yaml:"repos"`
}

type stackMatrixRepo struct {
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

type stackMatrixSelection struct {
	Track string
	Wave  int
	IDs   map[string]struct{}
}

func loadStackMatrixManifest(path string) (stackMatrixManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return stackMatrixManifest{}, fmt.Errorf("read %s: %w", path, err)
	}

	var manifest stackMatrixManifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&manifest); err != nil {
		return stackMatrixManifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validateStackMatrixManifest(manifest); err != nil {
		return stackMatrixManifest{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return manifest, nil
}

func validateStackMatrixManifest(manifest stackMatrixManifest) error {
	if manifest.SchemaVersion != stackMatrixSchemaVersion {
		return fmt.Errorf("schema_version = %d, want %d", manifest.SchemaVersion, stackMatrixSchemaVersion)
	}
	if len(manifest.Repos) == 0 {
		return fmt.Errorf("manifest contains no repos")
	}

	seenIDs := make(map[string]struct{}, len(manifest.Repos))
	for _, repo := range manifest.Repos {
		if repo.ID == "" {
			return fmt.Errorf("repo id must not be empty")
		}
		if _, dup := seenIDs[repo.ID]; dup {
			return fmt.Errorf("duplicate repo id %q", repo.ID)
		}
		seenIDs[repo.ID] = struct{}{}

		if repo.Wave < 1 {
			return fmt.Errorf("%s: wave = %d, want >= 1", repo.ID, repo.Wave)
		}
		switch repo.Track {
		case stackMatrixTrackRequired, stackMatrixTrackInformational:
		default:
			return fmt.Errorf("%s: track = %q, want %q or %q",
				repo.ID, repo.Track, stackMatrixTrackRequired, stackMatrixTrackInformational)
		}
		switch repo.DefaultCheck {
		case stackMatrixDefaultCheckDetect, stackMatrixDefaultCheckSmoke:
		default:
			return fmt.Errorf("%s: default_check = %q, want %q or %q",
				repo.ID, repo.DefaultCheck, stackMatrixDefaultCheckDetect, stackMatrixDefaultCheckSmoke)
		}
		if repo.Repo == "" {
			return fmt.Errorf("%s: repo must not be empty", repo.ID)
		}
		if len(repo.Ref) != 40 {
			return fmt.Errorf("%s: ref = %q, want 40-char git SHA", repo.ID, repo.Ref)
		}
		if repo.Stack == "" {
			return fmt.Errorf("%s: stack must not be empty", repo.ID)
		}
		if repo.ProjectSubdir == "" {
			return fmt.Errorf("%s: project_subdir must not be empty", repo.ID)
		}
		cleanSubdir := filepath.Clean(repo.ProjectSubdir)
		if filepath.IsAbs(cleanSubdir) || cleanSubdir == ".." ||
			strings.HasPrefix(cleanSubdir, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("%s: project_subdir %q must stay within the checkout", repo.ID, repo.ProjectSubdir)
		}
		if len(repo.Activate) == 0 {
			return fmt.Errorf("%s: activate must not be empty", repo.ID)
		}
		if len(repo.RequiredFormulas) == 0 {
			return fmt.Errorf("%s: required_formulas must not be empty", repo.ID)
		}
		if repo.Track == stackMatrixTrackRequired && len(repo.SmokeCommands) == 0 {
			return fmt.Errorf("%s: required repo must include smoke_commands", repo.ID)
		}
		if repo.Notes == "" {
			return fmt.Errorf("%s: notes must not be empty", repo.ID)
		}
	}
	return nil
}

func selectStackMatrixRepos(manifest stackMatrixManifest, selection stackMatrixSelection) []stackMatrixRepo {
	selected := make([]stackMatrixRepo, 0, len(manifest.Repos))
	for _, repo := range manifest.Repos {
		if selection.Track != "" && selection.Track != "all" && repo.Track != selection.Track {
			continue
		}
		if selection.Wave > 0 && repo.Wave != selection.Wave {
			continue
		}
		if len(selection.IDs) > 0 {
			if _, ok := selection.IDs[repo.ID]; !ok {
				continue
			}
		}
		selected = append(selected, repo)
	}
	return selected
}

func defaultStackMatrixManifestPath() string {
	candidates := []string{
		filepath.Clean(stackMatrixDefaultManifestPath),
		filepath.Clean(filepath.Join("..", stackMatrixDefaultManifestPath)),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}
