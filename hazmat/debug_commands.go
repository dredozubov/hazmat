//go:build hazmat_debug

package hazmat

import "github.com/spf13/cobra"

func addDebugCommands(root *cobra.Command) {
	traceCmd := newTraceCmd()
	traceCmd.GroupID = "run"
	root.AddCommand(traceCmd)
}
