//go:build darwin

package hazmat

import (
	"fmt"
	"os/exec"
	"strings"
)

func checkPlatform() error {
	cmd := exec.Command(hostUnamePath)
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "Darwin" {
		return fmt.Errorf("this tool is for macOS only")
	}
	return nil
}
