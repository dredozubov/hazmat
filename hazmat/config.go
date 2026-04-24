package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	projectSSHKeyNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	projectSSHHostPattern    = regexp.MustCompile(`^(?:\*(?:\.[A-Za-z0-9-]+)+|[A-Za-z0-9*?\[\]]+(?:[.\-][A-Za-z0-9*?\[\]]+)*)$`)
)

// ── Config file path ────────────────────────────────────────────────────────

var configFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/config.yaml")

// cloudCredentialPath stores the S3 secret key separately from the main
// config file. The config file is human-readable and safe to share; the
// credential file is 0600 and contains only the secret key.
var cloudCredentialPath = filepath.Join(os.Getenv("HOME"), ".hazmat/cloud-credentials")

// ── Config types ────────────────────────────────────────────────────────────

type HazmatConfig struct {
	Backup       BackupConfig             `yaml:"backup"`
	Session      SessionConfig            `yaml:"session"`
	Integrations IntegrationsConfig       `yaml:"integrations,omitempty"`
	Projects     map[string]ProjectConfig `yaml:"projects,omitempty"`
	Sandbox      SandboxConfig            `yaml:"sandbox,omitempty"`
	// SSHProfiles defines reusable SSH identities that can be referenced from
	// any project via ProjectSSHKey.Profile. See tla/MC_GitSSHRouting.tla for
	// the formal routing contract.
	SSHProfiles map[string]SSHProfile `yaml:"ssh_profiles,omitempty"`
}

// SSHProfile is one reusable SSH identity. The profile carries the key
// material (private_key + known_hosts) and an optional DefaultHosts list
// that project keys inherit when they declare no hosts of their own.
type SSHProfile struct {
	PrivateKeyPath string   `yaml:"private_key"`
	KnownHostsPath string   `yaml:"known_hosts,omitempty"`
	DefaultHosts   []string `yaml:"default_hosts,omitempty"`
	Description    string   `yaml:"description,omitempty"`
}

