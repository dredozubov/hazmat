package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
}

type ProjectConfig struct {
	Docker    dockerMode `yaml:"docker,omitempty"`
	ReadDirs  []string   `yaml:"read_dirs,omitempty"`
	WriteDirs []string   `yaml:"write_dirs,omitempty"`
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
}

type IntegrationsConfig struct {
	Homebrew *bool `yaml:"homebrew,omitempty"`
	// Pinned maps canonical project paths to integration names.
	// Input paths are normalized through Abs + EvalSymlinks before storage,
	// so matching is stable across different spellings of the same path.
	Pinned []IntegrationPin `yaml:"pinned,omitempty"`
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

// PinnedIntegrations returns the configured integration pins (nil if none).
func (c HazmatConfig) PinnedIntegrations() []IntegrationPin {
	return c.Integrations.Pinned
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
	Endpoint  string `yaml:"endpoint"`
	Bucket    string `yaml:"bucket"`
	AccessKey string `yaml:"access_key"`
	Password  string `yaml:"password"` // Kopia repo encryption password
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
		return dockerModeAuto, false
	}
	project, ok := c.Projects[projectDir]
	if !ok || !validDockerMode(project.Docker) || project.Docker == dockerModeAuto {
		return dockerModeAuto, false
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
  hazmat config edit         Open config in $EDITOR
  hazmat config agent        Configure API key and git identity
  hazmat config import claude Import portable Claude basics
  hazmat config import opencode Import portable OpenCode basics
  hazmat config cloud        Configure S3 cloud backup credentials
  hazmat config set K V      Set a configuration value

Examples:
  hazmat config
  hazmat config docker none -C ~/workspace/my-project
  hazmat config access add -C ~/workspace/my-project --read ~/other-code
  hazmat config agent
  hazmat config import claude --dry-run
  hazmat config import opencode --dry-run
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
		if cfg.Backup.Cloud.Password != "" {
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
				if validDockerMode(projectCfg.Docker) && projectCfg.Docker != dockerModeAuto {
					fmt.Printf("        Docker: %s\n", projectCfg.Docker)
				}
				if len(projectCfg.ReadDirs) > 0 {
					fmt.Printf("        Read-only: %s\n", strings.Join(projectCfg.ReadDirs, ", "))
				}
				if len(projectCfg.WriteDirs) > 0 {
					fmt.Printf("        Read-write: %s\n", strings.Join(projectCfg.WriteDirs, ", "))
				}
			}
		} else {
			fmt.Printf("    Project overrides: (none)\n")
		}
	} else {
		fmt.Printf("    Project overrides: (none)\n")
	}
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
		Use:   "docker <auto|none|sandbox>",
		Short: "Configure per-project Docker routing",
		Long: `Set the preferred Docker routing mode for a project.

Modes:
  auto     Use Hazmat's default Docker detection and routing
  none     Keep sessions in native containment for code-only work
  sandbox  Force Docker Sandbox mode for private-daemon Docker workflows

Examples:
  hazmat config docker none -C ~/workspace/my-project
  hazmat config docker sandbox -C ~/workspace/docker-app
  hazmat config docker auto -C ~/workspace/my-project`,
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

	if mode == dockerModeAuto {
		delete(cfg.Projects, projectDir)
	} else {
		cfg.Projects[projectDir] = ProjectConfig{Docker: mode}
	}
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
	return (validDockerMode(projectCfg.Docker) && projectCfg.Docker != dockerModeAuto) ||
		len(projectCfg.ReadDirs) > 0 ||
		len(projectCfg.WriteDirs) > 0
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
