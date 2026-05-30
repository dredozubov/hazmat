//go:build !hazmat_debug

package main

import "github.com/spf13/cobra"

func addDebugCommands(*cobra.Command) {}