type ProjectConfig struct {
	Docker    dockerMode           `yaml:"docker,omitempty"`
	ReadDirs  []string             `yaml:"read_dirs,omitempty"`
	WriteDirs []string             `yaml:"write_dirs,omitempty"`
	SSH       *ProjectSSHConfig    `yaml:"ssh,omitempty"`
	GitSSH    *ProjectGitSSHConfig `yaml:"git_ssh,omitempty"`
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
//   - Key: reference to the legacy provisioned inventory at
//     ~/.hazmat/ssh/keys/<name>/ (inline variant, predates profiles)
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

type SessionConfig struct {
	// SkipPermissions passes harness-specific auto-approval flags to agent
	// CLIs (for example Claude's --dangerously-skip-permissions and Codex's
	// --dangerously-bypass-approvals-and-sandbox). Default: true. The
	// containment is OS-level (user isolation + seatbelt + pf firewall), so
	// app-level permission prompts are usually redundant inside hazmat. Set
	// to false if you want those prompts as an additional layer.
	SkipPermissions *bool `yaml:"skip_permissions,omitempty"`

	// StatusBar enables Hazmat's terminal status bar for interactive sessions.
	// Default: false. Keep this opt-in until the terminal behavior is robust
	// across resume pickers and first-run environments.
	StatusBar *bool `yaml:"status_bar,omitempty"`

	// ReadDirs are automatically added as -R (read-only) directories for
	// every session. Default: empty. Visible in `hazmat config`, configurable
	// via `hazmat config set session.read_dirs.add <dir>`.
	ReadDirs *[]string `yaml:"read_dirs,omitempty"`

	// HarnessAssets enables managed sync of supported user-global prompt assets
	// for built-in harness commands. Default: true.
	HarnessAssets *bool `yaml:"harness_assets,omitempty"`
}

type IntegrationsConfig struct {
	Homebrew *bool `yaml:"homebrew,omitempty"`
	// Pinned maps canonical project paths to integration names.
	// Input paths are normalized through Abs + EvalSymlinks before storage,
	// so matching is stable across different spellings of the same path.
	Pinned   []IntegrationPin       `yaml:"pinned,omitempty"`
	Rejected []IntegrationRejection `yaml:"rejected,omitempty"`
}

type SandboxConfig struct {
	Backend *SandboxBackendConfig  `yaml:"backend,omitempty"`
	Managed []ManagedSandboxConfig `yaml:"managed,omitempty"`
}

type SandboxBackendConfig struct {
	Type           string `yaml:"type,omitempty"`
	PolicyProfile  string `yaml:"policy_profile,omitempty"`
	DesktopVersion string `yaml:"docker_desktop_version,omitempty"`
	ComposeVersion string `yaml:"compose_version,omitempty"`
	ConfiguredAt   string `yaml:"configured_at,omitempty"`
}

type ManagedSandboxConfig struct {
	Name          string `yaml:"name,omitempty"`
	BackendType   string `yaml:"backend_type,omitempty"`
	Agent         string `yaml:"agent,omitempty"`
	ProjectDir    string `yaml:"project,omitempty"`
	PolicyProfile string `yaml:"policy_profile,omitempty"`
	LastUsedAt    string `yaml:"last_used_at,omitempty"`
}

// IntegrationPin associates a project directory with a list of integration names.
type IntegrationPin struct {
	ProjectDir   string   `yaml:"project"`
	Integrations []string `yaml:"integrations"`
}

// IntegrationRejection records suggested integrations the user declined for a
// specific project, so Hazmat does not keep re-prompting on future launches.
type IntegrationRejection struct {
	ProjectDir   string   `yaml:"project"`
	Integrations []string `yaml:"integrations"`
}

// PinnedIntegrations returns the configured integration pins (nil if none).
func (c HazmatConfig) PinnedIntegrations() []IntegrationPin {
	return c.Integrations.Pinned
}

func (c HazmatConfig) RejectedIntegrations() []IntegrationRejection {
	return c.Integrations.Rejected
}

func (c HazmatConfig) ProjectPinnedIntegrations(projectDir string) []string {
	for _, pin := range c.PinnedIntegrations() {
		if pin.ProjectDir == projectDir {
			return append([]string(nil), pin.Integrations...)
		}
	}
	return nil
}

func (c HazmatConfig) ProjectRejectedIntegrations(projectDir string) []string {
	for _, rejection := range c.RejectedIntegrations() {
		if rejection.ProjectDir == projectDir {
			return append([]string(nil), rejection.Integrations...)
		}
	}
	return nil
}

type BackupConfig struct {
	Local    LocalBackupConfig `yaml:"local"`
	Excludes []string          `yaml:"excludes"`
	Cloud    *CloudBackup      `yaml:"cloud,omitempty"`
}

type LocalBackupConfig struct {
	Path      string          `yaml:"path"`
	Retention RetentionConfig `yaml:"retention"`
}

type RetentionConfig struct {
	KeepLatest int `yaml:"keep_latest"`
	KeepDaily  int `yaml:"keep_daily"`
	KeepWeekly int `yaml:"keep_weekly"`
}

type CloudBackup struct {
	Endpoint    string `yaml:"endpoint"`
	Bucket      string `yaml:"bucket"`
	AccessKey   string `yaml:"access_key"`
	RecoveryKey string `yaml:"recovery_key,omitempty"` // Kopia repo encryption key
	// LegacyRecoveryKey accepts the pre-rename `password:` YAML key. Migrated to
	// RecoveryKey on load and dropped on next save.
	LegacyRecoveryKey string `yaml:"password,omitempty"`
	// SecretKey is NOT stored here — it's in cloudCredentialPath
}

// ── Defaults ────────────────────────────────────────────────────────────────

// SkipPermissions returns whether Hazmat should pass harness-specific
// auto-approval flags to agent CLIs. Default: true.
func (c HazmatConfig) SkipPermissions() bool {
	if c.Session.SkipPermissions == nil {
		return true // default: skip permissions, containment is OS-level
	}
	return *c.Session.SkipPermissions
}

// StatusBar returns whether Hazmat should render its terminal status bar.
// Default: false.
func (c HazmatConfig) StatusBar() bool {
	if c.Session.StatusBar == nil {
		return false
	}
	return *c.Session.StatusBar
}

// SessionReadDirs returns the configured read-only directories.
// Default: empty (no automatic read-only dirs). Configure via
// hazmat config set session.read_dirs.add ~/workspace
func (c HazmatConfig) SessionReadDirs() []string {
	if c.Session.ReadDirs != nil {
		return *c.Session.ReadDirs
	}
	return nil
}

func (c HazmatConfig) HarnessAssets() bool {
	if c.Session.HarnessAssets == nil {
		return true
	}
	return *c.Session.HarnessAssets
}

func (c HazmatConfig) HomebrewIntegrationConsent() (bool, bool) {
	if c.Integrations.Homebrew == nil {
		return false, false
	}
	return *c.Integrations.Homebrew, true
}

func (c HazmatConfig) SandboxBackend() *SandboxBackendConfig {
	if c.Sandbox.Backend == nil || c.Sandbox.Backend.Type == "" {
		return nil
	}
	return c.Sandbox.Backend
}

func (c HazmatConfig) ManagedSandboxes() []ManagedSandboxConfig {
	return c.Sandbox.Managed
}

func (c HazmatConfig) ProjectDockerMode(projectDir string) (dockerMode, bool) {
	if len(c.Projects) == 0 {
		return dockerModeNone, false
	}
	project, ok := c.Projects[projectDir]
	if !ok || !validDockerMode(project.Docker) {
		return dockerModeNone, false
	}
	return project.Docker, true
}

func (c HazmatConfig) ProjectReadDirs(projectDir string) []string {
	if len(c.Projects) == 0 {
		return nil
	}
	project, ok := c.Projects[projectDir]
	if !ok {
		return nil
	}
	return append([]string(nil), project.ReadDirs...)
}

func (c HazmatConfig) ProjectWriteDirs(projectDir string) []string {
	if len(c.Projects) == 0 {
		return nil
	}
	project, ok := c.Projects[projectDir]
	if !ok {
		return nil
	}
	return append([]string(nil), project.WriteDirs...)
}

func (c HazmatConfig) ProjectSSH(projectDir string) *ProjectSSHConfig {
	if len(c.Projects) == 0 {
		return nil
	}
	project, ok := c.Projects[projectDir]
	if !ok || project.SSH == nil {
		return nil
	}
	cloned := *project.SSH
	if len(project.SSH.Keys) > 0 {
		cloned.Keys = make([]ProjectSSHKey, len(project.SSH.Keys))
		for i, key := range project.SSH.Keys {
			cloned.Keys[i] = key
			cloned.Keys[i].Hosts = append([]string(nil), key.Hosts...)
		}
	}
	return &cloned
}

// NormalizedKeys returns the Keys list of a project SSH config. Returns
// nil when no keys are declared. The pre-migration flat shape
// (PrivateKeyPath / Key / KnownHostsPath at the ProjectSSHConfig level
// with no Keys list) is rejected at config load by detectLegacyFlatSSH,
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

// detectLegacyFlatSSH recognises the pre-multi-key single-key shape and
// emits a copy-paste migration snippet. The any-host fallback that
// previously admitted this shape has been retired — inline keys must
// declare at least one host (TLA: InlineKeysHaveDeclaredHosts). Called
// from loadConfig before ValidateProjectSSHConfig so the user sees a
// specific migration message rather than a generic rejection.
func detectLegacyFlatSSH(projectDir string, c ProjectSSHConfig) error {
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

func (c HazmatConfig) ProjectGitSSH(projectDir string) *ProjectGitSSHConfig {
	if len(c.Projects) == 0 {
		return nil
	}
	project, ok := c.Projects[projectDir]
	if !ok || project.GitSSH == nil {
		return nil
	}
	cloned := *project.GitSSH
	cloned.AllowedHosts = append([]string(nil), project.GitSSH.AllowedHosts...)
	return &cloned
}

func defaultConfig() HazmatConfig {
	return HazmatConfig{
		Backup: BackupConfig{
			Local: LocalBackupConfig{
				Path: localRepoDir,
				Retention: RetentionConfig{
					KeepLatest: defaultKeepLatest,
					KeepDaily:  defaultKeepDaily,
					KeepWeekly: defaultKeepWeekly,
				},
			},
			Excludes: backupBuiltinExcludes,
		},
	}
}

// ── Load / Save ─────────────────────────────────────────────────────────────

func loadConfig() (HazmatConfig, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // defaults
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err == nil {
		if _, legacy := raw["packs"]; legacy {
			return cfg, fmt.Errorf("config key 'packs' was removed before v1; migrate pinned entries under 'integrations.pinned'")
		}
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Backup.Cloud != nil && cfg.Backup.Cloud.RecoveryKey == "" && cfg.Backup.Cloud.LegacyRecoveryKey != "" {
		cfg.Backup.Cloud.RecoveryKey = cfg.Backup.Cloud.LegacyRecoveryKey
		cfg.Backup.Cloud.LegacyRecoveryKey = ""
	}

	if err := ValidateSSHProfiles(cfg.SSHProfiles); err != nil {
		return cfg, err
	}
	for projectDir, project := range cfg.Projects {
		if project.SSH == nil {
			continue
		}
		if err := detectLegacyFlatSSH(projectDir, *project.SSH); err != nil {
			return cfg, err
		}
		if err := ValidateProjectSSHConfig(*project.SSH); err != nil {
			return cfg, fmt.Errorf("project %s: %w", projectDir, err)
		}
		if err := ValidateProjectSSHProfileRefs(*project.SSH, cfg.SSHProfiles); err != nil {
			return cfg, fmt.Errorf("project %s: %w", projectDir, err)
		}
	}

	return cfg, nil
}

func saveConfig(cfg HazmatConfig) error {
	dir := filepath.Dir(configFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	header := "# Hazmat configuration\n# Edit manually or via: hazmat config set <key> <value>\n\n"
	return os.WriteFile(configFilePath, []byte(header+string(data)), 0o600)
}

// ── Cloud credential (secret key only) ──────────────────────────────────────

func loadCloudSecretKey() (string, error) {
	// Environment variable takes precedence
	if key := os.Getenv("HAZMAT_CLOUD_SECRET_KEY"); key != "" {
		return key, nil
	}

	data, err := os.ReadFile(cloudCredentialPath)
	if err != nil {
		return "", fmt.Errorf("read cloud credentials: %w\nSet HAZMAT_CLOUD_SECRET_KEY or run: hazmat config cloud", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func saveCloudSecretKey(key string) error {
	dir := filepath.Dir(cloudCredentialPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(cloudCredentialPath, []byte(key+"\n"), 0o600)
}

// ── Commands ────────────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or edit hazmat configuration",
		Long: `Shows the current hazmat configuration.

Subcommands:
  hazmat config              Show current configuration
  hazmat config docker       Configure per-project Docker routing
  hazmat config access       Configure per-project read/write extensions
  hazmat config ssh          Configure per-project Git-over-SSH key selection
  hazmat config sudoers      Show or manage Hazmat's sudoers rules
  hazmat config edit         Open config in $EDITOR
  hazmat config agent        Configure API key and git identity
  hazmat config import claude Import portable Claude basics
  hazmat config import codex Import portable Codex basics
  hazmat config import opencode Import portable OpenCode basics
  hazmat config import gemini Import portable Gemini basics
  hazmat config cloud        Configure S3 cloud backup credentials
  hazmat config set K V      Set a configuration value

Examples:
  hazmat config
  hazmat config docker none -C ~/workspace/my-project
  hazmat config access add -C ~/workspace/my-project --read ~/other-code
  hazmat config ssh list-keys
  hazmat config ssh add --name github --host github.com ~/.ssh/id_ed25519
  hazmat config sudoers --enable-agent-maintenance
  hazmat config agent
  hazmat config import claude --dry-run
  hazmat config import codex --dry-run
  hazmat config import opencode --dry-run
  hazmat config import gemini --dry-run
  hazmat config cloud --endpoint s3.fr-par.scw.cloud --bucket my-backups
  hazmat config set backup.retention.keep_latest 30`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigShow()
		},
	}

	cmd.AddCommand(newConfigEditCmd())
	cmd.AddCommand(newConfigDockerCmd())
	cmd.AddCommand(newConfigAccessCmd())
	cmd.AddCommand(newConfigSSHCmd())
	cmd.AddCommand(newConfigSudoersCmd())
	cmd.AddCommand(newConfigAgentCmd())
	cmd.AddCommand(newConfigImportCmd())
	cmd.AddCommand(newConfigCloudCmd())
	cmd.AddCommand(newConfigSetCmd())

	return cmd
}

func runConfigShow() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Println()
	cBold.Println("  Backup")
	fmt.Println()
	fmt.Printf("    Local repo:   %s\n", cfg.Backup.Local.Path)
	fmt.Printf("    Retention:    %d latest, %d daily, %d weekly\n",
		cfg.Backup.Local.Retention.KeepLatest,
		cfg.Backup.Local.Retention.KeepDaily,
		cfg.Backup.Local.Retention.KeepWeekly)

	// Show excludes compactly
	if len(cfg.Backup.Excludes) > 0 {
		shown := cfg.Backup.Excludes
		extra := 0
		if len(shown) > 5 {
			extra = len(shown) - 5
			shown = shown[:5]
		}
		line := strings.Join(shown, " ")
		if extra > 0 {
			line += fmt.Sprintf(" +%d more", extra)
		}
		fmt.Printf("    Excludes:     %s\n", line)
	}

	fmt.Println()

	if cfg.Backup.Cloud != nil {
		cBold.Println("  Cloud")
		fmt.Println()
		fmt.Printf("    Endpoint:     %s\n", cfg.Backup.Cloud.Endpoint)
		fmt.Printf("    Bucket:       %s\n", cfg.Backup.Cloud.Bucket)
		if cfg.Backup.Cloud.AccessKey != "" {
			masked := cfg.Backup.Cloud.AccessKey
			if len(masked) > 8 {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			}
			fmt.Printf("    Access key:   %s\n", masked)
		}
		if _, err := loadCloudSecretKey(); err == nil {
			fmt.Printf("    Secret key:   ••••••••\n")
		} else {
			cYellow.Printf("    Secret key:   not configured\n")
		}
		if cfg.Backup.Cloud.RecoveryKey != "" {
			fmt.Printf("    Encryption:   ••••••••\n")
		}
		fmt.Println()
	} else {
		cDim.Println("  Cloud: not configured")
		cDim.Println("    Set up with: hazmat config cloud")
		fmt.Println()
	}

	cBold.Println("  Session")
	fmt.Println()
	fmt.Printf("    Skip permissions: %v (bypass Claude/Codex app prompts)\n", cfg.SkipPermissions())
	fmt.Printf("    Status bar:       %v (opt-in)\n", cfg.StatusBar())
	fmt.Printf("    Harness assets:   %v (managed prompt-asset sync)\n", cfg.HarnessAssets())
	readDirs := cfg.SessionReadDirs()
	if len(readDirs) > 0 {
		fmt.Printf("    Read dirs:        %s\n", strings.Join(readDirs, ", "))
	} else {
		fmt.Printf("    Read dirs:        (none)\n")
	}
	if len(cfg.Projects) > 0 {
		var projectKeys []string
		for projectDir, projectCfg := range cfg.Projects {
			if projectHasOverrides(projectCfg) {
				projectKeys = append(projectKeys, projectDir)
			}
		}
		sort.Strings(projectKeys)
		if len(projectKeys) > 0 {
			fmt.Printf("    Project overrides: %d configured\n", len(projectKeys))
			for _, projectDir := range projectKeys {
				projectCfg := cfg.Projects[projectDir]
				fmt.Printf("      - %s\n", projectDir)
				if validDockerMode(projectCfg.Docker) {
					fmt.Printf("        Docker: %s\n", projectCfg.Docker)
				}
				if len(projectCfg.ReadDirs) > 0 {
					fmt.Printf("        Read-only: %s\n", strings.Join(projectCfg.ReadDirs, ", "))
				}
				if len(projectCfg.WriteDirs) > 0 {
					fmt.Printf("        Read-write: %s\n", strings.Join(projectCfg.WriteDirs, ", "))
				}
				if projectCfg.SSH != nil {
					switch {
					case projectCfg.SSH.PrivateKeyPath != "":
						fmt.Printf("        SSH key: %s\n", projectCfg.SSH.PrivateKeyPath)
						if projectCfg.SSH.KnownHostsPath != "" {
							fmt.Printf("        SSH known_hosts: %s\n", projectCfg.SSH.KnownHostsPath)
						}
					case projectCfg.SSH.Key != "":
						fmt.Printf("        SSH key: %s\n", projectCfg.SSH.Key)
					}
				}
				if projectCfg.GitSSH != nil {
					fmt.Printf("        Legacy Git SSH hosts: %s\n", strings.Join(projectCfg.GitSSH.AllowedHosts, ", "))
					fmt.Printf("        Legacy Git SSH key: %s\n", projectCfg.GitSSH.PrivateKeyPath)
					fmt.Printf("        Legacy Git SSH known_hosts: %s\n", projectCfg.GitSSH.KnownHostsPath)
				}
			}
		} else {
			fmt.Printf("    Project overrides: (none)\n")
		}
	} else {
		fmt.Printf("    Project overrides: (none)\n")
	}
	fmt.Println()

	cBold.Println("  Privilege")
	fmt.Println()
	serviceBackend := nativeServiceBackendForHost()
	if serviceBackend.LaunchSudoersInstalled() {
		fmt.Printf("    Launch helper sudo:      installed (%s)\n", sudoersFile)
	} else {
		fmt.Printf("    Launch helper sudo:      missing (%s)\n", sudoersFile)
	}
	if serviceBackend.AgentMaintenanceSudoersInstalled() {
		fmt.Printf("    Agent maintenance sudo:  enabled (%s)\n", agentMaintenanceSudoersFile)
	} else {
		fmt.Printf("    Agent maintenance sudo:  disabled\n")
	}
	fmt.Printf("    sudo -u %s no prompt:    %v\n", agentUser, serviceBackend.GenericAgentPasswordlessAvailable())
	fmt.Println()

	cBold.Println("  Integrations")
	fmt.Println()
	if allowed, configured := cfg.HomebrewIntegrationConsent(); configured {
		state := "disabled"
		if allowed {
			state = "enabled"
		}
		fmt.Printf("    Homebrew metadata: %s\n", state)
	} else {
		fmt.Printf("    Homebrew metadata: ask on first use\n")
	}
	if len(cfg.PinnedIntegrations()) > 0 {
		fmt.Printf("    Pinned projects:   %d configured\n", len(cfg.PinnedIntegrations()))
	}
	if len(cfg.RejectedIntegrations()) > 0 {
		fmt.Printf("    Rejected sets:     %d configured\n", len(cfg.RejectedIntegrations()))
	}
	fmt.Println()

	cBold.Println("  Sandbox")
	fmt.Println()
	if backend := cfg.SandboxBackend(); backend != nil {
		fmt.Printf("    Backend:          %s\n", formatSandboxBackendLabel(backend.Type))
		fmt.Printf("    Policy profile:   %s\n", backend.PolicyProfile)
		if backend.DesktopVersion != "" {
			fmt.Printf("    Desktop version:  %s\n", backend.DesktopVersion)
		}
		if backend.ComposeVersion != "" {
			fmt.Printf("    Compose version:  %s\n", backend.ComposeVersion)
		}
		if backend.ConfiguredAt != "" {
			fmt.Printf("    Configured at:    %s\n", backend.ConfiguredAt)
		}
	} else {
		fmt.Printf("    Backend:          (not configured)\n")
	}
	if managed := cfg.ManagedSandboxes(); len(managed) > 0 {
		fmt.Printf("    Managed sandboxes: %d\n", len(managed))
	} else {
		fmt.Printf("    Managed sandboxes: (none)\n")
	}
	fmt.Println()

	cDim.Printf("  Config file: %s\n", configFilePath)
	fmt.Println()
	return nil
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open configuration in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Ensure config file exists with current values
			cfg, _ := loadConfig()
			if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
				if err := saveConfig(cfg); err != nil {
					return err
				}
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "nano"
			}
			cmd := exec.Command(editor, configFilePath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	}
}

func newConfigDockerCmd() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "docker <none|sandbox|auto>",
		Short: "Configure per-project Docker routing",
		Long: `Set the preferred Docker routing mode for a project.

Modes:
  none     Keep sessions in native containment for code-only work (default)
  sandbox  Force Docker Sandbox mode for private-daemon Docker workflows
  auto     Opt this project into Docker marker detection and routing

Examples:
  hazmat config docker none -C ~/workspace/my-project
  hazmat config docker sandbox -C ~/workspace/docker-app
  hazmat config docker auto -C ~/workspace/docker-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigDocker(project, args[0])
		},
	}

	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	return cmd
}

func newConfigAccessCmd() *cobra.Command {
	var project string

	newActionCmd := func(name string, remove bool) *cobra.Command {
		var readDirs []string
		var writeDirs []string

		short := "Add per-project directory extensions"
		if remove {
			short = "Remove per-project directory extensions"
		}

		cmd := &cobra.Command{
			Use:   name,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return runConfigAccess(project, readDirs, writeDirs, remove)
			},
		}
		cmd.Flags().StringVarP(&project, "project", "C", "",
			"Project directory (defaults to current directory)")
		cmd.Flags().StringArrayVar(&readDirs, "read", nil,
			"Read-only directory to persist for this project (repeatable)")
		cmd.Flags().StringArrayVar(&writeDirs, "write", nil,
			"Read-write directory to persist for this project (repeatable)")
		return cmd
	}

	cmd := &cobra.Command{
		Use:   "access",
		Short: "Configure per-project read/write directory extensions",
		Long: `Set explicit per-project directory extensions.

These extend Hazmat's default session contract with exact directory paths.
Read-only entries behave like persistent -R flags. Read-write entries add
extra writable roots beyond the project directory for that project only.

Examples:
  hazmat config access add -C ~/workspace/my-app --read ~/.nvm/versions/node/v22
  hazmat config access add -C ~/workspace/my-app --write ~/.venvs/my-app
  hazmat config access remove -C ~/workspace/my-app --write ~/.venvs/my-app`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newActionCmd("add", false),
		newActionCmd("remove", true),
	)
	return cmd
}

func newConfigSSHCmd() *cobra.Command {
	var project string
	var host string
	var listKeyDir string
	var addName string
	var addHosts []string
	var addInventory string
	var addProfile string
	var removeName string

	addCmd := &cobra.Command{
		Use:   "add [key]",
		Short: "Add a named SSH key with host scoping to a project",
		Long: `Append a named SSH key to a project's SSH configuration. When two or
more keys are configured, each must declare its own --host list; the wrapper
routes destination hosts to exactly one key.

Provide exactly one identity source per key: a private-key path, --inventory
for a provisioned inventory key, or --profile to reference a shared profile
from ssh_profiles.

Examples:
  hazmat config ssh add --name github --host github.com ~/.ssh/id_ed25519
  hazmat config ssh add --name prod --host prod.example.com --host '*.prod.example.com' ~/.ssh/prod_key
  hazmat config ssh add --name github --host github.com --inventory github-bot
  hazmat config ssh add --name work --profile github-work
  hazmat config ssh add --name enterprise --profile github-work --host enterprise.internal`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			keyArg := ""
			if len(args) == 1 {
				keyArg = args[0]
			}
			return runConfigSSHAdd(project, addName, addHosts, addInventory, addProfile, keyArg)
		},
	}
	addCmd.ValidArgsFunction = completeSSHAddKeyArgs
	addCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	addCmd.Flags().StringVar(&addName, "name", "",
		"Name for this key (used for routing and display)")
	addCmd.Flags().StringArrayVar(&addHosts, "host", nil,
		"Destination host this key serves (repeatable, supports glob)")
	addCmd.Flags().StringVar(&addInventory, "inventory", "",
		"Reference a provisioned key from ~/.hazmat/ssh/keys/<name>/ instead of a path")
	addCmd.Flags().StringVar(&addProfile, "profile", "",
		"Reference a shared SSH profile defined in ssh_profiles")

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a named SSH key from a project",
		Long: `Remove a single named SSH key from a project's Keys list. When the
last key is removed, the project's SSH configuration is cleared.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSSHRemove(project, removeName)
		},
	}
	removeCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	removeCmd.Flags().StringVar(&removeName, "name", "",
		"Name of the key to remove (required)")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the SSH key assigned to a project",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSSHShow(project)
		},
	}
	showCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")

	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Test the assigned SSH key against a Git SSH host",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSSHTest(project, host)
		},
	}
	testCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	testCmd.Flags().StringVar(&host, "host", "",
		"Git SSH host to probe (for example github.com)")

	unsetCmd := &cobra.Command{
		Use:               "unset [key]",
		Short:             "Remove the SSH assignment from a project",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSSHUnsetKeyArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			selectedKey := ""
			if len(args) == 1 {
				selectedKey = args[0]
			}
			return runConfigSSHUnset(project, selectedKey)
		},
	}
	unsetCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")

	clearCmd := &cobra.Command{
		Use:    "clear",
		Short:  "Deprecated alias for unset",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			selectedKey := ""
			if len(args) == 1 {
				selectedKey = args[0]
			}
			return runConfigSSHUnset(project, selectedKey)
		},
	}
	clearCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")

	listCmd := &cobra.Command{
		Use:   "list-keys",
		Short: "List SSH keys in a directory",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSSHListKeys(listKeyDir)
		},
	}
	listCmd.Flags().StringVar(&listKeyDir, "dir", "",
		"Directory containing SSH keys (defaults to ~/.ssh)")

	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Configure per-project Git-over-SSH key selection",
		Long: `Assign an SSH key from a chosen directory to a project for Git-over-SSH use.

Hazmat keeps the private key in host-owned storage, loads it into a fresh
session-local ssh-agent at launch time, and still restricts the session to
Git SSH transports rather than arbitrary remote shells.

By default Hazmat looks for keys in ~/.ssh and uses known_hosts from the same
directory. To use a different directory, pass the full key path as the key
argument.

Examples:
  hazmat config ssh list-keys
  hazmat config ssh list-keys --dir ~/.config/hazmat/ssh
  hazmat config ssh add --name github --host github.com ~/.ssh/id_ed25519
  hazmat config ssh add -C ~/workspace/my-app --name github --host github.com ~/.config/hazmat/ssh/deploy_key
  hazmat config ssh add -C ~/workspace/my-app --name work --profile github-work
  hazmat config ssh show -C ~/workspace/my-app
  hazmat config ssh test -C ~/workspace/my-app --host github.com
  hazmat config ssh unset -C ~/workspace/my-app`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(addCmd, removeCmd, showCmd, testCmd, unsetCmd, clearCmd, listCmd, newConfigSSHProfileCmd())
	return cmd
}

