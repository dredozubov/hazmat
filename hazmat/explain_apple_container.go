package hazmat

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sort"

	"github.com/spf13/cobra"

	applecontainerspec "hazmat/containment/applecontainer"
	"hazmat/sessionmeta"
)

// explainBackendAppleContainer is the only non-default value accepted by
// `hazmat explain --backend`. Session launch commands do not accept the flag:
// the Apple Container backend is plan-only and must stay non-executable until
// the MC_AppleContainerLaunch runtime ordering is implemented.
const explainBackendAppleContainer = "apple-container"

// explainPreviewSessionID keeps plan-only previews deterministic.
const explainPreviewSessionID = "explain-preview"

func runExplainAppleContainer(cmd *cobra.Command, target, image string, opts harnessSessionOpts, outputJSON bool) error {
	if image == "" {
		return fmt.Errorf("--backend=apple-container requires --image (the backend never guesses an image)")
	}
	if opts.dockerModeExplicit && opts.dockerMode != string(dockerModeNone) {
		return fmt.Errorf("--backend=apple-container cannot be combined with --docker=%s", opts.dockerMode)
	}

	// Reuse the native plan-only resolver so project, read-only, and
	// read-write inputs pass the same typed path validation (including
	// credential deny-zone rejection) as every other session preview.
	cfg, _, err := resolveExplainSession(target, opts)
	if err != nil {
		return err
	}
	policy, err := buildNativeSessionPolicy(cfg)
	if err != nil {
		return err
	}

	spec, err := applecontainerspec.Compile(policy.Contract, applecontainerspec.CompileOptions{
		Harness:            target,
		Image:              image,
		SessionID:          explainPreviewSessionID,
		IntegrationEnvKeys: integrationEnvKeyNames(cfg.IntegrationEnv),
		Host: applecontainerspec.HostReport{
			GOOS:   runtime.GOOS,
			GOARCH: runtime.GOARCH,
			// Admission probes (macOS version, CLI path/version, API server
			// health, agent-user execution) are not run by explain; they
			// surface below as honest capability gaps until the host spike
			// wires real inspection.
		},
	})
	if err != nil {
		return err
	}

	if outputJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(spec)
	}
	printAppleContainerPlan(cmd.OutOrStdout(), spec)
	return nil
}

func printAppleContainerPlan(w io.Writer, spec applecontainerspec.LaunchSpec) {
	fmt.Fprintf(w, "Mode:                 %s (plan-only preview)\n", sessionmeta.ModeAppleContainer.Label())
	fmt.Fprintf(w, "Backend:              %s\n", spec.Backend)
	fmt.Fprintf(w, "Image:                %s\n", spec.Image)
	fmt.Fprintf(w, "Container name:       %s\n", spec.ContainerName)
	fmt.Fprintf(w, "Host identity:        %s macOS user\n", agentUser)
	fmt.Fprintf(w, "Guest identity:       uid %d gid %d (non-root)\n", spec.User.UID, spec.User.GID)
	for _, mount := range spec.Mounts {
		access := "rw"
		if mount.Access == "read-only" {
			access = "ro"
		}
		label := "Mount:            "
		if mount.Target == spec.Workdir {
			label = "Project:          "
		}
		fmt.Fprintf(w, "%s    %s (%s bind mount)\n", label, mount.Target, access)
	}
	credDelivery := "none planned"
	if spec.Environment.CredentialEnvFile != "" {
		credDelivery = "provider env-file, session-scoped"
	}
	fmt.Fprintf(w, "Credential delivery:  %s\n", credDelivery)
	fmt.Fprintf(w, "Network:              %s\n", sessionmeta.NetworkContractLabel(spec.Network.Mode, sessionmeta.ModeAppleContainer))
	fmt.Fprintf(w, "Unsupported policy:   network none, egress allowlist (fail closed)\n")
	fmt.Fprintf(w, "Cleanup:              remove named container + generated credential files (never prune)\n")
	if len(spec.CapabilityGaps) > 0 {
		fmt.Fprintf(w, "\nCapability gaps (why this plan cannot launch):\n")
		for _, gap := range spec.CapabilityGaps {
			if gap.State != "" {
				fmt.Fprintf(w, "  - %s: %s (%s)\n", gap.Code, gap.Message, gap.State)
				continue
			}
			fmt.Fprintf(w, "  - %s: %s\n", gap.Code, gap.Message)
		}
	}
	fmt.Fprintf(w, "\nThe Apple Container backend is plan-only. Hazmat will not launch Apple\nContainer sessions until the proved launch model is implemented.\n")
}

func integrationEnvKeyNames(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
