package configmodel

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	projectSSHKeyNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	projectSSHHostPattern    = regexp.MustCompile(`^(?:\*(?:\.[A-Za-z0-9-]+)+|[A-Za-z0-9*?\[\]]+(?:[.\-][A-Za-z0-9*?\[\]]+)*)$`)
)

// SSHProfile is one reusable SSH identity. The profile carries the key
// material (private_key + known_hosts) and an optional DefaultHosts list
// that project keys inherit when they declare no hosts of their own.
type SSHProfile struct {
	PrivateKeyPath string   `yaml:"private_key"`
	KnownHostsPath string   `yaml:"known_hosts,omitempty"`
	DefaultHosts   []string `yaml:"default_hosts,omitempty"`
	Description    string   `yaml:"description,omitempty"`
}

type ProjectSSHConfig struct {
	// Legacy single-key fields from the pre-multi-key config shape. This shape
	// is rejected at config load with a migration snippet; new config must use
	// Keys.
	Key            string `yaml:"key,omitempty"`
	PrivateKeyPath string `yaml:"private_key,omitempty"`
	KnownHostsPath string `yaml:"known_hosts,omitempty"`

	// Keys is the multi-key form. When non-empty, the legacy fields above
	// must be empty. Each inline key carries an explicit host list; profile
	// keys may inherit default_hosts.
	Keys []ProjectSSHKey `yaml:"keys,omitempty"`
}

// ProjectSSHKey is one named SSH identity within a project's SSH config.
// Exactly one identity source must be set:
//   - Profile: reference to a shared identity in HazmatConfig.SSHProfiles
//   - PrivateKeyPath: filesystem path to a private key (inline)
//   - Key: reference to the typed provisioned inventory under the host
//     secret store at ~/.hazmat/secrets/git-ssh/provisioned/<name>/
//
// Combining two of these is a config-load error. Hosts overrides any
// Profile-inherited default_hosts when non-empty.
type ProjectSSHKey struct {
	Name           string   `yaml:"name"`
	Profile        string   `yaml:"profile,omitempty"`
	Key            string   `yaml:"key,omitempty"`
	PrivateKeyPath string   `yaml:"private_key,omitempty"`
	KnownHostsPath string   `yaml:"known_hosts,omitempty"`
	Hosts          []string `yaml:"hosts,omitempty"`
}

type ProjectGitSSHConfig struct {
	PrivateKeyPath string   `yaml:"private_key,omitempty"`
	KnownHostsPath string   `yaml:"known_hosts,omitempty"`
	AllowedHosts   []string `yaml:"allowed_hosts,omitempty"`
}

func ValidProjectSSHKeyName(name string) bool {
	return projectSSHKeyNamePattern.MatchString(name)
}

// NormalizedKeys returns the Keys list of a project SSH config. Returns
// nil when no keys are declared. The pre-migration flat shape
// (PrivateKeyPath / Key / KnownHostsPath at the ProjectSSHConfig level
// with no Keys list) is rejected at config load by DetectLegacyFlatSSH,
// so it never reaches this function.
func (c ProjectSSHConfig) NormalizedKeys() []ProjectSSHKey {
	if len(c.Keys) == 0 {
		return nil
	}
	out := make([]ProjectSSHKey, len(c.Keys))
	for i, key := range c.Keys {
		out[i] = key
		out[i].Hosts = append([]string(nil), key.Hosts...)
	}
	return out
}

// DetectLegacyFlatSSH recognises the pre-multi-key single-key shape and
// emits a copy-paste migration snippet. The any-host fallback that
// previously admitted this shape has been retired — inline keys must
// declare at least one host (TLA: InlineKeysHaveDeclaredHosts). Called
// from loadConfig before ValidateProjectSSHConfig so the user sees a
// specific migration message rather than a generic rejection.
func DetectLegacyFlatSSH(projectDir string, c ProjectSSHConfig) error {
	hasFlat := strings.TrimSpace(c.PrivateKeyPath) != "" ||
		strings.TrimSpace(c.Key) != "" ||
		strings.TrimSpace(c.KnownHostsPath) != ""
	if !hasFlat || len(c.Keys) > 0 {
		return nil
	}

	var snippet strings.Builder
	snippet.WriteString("\n\n  projects:\n")
	fmt.Fprintf(&snippet, "    %s:\n", projectDir)
	snippet.WriteString("      ssh:\n")
	snippet.WriteString("        keys:\n")
	snippet.WriteString("          - name: default\n")
	if c.PrivateKeyPath != "" {
		fmt.Fprintf(&snippet, "            private_key: %s\n", c.PrivateKeyPath)
	}
	if c.Key != "" {
		fmt.Fprintf(&snippet, "            key: %s\n", c.Key)
	}
	if c.KnownHostsPath != "" {
		fmt.Fprintf(&snippet, "            known_hosts: %s\n", c.KnownHostsPath)
	}
	snippet.WriteString("            hosts: [github.com]   # replace with your host(s)\n")

	return fmt.Errorf(
		"project %s uses the retired single-key SSH shape. The any-host fallback was removed; edit ~/.hazmat/config.yaml to use the multi-key form:%s",
		projectDir, snippet.String())
}

