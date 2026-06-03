//go:build hazmat_debug

package hazmat

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDebugBuildRegistersTraceCommand(t *testing.T) {
	root := &cobra.Command{Use: "hazmat"}

	addDebugCommands(root)

	var traceCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "trace" {
			traceCmd = cmd
			break
		}
	}
	if traceCmd == nil {
		t.Fatal("debug build did not register trace command")
	}
	if traceCmd.GroupID != "run" {
		t.Fatalf("trace GroupID = %q, want run", traceCmd.GroupID)
	}
}
