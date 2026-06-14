// Package docker compiles Hazmat's backend-neutral containment contract into a
// Docker Sandbox launch spec. It does not approve, create, run, or clean up
// Docker sandboxes.
package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"hazmat/containment"
	"hazmat/sessionbackend"
)

var sandboxNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

type PolicyProfile struct {
	Name       string   `json:"name"`
	Policy     string   `json:"policy"`
	AllowHosts []string `json:"allow_hosts,omitempty"`
}

type CompileOptions struct {
	Agent       string
	BackendPlan sessionbackend.Plan
	Profile     PolicyProfile
}

type LaunchSpec struct {
	Name           string              `json:"name"`
	Agent          string              `json:"agent"`
	ProjectDir     string              `json:"project_dir"`
	BackendPlan    sessionbackend.Plan `json:"backend_plan"`
	Profile        PolicyProfile       `json:"profile"`
	MountReadDirs  []string            `json:"mount_read_dirs,omitempty"`
	MountWriteDirs []string            `json:"mount_write_dirs,omitempty"`
}

// DockerCreateArgs returns the Docker Sandbox create argv represented by spec.
func DockerCreateArgs(spec LaunchSpec) []string {
	createArgs := []string{"sandbox", "create", "--name", spec.Name, spec.Agent, spec.ProjectDir}
	createArgs = append(createArgs, spec.MountWriteDirs...)
	for _, dir := range spec.MountReadDirs {
		createArgs = append(createArgs, dir+":ro")
	}
	return createArgs
}

// NetworkProxyArgs returns the Docker Sandbox network proxy argv represented by spec.
func NetworkProxyArgs(spec LaunchSpec) []string {
	networkArgs := []string{"sandbox", "network", "proxy", spec.Name, "--policy", spec.Profile.Policy}
	for _, host := range spec.Profile.AllowHosts {
		networkArgs = append(networkArgs, "--allow-host", host)
	}
	return networkArgs
}

