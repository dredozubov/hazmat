//go:build darwin

package hazmat

import (
	"os/exec"
	"strings"
)

const darwinACLReadBatchMaxPaths = 64

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

func (b darwinACLBackend) ReadACLsForPaths(paths []string) map[string]aclReadResult {
	results := make(map[string]aclReadResult, len(paths))
	for start := 0; start < len(paths); start += darwinACLReadBatchMaxPaths {
		end := start + darwinACLReadBatchMaxPaths
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[start:end]
		args := append([]string{"-leOd"}, chunk...)
		out, err := exec.Command(hostLsPath, args...).CombinedOutput()
		if err != nil {
			for _, path := range chunk {
				rows, readErr := b.ReadACLs(path)
				results[path] = aclReadResult{Rows: rows, OK: readErr == nil}
			}
			continue
		}

		parsed := parseDarwinACLBatchOutput(string(out), chunk)
		for _, path := range chunk {
			rows, ok := parsed[path]
			if ok {
				results[path] = aclReadResult{Rows: rows, OK: true}
				continue
			}
			rows, readErr := b.ReadACLs(path)
			results[path] = aclReadResult{Rows: rows, OK: readErr == nil}
		}
	}
	return results
}

func parseDarwinACLBatchOutput(out string, paths []string) map[string][]ACLRow {
	rowsByPath := make(map[string][]ACLRow, len(paths))
	currentPath := ""
	for _, line := range strings.Split(out, "\n") {
		if row, ok := parseACLRow(line); ok {
			if currentPath != "" {
				rowsByPath[currentPath] = append(rowsByPath[currentPath], row)
			}
			continue
		}
		if path, ok := darwinACLHeaderPath(line, paths); ok {
			currentPath = path
			if _, exists := rowsByPath[currentPath]; !exists {
				rowsByPath[currentPath] = nil
			}
			continue
		}
		if strings.TrimSpace(line) != "" {
			currentPath = ""
		}
	}
	return rowsByPath
}

func darwinACLHeaderPath(line string, paths []string) (string, bool) {
	longest := ""
	for _, path := range paths {
		if path == "" {
			continue
		}
		if line != path && !strings.HasSuffix(line, " "+path) {
			continue
		}
		if len(path) > len(longest) {
			longest = path
		}
	}
	if longest == "" {
		return "", false
	}
	return longest, true
}

func (darwinACLBackend) Chmod(args ...string) error {
	return exec.Command(hostChmodPath, args...).Run()
}

func (darwinACLBackend) SudoChmod(runner *Runner, reason string, args ...string) error {
	return runner.Sudo(reason, append([]string{"chmod"}, args...)...)
}
