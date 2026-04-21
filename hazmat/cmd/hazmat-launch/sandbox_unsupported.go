//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

func sandboxInit(string) error {
	return fmt.Errorf("sandbox launch is not implemented for %s", runtime.GOOS)
}