// ValidateProjectSSHConfig enforces format-level invariants from
// tla/MC_GitSSHRouting.tla that do not require cross-reference to
// ssh_profiles:
//   - Names are non-empty, unique, and well-formed.
//   - Exactly one identity source (Profile | PrivateKeyPath | Key) per entry
//     (the NoProfileInlineConflict + PresentKeysHaveIdentity invariants).
//   - Every inline key declares at least one host (InlineKeysHaveDeclaredHosts).
//   - Hosts are non-empty bare hostnames (plus "*" wildcards) and are
//     pairwise disjoint on declared values.
//
// Mixing legacy flat fields with a non-empty Keys list is rejected.
//
// Cross-reference checks (dangling profile references, overlap on
// profile-inherited effective hosts) live in ValidateProjectSSHProfileRefs.
// Socket-collision checks are session-time artifacts owned by git_ssh.go.
func ValidateProjectSSHConfig(c ProjectSSHConfig) error {
	hasLegacy := strings.TrimSpace(c.PrivateKeyPath) != "" || strings.TrimSpace(c.Key) != ""
	if len(c.Keys) > 0 && hasLegacy {
		return fmt.Errorf("ssh: cannot combine 'keys' list with legacy 'key' or 'private_key'")
	}
	if len(c.Keys) == 0 {
		return nil
	}

	seenName := make(map[string]struct{}, len(c.Keys))
	keyHostSets := make([]map[string]struct{}, len(c.Keys))
	inlineEmptyCount := 0

	for i, key := range c.Keys {
		name := strings.TrimSpace(key.Name)
		if name == "" {
			return fmt.Errorf("ssh.keys[%d]: name is required", i)
		}
		if !projectSSHKeyNamePattern.MatchString(name) {
			return fmt.Errorf("ssh.keys[%d]: invalid name %q (use letters, digits, '-', '_', '.')", i, name)
		}
		if _, dup := seenName[name]; dup {
			return fmt.Errorf("ssh.keys: duplicate name %q", name)
		}
		seenName[name] = struct{}{}

		if err := validateProjectSSHKeyIdentity(name, key); err != nil {
			return err
		}

		hostSet, err := normalizeProjectSSHHosts(key.Hosts)
		if err != nil {
			return fmt.Errorf("ssh.keys[%q].hosts: %w", name, err)
		}
		if len(hostSet) == 0 && strings.TrimSpace(key.Profile) == "" {
			inlineEmptyCount++
		}
		keyHostSets[i] = hostSet
	}

	if inlineEmptyCount > 0 {
		return fmt.Errorf("ssh.keys: inline key has no declared hosts; every inline key must declare at least one --host (any-host fallback was retired)")
	}

	for i := 0; i < len(c.Keys); i++ {
		for j := i + 1; j < len(c.Keys); j++ {
			if overlap := firstHostOverlap(keyHostSets[i], keyHostSets[j]); overlap != "" {
				return fmt.Errorf("ssh.keys: %q and %q both match host %q", c.Keys[i].Name, c.Keys[j].Name, overlap)
			}
		}
	}

	return nil
}

// validateProjectSSHKeyIdentity enforces that exactly one identity source
// is set on a key: profile, inline private key path, or legacy inventory
// reference. Orphans (no source) and conflicts (two or more sources) are
// both rejected.
func validateProjectSSHKeyIdentity(name string, key ProjectSSHKey) error {
	hasProfile := strings.TrimSpace(key.Profile) != ""
	hasPath := strings.TrimSpace(key.PrivateKeyPath) != ""
	hasRef := strings.TrimSpace(key.Key) != ""

	count := 0
	if hasProfile {
		count++
	}
	if hasPath {
		count++
	}
	if hasRef {
		count++
	}
	switch count {
	case 0:
		return fmt.Errorf("ssh.keys[%q]: one of 'profile', 'private_key', or 'key' is required", name)
	case 1:
		return nil
	default:
		return fmt.Errorf("ssh.keys[%q]: set exactly one of 'profile', 'private_key', or 'key' (not %d)", name, count)
	}
}