func newConfigSSHProfileCmd() *cobra.Command {
	var (
		addKnownHosts   string
		addDefaultHosts []string
		addDescription  string
		removeForce     bool
	)

	addCmd := &cobra.Command{
		Use:   "add <name> <private_key_path>",
		Short: "Define a reusable SSH profile",
		Long: `Create a named SSH profile usable from any project via
'hazmat config ssh add --profile <name>'. The profile holds the private
key identity, optional known_hosts override, and an optional
default_hosts list that projects inherit unless they override with --host.

Examples:
  hazmat config ssh profile add github ~/.ssh/keys/github/id_ed25519 \
      --default-host github.com --description "personal github"
  hazmat config ssh profile add prod ~/.ssh/keys/prod/id_ed25519 \
      --known-hosts ~/.ssh/keys/prod/known_hosts \
      --default-host prod.example.com --default-host '*.prod.example.com'`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigSSHProfileAdd(args[0], args[1], addKnownHosts, addDefaultHosts, addDescription)
		},
	}
	addCmd.ValidArgsFunction = completeSSHProfileAddArgs
	addCmd.Flags().StringVar(&addKnownHosts, "known-hosts", "",
		"known_hosts file for this profile (defaults to <private_key_dir>/known_hosts)")
	addCmd.Flags().StringArrayVar(&addDefaultHosts, "default-host", nil,
		"Default destination host for projects that reference this profile (repeatable)")
	addCmd.Flags().StringVar(&addDescription, "description", "",
		"Human-readable description for ssh profile list")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List defined SSH profiles and their project referrers",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSSHProfileList()
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one SSH profile in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigSSHProfileShow(args[0])
		},
	}

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an SSH profile (refuses while projects reference it)",
		Long: `Remove a profile from ssh_profiles. If any project keys still
reference the profile, the command refuses and lists the referrers. Pass
--force to detach every project reference AND remove the profile in one
operation.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigSSHProfileRemove(args[0], removeForce)
		},
	}
	removeCmd.Flags().BoolVar(&removeForce, "force", false,
		"Detach project references and remove the profile")

	renameCmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename an SSH profile and update all project references",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigSSHProfileRename(args[0], args[1])
		},
	}

	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage reusable SSH profiles shared across projects",
		Long: `Reusable SSH profiles let one identity serve many projects.
