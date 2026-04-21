//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

func checkPlatform() error {
	return fmt.Errorf("hazmat does not implement native setup for %s yet; supported platform is macOS", runtime.GOOS)
}
