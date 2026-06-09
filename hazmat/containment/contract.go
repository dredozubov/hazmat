// Package containment defines Hazmat's backend-neutral session authority
// contract. Platform backends compile this contract into concrete enforcement
// artifacts such as macOS SBPL, Linux launch specs, or Docker Sandbox specs.
package containment

import (
	"fmt"
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

// CredentialFloor is the non-omittable credential deny boundary for a
// contract. It is created by constructors so backend compilers can distinguish
// a real deny floor from a caller-supplied empty slice.
type CredentialFloor struct {
	denies []CredentialDeny
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

// ContractInput contains all caller-supplied contract fields except the
// credential deny floor, which must be supplied as a CredentialFloor.
type ContractInput struct {
	Project       PathGrant
	ReadOnlyDirs  []PathGrant
	ReadWriteDirs []PathGrant
	AgentHome     AgentHomePolicy
	Temp          TempPolicy
	Network       NetworkPolicy
	Process       ProcessPolicy
	Services      []ServiceGrant
	Metadata      sessionmeta.LaunchMetadata
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

	credentialFloorConfigured bool
	credentialFloor           []CredentialDeny
}

// NewCredentialFloor derives a credential deny floor from a base home and the
// deny subpaths that must remain inaccessible to contained sessions.
func NewCredentialFloor(home string, subpaths []string) (CredentialFloor, error) {
	if strings.TrimSpace(home) == "" {
		return CredentialFloor{}, fmt.Errorf("credential deny home is required")
	}
	if len(subpaths) == 0 {
		return CredentialFloor{}, fmt.Errorf("credential deny subpaths are required")
	}
	denies := make([]CredentialDeny, 0, len(subpaths))
	for _, subpath := range subpaths {
		if strings.TrimSpace(subpath) == "" {
			return CredentialFloor{}, fmt.Errorf("credential deny subpath is required")
		}
		denies = append(denies, CredentialDeny{Path: filepath.Clean(home) + subpath})
	}
	return CredentialFloorFromDenies(denies)
}

// CredentialFloorFromDenies adapts an already-derived deny list into a
// structural floor. This is intended for backend tests and compatibility
// adapters, not for untrusted wire DTOs.
func CredentialFloorFromDenies(denies []CredentialDeny) (CredentialFloor, error) {
	if len(denies) == 0 {
		return CredentialFloor{}, fmt.Errorf("credential deny floor is required")
	}
	out := make([]CredentialDeny, len(denies))
	for i, deny := range denies {
		if strings.TrimSpace(deny.Path) == "" {
			return CredentialFloor{}, fmt.Errorf("credential deny path is required")
		}
		out[i] = deny
		out[i].Path = filepath.Clean(deny.Path)
	}
	return CredentialFloor{denies: out}, nil
}

// WithHostAuthorityDenies returns a new floor that also denies the given
// absolute host-authority paths (for example a dr-owned Beadpost broker
// attestation key). Host-authority paths are dr-owned ABSOLUTE paths, not
// agent-home credential subpaths; they are appended to the same fail-closed
// deny floor so backend compilers emit an identical (deny file-read*
// file-write*) rule for them, and — unlike the agent login keychain — they
// never receive a post-deny re-allow exception. The receiver floor must
// already be a configured credential floor.
func (f CredentialFloor) WithHostAuthorityDenies(paths ...string) (CredentialFloor, error) {
	if len(f.denies) == 0 {
		return CredentialFloor{}, fmt.Errorf("credential deny floor is required before adding host-authority denies")
	}
	merged := make([]CredentialDeny, len(f.denies), len(f.denies)+len(paths))
	copy(merged, f.denies)
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" {
			return CredentialFloor{}, fmt.Errorf("host-authority deny path is required")
		}
		if !filepath.IsAbs(raw) {
			return CredentialFloor{}, fmt.Errorf("host-authority deny path %q must be absolute", raw)
		}
		merged = append(merged, CredentialDeny{Path: filepath.Clean(raw), Reason: "host-authority"})
	}
	return CredentialFloorFromDenies(merged)
}

