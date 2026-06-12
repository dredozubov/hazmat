package hazmat

import (
	"reflect"
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommonSessionFlagsDefaults(t *testing.T) {
	var flags sessionCommandFlags
	cmd := &cobra.Command{Use: "test"}
	bindCommonSessionFlags(cmd, &flags)

	opts := flags.harnessSessionOpts(cmd)
	if opts.project != "" || opts.noBackup || opts.github || opts.useSandbox || opts.allowDocker ||
		opts.skipHarnessAssetsSync || opts.metadataJSON {
		t.Fatalf("boolean/string defaults = %+v, want empty and false", opts)
	}
	if len(opts.readDirs) != 0 || len(opts.writeDirs) != 0 || len(opts.integrations) != 0 {
		t.Fatalf("slice defaults = read:%v write:%v integrations:%v, want empty", opts.readDirs, opts.writeDirs, opts.integrations)
	}
	if opts.dockerMode != string(dockerModeNone) {
		t.Fatalf("dockerMode = %q, want %q", opts.dockerMode, dockerModeNone)
	}
	if opts.dockerModeExplicit {
		t.Fatal("dockerModeExplicit should be false by default")
	}
	if opts.networkMode != string(sessionNetworkDefault) {
		t.Fatalf("networkMode = %q, want %q", opts.networkMode, sessionNetworkDefault)
	}
	if opts.networkModeExplicit {
		t.Fatal("networkModeExplicit should be false by default")
	}
}

func TestCommonHarnessSessionFlagsEnableAssetSyncSkip(t *testing.T) {
	var flags sessionCommandFlags
	cmd := &cobra.Command{Use: "test"}
	bindCommonHarnessSessionFlags(cmd, &flags)

	if err := cmd.Flags().Set("skip-harness-assets-sync", "true"); err != nil {
		t.Fatalf("set skip-harness-assets-sync: %v", err)
	}

	opts := flags.harnessSessionOpts(cmd)
	if !opts.skipHarnessAssetsSync {
		t.Fatal("expected skipHarnessAssetsSync")
	}
}

func TestExplainSessionFlagsBuildPlanOnlyOpts(t *testing.T) {
	var flags sessionCommandFlags
	cmd := &cobra.Command{Use: "test"}
	bindExplainSessionFlags(cmd, &flags)

	if flag := cmd.Flags().Lookup("metadata-json"); flag != nil {
		t.Fatal("explain flags should not expose metadata-json launch output")
	}

	setFlag := func(name, value string) {
		t.Helper()
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	setFlag("project", "/project")
	setFlag("read", "/ro")
	setFlag("write", "/rw")
	setFlag("integration", "go")
	setFlag("skip-harness-assets-sync", "true")
	setFlag("no-backup", "true")
	setFlag("github", "true")
	setFlag("docker", "sandbox")
	setFlag("network", "none")

	opts := flags.explainSessionOpts(cmd)
	if !opts.planOnly {
		t.Fatal("explain opts should be plan-only")
	}
	if opts.metadataJSON {
		t.Fatal("explain opts should not request launch metadata JSON")
	}
	if opts.project != "/project" {
		t.Fatalf("project = %q, want /project", opts.project)
	}
	if !slices.Equal(opts.readDirs, []string{"/ro"}) {
		t.Fatalf("readDirs = %v, want [/ro]", opts.readDirs)
	}
	if !slices.Equal(opts.writeDirs, []string{"/rw"}) {
		t.Fatalf("writeDirs = %v, want [/rw]", opts.writeDirs)
	}
	if !slices.Equal(opts.integrations, []string{"go"}) {
		t.Fatalf("integrations = %v, want [go]", opts.integrations)
	}
	if !opts.skipHarnessAssetsSync || !opts.noBackup || !opts.github {
		t.Fatalf("boolean opts = skip:%v noBackup:%v github:%v, want all true", opts.skipHarnessAssetsSync, opts.noBackup, opts.github)
	}
	if opts.dockerMode != "sandbox" || !opts.dockerModeExplicit {
		t.Fatalf("docker opts = mode:%q explicit:%v, want sandbox explicit", opts.dockerMode, opts.dockerModeExplicit)
	}
	if opts.networkMode != "none" || !opts.networkModeExplicit {
		t.Fatalf("network opts = mode:%q explicit:%v, want none explicit", opts.networkMode, opts.networkModeExplicit)
	}
}

func TestSessionFlagBindersShareCoreSurface(t *testing.T) {
	var launchFlags sessionCommandFlags
	launchCmd := &cobra.Command{Use: "launch"}
	bindCommonHarnessSessionFlags(launchCmd, &launchFlags)

	var explainFlags sessionCommandFlags
	explainCmd := &cobra.Command{Use: "explain"}
	bindExplainSessionFlags(explainCmd, &explainFlags)

	shared := []string{
		"project",
		"read",
		"write",
		"integration",
		"skip-harness-assets-sync",
		"no-backup",
		"github",
		"docker",
		"network",
		"sandbox",
		"ignore-docker",
	}
	for _, name := range shared {
		if launchCmd.Flags().Lookup(name) == nil {
			t.Fatalf("launch flags missing %s", name)
		}
		if explainCmd.Flags().Lookup(name) == nil {
			t.Fatalf("explain flags missing %s", name)
		}
	}
	if launchCmd.Flags().Lookup("metadata-json") == nil {
		t.Fatal("launch flags missing metadata-json")
	}
	if explainCmd.Flags().Lookup("metadata-json") != nil {
		t.Fatal("explain flags should not include metadata-json")
	}
}

func TestParseHarnessCommandArgsRendersHelp(t *testing.T) {
	var renderedHelp bool
	cmd := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(func(*cobra.Command, []string) {
		renderedHelp = true
	})

	opts, forwarded, handled, err := parseHarnessCommandArgs(cmd, []string{"--help"}, parseHarnessArgs)
	if err != nil {
		t.Fatalf("parseHarnessCommandArgs: %v", err)
	}
	if !handled {
		t.Fatal("expected handled help")
	}
	if !reflect.DeepEqual(opts, harnessSessionOpts{}) {
		t.Fatalf("opts = %+v, want zero", opts)
	}
	if forwarded != nil {
		t.Fatalf("forwarded = %v, want nil", forwarded)
	}
	if !renderedHelp {
		t.Fatal("expected command help output")
	}
}

func TestCommonSessionFlagsBuildHarnessOpts(t *testing.T) {
	var flags sessionCommandFlags
	cmd := &cobra.Command{Use: "test"}
	bindCommonSessionFlags(cmd, &flags)

	setFlag := func(name, value string) {
		t.Helper()
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	setFlag("project", "/project")
	setFlag("read", "/ro")
	setFlag("read", "/sdk")
	setFlag("write", "/rw")
	setFlag("integration", "go")
	setFlag("integration", "node")
	setFlag("no-backup", "true")
	setFlag("github", "true")
	setFlag("docker", "auto")
	setFlag("network", "none")
	setFlag("metadata-json", "true")

	opts := flags.harnessSessionOpts(cmd)
	if opts.project != "/project" {
		t.Fatalf("project = %q, want /project", opts.project)
	}
	if !slices.Equal(opts.readDirs, []string{"/ro", "/sdk"}) {
		t.Fatalf("readDirs = %v, want [/ro /sdk]", opts.readDirs)
	}
	if !slices.Equal(opts.writeDirs, []string{"/rw"}) {
		t.Fatalf("writeDirs = %v, want [/rw]", opts.writeDirs)
	}
	if !slices.Equal(opts.integrations, []string{"go", "node"}) {
		t.Fatalf("integrations = %v, want [go node]", opts.integrations)
	}
	if !opts.noBackup || !opts.github || !opts.metadataJSON {
		t.Fatalf("boolean opts = noBackup:%v github:%v metadataJSON:%v, want all true", opts.noBackup, opts.github, opts.metadataJSON)
	}
	if opts.dockerMode != "auto" || !opts.dockerModeExplicit {
		t.Fatalf("docker opts = mode:%q explicit:%v, want auto explicit", opts.dockerMode, opts.dockerModeExplicit)
	}
	if opts.networkMode != "none" || !opts.networkModeExplicit {
		t.Fatalf("network opts = mode:%q explicit:%v, want none explicit", opts.networkMode, opts.networkModeExplicit)
	}
}
