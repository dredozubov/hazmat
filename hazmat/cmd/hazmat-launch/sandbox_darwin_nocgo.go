//go:build darwin && !cgo

package main

import "fmt"

func sandboxInit(string) error {
	return fmt.Errorf("darwin sandbox launch requires CGO_ENABLED=1 for sandbox_init")
}
