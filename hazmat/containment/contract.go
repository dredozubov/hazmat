// Package containment defines Hazmat's backend-neutral session authority
// contract. Platform backends compile this contract into concrete enforcement
// artifacts such as macOS SBPL, Linux launch specs, or Docker Sandbox specs.
package containment

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hazmat/sessionmeta"
)

// PathAccess is the requested authority for a filesystem grant.
type PathAccess string

const (
	PathReadOnly  PathAccess = "read-only"
	PathReadWrite PathAccess = "read-write"
)

// PathGrant grants a path to the contained session.
type PathGrant struct {
	Path   string     `json:"path"`
	Access PathAccess `json:"access"`
	Source string     `json:"source,omitempty"`
}

// PathGrants creates path grants with one shared access level.
func PathGrants(paths []string, access PathAccess) []PathGrant {
	if len(paths) == 0 {
		return nil
	}
	grants := make([]PathGrant, 0, len(paths))
	for _, path := range paths {
		grants = append(grants, PathGrant{Path: path, Access: access})
	}
	return grants
}

// AgentHomePolicy describes the agent identity's home exposure.
type AgentHomePolicy struct {
	Path string `json:"path"`
}

// TempPolicy describes the per-session temp authority.
type TempPolicy struct {
	Path string `json:"path"`
}

// CredentialDeny is an absolute path that must remain denied by a backend.
type CredentialDeny struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

// NetworkPolicy describes session network authority.
type NetworkPolicy struct {
	Mode sessionmeta.NetworkMode `json:"mode"`
}

// ProcessPolicy describes process-level authority that all native backends
// must account for, even when enforcement mechanisms differ.
type ProcessPolicy struct {
	AllowFork bool `json:"allow_fork"`
}

// ServiceGrant is a named service authority, such as Docker or Git-over-SSH.
type ServiceGrant struct {
	Name string `json:"name"`
}

// Contract is the backend-neutral session authority model.
type Contract struct {
	Project          PathGrant                  `json:"project"`
	ReadOnlyDirs     []PathGrant                `json:"read_only_dirs,omitempty"`
	ReadWriteDirs    []PathGrant                `json:"read_write_dirs,omitempty"`
	AgentHome        AgentHomePolicy            `json:"agent_home"`
	Temp             TempPolicy                 `json:"temp"`
	CredentialDenies []CredentialDeny           `json:"credential_denies,omitempty"`
	Network          NetworkPolicy              `json:"network"`
	Process          ProcessPolicy              `json:"process"`
	Services         []ServiceGrant             `json:"services,omitempty"`
	Metadata         sessionmeta.LaunchMetadata `json:"metadata,omitempty"`
}

// ProjectPath returns the writable project root.
func (c Contract) ProjectPath() string {
	return c.Project.Path
}

// ReadOnlyPaths returns a defensive copy of read-only grant paths.
func (c Contract) ReadOnlyPaths() []string {
	return grantPaths(c.ReadOnlyDirs)
}

// ReadWritePaths returns a defensive copy of read-write extension paths.
func (c Contract) ReadWritePaths() []string {
	return grantPaths(c.ReadWriteDirs)
}

// CredentialDenyPaths returns a defensive copy of absolute deny paths.
func (c Contract) CredentialDenyPaths() []string {
	if len(c.CredentialDenies) == 0 {
		return nil
	}
	paths := make([]string, 0, len(c.CredentialDenies))
	for _, deny := range c.CredentialDenies {
		paths = append(paths, deny.Path)
	}
	return paths
}

// HostPaths returns every host path intentionally exposed by the contract.
func (c Contract) HostPaths() []string {
	hostPaths := []string{c.ProjectPath()}
	hostPaths = append(hostPaths, c.ReadOnlyPaths()...)
	hostPaths = append(hostPaths, c.ReadWritePaths()...)
	return hostPaths
}

// AncestorMetadataDirs returns parent directories that may need metadata-only
// access so tools can canonicalize exposed paths.
func (c Contract) AncestorMetadataDirs() []string {
	ancestors := make(map[string]struct{})
	for _, dir := range c.HostPaths() {
		for cur := filepath.Dir(dir); cur != "/" && cur != "."; cur = filepath.Dir(cur) {
			ancestors[cur] = struct{}{}
		}
	}
	if len(ancestors) == 0 {
		return nil
	}
	dirs := make([]string, 0, len(ancestors))
	for dir := range ancestors {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// EffectiveReadOnlyDirs removes read-only grants covered by writable roots or
// broader read-only grants.
func (c Contract) EffectiveReadOnlyDirs() []string {
	readDirs := c.ReadOnlyPaths()
	if len(readDirs) == 0 {
		return nil
	}
	writeDirs := c.ReadWritePaths()
	var pending []string
	for _, dir := range readDirs {
		if IsWithinDir(c.ProjectPath(), dir) {
			continue
		}
		coveredByWrite := false
		for _, writeDir := range writeDirs {
			if IsWithinDir(writeDir, dir) {
				coveredByWrite = true
				break
			}
		}
		if coveredByWrite {
			continue
		}
		covered := false
		for _, other := range readDirs {
			if other != dir && IsWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		pending = append(pending, dir)
	}
	return pending
}

// EffectiveWritableDirs removes write grants covered by the project root or a
// broader write grant.
func (c Contract) EffectiveWritableDirs() []string {
	writeDirs := c.ReadWritePaths()
	if len(writeDirs) == 0 {
		return nil
	}
	var pending []string
	for _, dir := range writeDirs {
		if IsWithinDir(c.ProjectPath(), dir) {
			continue
		}
		covered := false
		for _, other := range writeDirs {
			if other != dir && IsWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		pending = append(pending, dir)
	}
	return pending
}

// IsWithinDir reports whether target is base or below base.
func IsWithinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func grantPaths(grants []PathGrant) []string {
	if len(grants) == 0 {
		return nil
	}
	paths := make([]string, 0, len(grants))
	for _, grant := range grants {
		paths = append(paths, grant.Path)
	}
	return paths
}
