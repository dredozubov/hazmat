package main

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

			prepared, err := prepareAndBeginLaunchSession("hermes", opts, true, true)
			if err != nil {
				return err
			}
			if prepared.Mode == sessionModeDockerSandbox {
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
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return strings.ToLower(arg)
	}
	return ""
}