Create a profile with 'profile add', then reference it from any project
with 'hazmat config ssh add --profile <name>'. A profile's default_hosts
are inherited by referring project keys that declare no hosts of their
own; declared --host lists always override.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}
	cmd.AddCommand(addCmd, listCmd, showCmd, removeCmd, renameCmd)
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value by dotted key path.

Keys:
  backup.retention.keep_latest   Number of latest snapshots to keep per project
  backup.retention.keep_daily    Number of daily snapshots to keep
  backup.retention.keep_weekly   Number of weekly snapshots to keep
  backup.excludes.add            Add an exclude pattern
  backup.excludes.remove         Remove an exclude pattern
  backup.cloud.endpoint          S3-compatible endpoint
  backup.cloud.bucket            S3 bucket name
  backup.cloud.access_key        S3 access key
  session.skip_permissions       Bypass Claude/Codex app-level permission prompts (default: true)
  session.status_bar             Enable Hazmat's terminal status bar (default: false)
  session.harness_assets         Enable managed harness prompt-asset sync (default: true)
  session.read_dirs.add          Add a read-only directory to auto-include in sessions
  session.read_dirs.remove       Remove a read-only directory from auto-include
  integrations.homebrew          Homebrew-backed integration resolution: enabled, disabled, or ask
  integrations.pin               Pin integrations to a project (value: project:name1,name2)
  integrations.unpin             Remove integration pinning for a project (value: project path)

