//go:build !hazmat_debug

package hazmat

import "github.com/spf13/cobra"

func addDebugCommands(*cobra.Command) {}
