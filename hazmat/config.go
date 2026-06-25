package hazmat

import (
	"fmt"
	"hazmat/credentials"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"hazmat/configmodel"
	"hazmat/internal/frontend/cli"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Config file path ────────────────────────────────────────────────────────

var configFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/config.yaml")

// cloudCredentialPath is the pre-secret-store S3 secret key location.
// Current releases migrate it into ~/.hazmat/secrets/cloud/s3-secret-key.
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

type SSHProfile = configmodel.SSHProfile

type ProjectConfig = configmodel.ProjectConfig
type ProjectSSHConfig = configmodel.ProjectSSHConfig
type ProjectSSHKey = configmodel.ProjectSSHKey
type ProjectGitSSHConfig = configmodel.ProjectGitSSHConfig
type SessionConfig = configmodel.SessionConfig
type IntegrationsConfig = configmodel.IntegrationsConfig
type IntegrationPin = configmodel.IntegrationPin
type IntegrationRejection = configmodel.IntegrationRejection
type SandboxConfig = configmodel.SandboxConfig
type SandboxBackendConfig = configmodel.SandboxBackendConfig
type ManagedSandboxConfig = configmodel.ManagedSandboxConfig
type BackupConfig = configmodel.BackupConfig
type LocalBackupConfig = configmodel.LocalBackupConfig
type RetentionConfig = configmodel.RetentionConfig
type CloudBackup = configmodel.CloudBackup
type dockerMode = configmodel.DockerMode

const (
	dockerModeAuto    = configmodel.DockerModeAuto
	dockerModeNone    = configmodel.DockerModeNone
	dockerModeSandbox = configmodel.DockerModeSandbox
)

// PinnedIntegrations returns the configured integration pins (nil if none).
func (c HazmatConfig) PinnedIntegrations() []IntegrationPin {
	return cloneIntegrationPins(c.Integrations.Pinned)
}

func (c HazmatConfig) RejectedIntegrations() []IntegrationRejection {
	return cloneIntegrationRejections(c.Integrations.Rejected)
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

func cloneIntegrationPins(values []IntegrationPin) []IntegrationPin {
	if len(values) == 0 {
		return nil
	}
	out := make([]IntegrationPin, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Integrations = append([]string(nil), value.Integrations...)
	}
	return out
}

func cloneIntegrationRejections(values []IntegrationRejection) []IntegrationRejection {
	if len(values) == 0 {
		return nil
	}
	out := make([]IntegrationRejection, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Integrations = append([]string(nil), value.Integrations...)
	}
	return out
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
		return append([]string(nil), (*c.Session.ReadDirs)...)
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
	backend := *c.Sandbox.Backend
	return &backend
}

func (c HazmatConfig) ManagedSandboxes() []ManagedSandboxConfig {
	return append([]ManagedSandboxConfig(nil), c.Sandbox.Managed...)
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
func detectLegacyFlatSSH(projectDir string, c ProjectSSHConfig) error {
	return configmodel.DetectLegacyFlatSSH(projectDir, c)
}

func ValidateProjectSSHConfig(c ProjectSSHConfig) error {
	return configmodel.ValidateProjectSSHConfig(c)
}

func ValidateSSHProfiles(profiles map[string]SSHProfile) error {
	return configmodel.ValidateSSHProfiles(profiles)
}

func ValidateProjectSSHProfileRefs(c ProjectSSHConfig, profiles map[string]SSHProfile) error {
	return configmodel.ValidateProjectSSHProfileRefs(c, profiles)
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
	cloudMigrated, err := migrateCloudCredentialsIntoSecretStore(&cfg)
	if err != nil {
		return cfg, err
	}

	if err := ValidateSSHProfiles(cfg.SSHProfiles); err != nil {
		return cfg, err
	}
	sandboxConfig, err := configmodel.NormalizeSandboxConfig(cfg.Sandbox)
	if err != nil {
		return cfg, err
	}
	cfg.Sandbox = sandboxConfig
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

	if cloudMigrated {
		if err := saveConfig(cfg); err != nil {
			return cfg, fmt.Errorf("save migrated cloud credential config: %w", err)
		}
	}
	return cfg, nil
}

func saveConfig(cfg HazmatConfig) error {
	dir := filepath.Dir(configFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	safeCfg := scrubConfigSecretsForSave(cfg)
	data, err := yaml.Marshal(&safeCfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	header := "# Hazmat configuration\n# Edit manually or via: hazmat config set <key> <value>\n\n"
	return os.WriteFile(configFilePath, []byte(header+string(data)), 0o600)
}

func scrubConfigSecretsForSave(cfg HazmatConfig) HazmatConfig {
	if cfg.Backup.Cloud == nil {
		return cfg
	}
	cloud := *cfg.Backup.Cloud
	cloud.AccessKey = ""
	cloud.RecoveryKey = ""
	cloud.LegacyRecoveryKey = ""
	cfg.Backup.Cloud = &cloud
	return cfg
}

// ── Cloud credentials ──────────────────────────────────────────────────────

func migrateCloudCredentialsIntoSecretStore(cfg *HazmatConfig) (bool, error) {
	if cfg.Backup.Cloud == nil {
		return false, nil
	}

	migrated := false
	if cfg.Backup.Cloud.AccessKey != "" {
		if err := saveCloudStoredCredential(credentials.CloudS3AccessKeyID, cfg.Backup.Cloud.AccessKey); err != nil {
			return false, fmt.Errorf("migrate cloud access key: %w", err)
		}
		migrated = true
	}
	if cfg.Backup.Cloud.RecoveryKey != "" {
		if err := saveCloudStoredCredential(credentials.CloudKopiaRecovery, cfg.Backup.Cloud.RecoveryKey); err != nil {
			return false, fmt.Errorf("migrate cloud recovery key: %w", err)
		}
		migrated = true
	}

	cfg.Backup.Cloud.AccessKey = ""
	cfg.Backup.Cloud.RecoveryKey = ""
	cfg.Backup.Cloud.LegacyRecoveryKey = ""

	if accessKey, ok, err := readCloudStoredCredential(credentials.CloudS3AccessKeyID); err != nil {
		return migrated, fmt.Errorf("read cloud access key: %w", err)
	} else if ok {
		cfg.Backup.Cloud.AccessKey = accessKey
	}
	if recoveryKey := os.Getenv("HAZMAT_CLOUD_PASSWORD"); recoveryKey != "" {
		cfg.Backup.Cloud.RecoveryKey = recoveryKey
	} else if recoveryKey, ok, err := readCloudStoredCredential(credentials.CloudKopiaRecovery); err != nil {
		return migrated, fmt.Errorf("read cloud recovery key: %w", err)
	} else if ok {
		cfg.Backup.Cloud.RecoveryKey = recoveryKey
	}

	return migrated, nil
}

func loadCloudAccessKey() (string, error) {
	if key, ok, err := readCloudStoredCredential(credentials.CloudS3AccessKeyID); err != nil {
		return "", fmt.Errorf("read cloud access key: %w", err)
	} else if ok {
		return key, nil
	}
	return "", fmt.Errorf("cloud access key not configured\nRun: hazmat config cloud")
}

func saveCloudAccessKey(key string) error {
	return saveCloudStoredCredential(credentials.CloudS3AccessKeyID, key)
}

func loadCloudRecoveryKey() (string, error) {
	if key := os.Getenv("HAZMAT_CLOUD_PASSWORD"); key != "" {
		return key, nil
	}
	if key, ok, err := readCloudStoredCredential(credentials.CloudKopiaRecovery); err != nil {
		return "", fmt.Errorf("read cloud recovery key: %w", err)
	} else if ok {
		return key, nil
	}
	return "", fmt.Errorf("cloud recovery key not configured\nRun: hazmat config cloud")
}

func saveCloudRecoveryKey(key string) error {
	return saveCloudStoredCredential(credentials.CloudKopiaRecovery, key)
}

func loadCloudSecretKey() (string, error) {
	// Environment variable takes precedence
	if key := os.Getenv("HAZMAT_CLOUD_SECRET_KEY"); key != "" {
		return key, nil
	}
	if key, ok, err := readCloudStoredCredential(credentials.CloudS3SecretKey); err != nil {
		return "", fmt.Errorf("read cloud secret key: %w", err)
	} else if ok {
		return key, nil
	}

	data, err := os.ReadFile(cloudCredentialPath)
	if err != nil {
		return "", fmt.Errorf("read cloud credentials: %w\nSet HAZMAT_CLOUD_SECRET_KEY or run: hazmat config cloud", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("legacy cloud credential file is empty\nSet HAZMAT_CLOUD_SECRET_KEY or run: hazmat config cloud")
	}
	if err := saveCloudSecretKey(key); err != nil {
		return "", fmt.Errorf("migrate legacy cloud credential: %w", err)
	}
	if err := os.Remove(cloudCredentialPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove legacy cloud credential %s: %w", cloudCredentialPath, err)
	}
	return key, nil
}

func saveCloudSecretKey(key string) error {
	if err := saveCloudStoredCredential(credentials.CloudS3SecretKey, key); err != nil {
		return err
	}
	if err := os.Remove(cloudCredentialPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy cloud credential %s: %w", cloudCredentialPath, err)
	}
	return nil
}

// ── Commands ────────────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	return cli.NewConfigCommand(cli.ConfigCommandConfig{
		RunShow:               runConfigShow,
		RunEdit:               runConfigEdit,
		RunDocker:             runConfigDocker,
		RunAccess:             runConfigAccess,
		RunSet:                runConfigSet,
		RunSSHAdd:             runConfigSSHAdd,
		RunSSHRemove:          runConfigSSHRemove,
		RunSSHShow:            runConfigSSHShow,
		RunSSHTest:            runConfigSSHTest,
		RunSSHUnset:           runConfigSSHUnset,
		RunSSHListKey:         runConfigSSHListKeys,
		RunSSHProfileAdd:      runConfigSSHProfileAdd,
		RunSSHProfileList:     runConfigSSHProfileList,
		RunSSHProfileShow:     runConfigSSHProfileShow,
		RunSSHProfileRemove:   runConfigSSHProfileRemove,
		RunSSHProfileRename:   runConfigSSHProfileRename,
		CompleteSSHAdd:        completeSSHAddKeyArgs,
		CompleteSSHUnset:      completeSSHUnsetKeyArgs,
		CompleteSSHProfileAdd: completeSSHProfileAddArgs,
		ExtraCommands: []*cobra.Command{
			newConfigSudoersCmd(),
			newConfigAgentCmd(),
			newConfigGitHubCmd(),
			newConfigImportCmd(),
			newConfigCloudCmd(),
		},
	})
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
	if _, ok, err := readGitHubStoredToken(); err == nil && ok {
		fmt.Printf("    GitHub API:       configured (enable per session with --github)\n")
	} else {
		fmt.Printf("    GitHub API:       not configured\n")
	}
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

func runConfigEdit() error {
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
		if err := saveCloudAccessKey(value); err != nil {
			return err
		}
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
		cfg.Integrations.Homebrew = parsed.ptr()
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

	mergedKeys := append([]ProjectSSHKey(nil), normalizedProjectSSHKeysForMerge(projectCfg.SSH)...)
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

	keys := normalizedProjectSSHKeysForMerge(projectCfg.SSH)
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

// normalizedProjectSSHKeysForMerge returns the existing project SSH config as
// a Keys list, suitable for appending with 'ssh add'. Nil and empty configs
// return an empty slice; legacy flat configs are rejected at config load before
// this helper runs.
func normalizedProjectSSHKeysForMerge(cfg *ProjectSSHConfig) []ProjectSSHKey {
	if cfg == nil {
		return nil
	}
	return cfg.NormalizedKeys()
}

func runConfigSSHProfileAdd(name, privateKeyArg, knownHosts string, defaultHosts []string, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	if !configmodel.ValidProjectSSHKeyName(name) {
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
	if !configmodel.ValidProjectSSHKeyName(newName) {
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
	managedGitSSH, err := resolveManagedGitSSH(cfg)
	if err != nil {
		return err
	}
	cfg.GitSSH = managedGitSSH.ptr()
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

type optionalBoolValue struct {
	value bool
	set   bool
}

func (value optionalBoolValue) ptr() *bool {
	if !value.set {
		return nil
	}
	return boolPtr(value.value)
}

func parseOptionalBool(value string) (optionalBoolValue, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enabled", "enable", "true", "1", "yes", "on":
		return optionalBoolValue{value: true, set: true}, nil
	case "disabled", "disable", "false", "0", "no", "off":
		return optionalBoolValue{value: false, set: true}, nil
	case "ask", "unset", "default", "auto":
		return optionalBoolValue{}, nil
	default:
		return optionalBoolValue{}, fmt.Errorf("invalid value %q (want enabled, disabled, or ask)", value)
	}
}
