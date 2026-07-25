package hazmat

import (
	"strings"

	"github.com/spf13/cobra"
)

func newQwenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qwen [hazmat-flags] [qwen-flags] [qwen-args...]",
		Short: "Launch Qwen Code in containment",
		Long: `Launch Qwen Code in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --skip-harness-assets-sync  Skip managed Qwen prompt-asset sync for this launch
  --no-backup            Skip pre-session snapshot
  --github               Grant configured GitHub API token as GH_TOKEN
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --network <mode>       Native network policy: default or none
  --metadata-json        Emit one launch metadata JSON line to stderr
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Qwen Code.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root. Qwen uses contained state under /Users/agent/.qwen;
host ~/.qwen is not imported.

Examples:
  hazmat qwen
  hazmat qwen -p "explain this repo"
  hazmat qwen --model qwen3-coder-plus -p "review this"
  hazmat qwen --docker=sandbox -C /proj
  hazmat qwen --docker=auto -C /proj
  hazmat qwen --network none -p "review offline"
  hazmat qwen --github -p "review this PR"
  hazmat qwen --no-backup`,
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
				"qwen",
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
			forwarded = qwenLaunchArgs(forwarded, qwenShouldSkipPermissions())
			if prepared.Runtime.UsesDockerSandbox() {
				return runPreparedSandboxQwenSession(prepared, forwarded)
			}
			return runPreparedAgentSeatbeltScript(prepared, qwenLaunchScript(), forwarded...)
		},
	}
	return cmd
}

func qwenShouldSkipPermissions() bool {
	hcfg, _ := loadConfig()
	return hcfg.SkipPermissions()
}

func qwenLaunchArgs(forwarded []string, skipPermissions bool) []string {
	if !skipPermissions || qwenYoloArgPresent(forwarded) {
		return forwarded
	}
	return append([]string{"--yolo"}, forwarded...)
}

func qwenYoloArgPresent(args []string) bool {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "--yolo" || trimmed == "-y" {
			return true
		}
	}
	return false
}
