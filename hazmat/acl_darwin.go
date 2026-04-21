//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

type darwinACLBackend struct{}

func newPlatformACLBackend() platformACLBackend {
	return darwinACLBackend{}
}

// ReadACLs parses `ls -leOd` output for path and returns the ACL rows.
// -d keeps directory arguments referring to the directory itself rather than
// its contents. -O surfaces the "inherited" flag on propagated rows.
func (darwinACLBackend) ReadACLs(path string) ([]ACLRow, error) {
	out, err := exec.Command(hostLsPath, "-leOd", path).CombinedOutput()
	if err != nil {
		return nil, err
	}
	var rows []ACLRow
	for _, line := range strings.Split(string(out), "\n") {
		if row, ok := parseACLRow(line); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (darwinACLBackend) Chmod(args ...string) error {
	return exec.Command(hostChmodPath, args...).Run()
}

func (darwinACLBackend) SudoChmod(runner *Runner, reason string, args ...string) error {
	return runner.Sudo(reason, append([]string{"chmod"}, args...)...)
}
