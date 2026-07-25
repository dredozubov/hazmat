package hazmat

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newHermesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hermes [hazmat-flags] [hermes-flags] [hermes-args...]",
		Short: "Launch Hermes in containment",
		Long: `Launch Hermes in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --skip-harness-assets-sync  Accepted; Hermes has no host assets in v1
  --no-backup            Skip pre-session snapshot
  --github               Grant configured GitHub API token as GH_TOKEN
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --network <mode>       Native network policy: default or none
  --api-proxy <mode>     API proxy mode: none (default) or muginn
  --metadata-json        Emit one launch metadata JSON line to stderr
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Hermes.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root. Hermes uses a managed HERMES_HOME under the agent
home; host ~/.hermes is not imported.

Examples:
  hazmat hermes
  hazmat hermes -- --version
  hazmat hermes -- chat --toolsets terminal,file
  hazmat hermes -C /proj -- chat
  hazmat hermes --network none -- --version
  hazmat hermes --api-proxy=muginn -- chat --model muginn/subscription-auto
  hazmat hermes --github -- chat`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, handled, err := parseHarnessCommandArgs(cmd, args, parseHarnessArgs)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
			if err := rejectHermesDeferredEntrypoint(forwarded); err != nil {
				return err
			}

			prepared, err := prepareAndBeginLaunchSession(
				"hermes",
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
				return runPreparedSandboxHermesSession(prepared, forwarded)
			}
			return runPreparedAgentSeatbeltScript(prepared, hermesLaunchScript(), forwarded...)
		},
	}
	return cmd
}

var hermesDeferredEntrypoints = map[string]string{
	"api":       "dashboard/API",
	"cron":      "persistent cron",
	"dashboard": "dashboard/API",
	"gateway":   "gateway",
	"serve":     "server",
	"server":    "server",
	"web":       "dashboard/API",
}

func rejectHermesDeferredEntrypoint(forwarded []string) error {
	entrypoint := firstHermesEntrypoint(forwarded)
	if entrypoint == "" {
		return nil
	}
	if label, blocked := hermesDeferredEntrypoints[entrypoint]; blocked {
		return fmt.Errorf("Hermes %s mode is not supported by hazmat hermes v1; run a foreground Hermes command such as `hazmat hermes -- chat ...` instead. Gateway, dashboard/API, server, and persistent cron support require a Hazmat service lifecycle first", label)
	}
	return nil
}

func firstHermesEntrypoint(args []string) string {
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.TrimSpace(arg) == "" {
			continue
		}
		if arg == "--" {
			continue
		}
		if hermesFlagHasInlineValue(arg) {
			continue
		}
		if hermesFlagConsumesNext(arg) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return strings.ToLower(arg)
	}
	return ""
}

var hermesFlagsWithValues = map[string]struct{}{
	"-c":            {},
	"-m":            {},
	"-p":            {},
	"-t":            {},
	"-w":            {},
	"--config":      {},
	"--config-file": {},
	"--model":       {},
	"--profile":     {},
	"--provider":    {},
	"--toolsets":    {},
	"--tools":       {},
	"--workspace":   {},
}

func hermesFlagHasInlineValue(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}
	if strings.HasPrefix(arg, "--") {
		name, _, ok := strings.Cut(arg, "=")
		if !ok {
			return false
		}
		_, takesValue := hermesFlagsWithValues[name]
		return takesValue
	}
	_, takesValue := hermesFlagsWithValues[arg[:min(len(arg), 2)]]
	return takesValue && len(arg) > 2
}

func hermesFlagConsumesNext(arg string) bool {
	if !strings.HasPrefix(arg, "-") || arg == "--" {
		return false
	}
	if strings.HasPrefix(arg, "--") {
		if name, _, ok := strings.Cut(arg, "="); ok {
			arg = name
		}
	}
	_, takesValue := hermesFlagsWithValues[arg]
	return takesValue
}