// Denies returns a defensive copy of the floor's credential deny paths.
func (f CredentialFloor) Denies() []CredentialDeny {
	if len(f.denies) == 0 {
		return nil
	}
	out := make([]CredentialDeny, len(f.denies))
	copy(out, f.denies)
	return out
}

// NewContract builds a backend-neutral containment contract with a structural
// credential deny floor.
func NewContract(input ContractInput, floor CredentialFloor) (Contract, error) {
	contract := Contract{
		Project:          input.Project,
		ReadOnlyDirs:     copyPathGrants(input.ReadOnlyDirs),
		ReadWriteDirs:    copyPathGrants(input.ReadWriteDirs),
		AgentHome:        input.AgentHome,
		Temp:             input.Temp,
		CredentialDenies: floor.Denies(),
		Network:          input.Network,
		Process:          input.Process,
		Services:         copyServiceGrants(input.Services),
		Metadata:         input.Metadata,

		credentialFloorConfigured: len(floor.denies) > 0,
		credentialFloor:           floor.Denies(),
	}
	if err := contract.Validate(); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

// Validate verifies contract invariants shared by backend compilers.
func (c Contract) Validate() error {
	if c.Project.Path == "" {
		return fmt.Errorf("project path is required")
	}
	if c.Project.Access != PathReadWrite {
		return fmt.Errorf("project path must be read-write")
	}
	if c.AgentHome.Path == "" {
		return fmt.Errorf("agent home path is required")
	}
	if c.Temp.Path == "" {
		return fmt.Errorf("temp path is required")
	}
	if !c.credentialFloorConfigured {
		return fmt.Errorf("credential deny floor is required")
	}
	if len(c.CredentialDenies) == 0 {
		return fmt.Errorf("credential deny floor is required")
	}
	if !credentialDeniesEqual(c.CredentialDenies, c.credentialFloor) {
		return fmt.Errorf("credential deny floor must match structural floor")
	}
	switch sessionmeta.NormalizeNetworkMode(c.Network.Mode) {
	case sessionmeta.NetworkDefault, sessionmeta.NetworkNone:
	default:
		return fmt.Errorf("unsupported network mode %q", c.Network.Mode)
	}

	grants := []PathGrant{c.Project}
	grants = append(grants, c.ReadOnlyDirs...)
	grants = append(grants, c.ReadWriteDirs...)
	for _, grant := range grants {
		if grant.Path == "" {
			return fmt.Errorf("path grant is required")
		}
	}
	for _, grant := range c.ReadOnlyDirs {
		if grant.Access != PathReadOnly {
			return fmt.Errorf("read-only path grant %q must be read-only", grant.Path)
		}
	}
	for _, grant := range c.ReadWriteDirs {
		if grant.Access != PathReadWrite {
			return fmt.Errorf("read-write path grant %q must be read-write", grant.Path)
		}
	}
	for _, deny := range c.CredentialDenies {
		if deny.Path == "" {
			return fmt.Errorf("credential deny path is required")
		}
		for _, grant := range grants {
			if pathsOverlap(grant.Path, deny.Path) {
				return fmt.Errorf("path grant %q overlaps credential deny path %q", grant.Path, deny.Path)
			}
		}
	}
	return nil
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
	denies := c.CredentialDenies
	if c.credentialFloorConfigured {
		denies = c.credentialFloor
	}
	if len(denies) == 0 {
		return nil
	}
	paths := make([]string, 0, len(denies))
	for _, deny := range denies {
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

func copyPathGrants(grants []PathGrant) []PathGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]PathGrant, len(grants))
	copy(out, grants)
	return out
}

func copyServiceGrants(grants []ServiceGrant) []ServiceGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]ServiceGrant, len(grants))
	copy(out, grants)
	return out
}

func pathsOverlap(a, b string) bool {
	return IsWithinDir(a, b) || IsWithinDir(b, a)
}

func credentialDeniesEqual(a, b []CredentialDeny) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
