package main

import (
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
