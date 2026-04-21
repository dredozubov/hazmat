//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

type unsupportedACLBackend struct{}

func newPlatformACLBackend() platformACLBackend {
	return unsupportedACLBackend{}
}

func (unsupportedACLBackend) ReadACLs(string) ([]ACLRow, error) {
	return nil, unsupportedACLError()
}

func (unsupportedACLBackend) Chmod(...string) error {
	return unsupportedACLError()
}

func (unsupportedACLBackend) SudoChmod(*Runner, string, ...string) error {
	return unsupportedACLError()
}

func unsupportedACLError() error {
	return fmt.Errorf("native ACL operations are not implemented on %s yet; supported platform is macOS", runtime.GOOS)
}
