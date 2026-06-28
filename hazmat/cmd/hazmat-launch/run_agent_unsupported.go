//go:build !linux || (!amd64 && !arm64)

package main

import (
	"fmt"
	"os"
)

func runAgentCommand(args []string, _ *launchProfile) {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(os.Stdout, "usage: hazmat-launch run-agent --spec <path> --spec-sha256 <hex> --nonce <hex> --metadata <path>")
		return
	}
	die("hazmat-launch: run-agent is only supported on linux/amd64 and linux/arm64")
}
