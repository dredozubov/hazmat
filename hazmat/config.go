package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Backup  BackupConfig  `yaml:"backup"`
	Session SessionConfig `yaml:"session"`
	Packs   PacksConfig   `yaml:"packs,omitempty"`
	Sandbox SandboxConfig `yaml:"sandbox,omitempty"`
}

type SessionConfig struct {
	// SkipPermissions passes --dangerously-skip-permissions to Claude Code.
	// Default: true. The containment is OS-level (user isolation + seatbelt
	// + pf firewall); Claude's permission prompts are redundant inside hazmat.
	// Set to false if you want Claude's own permission prompts as an
	// additional layer.
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

type SandboxConfig struct {
	Backend *SandboxBackendConfig `yaml:"backend,omitempty"`
}

type SandboxBackendConfig struct {
	Type           string `yaml:"type,omitempty"`
	PolicyProfile  string `yaml:"policy_profile,omitempty"`
	DesktopVersion string `yaml:"docker_desktop_version,omitempty"`
	ComposeVersion string `yaml:"compose_version,omitempty"`
	ConfiguredAt   string `yaml:"configured_at,omitempty"`
}

// PacksConfig holds per-project pack pinning.
type PacksConfig struct {
	// Pinned maps canonical project paths to pack names.
	// Input paths are normalized through Abs + EvalSymlinks before storage,
	// so matching is stable across different spellings of the same path.
	Pinned []PackPin `yaml:"pinned,omitempty"`
}

// PackPin associates a project directory with a list of pack names.
type PackPin struct {
	ProjectDir string   `yaml:"project"`
	Packs      []string `yaml:"packs"`
}

// PackPins returns the configured pack pins (nil if none).
func (c HazmatConfig) PackPins() []PackPin {
	return c.Packs.Pinned
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

// SkipPermissions returns whether --dangerously-skip-permissions should be
// passed to Claude Code. Default: true.
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

func (c HazmatConfig) SandboxBackend() *SandboxBackendConfig {
	if c.Sandbox.Backend == nil || c.Sandbox.Backend.Type == "" {
		return nil
	}
	return c.Sandbox.Backend
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

	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
  hazmat config edit         Open config in $EDITOR
  hazmat config agent        Configure API key and git identity
  hazmat config import claude Import portable Claude basics
  hazmat config import opencode Import portable OpenCode basics
  hazmat config cloud        Configure S3 cloud backup credentials
  hazmat config set K V      Set a configuration value

Examples:
  hazmat config
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
	fmt.Printf("    Skip permissions: %v (--dangerously-skip-permissions)\n", cfg.SkipPermissions())
	fmt.Printf("    Status bar:       %v (opt-in)\n", cfg.StatusBar())
	readDirs := cfg.SessionReadDirs()
	if len(readDirs) > 0 {
		fmt.Printf("    Read dirs:        %s\n", strings.Join(readDirs, ", "))
	} else {
		fmt.Printf("    Read dirs:        (none)\n")
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
  session.skip_permissions       Pass --dangerously-skip-permissions to Claude (default: true)
  session.status_bar             Enable Hazmat's terminal status bar (default: false)
  session.read_dirs.add          Add a read-only directory to auto-include in sessions
  session.read_dirs.remove       Remove a read-only directory from auto-include
  packs.pin                      Pin packs to a project (value: project:pack1,pack2)
  packs.unpin                    Remove pack pinning for a project (value: project path)

Examples:
  hazmat config set backup.retention.keep_latest 30
  hazmat config set backup.excludes.add .idea/
  hazmat config set session.skip_permissions false
  hazmat config set session.status_bar true
  hazmat config set session.read_dirs.add ~/other-code
  hazmat config set packs.pin "~/workspace/my-app:node,python-poetry"
  hazmat config set packs.unpin ~/workspace/my-app`,
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
	case "packs.pin":
		// Format: "project:pack1,pack2"
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("packs.pin format: project:pack1,pack2")
		}
		project := strings.TrimSpace(parts[0])
		if project == "" {
			return fmt.Errorf("packs.pin format: project:pack1,pack2")
		}
		// Canonicalize the project path so pin/unpin/match all use the
		// same resolved form. This prevents ~/app and /Users/dr/app from
		// creating duplicate pins.
		canonProject, err := canonicalizePath(expandTilde(project))
		if err != nil {
			return fmt.Errorf("resolve project path %q: %w", project, err)
		}
		rawPackNames := strings.Split(parts[1], ",")
		packNames := make([]string, 0, len(rawPackNames))
		seenPackNames := make(map[string]struct{}, len(rawPackNames))
		// Validate pack names exist.
		for _, rawName := range rawPackNames {
			name := strings.TrimSpace(rawName)
			if name == "" {
				return fmt.Errorf("packs.pin format: project:pack1,pack2")
			}
			if _, seen := seenPackNames[name]; seen {
				continue
			}
			if _, err := loadPackByName(name); err != nil {
				return fmt.Errorf("unknown pack %q: %w", name, err)
			}
			packNames = append(packNames, name)
			seenPackNames[name] = struct{}{}
		}
		// Replace existing pin for this project, or append.
		found := false
		for i, pin := range cfg.Packs.Pinned {
			if pin.ProjectDir == canonProject {
				cfg.Packs.Pinned[i].Packs = packNames
				found = true
				break
			}
		}
		if !found {
			cfg.Packs.Pinned = append(cfg.Packs.Pinned, PackPin{
				ProjectDir: canonProject,
				Packs:      packNames,
			})
		}
	case "packs.unpin":
		unpinPath := strings.TrimSpace(value)
		// Canonicalize so the unpin matches regardless of path spelling.
		if canonical, err := canonicalizePath(expandTilde(unpinPath)); err == nil {
			unpinPath = canonical
		}
		filtered := cfg.Packs.Pinned[:0]
		for _, pin := range cfg.Packs.Pinned {
			if pin.ProjectDir != unpinPath {
				filtered = append(filtered, pin)
			}
		}
		cfg.Packs.Pinned = filtered
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

func ensureCloudConfig(cfg *HazmatConfig) {
	if cfg.Backup.Cloud == nil {
		cfg.Backup.Cloud = &CloudBackup{}
	}
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