func Compile(contract containment.Contract, opts CompileOptions) (LaunchSpec, error) {
	if err := contract.Validate(); err != nil {
		return LaunchSpec{}, fmt.Errorf("invalid containment contract: %w", err)
	}

	projectDir := contract.ProjectPath()
	writeDirs := contract.ReadWritePaths()
	readDirs := contract.ReadOnlyPaths()

	var mountWriteDirs []string
	writeSeen := make(map[string]struct{})
	for _, dir := range writeDirs {
		if containment.IsWithinDir(projectDir, dir) {
			continue
		}
		if containment.IsWithinDir(dir, projectDir) {
			for _, s := range expandAncestorDirExcludingPaths(dir, []string{projectDir}) {
				if pathCoveredByDirs(s, mountWriteDirs) {
					continue
				}
				if _, dup := writeSeen[s]; dup {
					continue
				}
				writeSeen[s] = struct{}{}
				mountWriteDirs = append(mountWriteDirs, s)
			}
			continue
		}
		covered := false
		for _, other := range writeDirs {
			if other != dir && containment.IsWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if _, dup := writeSeen[dir]; dup {
			continue
		}
		writeSeen[dir] = struct{}{}
		mountWriteDirs = append(mountWriteDirs, dir)
	}

	var mountReadDirs []string
	seen := make(map[string]struct{})
	for _, dir := range readDirs {
		if containment.IsWithinDir(projectDir, dir) {
			continue
		}
		if skip, _ := readDirWriteOverlap(dir, mountWriteDirs); skip {
			continue
		}

		excluded := excludedDescendants(dir, projectDir, mountWriteDirs)
		if len(excluded) > 0 {
			for _, s := range expandAncestorDirExcludingPaths(dir, excluded) {
				if skip, _ := readDirWriteOverlap(s, mountWriteDirs); skip {
					continue
				}
				if _, dup := seen[s]; !dup {
					mountReadDirs = append(mountReadDirs, s)
					seen[s] = struct{}{}
				}
			}
			continue
		}
		covered := false
		for _, other := range readDirs {
			if other != dir && containment.IsWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if _, dup := seen[dir]; !dup {
			mountReadDirs = append(mountReadDirs, dir)
			seen[dir] = struct{}{}
		}
	}

	profile := PolicyProfile{
		Name:       opts.Profile.Name,
		Policy:     opts.Profile.Policy,
		AllowHosts: append([]string(nil), opts.Profile.AllowHosts...),
	}
	return LaunchSpec{
		Name:           SandboxName(opts.Agent, projectDir, mountReadDirs, mountWriteDirs, profile.Name),
		Agent:          opts.Agent,
		ProjectDir:     projectDir,
		BackendPlan:    opts.BackendPlan,
		Profile:        profile,
		MountReadDirs:  mountReadDirs,
		MountWriteDirs: mountWriteDirs,
	}, nil
}

// ExpandAncestorReadDir keeps compatibility with the legacy project-only
// ancestor expansion behavior.
func ExpandAncestorReadDir(ancestor, projectDir string) []string {
	return expandAncestorDirExcludingPaths(ancestor, []string{projectDir})
}

func readDirWriteOverlap(readDir string, writeDirs []string) (skip bool, conflict string) {
	for _, writeDir := range writeDirs {
		if containment.IsWithinDir(writeDir, readDir) {
			return true, ""
		}
		if containment.IsWithinDir(readDir, writeDir) {
			return false, writeDir
		}
	}
	return false, ""
}

func excludedDescendants(ancestor, projectDir string, writeDirs []string) []string {
	var excluded []string
	appendExcluded := func(candidate string) {
		if !containment.IsWithinDir(ancestor, candidate) {
			return
		}
		for i, existing := range excluded {
			if containment.IsWithinDir(existing, candidate) {
				return
			}
			if containment.IsWithinDir(candidate, existing) {
				excluded[i] = candidate
				return
			}
		}
		excluded = append(excluded, candidate)
	}

	appendExcluded(projectDir)
	for _, writeDir := range writeDirs {
		appendExcluded(writeDir)
	}
	return excluded
}

func pathCoveredByDirs(target string, dirs []string) bool {
	for _, dir := range dirs {
		if containment.IsWithinDir(dir, target) {
			return true
		}
	}
	return false
}

func expandAncestorDirExcludingPaths(ancestor string, excluded []string) []string {
	var relParts [][]string
	for _, excludedPath := range excluded {
		rel, err := filepath.Rel(ancestor, excludedPath)
		if err != nil {
			continue
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) == 0 || (len(parts) == 1 && parts[0] == ".") {
			return nil
		}
		relParts = append(relParts, parts)
	}
	if len(relParts) == 0 {
		return nil
	}
	return expandAncestorDirLevel(ancestor, relParts)
}

func expandAncestorDirLevel(current string, excluded [][]string) []string {
	var result []string
	entries, err := os.ReadDir(current)
	if err != nil {
		return nil
	}

	childExcluded := make(map[string][][]string)
	childTerminal := make(map[string]bool)
	for _, parts := range excluded {
		if len(parts) == 0 {
			return nil
		}
		if len(parts) == 1 {
			childTerminal[parts[0]] = true
			continue
		}
		childExcluded[parts[0]] = append(childExcluded[parts[0]], parts[1:])
	}

	for _, e := range entries {
		child := filepath.Join(current, e.Name())
		resolved, err := filepath.EvalSymlinks(child)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}

		if childTerminal[e.Name()] {
			continue
		}
		if tails, ok := childExcluded[e.Name()]; ok {
			result = append(result, expandAncestorDirLevel(resolved, tails)...)
			continue
		}
		result = append(result, resolved)
	}
	return result
}

func SandboxName(agent, projectDir string, mountReadDirs, mountWriteDirs []string, profileName string) string {
	base := strings.ToLower(filepath.Base(projectDir))
	base = sandboxNamePattern.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "workspace"
	}

	h := sha256.New()
	_, _ = h.Write([]byte(agent))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(projectDir))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(profileName))
	for _, dir := range mountWriteDirs {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dir))
	}
	for _, dir := range mountReadDirs {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dir))
	}
	sum := hex.EncodeToString(h.Sum(nil)[:6])
	return fmt.Sprintf("hazmat-%s-%s-%s", agent, base, sum)
}
