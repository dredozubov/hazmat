package configmodel

import (
	"fmt"
	"strings"
)

type DockerMode string

const (
	DockerModeAuto    DockerMode = "auto"
	DockerModeNone    DockerMode = "none"
	DockerModeSandbox DockerMode = "sandbox"
)

// ExecutionProvider selects an external semantic execution boundary. Empty
// keeps Hazmat's existing local runtime selection; Planescape is opt-in and
// must never fall back to that local path.
type ExecutionProvider string

const (
	ExecutionProviderLocal      ExecutionProvider = ""
	ExecutionProviderPlanescape ExecutionProvider = "planescape"
)

type ProjectConfig struct {
	Docker    DockerMode           `yaml:"docker,omitempty"`
	ReadDirs  []string             `yaml:"read_dirs,omitempty"`
	WriteDirs []string             `yaml:"write_dirs,omitempty"`
	SSH       *ProjectSSHConfig    `yaml:"ssh,omitempty"`
	GitSSH    *ProjectGitSSHConfig `yaml:"git_ssh,omitempty"`
}

type SessionConfig struct {
	// ExecutionProvider selects the external provider for every product
	// session. Planescape remains fail-closed until its live endpoint and RPC
	// admission path are available.
	ExecutionProvider ExecutionProvider `yaml:"execution_provider,omitempty"`

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
	Endpoint string `yaml:"endpoint"`
	Bucket   string `yaml:"bucket"`
	// AccessKey and RecoveryKey are hydrated from the host secret store at
	// load time for existing callers. saveConfig always scrubs them from YAML.
	AccessKey   string `yaml:"access_key,omitempty"`
	RecoveryKey string `yaml:"recovery_key,omitempty"` // legacy Kopia repo encryption key location
	// LegacyRecoveryKey accepts the pre-rename `password:` YAML key. Migrated to
	// RecoveryKey on load and dropped on next save.
	LegacyRecoveryKey string `yaml:"password,omitempty"`
	// SecretKey is not represented in config; it lives in the host secret store.
}

func ValidDockerMode(mode DockerMode) bool {
	switch mode {
	case DockerModeAuto, DockerModeNone, DockerModeSandbox:
		return true
	default:
		return false
	}
}

func ParseDockerMode(raw string) (DockerMode, error) {
	mode := DockerMode(strings.ToLower(strings.TrimSpace(raw)))
	if ValidDockerMode(mode) {
		return mode, nil
	}
	return "", fmt.Errorf("invalid Docker mode %q (want auto, none, or sandbox)", raw)
}

func ParseExecutionProvider(raw string) (ExecutionProvider, error) {
	provider := ExecutionProvider(strings.ToLower(strings.TrimSpace(raw)))
	switch provider {
	case ExecutionProviderLocal, ExecutionProviderPlanescape:
		return provider, nil
	default:
		return "", fmt.Errorf("invalid execution provider %q (want planescape or empty)", raw)
	}
}
