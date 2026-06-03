package hazmat

import "github.com/spf13/cobra"

func newCursorAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cursor-agent [hazmat-flags] [cursor-agent-flags] [cursor-agent-args...]",
		Short: "Launch Cursor Agent in containment",
		Long: `Launch Cursor Agent in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --skip-harness-assets-sync  Accepted; Cursor Agent has no host assets in v1
  --no-backup            Skip pre-session snapshot
  --github               Grant configured GitHub API token as GH_TOKEN
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --network <mode>       Native network policy: default or none
  --metadata-json        Emit one launch metadata JSON line to stderr
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Cursor Agent.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root. Cursor Agent uses contained agent-side state under
/Users/agent; host Cursor IDE state, host ~/.cursor, and host auth settings are
not imported.

Examples:
  hazmat cursor-agent
  hazmat cursor-agent -- status
  hazmat cursor-agent -- login
  hazmat cursor-agent --print --output-format stream-json --force --trust
  hazmat cursor-agent --docker=sandbox -C /proj
  hazmat cursor-agent --docker=auto -C /proj
  hazmat cursor-agent --network none --print --output-format text
  hazmat cursor-agent --no-backup`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, handled, err := parseHarnessCommandArgs(cmd, args, parseHarnessArgs)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}

			prepared, err := prepareAndBeginLaunchSession("cursor-agent", opts, true, true)
			if err != nil {
				return err
			}
			if prepared.Mode == sessionModeDockerSandbox {
				return runPreparedSandboxCursorAgentSession(prepared, forwarded)
			}
			return runPreparedAgentSeatbeltScript(prepared, cursorAgentLaunchScript(), forwarded...)
		},
	}
	return cmd
}
