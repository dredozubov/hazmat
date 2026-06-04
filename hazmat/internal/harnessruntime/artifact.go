package harnessruntime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type ArtifactKind string

const (
	ArtifactFile ArtifactKind = "file"
	ArtifactDir  ArtifactKind = "dir"
)

type ArtifactOwnership string

const (
	ArtifactOwnedByHazmat ArtifactOwnership = "hazmat-owned"
)

type ArtifactSymlinkPolicy string

const (
	ArtifactSymlinkDisallowed ArtifactSymlinkPolicy = "disallowed"
	ArtifactSymlinkAllowed    ArtifactSymlinkPolicy = "allowed"
)

type Artifact struct {
	Path            string
	Kind            ArtifactKind
	Description     string
	Ownership       ArtifactOwnership
	SymlinkPolicy   ArtifactSymlinkPolicy
	PackageManager  string
	PackageName     string
	CreatedByUpdate bool
}

type ArtifactStatus struct {
	Artifact    Artifact
	Exists      bool
	Symlink     bool
	PackageName string
	Drift       string
}

type CommandReader = func(args ...string) (string, error)

type ArtifactRemover = func(reason string, args ...string) error

func FileArtifact(path, description string) Artifact {
	return Artifact{
		Path:            path,
		Kind:            ArtifactFile,
		Description:     description,
		Ownership:       ArtifactOwnedByHazmat,
		SymlinkPolicy:   ArtifactSymlinkAllowed,
		CreatedByUpdate: true,
	}
}

func DirArtifact(path, description string) Artifact {
	return Artifact{
		Path:            path,
		Kind:            ArtifactDir,
		Description:     description,
		Ownership:       ArtifactOwnedByHazmat,
		SymlinkPolicy:   ArtifactSymlinkDisallowed,
		CreatedByUpdate: true,
	}
}

func NpmPackageDirArtifact(path, packageName, description string) Artifact {
	artifact := DirArtifact(path, description)
	artifact.PackageManager = "npm"
	artifact.PackageName = packageName
	return artifact
}

func SymlinkArtifact(path, description string) Artifact {
	artifact := FileArtifact(path, description)
	artifact.SymlinkPolicy = ArtifactSymlinkAllowed
	return artifact
}

func RegularFileArtifact(path, description string) Artifact {
	artifact := FileArtifact(path, description)
	artifact.SymlinkPolicy = ArtifactSymlinkDisallowed
	return artifact
}

func InspectArtifact(read CommandReader, agentHome string, artifact Artifact) ArtifactStatus {
	artifact = NormalizeArtifact(artifact)
	status := ArtifactStatus{Artifact: artifact}
	if err := ValidateArtifactPath(agentHome, artifact.Path); err != nil {
		status.Drift = err.Error()
		return status
	}

	exists := artifactExists(read, artifact.Path)
	status.Exists = exists
	if !exists {
		return status
	}

	status.Symlink = artifactIsSymlink(read, artifact.Path)
	if status.Symlink && artifact.SymlinkPolicy != ArtifactSymlinkAllowed {
		status.Drift = "symlink not allowed"
		return status
	}

	switch artifact.Kind {
	case ArtifactFile:
		if !artifactIsFile(read, artifact.Path) {
			status.Drift = "expected file"
		}
	case ArtifactDir:
		if status.Symlink {
			status.Drift = "expected directory, got symlink"
			return status
		}
		if !artifactIsDir(read, artifact.Path) {
			status.Drift = "expected directory"
		}
	default:
		status.Drift = "unknown artifact kind " + string(artifact.Kind)
	}
	if status.Drift != "" {
		return status
	}
	if artifact.PackageManager == "npm" && artifact.PackageName != "" {
		packageName, err := InspectNpmPackageName(read, artifact.Path)
		if err != nil {
			status.Drift = err.Error()
			return status
		}
		status.PackageName = packageName
		if packageName != artifact.PackageName {
			status.Drift = fmt.Sprintf("expected npm package %s, got %s", artifact.PackageName, packageName)
		}
	}
	return status
}

func NormalizeArtifact(artifact Artifact) Artifact {
	if artifact.Ownership == "" {
		artifact.Ownership = ArtifactOwnedByHazmat
	}
	if artifact.SymlinkPolicy == "" {
		artifact.SymlinkPolicy = ArtifactSymlinkDisallowed
	}
	return artifact
}

func ValidateArtifactPath(agentHome, path string) error {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(agentHome) || !usesManagedAgentPath(agentHome, clean) {
		return fmt.Errorf("path is outside the managed agent home")
	}
	return nil
}

func RemoveArtifact(remove ArtifactRemover, agentHome string, artifact Artifact) error {
	artifact = NormalizeArtifact(artifact)
	if err := ValidateArtifactPath(agentHome, artifact.Path); err != nil {
		return err
	}
	flag := "-f"
	if artifact.Kind == ArtifactDir {
		flag = "-rf"
	}
	if err := remove("remove "+artifact.Description, "/bin/rm", flag, "--", artifact.Path); err != nil {
		return fmt.Errorf("remove %s: %w", artifact.Path, err)
	}
	return nil
}

func FormatArtifactStatus(status ArtifactStatus) string {
	state := "missing"
	if status.Exists {
		state = "present"
	}
	if status.Drift != "" {
		state = "drifted: " + status.Drift
	}
	artifact := NormalizeArtifact(status.Artifact)
	parts := []string{
		artifact.Description,
		string(artifact.Kind),
		string(artifact.Ownership),
		state,
	}
	if artifact.SymlinkPolicy == ArtifactSymlinkAllowed {
		parts = append(parts, "symlink allowed")
	}
	if artifact.PackageManager != "" {
		packageName := artifact.PackageName
		if packageName == "" {
			packageName = "unknown"
		}
		parts = append(parts, artifact.PackageManager+":"+packageName)
	}
	if artifact.CreatedByUpdate {
		parts = append(parts, "created by update")
	} else {
		parts = append(parts, "verified only")
	}
	return strings.Join(parts, ", ")
}

func artifactExists(read CommandReader, path string) bool {
	if _, err := read("/usr/bin/test", "-e", path); err == nil {
		return true
	}
	if _, err := read("/usr/bin/test", "-L", path); err == nil {
		return true
	}
	return false
}

func artifactIsSymlink(read CommandReader, path string) bool {
	_, err := read("/usr/bin/test", "-L", path)
	return err == nil
}

func artifactIsFile(read CommandReader, path string) bool {
	if _, err := read("/usr/bin/test", "-f", path); err == nil {
		return true
	}
	if artifactIsSymlink(read, path) {
		return true
	}
	return false
}

func artifactIsDir(read CommandReader, path string) bool {
	_, err := read("/usr/bin/test", "-d", path)
	return err == nil
}

func InspectNpmPackageName(read CommandReader, packageDir string) (string, error) {
	packageJSON := filepath.Join(packageDir, "package.json")
	raw, err := read("/bin/cat", packageJSON)
	if err != nil {
		return "", fmt.Errorf("inspect npm package metadata %s: %w", packageJSON, err)
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("parse npm package metadata %s: %w", packageJSON, err)
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return "", fmt.Errorf("npm package metadata %s has no name", packageJSON)
	}
	return name, nil
}

func usesManagedAgentPath(home, target string) bool {
	rel, err := filepath.Rel(home, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
