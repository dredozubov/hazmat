//go:build !hazmat_debug

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestReleaseBuildDoesNotRegisterTraceCommand(t *testing.T) {
	root := &cobra.Command{Use: "hazmat"}

	addDebugCommands(root)

	for _, cmd := range root.Commands() {
		if cmd.Name() == "trace" {
			t.Fatal("release build registered trace command")
		}
	}
}