// ValidateSSHProfiles enforces the ssh_profiles: map invariants:
//   - Profile names are well-formed.
//   - Each profile has a PrivateKeyPath.
//   - Default host patterns are valid.
func ValidateSSHProfiles(profiles map[string]SSHProfile) error {
	for name, profile := range profiles {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return fmt.Errorf("ssh_profiles: profile name is required")
		}
		if !projectSSHKeyNamePattern.MatchString(trimmed) {
			return fmt.Errorf("ssh_profiles[%q]: invalid name (use letters, digits, '-', '_', '.')", name)
		}
		if strings.TrimSpace(profile.PrivateKeyPath) == "" {
			return fmt.Errorf("ssh_profiles[%q]: 'private_key' is required", name)
		}
		if _, err := normalizeProjectSSHHosts(profile.DefaultHosts); err != nil {
			return fmt.Errorf("ssh_profiles[%q].default_hosts: %w", name, err)
		}
	}
	return nil
}

// ValidateProjectSSHProfileRefs runs the cross-reference checks that the
// format-level ValidateProjectSSHConfig cannot: every Profile reference
// must point to a defined profile in ssh_profiles:, and the effective
// host sets (after profile default_hosts inheritance) must be pairwise
// disjoint across the keys.
//
// These map to MC_GitSSHRouting's NoDanglingProfileRefs invariant and the
// profile-aware portion of OverlapRejectedAtConfigTime.
func ValidateProjectSSHProfileRefs(c ProjectSSHConfig, profiles map[string]SSHProfile) error {
	if len(c.Keys) == 0 {
		return nil
	}

	effective := make([]map[string]struct{}, len(c.Keys))
	for i, key := range c.Keys {
		profileName := strings.TrimSpace(key.Profile)
		if profileName != "" {
			if _, ok := profiles[profileName]; !ok {
				return fmt.Errorf("ssh.keys[%q].profile: %q is not defined in ssh_profiles", key.Name, profileName)
			}
		}

		eff, err := effectiveKeyHosts(key, profiles)
		if err != nil {
			return err
		}
		effective[i] = eff
	}

	for i := 0; i < len(c.Keys); i++ {
		for j := i + 1; j < len(c.Keys); j++ {
			if overlap := firstHostOverlap(effective[i], effective[j]); overlap != "" {
				return fmt.Errorf("ssh.keys: %q and %q both resolve to host %q after profile inheritance", c.Keys[i].Name, c.Keys[j].Name, overlap)
			}
		}
	}
	return nil
}

// effectiveKeyHosts returns the normalized host set for a project key
// after profile default_hosts inheritance. Declared Hosts override; when
// a profile-referencing key declares no hosts, the profile's DefaultHosts
// are used.
func effectiveKeyHosts(key ProjectSSHKey, profiles map[string]SSHProfile) (map[string]struct{}, error) {
	if len(key.Hosts) > 0 {
		return normalizeProjectSSHHosts(key.Hosts)
	}
	profileName := strings.TrimSpace(key.Profile)
	if profileName == "" {
		return map[string]struct{}{}, nil
	}
	profile, ok := profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("ssh.keys[%q].profile: %q is not defined in ssh_profiles", key.Name, profileName)
	}
	return normalizeProjectSSHHosts(profile.DefaultHosts)
}

// EffectiveHosts returns the host list a project key routes after profile
// default_hosts inheritance. Intended for display and resolver use; the
// result is lowercase, deduplicated, and sorted by the underlying
// normalizeProjectSSHHosts helper. Returns nil when the key has neither
// declared hosts nor an inheriting profile with defaults.
func (key ProjectSSHKey) EffectiveHosts(profiles map[string]SSHProfile) []string {
	set, err := effectiveKeyHosts(key, profiles)
	if err != nil || len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for host := range set {
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

func normalizeProjectSSHHosts(hosts []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" {
			continue
		}
		if !projectSSHHostPattern.MatchString(host) {
			return nil, fmt.Errorf("invalid host %q (expected bare hostname or wildcard like '*.example.com')", raw)
		}
		out[host] = struct{}{}
	}
	return out, nil
}

func firstHostOverlap(a, b map[string]struct{}) string {
	for host := range a {
		if _, ok := b[host]; ok {
			return host
		}
		if wildcardHostOverlap(host, b) {
			return host
		}
	}
	for host := range b {
		if wildcardHostOverlap(host, a) {
			return host
		}
	}
	return ""
}

// wildcardHostOverlap reports whether `pattern` (which may contain '*' /
// '?' / character classes per filepath.Match) matches any host literal in
// `set`. Two wildcards that both match some unseen third host will NOT be
// caught here — but in practice two overlapping wildcards fail the
// stricter check at session-resolve time when we know the concrete host.
func wildcardHostOverlap(pattern string, set map[string]struct{}) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		return false
	}
	for host := range set {
		if ok, _ := filepath.Match(pattern, host); ok {
			return true
		}
	}
	return false
}
