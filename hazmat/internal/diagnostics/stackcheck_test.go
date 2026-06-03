package diagnostics

import (
	"bytes"
	"io"
	"slices"
	"testing"
)

func TestStackcheckCommandPassesFlagsToRunner(t *testing.T) {
	var gotMode string
	var gotOptions StackcheckOptions
	var gotStdout bool
	var gotStderr bool
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewStackCheckCommand(StackcheckCommandConfig{
		DefaultManifestPath:  func() string { return "default-manifest.yaml" },
		DefaultWorkspaceRoot: func() string { return "/tmp/default-stackcheck" },
		DefaultTrack:         "required",
		Run: func(mode string, opts StackcheckOptions, out, errOut io.Writer) error {
			gotMode = mode
			gotOptions = opts
			gotStdout = out == &stdout
			gotStderr = errOut == &stderr
			return nil
		},
	})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"detect",
		"--manifest", "manifest.yaml",
		"--workspace-root", "/tmp/stackcheck",
		"--track", "all",
		"--wave", "2",
		"--id", "next-js",
		"--id", "ruff",
		"--upstream-head",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotMode != "detect" {
		t.Fatalf("mode = %q", gotMode)
	}
	if gotOptions.ManifestPath != "manifest.yaml" ||
		gotOptions.WorkspaceRoot != "/tmp/stackcheck" ||
		gotOptions.Track != "all" ||
		gotOptions.Wave != 2 ||
		!gotOptions.UpstreamHead ||
		!slices.Equal(gotOptions.IDs, []string{"next-js", "ruff"}) {
		t.Fatalf("options = %+v", gotOptions)
	}
	if !gotStdout || !gotStderr {
		t.Fatalf("runner did not receive command stdout/stderr: stdout=%v stderr=%v", gotStdout, gotStderr)
	}
}

func TestStackcheckCommandUsesDefaults(t *testing.T) {
	var gotOptions StackcheckOptions
	cmd := NewStackCheckCommand(StackcheckCommandConfig{
		DefaultManifestPath:  func() string { return "default-manifest.yaml" },
		DefaultWorkspaceRoot: func() string { return "/tmp/default-stackcheck" },
		DefaultTrack:         "required",
		Run: func(_ string, opts StackcheckOptions, _, _ io.Writer) error {
			gotOptions = opts
			return nil
		},
	})
	cmd.SetArgs([]string{"smoke"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotOptions.ManifestPath != "default-manifest.yaml" ||
		gotOptions.WorkspaceRoot != "/tmp/default-stackcheck" ||
		gotOptions.Track != "required" {
		t.Fatalf("options = %+v", gotOptions)
	}
}

func TestStackcheckCommandRequiresRunner(t *testing.T) {
	cmd := NewStackCheckCommand(StackcheckCommandConfig{})
	cmd.SetArgs([]string{"contract"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() succeeded without runner")
	}
}
