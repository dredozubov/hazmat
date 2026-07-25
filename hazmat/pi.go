package hazmat

import "github.com/spf13/cobra"

func newPiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pi [hazmat-flags] [pi-flags] [pi-args...]",
		Short: "Launch Pi in containment",
		Long: `Launch Pi in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --skip-harness-assets-sync  Accepted; Pi has no host assets in v1
  --no-backup            Skip pre-session snapshot
  --github               Grant configured GitHub API token as GH_TOKEN
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --network <mode>       Native network policy: default or none
  --metadata-json        Emit one launch metadata JSON line to stderr
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Pi.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root. Pi uses contained agent-side state under
/Users/agent/.pi/agent; host ~/.pi/agent is not imported.

Examples:
  hazmat pi
  hazmat pi -- --help
  hazmat pi -- --mode rpc
  hazmat pi -C /proj
  hazmat pi --docker=sandbox -C /proj
  hazmat pi --network none -- --help`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, handled, err := parseHarnessCommandArgs(cmd, args, parseHarnessArgs)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}

			prepared, err := prepareAndBeginLaunchSession(
				"pi",
				forwarded,
				opts,
				true,
				true,
			)
			if err != nil {
				return err
			}
			if prepared.completedByPlanescape() {
				return nil
			}
			if prepared.Runtime.UsesDockerSandbox() {
				return runPreparedSandboxPiSession(prepared, forwarded)
			}
			return runPreparedAgentSeatbeltScript(prepared, piLaunchScript(), forwarded...)
		},
	}
	return cmd
}
