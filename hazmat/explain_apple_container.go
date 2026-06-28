package hazmat

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sort"

	"github.com/spf13/cobra"

	applecontainerspec "hazmat/containment/applecontainer"
	applecontainerruntime "hazmat/internal/runtime/applecontainer"
	"hazmat/runtimeprovider"
	"hazmat/sessionmeta"
)

const appleContainerGateEnv = applecontainerruntime.EnvExperimentalGate

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
	phaseLabel := "plan-only preview"
	if spec.Phase == applecontainerspec.PhaseExperimental {
		phaseLabel = "experimental"
	}
	fmt.Fprintf(w, "Mode:                 %s (%s)\n", sessionmeta.ModeAppleContainer.Label(), phaseLabel)
	fmt.Fprintf(w, "Backend:              %s\n", spec.Backend)
	fmt.Fprintf(w, "Provider status:      %s\n", appleContainerProviderStatus(spec.Phase))
	fmt.Fprintf(w, "Image:                %s\n", spec.Image)
	fmt.Fprintf(w, "Container name:       %s\n", spec.ContainerName)
	fmt.Fprintf(w, "Host identity:        invoking user (host account isolation NOT provided)\n")
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
		for _, line := range runtimeprovider.RenderGaps(appleContainerGapRecords(spec)) {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
	if spec.Phase != applecontainerspec.PhaseExperimental {
		fmt.Fprintf(w, "\nThis is a plan-only preview. Launch requires the experimental gate:\n  %s=1 hazmat exec --backend=apple-container --image ... -- <command>\n", appleContainerGateEnv)
	}
}

func appleContainerProviderStatus(phase string) runtimeprovider.Status {
	if phase == applecontainerspec.PhaseExperimental {
		return runtimeprovider.StatusExperimental
	}
	return runtimeprovider.StatusPlanOnly
}

func appleContainerGapRecords(spec applecontainerspec.LaunchSpec) []runtimeprovider.GapRecord {
	if len(spec.CapabilityGaps) == 0 {
		return nil
	}
	status := appleContainerProviderStatus(spec.Phase)
	records := make([]runtimeprovider.GapRecord, 0, len(spec.CapabilityGaps))
	for _, gap := range spec.CapabilityGaps {
		records = append(records, runtimeprovider.MustGapRecord(
			runtimeprovider.KindAppleContainer,
			status,
			gap.Code,
			gap.Message,
			gap.State,
		))
	}
	return records
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