Examples:
  hazmat config set backup.retention.keep_latest 30
  hazmat config set backup.excludes.add .idea/
  hazmat config set session.skip_permissions false
  hazmat config set session.status_bar true
  hazmat config set session.harness_assets false
  hazmat config set session.read_dirs.add ~/other-code
  hazmat config set integrations.homebrew enabled
  hazmat config set integrations.pin "~/workspace/my-app:node,python-uv"
  hazmat config set integrations.unpin ~/workspace/my-app`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runConfigSet(args[0], args[1])
		},
	}
}

func runConfigSet(key, value string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	switch key {
	case "backup.retention.keep_latest":
		n, err := parseInt(value)
		if err != nil {
			return err
		}
		cfg.Backup.Local.Retention.KeepLatest = n
	case "backup.retention.keep_daily":
		n, err := parseInt(value)
		if err != nil {
			return err
		}
		cfg.Backup.Local.Retention.KeepDaily = n
	case "backup.retention.keep_weekly":
		n, err := parseInt(value)
		if err != nil {
			return err
		}
		cfg.Backup.Local.Retention.KeepWeekly = n
	case "backup.excludes.add":
		cfg.Backup.Excludes = append(cfg.Backup.Excludes, value)
	case "backup.excludes.remove":
		filtered := cfg.Backup.Excludes[:0]
		for _, e := range cfg.Backup.Excludes {
			if e != value {
				filtered = append(filtered, e)
			}
		}
		cfg.Backup.Excludes = filtered
	case "backup.cloud.endpoint":
		ensureCloudConfig(&cfg)
		cfg.Backup.Cloud.Endpoint = value
	case "backup.cloud.bucket":
		ensureCloudConfig(&cfg)
		cfg.Backup.Cloud.Bucket = value
	case "backup.cloud.access_key":
		ensureCloudConfig(&cfg)
		cfg.Backup.Cloud.AccessKey = value
	case "session.skip_permissions":
		b := value == "true" || value == "1" || value == "yes"
		cfg.Session.SkipPermissions = &b
	case "session.status_bar":
		b := value == "true" || value == "1" || value == "yes"
		cfg.Session.StatusBar = &b
	case "session.harness_assets":
		b := value == "true" || value == "1" || value == "yes"
		cfg.Session.HarnessAssets = &b
	case "session.read_dirs.add":
		dirs := cfg.SessionReadDirs()
		for _, d := range dirs {
			if d == value {
				fmt.Printf("Already in read_dirs: %s\n", value)
				return nil
			}
		}
		dirs = append(dirs, value)
		cfg.Session.ReadDirs = &dirs
	case "session.read_dirs.remove":
		dirs := cfg.SessionReadDirs()
		filtered := dirs[:0]
		for _, d := range dirs {
			if d != value {
				filtered = append(filtered, d)
			}
		}
		cfg.Session.ReadDirs = &filtered
	case "integrations.homebrew":
		parsed, err := parseOptionalBool(value)
		if err != nil {
			return err
		}
		cfg.Integrations.Homebrew = parsed
	case "integrations.pin":
		// Format: "project:name1,name2"
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("integrations.pin format: project:name1,name2")
		}
		project := strings.TrimSpace(parts[0])
		if project == "" {
			return fmt.Errorf("integrations.pin format: project:name1,name2")
		}
		// Canonicalize the project path so pin/unpin/match all use the
		// same resolved form. This prevents ~/app and /Users/dr/app from
		// creating duplicate pins.
		canonProject, err := canonicalizePath(expandTilde(project))
		if err != nil {
			return fmt.Errorf("resolve project path %q: %w", project, err)
		}
		rawIntegrationNames := strings.Split(parts[1], ",")
		integrationNames := make([]string, 0, len(rawIntegrationNames))
		seenIntegrationNames := make(map[string]struct{}, len(rawIntegrationNames))
		for _, rawName := range rawIntegrationNames {
			name := strings.TrimSpace(rawName)
			if name == "" {
				return fmt.Errorf("integrations.pin format: project:name1,name2")
			}
			if _, seen := seenIntegrationNames[name]; seen {
				continue
			}
			if _, err := loadIntegrationSpecByName(name); err != nil {
				return fmt.Errorf("unknown integration %q: %w", name, err)
			}
			integrationNames = append(integrationNames, name)
			seenIntegrationNames[name] = struct{}{}
		}
		found := false
		for i, pin := range cfg.Integrations.Pinned {
			if pin.ProjectDir == canonProject {
				cfg.Integrations.Pinned[i].Integrations = integrationNames
				found = true
				break
			}
		}
		if !found {
			cfg.Integrations.Pinned = append(cfg.Integrations.Pinned, IntegrationPin{
				ProjectDir:   canonProject,
				Integrations: integrationNames,
			})
		}
	case "integrations.unpin":
		unpinPath := strings.TrimSpace(value)
		// Canonicalize so the unpin matches regardless of path spelling.
		if canonical, err := canonicalizePath(expandTilde(unpinPath)); err == nil {
			unpinPath = canonical
		}
		filtered := cfg.Integrations.Pinned[:0]
		for _, pin := range cfg.Integrations.Pinned {
			if pin.ProjectDir != unpinPath {
				filtered = append(filtered, pin)
			}
		}
		cfg.Integrations.Pinned = filtered
	case "packs.pin":
		return fmt.Errorf("packs.pin was removed before v1; use integrations.pin")
	case "packs.unpin":
		return fmt.Errorf("packs.unpin was removed before v1; use integrations.unpin")
	default:
		return fmt.Errorf("unknown key: %s\nRun 'hazmat config set --help' for available keys.", key)
	}

	if err := saveConfig(cfg); err != nil {
		return err
	}

	// If retention changed, update the Kopia repo policy too.
	if strings.HasPrefix(key, "backup.retention.") {
		if err := updateRetentionFromConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update Kopia retention policy: %v\n", err)
		}
	}

	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

func runConfigDocker(project, rawMode string) error {
	mode, err := parseDockerMode(rawMode)
	if err != nil {
		return err
	}
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectConfig)
	}

	projectCfg := cfg.Projects[projectDir]
	projectCfg.Docker = mode
	cfg.Projects[projectDir] = projectCfg
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}

	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Set Docker mode for %s = %s\n", projectDir, mode)
	return nil
}

func runConfigAccess(project string, readDirs, writeDirs []string, remove bool) error {
	if len(readDirs) == 0 && len(writeDirs) == 0 {
		return fmt.Errorf("specify at least one --read or --write directory")
	}

	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}
	readDirs, err = canonicalizeConfiguredDirs(readDirs)
	if err != nil {
		return fmt.Errorf("read dirs: %w", err)
	}
	writeDirs, err = canonicalizeConfiguredDirs(writeDirs)
	if err != nil {
		return fmt.Errorf("write dirs: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectConfig)
	}

	projectCfg := cfg.Projects[projectDir]
	projectCfg.ReadDirs = mergeConfiguredDirs(projectCfg.ReadDirs, readDirs, remove)
	projectCfg.WriteDirs = mergeConfiguredDirs(projectCfg.WriteDirs, writeDirs, remove)
	if projectHasOverrides(projectCfg) {
		cfg.Projects[projectDir] = projectCfg
	} else {
		delete(cfg.Projects, projectDir)
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}

	if err := saveConfig(cfg); err != nil {
		return err
	}

	action := "Updated"
	if remove {
		action = "Removed"
	}
	fmt.Printf("%s project access for %s\n", action, projectDir)
	return nil
}

func runConfigSSHAdd(project, name string, hosts []string, inventory, profile, keyArg string) error {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}

	name = strings.TrimSpace(name)
	inventory = strings.TrimSpace(inventory)
	profile = strings.TrimSpace(profile)
	keyArg = strings.TrimSpace(keyArg)

	if name == "" {
		return fmt.Errorf("--name is required")
	}
	sources := 0
	if inventory != "" {
		sources++
	}
	if profile != "" {
		sources++
	}
	if keyArg != "" {
		sources++
	}
	switch sources {
	case 0:
		return fmt.Errorf("pass one of: a private key path, --inventory <name>, or --profile <name>")
	case 1:
	default:
		return fmt.Errorf("pass exactly one of: private key path, --inventory, --profile (got %d)", sources)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectConfig)
	}
	projectCfg := cfg.Projects[projectDir]

	newKey := ProjectSSHKey{Name: name, Hosts: hosts}
	inheritedHosts := []string(nil)
	switch {
	case profile != "":
		prof, ok := cfg.SSHProfiles[profile]
		if !ok {
			return fmt.Errorf("--profile: %q is not defined in ssh_profiles (run 'hazmat config ssh profile add')", profile)
		}
		newKey.Profile = profile
		if len(hosts) == 0 {
			inheritedHosts = append([]string(nil), prof.DefaultHosts...)
		}
	case inventory != "":
		provisioned, err := findProvisionedSSHKey(inventory)
		if err != nil {
			return fmt.Errorf("--inventory: %w", err)
		}
		if !provisioned.Usable() {
			return fmt.Errorf("--inventory %q is not usable: %s", provisioned.Name, provisioned.Status)
		}
		newKey.Key = provisioned.Name
	default:
		selected, err := resolveSSHKeyPathArg(keyArg)
		if err != nil {
			return err
		}
		newKey.PrivateKeyPath = selected.PrivateKeyPath
		newKey.KnownHostsPath = selected.KnownHostsPath
	}

	mergedKeys := append([]ProjectSSHKey(nil), projectCfg.SSH.normalizedForMerge()...)
	for _, existing := range mergedKeys {
		if existing.Name == name {
			return fmt.Errorf("ssh key %q already exists; remove it first with 'hazmat config ssh remove --name %s'", name, name)
		}
	}
	mergedKeys = append(mergedKeys, newKey)

	newSSH := &ProjectSSHConfig{Keys: mergedKeys}
	if err := ValidateProjectSSHConfig(*newSSH); err != nil {
		return err
	}
	if err := ValidateProjectSSHProfileRefs(*newSSH, cfg.SSHProfiles); err != nil {
		return err
	}

	projectCfg.SSH = newSSH
	projectCfg.GitSSH = nil
	cfg.Projects[projectDir] = projectCfg
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Added SSH key %q to %s\n", name, projectDir)
	if profile != "" {
		fmt.Printf("  Profile: %s\n", profile)
	}
	switch {
	case len(hosts) > 0:
		fmt.Printf("  Hosts: %s\n", strings.Join(hosts, ", "))
	case len(inheritedHosts) > 0:
		fmt.Printf("  Hosts: %s (inherited from profile default_hosts)\n", strings.Join(inheritedHosts, ", "))
	default:
		if profile != "" {
			fmt.Printf("  Hosts: (none - profile has no default_hosts; this key routes nothing until hosts are added)\n")
		} else {
			fmt.Printf("  Hosts: (none)\n")
		}
	}
	return nil
}

func runConfigSSHRemove(project, name string) error {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	projectCfg, ok := cfg.Projects[projectDir]
	if !ok || projectCfg.SSH == nil {
		return fmt.Errorf("no SSH configuration for %s", projectDir)
	}

	keys := projectCfg.SSH.normalizedForMerge()
	filtered := make([]ProjectSSHKey, 0, len(keys))
	removed := false
	for _, key := range keys {
		if key.Name == name {
			removed = true
			continue
		}
		filtered = append(filtered, key)
	}
	if !removed {
		return fmt.Errorf("ssh key %q is not configured for %s", name, projectDir)
	}

	if len(filtered) == 0 {
		projectCfg.SSH = nil
	} else {
		projectCfg.SSH = &ProjectSSHConfig{Keys: filtered}
		if err := ValidateProjectSSHConfig(*projectCfg.SSH); err != nil {
			return err
		}
	}
	if projectHasOverrides(projectCfg) {
		cfg.Projects[projectDir] = projectCfg
	} else {
		delete(cfg.Projects, projectDir)
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Removed SSH key %q from %s\n", name, projectDir)
	return nil
}

func resolveSSHKeyPathArg(keyArg string) (sshKeyCandidate, error) {
	keyDir := defaultSSHKeyDirectory()
	if strings.Contains(keyArg, string(os.PathSeparator)) || filepath.IsAbs(keyArg) {
		if expanded := expandTilde(keyArg); expanded != "" {
			keyDir = filepath.Dir(expanded)
		}
	}
	canonicalKeyDir, err := resolveSSHKeyDirectory(keyDir)
	if err != nil {
		return sshKeyCandidate{}, err
	}
	keys, err := discoverSSHKeyCandidates(canonicalKeyDir)
	if err != nil {
		return sshKeyCandidate{}, err
	}
	selected, err := findSSHKeyCandidate(keys, keyArg)
	if err != nil {
		return sshKeyCandidate{}, err
	}
	if !selected.Usable() {
		return sshKeyCandidate{}, fmt.Errorf("SSH key %q is not usable: %s", selected.DisplayName(), selected.Status)
	}
	return selected, nil
}

// normalizedForMerge returns the existing project SSH config as a Keys list,
// suitable for appending with 'ssh add'. Nil and empty configs return an
// empty slice; legacy flat configs are rejected at config load before this
// helper runs.
func (c *ProjectSSHConfig) normalizedForMerge() []ProjectSSHKey {
	if c == nil {
		return nil
	}
	return c.NormalizedKeys()
}

func runConfigSSHProfileAdd(name, privateKeyArg, knownHosts string, defaultHosts []string, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	if !projectSSHKeyNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q (use letters, digits, '-', '_', '.')", name)
	}
	if strings.TrimSpace(privateKeyArg) == "" {
		return fmt.Errorf("private key path is required")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, exists := cfg.SSHProfiles[name]; exists {
		return fmt.Errorf("profile %q already exists; remove it first with 'hazmat config ssh profile remove %s'", name, name)
	}

	privateKeyPath, err := canonicalizeConfiguredFile(privateKeyArg)
	if err != nil {
		return fmt.Errorf("private_key: %w", err)
	}

	profile := SSHProfile{
		PrivateKeyPath: privateKeyPath,
		DefaultHosts:   append([]string(nil), defaultHosts...),
		Description:    strings.TrimSpace(description),
	}
	if strings.TrimSpace(knownHosts) != "" {
		resolved, err := canonicalizeConfiguredFile(knownHosts)
		if err != nil {
			return fmt.Errorf("known_hosts: %w", err)
		}
		profile.KnownHostsPath = resolved
	}

	if cfg.SSHProfiles == nil {
		cfg.SSHProfiles = make(map[string]SSHProfile)
	}
	cfg.SSHProfiles[name] = profile

	if err := ValidateSSHProfiles(cfg.SSHProfiles); err != nil {
		return err
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Added SSH profile %q\n", name)
	fmt.Printf("  Private key: %s\n", privateKeyPath)
	if profile.KnownHostsPath != "" {
		fmt.Printf("  Known hosts: %s\n", profile.KnownHostsPath)
	}
	if len(profile.DefaultHosts) > 0 {
		fmt.Printf("  Default hosts: %s\n", strings.Join(profile.DefaultHosts, ", "))
	}
	if profile.Description != "" {
		fmt.Printf("  Description: %s\n", profile.Description)
	}
	return nil
}

func runConfigSSHProfileList() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.SSHProfiles) == 0 {
		fmt.Println("No SSH profiles defined.")
		fmt.Println("Create one with: hazmat config ssh profile add <name> <private_key_path>")
		return nil
	}
	names := make([]string, 0, len(cfg.SSHProfiles))
	for name := range cfg.SSHProfiles {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Defined SSH profiles:")
	for _, name := range names {
		profile := cfg.SSHProfiles[name]
		referrers := findProfileReferrers(cfg, name)
		fmt.Printf("\n  %s\n", name)
		fmt.Printf("    Private key:    %s\n", profile.PrivateKeyPath)
		if len(profile.DefaultHosts) > 0 {
			fmt.Printf("    Default hosts:  %s\n", strings.Join(profile.DefaultHosts, ", "))
		}
		if profile.Description != "" {
			fmt.Printf("    Description:    %s\n", profile.Description)
		}
		if len(referrers) == 0 {
			fmt.Printf("    Referrers:      (none)\n")
		} else {
			fmt.Printf("    Referrers:      %d project(s)\n", len(referrers))
			for _, r := range referrers {
				fmt.Printf("      - %s\n", r)
			}
		}
	}
	return nil
}

func runConfigSSHProfileShow(name string) error {
	name = strings.TrimSpace(name)
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.SSHProfiles[name]
	if !ok {
		return fmt.Errorf("profile %q is not defined", name)
	}

	status := "usable"
	if _, err := canonicalizeConfiguredFile(profile.PrivateKeyPath); err != nil {
		status = fmt.Sprintf("broken (private_key: %v)", err)
	}

	fmt.Printf("Profile: %s\n", name)
	fmt.Printf("  Private key:   %s\n", profile.PrivateKeyPath)
	if profile.KnownHostsPath != "" {
		fmt.Printf("  Known hosts:   %s\n", profile.KnownHostsPath)
	}
	if len(profile.DefaultHosts) > 0 {
		fmt.Printf("  Default hosts: %s\n", strings.Join(profile.DefaultHosts, ", "))
	}
	if profile.Description != "" {
		fmt.Printf("  Description:   %s\n", profile.Description)
	}
	fmt.Printf("  Status:        %s\n", status)

	referrers := findProfileReferrers(cfg, name)
	if len(referrers) == 0 {
		fmt.Printf("  Referrers:     (none)\n")
	} else {
		fmt.Printf("  Referrers:\n")
		for _, r := range referrers {
			fmt.Printf("    - %s\n", r)
		}
	}
	return nil
}

func runConfigSSHProfileRemove(name string, force bool) error {
	name = strings.TrimSpace(name)
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, ok := cfg.SSHProfiles[name]; !ok {
		return fmt.Errorf("profile %q is not defined", name)
	}

	referrers := findProfileReferrers(cfg, name)
	if len(referrers) > 0 && !force {
		msg := fmt.Sprintf("profile %q is referenced by %d project(s):", name, len(referrers))
		for _, r := range referrers {
			msg += "\n  - " + r
		}
		msg += "\nrerun with --force to detach every reference and remove the profile,"
		msg += "\nor remove each project's reference first with 'hazmat config ssh remove --name <key>'."
		return fmt.Errorf("%s", msg)
	}

	detached := 0
	for _, projectDir := range referrers {
		project := cfg.Projects[projectDir]
		if project.SSH == nil {
			continue
		}
		filtered := make([]ProjectSSHKey, 0, len(project.SSH.Keys))
		for _, key := range project.SSH.Keys {
			if strings.TrimSpace(key.Profile) == name {
				detached++
				continue
			}
			filtered = append(filtered, key)
		}
		if len(filtered) == 0 {
			project.SSH = nil
		} else {
			project.SSH = &ProjectSSHConfig{Keys: filtered}
		}
		if projectHasOverrides(project) {
			cfg.Projects[projectDir] = project
		} else {
			delete(cfg.Projects, projectDir)
		}
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}
	delete(cfg.SSHProfiles, name)
	if len(cfg.SSHProfiles) == 0 {
		cfg.SSHProfiles = nil
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Removed SSH profile %q", name)
	if detached > 0 {
		fmt.Printf(" (detached %d project reference(s))", detached)
	}
	fmt.Println()
	return nil
}

func runConfigSSHProfileRename(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("both old and new names are required")
	}
	if oldName == newName {
		return fmt.Errorf("new name matches old name")
	}
	if !projectSSHKeyNamePattern.MatchString(newName) {
		return fmt.Errorf("invalid new name %q", newName)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.SSHProfiles[oldName]
	if !ok {
		return fmt.Errorf("profile %q is not defined", oldName)
	}
	if _, exists := cfg.SSHProfiles[newName]; exists {
		return fmt.Errorf("profile %q already exists", newName)
	}

	delete(cfg.SSHProfiles, oldName)
	cfg.SSHProfiles[newName] = profile

	referrers := findProfileReferrers(cfg, oldName) // recompute with OLD name (projects still point at oldName)
	for _, projectDir := range referrers {
		project := cfg.Projects[projectDir]
		if project.SSH == nil {
			continue
		}
		for i, key := range project.SSH.Keys {
			if strings.TrimSpace(key.Profile) == oldName {
				project.SSH.Keys[i].Profile = newName
			}
		}
		cfg.Projects[projectDir] = project
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Renamed SSH profile %q to %q", oldName, newName)
	if len(referrers) > 0 {
		fmt.Printf(" (updated %d project reference(s))", len(referrers))
	}
	fmt.Println()
	return nil
}

// findProfileReferrers returns the sorted list of project directories
// whose SSH config references the named profile.
func findProfileReferrers(cfg HazmatConfig, profileName string) []string {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return nil
	}
	var referrers []string
	for projectDir, project := range cfg.Projects {
		if project.SSH == nil {
			continue
		}
		for _, key := range project.SSH.Keys {
			if strings.TrimSpace(key.Profile) == profileName {
				referrers = append(referrers, projectDir)
				break
			}
		}
	}
	sort.Strings(referrers)
	return referrers
}

func runConfigSSHShow(project string) error {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	projectCfg, ok := cfg.Projects[projectDir]
	if !ok || (projectCfg.SSH == nil && projectCfg.GitSSH == nil) {
		fmt.Printf("No SSH key assigned to %s\n", projectDir)
		fmt.Printf("Set one with:\n  hazmat config ssh add -C %s --name github --host github.com <private-key>\n", projectDir)
		return nil
	}

	fmt.Printf("SSH configuration for %s\n\n", projectDir)
	if projectCfg.SSH != nil && len(projectCfg.SSH.Keys) > 0 {
		for i, key := range projectCfg.SSH.Keys {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("  Name:          %s\n", key.Name)
			switch {
			case strings.TrimSpace(key.PrivateKeyPath) != "":
				status := "usable"
				if _, err := canonicalizeConfiguredFile(key.PrivateKeyPath); err != nil {
					status = "broken (private key not found)"
				}
				fmt.Printf("  Private key:   %s\n", key.PrivateKeyPath)
				if key.KnownHostsPath != "" {
					fmt.Printf("  Known hosts:   %s\n", key.KnownHostsPath)
				}
				if fingerprint := sshKeyFingerprint(resolveConfiguredPublicKeyPath(key.PrivateKeyPath)); fingerprint != "" {
					fmt.Printf("  Fingerprint:   %s\n", fingerprint)
				}
				fmt.Printf("  Status:        %s\n", status)
			case strings.TrimSpace(key.Key) != "":
				provisioned, err := findProvisionedSSHKey(key.Key)
				if err != nil {
					fmt.Printf("  Inventory ref: %s\n", key.Key)
					fmt.Printf("  Status:        broken (%v)\n", err)
				} else {
					fmt.Printf("  Inventory ref: %s\n", provisioned.Name)
					fmt.Printf("  Private key:   %s\n", provisioned.PrivateKeyPath)
					fmt.Printf("  Known hosts:   %s\n", provisioned.KnownHostsPath)
					if provisioned.Fingerprint != "" {
						fmt.Printf("  Fingerprint:   %s\n", provisioned.Fingerprint)
					}
					fmt.Printf("  Status:        %s\n", provisioned.Status)
				}
			case strings.TrimSpace(key.Profile) != "":
				profileName := strings.TrimSpace(key.Profile)
				profile, ok := cfg.SSHProfiles[profileName]
				fmt.Printf("  Profile:       %s\n", profileName)
				if !ok {
					fmt.Printf("  Status:        broken (profile not defined)\n")
					break
				}
				status := "usable"
				if _, err := canonicalizeConfiguredFile(profile.PrivateKeyPath); err != nil {
					status = fmt.Sprintf("broken (profile private_key: %v)", err)
				}
				fmt.Printf("  Private key:   %s\n", profile.PrivateKeyPath)
				if profile.KnownHostsPath != "" {
					fmt.Printf("  Known hosts:   %s\n", profile.KnownHostsPath)
				}
				if fingerprint := sshKeyFingerprint(resolveConfiguredPublicKeyPath(profile.PrivateKeyPath)); fingerprint != "" {
					fmt.Printf("  Fingerprint:   %s\n", fingerprint)
				}
				fmt.Printf("  Status:        %s\n", status)
			}
			if len(key.Hosts) > 0 {
				fmt.Printf("  Hosts:         %s\n", strings.Join(key.Hosts, ", "))
			} else if strings.TrimSpace(key.Profile) != "" {
				effectiveHosts := key.EffectiveHosts(cfg.SSHProfiles)
				if len(effectiveHosts) > 0 {
					fmt.Printf("  Hosts:         %s (inherited from profile default_hosts)\n", strings.Join(effectiveHosts, ", "))
				} else {
					fmt.Printf("  Hosts:         (none - profile has no default_hosts; this key routes nothing)\n")
				}
			} else {
				fmt.Printf("  Hosts:         (none)\n")
			}
		}
		fmt.Printf("\nTest with:\n  hazmat config ssh test -C %s --host github.com\n", projectDir)
		return nil
	}
	if projectCfg.SSH != nil {
		if strings.TrimSpace(projectCfg.SSH.PrivateKeyPath) != "" {
			status := "usable"
			if _, err := canonicalizeConfiguredFile(projectCfg.SSH.PrivateKeyPath); err != nil {
				status = "broken (private key not found)"
			}
			if _, err := canonicalizeConfiguredFile(projectCfg.SSH.KnownHostsPath); err != nil {
				status = "broken (known_hosts not found)"
			}
			fmt.Printf("  Assigned key:  %s\n", filepath.Base(projectCfg.SSH.PrivateKeyPath))
			fmt.Printf("  Private key:   %s\n", projectCfg.SSH.PrivateKeyPath)
			fmt.Printf("  Known hosts:   %s\n", projectCfg.SSH.KnownHostsPath)
			fingerprint := sshKeyFingerprint(resolveConfiguredPublicKeyPath(projectCfg.SSH.PrivateKeyPath))
			if fingerprint != "" {
				fmt.Printf("  Fingerprint:   %s\n", fingerprint)
			}
			fmt.Printf("  Status:        %s\n", status)
			fmt.Printf("\nTest with:\n  hazmat config ssh test -C %s --host github.com\n", projectDir)
			return nil
		}
		key, err := findProvisionedSSHKey(projectCfg.SSH.Key)
		if err != nil {
			fmt.Printf("  Assigned key:  %s\n", projectCfg.SSH.Key)
			fmt.Printf("  Status:        broken (%v)\n", err)
			return nil
		}
		fmt.Printf("  Assigned key:  %s\n", key.Name)
		fmt.Printf("  Private key:   %s\n", key.PrivateKeyPath)
		fmt.Printf("  Known hosts:   %s\n", key.KnownHostsPath)
		if key.Fingerprint != "" {
			fmt.Printf("  Fingerprint:   %s\n", key.Fingerprint)
		}
		fmt.Printf("  Status:        %s\n", key.Status)
		fmt.Printf("\nTest with:\n  hazmat config ssh test -C %s --host github.com\n", projectDir)
		return nil
	}

	fmt.Printf("  Status:        legacy git_ssh configuration\n")
	fmt.Printf("  Private key:   %s\n", projectCfg.GitSSH.PrivateKeyPath)
	fmt.Printf("  Known hosts:   %s\n", projectCfg.GitSSH.KnownHostsPath)
	if len(projectCfg.GitSSH.AllowedHosts) > 0 {
		fmt.Printf("  Allowed hosts: %s\n", strings.Join(projectCfg.GitSSH.AllowedHosts, ", "))
	}
	return nil
}

func runConfigSSHTest(project, host string) error {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("project: %w", err)
	}

	target, err := resolveGitSSHTestTarget(host)
	if err != nil {
		return err
	}

	cfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		return err
	}
	cfg.GitSSH, err = resolveManagedGitSSH(cfg)
	if err != nil {
		return err
	}
	if cfg.GitSSH == nil {
		return fmt.Errorf("no SSH key assigned to %s\nrun:\n  hazmat config ssh add -C %s --name github --host github.com <private-key>", projectDir, projectDir)
	}

	selected, err := selectSessionGitSSHKey(cfg.GitSSH, target.RequestedHost)
	if err != nil {
		return err
	}

	fmt.Printf("Testing SSH for %s\n", projectDir)
	fmt.Printf("Using key: %s\n", selected.Name)
	fmt.Printf("Target host: %s\n", target.RequestedHost)
	if target.ResolvedFromSSHConfig {
		fmt.Printf("Resolved via ~/.ssh/config: %s\n", target.resolutionSummary())
	}
	if len(target.JumpTargets) > 0 {
		jumps := make([]string, 0, len(target.JumpTargets))
		for _, jump := range target.JumpTargets {
			jumps = append(jumps, jump.summary())
		}
		fmt.Printf("ProxyJump via ~/.ssh/config: %s\n", strings.Join(jumps, ","))
	}
	fmt.Println()

	output, err := probeGitSSHHost(*selected, target)
	if err == nil {
		fmt.Println("SSH test succeeded.")
		return nil
	}
	if output != "" {
		fmt.Println(strings.TrimSpace(output))
		fmt.Println()
	}
	return err
}

func runConfigSSHUnset(project, keyName string) error {
	projectDir, projectCfg, err := loadProjectSSHConfig(project)
	if err != nil {
		return err
	}
	if projectCfg == nil {
		fmt.Printf("No SSH key assigned to %s\n", projectDir)
		return nil
	}

	if err := validateSSHUnsetSelection(projectCfg, keyName); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	projectCfgValue := cfg.Projects[projectDir]
	projectCfgValue.SSH = nil
	projectCfgValue.GitSSH = nil
	if projectHasOverrides(projectCfgValue) {
		cfg.Projects[projectDir] = projectCfgValue
	} else {
		delete(cfg.Projects, projectDir)
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = nil
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Unset SSH configuration for %s\n", projectDir)
	return nil
}

func loadProjectSSHConfig(project string) (string, *ProjectConfig, error) {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return "", nil, fmt.Errorf("project: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return "", nil, err
	}
	if cfg.Projects == nil {
		return projectDir, nil, nil
	}
	projectCfg, ok := cfg.Projects[projectDir]
	if !ok || (projectCfg.SSH == nil && projectCfg.GitSSH == nil) {
		return projectDir, nil, nil
	}
	return projectDir, &projectCfg, nil
}

func validateSSHUnsetSelection(projectCfg *ProjectConfig, selection string) error {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return nil
	}

	suggestions := projectSSHUnsetSuggestions(*projectCfg)
	for _, suggestion := range suggestions {
		if selection == suggestion {
			return nil
		}
	}

	assigned := "(unknown)"
	if len(suggestions) > 0 {
		assigned = suggestions[0]
	}
	return fmt.Errorf("SSH key %q does not match the current project assignment %q", selection, assigned)
}

func runConfigSSHListKeys(keyDir string) error {
	canonicalKeyDir, err := resolveSSHKeyDirectory(keyDir)
	if err != nil {
		return err
	}
	keys, err := discoverSSHKeyCandidates(canonicalKeyDir)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Printf("No SSH keys found in %s\n", canonicalKeyDir)
		return nil
	}

	fmt.Printf("Available SSH keys in %s\n", canonicalKeyDir)
	for _, key := range keys {
		fmt.Printf("\n  %s\n", key.DisplayName())
		if key.PrivateKeyPath != "" {
			fmt.Printf("    Private key:  %s\n", key.PrivateKeyPath)
		}
		if key.PublicKeyPath != "" {
			fmt.Printf("    Public key:   %s\n", key.PublicKeyPath)
		}
		if key.KnownHostsPath != "" {
			fmt.Printf("    Known hosts:  %s\n", key.KnownHostsPath)
		}
		if key.Fingerprint != "" {
			fmt.Printf("    Fingerprint:  %s\n", key.Fingerprint)
		}
		fmt.Printf("    Status:       %s\n", key.Status)
	}
	return nil
}

func completeSSHUnsetKeyArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	project, _ := cmd.Flags().GetString("project")
	_, projectCfg, err := loadProjectSSHConfig(project)
	if err != nil || projectCfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	suggestions := filterSSHUnsetSuggestions(projectSSHUnsetSuggestions(*projectCfg), toComplete)
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func projectSSHUnsetSuggestions(projectCfg ProjectConfig) []string {
	if projectCfg.SSH != nil && len(projectCfg.SSH.Keys) > 0 {
		first := projectCfg.SSH.Keys[0]
		if privateKeyPath := strings.TrimSpace(first.PrivateKeyPath); privateKeyPath != "" {
			basename := filepath.Base(privateKeyPath)
			defaultDir, err := resolveSSHKeyDirectory("")
			if err == nil && filepath.Dir(privateKeyPath) == defaultDir {
				return []string{basename, privateKeyPath}
			}
			return []string{privateKeyPath, basename}
		}
	}
	if projectCfg.GitSSH != nil && strings.TrimSpace(projectCfg.GitSSH.PrivateKeyPath) != "" {
		privateKeyPath := strings.TrimSpace(projectCfg.GitSSH.PrivateKeyPath)
		return []string{privateKeyPath, filepath.Base(privateKeyPath)}
	}
	return nil
}

func filterSSHUnsetSuggestions(suggestions []string, toComplete string) []string {
	toComplete = strings.TrimSpace(toComplete)
	if toComplete == "" {
		if len(suggestions) == 0 {
			return nil
		}
		return []string{suggestions[0]}
	}

	filtered := make([]string, 0, len(suggestions))
	seen := make(map[string]struct{}, len(suggestions))
	canonicalPrefix := canonicalizeSSHCompletionPrefix(toComplete)
	for _, suggestion := range suggestions {
		if !strings.HasPrefix(suggestion, toComplete) && (canonicalPrefix == "" || !strings.HasPrefix(suggestion, canonicalPrefix)) {
			continue
		}
		if _, ok := seen[suggestion]; ok {
			continue
		}
		seen[suggestion] = struct{}{}
		filtered = append(filtered, suggestion)
	}
	sort.Strings(filtered)
	return filtered
}

func canonicalizeSSHCompletionPrefix(prefix string) string {
	if !strings.Contains(prefix, string(os.PathSeparator)) {
		return ""
	}

	expanded := expandTilde(prefix)
	if !filepath.IsAbs(expanded) {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		expanded = filepath.Join(wd, expanded)
	}

	dir := filepath.Dir(expanded)
	base := filepath.Base(expanded)
	resolvedDir, err := resolveDir(dir, false)
	if err != nil {
		return ""
	}
	return filepath.Join(resolvedDir, base)
}

func completeSSHAddKeyArgs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	suggestions, err := completeSSHKeyCandidates(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func completeSSHProfileAddArgs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	suggestions, err := completeSSHKeyCandidates(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func completeSSHKeyCandidates(toComplete string) ([]string, error) {
	dir, prefix, suggestionPrefix, err := resolveSSHCompletionScope(toComplete)
	if err != nil {
		return nil, err
	}

	keys, err := discoverSSHKeyCandidates(dir)
	if err != nil {
		return nil, err
	}

	suggestions := make([]string, 0, len(keys))
	for _, key := range usableSSHKeyCandidates(keys) {
		name := key.DisplayName()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		suggestions = append(suggestions, suggestionPrefix+name)
	}
	sort.Strings(suggestions)
	return suggestions, nil
}

func resolveSSHCompletionScope(toComplete string) (dir, prefix, suggestionPrefix string, err error) {
	toComplete = strings.TrimSpace(toComplete)
	if toComplete == "" {
		dir, err = resolveSSHKeyDirectory("")
		return dir, "", "", err
	}

	if strings.Contains(toComplete, string(os.PathSeparator)) {
		rawDir := filepath.Dir(toComplete)
		prefix = filepath.Base(toComplete)
		dir, err = resolveSSHCompletionDir(rawDir)
		if err != nil {
			return "", "", "", err
		}
		if rawDir == "." {
			return dir, prefix, "./", nil
		}
		return dir, prefix, rawDir + string(os.PathSeparator), nil
	}

	dir, err = resolveSSHKeyDirectory("")
	if err != nil {
		return "", "", "", err
	}
	return dir, toComplete, "", nil
}

func resolveSSHCompletionDir(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return resolveSSHKeyDirectory("")
	}
	expanded := expandTilde(raw)
	if filepath.IsAbs(expanded) {
		return resolveDir(expanded, false)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return resolveDir(filepath.Join(wd, expanded), false)
}

func ensureCloudConfig(cfg *HazmatConfig) {
	if cfg.Backup.Cloud == nil {
		cfg.Backup.Cloud = &CloudBackup{}
	}
}

func canonicalizeConfiguredDirs(paths []string) ([]string, error) {
	seen := make(map[string]struct{}, len(paths))
	var resolved []string
	for _, path := range paths {
		dir, err := resolveDir(expandTilde(path), false)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		resolved = append(resolved, dir)
	}
	return resolved, nil
}

func mergeConfiguredDirs(existing, values []string, remove bool) []string {
	if !remove {
		seen := make(map[string]struct{}, len(existing)+len(values))
		merged := make([]string, 0, len(existing)+len(values))
		for _, dir := range existing {
			if _, dup := seen[dir]; dup {
				continue
			}
			seen[dir] = struct{}{}
			merged = append(merged, dir)
		}
		for _, dir := range values {
			if _, dup := seen[dir]; dup {
				continue
			}
			seen[dir] = struct{}{}
			merged = append(merged, dir)
		}
		return merged
	}

	removeSet := make(map[string]struct{}, len(values))
	for _, dir := range values {
		removeSet[dir] = struct{}{}
	}
	filtered := existing[:0]
	for _, dir := range existing {
		if _, drop := removeSet[dir]; drop {
			continue
		}
		filtered = append(filtered, dir)
	}
	return filtered
}

func projectHasOverrides(projectCfg ProjectConfig) bool {
	return validDockerMode(projectCfg.Docker) ||
		len(projectCfg.ReadDirs) > 0 ||
		len(projectCfg.WriteDirs) > 0 ||
		projectCfg.SSH != nil ||
		projectCfg.GitSSH != nil
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %s", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("value must be non-negative: %d", n)
	}
	return n, nil
}

func parseOptionalBool(value string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enabled", "enable", "true", "1", "yes", "on":
		return boolPtr(true), nil
	case "disabled", "disable", "false", "0", "no", "off":
		return boolPtr(false), nil
	case "ask", "unset", "default", "auto":
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid value %q (want enabled, disabled, or ask)", value)
	}
}
