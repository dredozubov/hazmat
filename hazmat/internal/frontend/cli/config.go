package cli

import "github.com/spf13/cobra"

type ConfigCommandConfig struct {
	RunShow   func() error
	RunEdit   func() error
	RunDocker func(project, rawMode string) error
	RunAccess func(project string, readDirs, writeDirs []string, remove bool) error
	RunSet    func(key, value string) error

	RunSSHAdd     func(project, name string, hosts []string, inventory, profile, keyArg string) error
	RunSSHRemove  func(project, name string) error
	RunSSHShow    func(project string) error
	RunSSHTest    func(project, host string) error
	RunSSHUnset   func(project, keyName string) error
	RunSSHListKey func(keyDir string) error

	RunSSHProfileAdd    func(name, privateKeyArg, knownHosts string, defaultHosts []string, description string) error
	RunSSHProfileList   func() error
	RunSSHProfileShow   func(name string) error
	RunSSHProfileRemove func(name string, force bool) error
	RunSSHProfileRename func(oldName, newName string) error

	CompleteSSHAdd        func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)
	CompleteSSHUnset      func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)
	CompleteSSHProfileAdd func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

	ExtraCommands []*cobra.Command
}

func NewConfigCommand(config ConfigCommandConfig) *cobra.Command {
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
  hazmat config github       Configure explicit GitHub API session access
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
  hazmat config github --token-from-env
  hazmat config import claude --dry-run
  hazmat config import codex --dry-run
  hazmat config import opencode --dry-run
  hazmat config import gemini --dry-run
  hazmat config cloud --endpoint s3.fr-par.scw.cloud --bucket my-backups
  hazmat config set backup.retention.keep_latest 30`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.RunShow()
		},
	}

	cmd.AddCommand(newConfigEditCommand(config.RunEdit))
	cmd.AddCommand(newConfigDockerCommand(config.RunDocker))
	cmd.AddCommand(newConfigAccessCommand(config.RunAccess))
	cmd.AddCommand(newConfigSSHCommand(config))
	for _, command := range config.ExtraCommands {
		if command != nil {
			cmd.AddCommand(command)
		}
	}
	cmd.AddCommand(newConfigSetCommand(config.RunSet))

	return cmd
}

func newConfigEditCommand(runEdit func() error) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open configuration in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runEdit()
		},
	}
}

func newConfigDockerCommand(runDocker func(project, rawMode string) error) *cobra.Command {
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
			return runDocker(project, args[0])
		},
	}

	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	return cmd
}

func newConfigAccessCommand(runAccess func(project string, readDirs, writeDirs []string, remove bool) error) *cobra.Command {
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
				return runAccess(project, readDirs, writeDirs, remove)
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

func newConfigSSHCommand(config ConfigCommandConfig) *cobra.Command {
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
			return config.RunSSHAdd(project, addName, addHosts, addInventory, addProfile, keyArg)
		},
	}
	addCmd.ValidArgsFunction = config.CompleteSSHAdd
	addCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	addCmd.Flags().StringVar(&addName, "name", "",
		"Name for this key (used for routing and display)")
	addCmd.Flags().StringArrayVar(&addHosts, "host", nil,
		"Destination host this key serves (repeatable, supports glob)")
	addCmd.Flags().StringVar(&addInventory, "inventory", "",
		"Reference a provisioned key from ~/.hazmat/secrets/git-ssh/provisioned/<name>/ instead of a path")
	addCmd.Flags().StringVar(&addProfile, "profile", "",
		"Reference a shared SSH profile defined in ssh_profiles")

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a named SSH key from a project",
		Long: `Remove a single named SSH key from a project's Keys list. When the
last key is removed, the project's SSH configuration is cleared.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.RunSSHRemove(project, removeName)
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
			return config.RunSSHShow(project)
		},
	}
	showCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")

	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Test the assigned SSH key against a Git SSH host",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.RunSSHTest(project, host)
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
		ValidArgsFunction: config.CompleteSSHUnset,
		RunE: func(_ *cobra.Command, args []string) error {
			selectedKey := ""
			if len(args) == 1 {
				selectedKey = args[0]
			}
			return config.RunSSHUnset(project, selectedKey)
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
			return config.RunSSHUnset(project, selectedKey)
		},
	}
	clearCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")

	listCmd := &cobra.Command{
		Use:   "list-keys",
		Short: "List SSH keys in a directory",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.RunSSHListKey(listKeyDir)
		},
	}
	listCmd.Flags().StringVar(&listKeyDir, "dir", "",
		"Directory containing SSH keys (defaults to ~/.ssh)")

	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Configure per-project Git-over-SSH key selection",
		Long: `Assign an SSH key from a chosen directory to a project for Git-over-SSH use.

Hazmat keeps the private key in host-owned storage and routes in-session Git
SSH through a session-scoped transport broker. No session ssh-agent socket is
exposed, and arbitrary remote SSH shells remain unsupported.

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
	cmd.AddCommand(addCmd, removeCmd, showCmd, testCmd, unsetCmd, clearCmd, listCmd, newConfigSSHProfileCommand(config))
	return cmd
}

func newConfigSSHProfileCommand(config ConfigCommandConfig) *cobra.Command {
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
			return config.RunSSHProfileAdd(args[0], args[1], addKnownHosts, addDefaultHosts, addDescription)
		},
	}
	addCmd.ValidArgsFunction = config.CompleteSSHProfileAdd
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
			return config.RunSSHProfileList()
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one SSH profile in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return config.RunSSHProfileShow(args[0])
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
			return config.RunSSHProfileRemove(args[0], removeForce)
		},
	}
	removeCmd.Flags().BoolVar(&removeForce, "force", false,
		"Detach project references and remove the profile")

	renameCmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename an SSH profile and update all project references",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return config.RunSSHProfileRename(args[0], args[1])
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

func newConfigSetCommand(runSet func(key, value string) error) *cobra.Command {
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
			return runSet(args[0], args[1])
		},
	}
}
