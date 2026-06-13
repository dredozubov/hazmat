package hazmat

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func validateHostWrapper(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("host wrapper missing %s: %w", path, err)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("host wrapper not executable: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read host wrapper %s: %w", path, err)
	}
	pinned, ok, err := hostWrapperPinnedHazmatBin(string(data))
	if err != nil {
		return fmt.Errorf("host wrapper %s has invalid pinned Hazmat binary: %w", path, err)
	}
	if !ok {
		return fmt.Errorf("host wrapper %s does not pin HAZMAT_BIN", path)
	}
	target, err := os.Stat(pinned)
	if err != nil {
		return fmt.Errorf("host wrapper %s pins missing Hazmat binary %s: %w", path, pinned, err)
	}
	if target.Mode()&0o111 == 0 {
		return fmt.Errorf("host wrapper %s pins non-executable Hazmat binary: %s", path, pinned)
	}
	return nil
}

func hostWrapperPinnedHazmatBin(content string) (string, bool, error) {
	for _, line := range strings.Split(content, "\n") {
		raw, ok := strings.CutPrefix(strings.TrimSpace(line), "HAZMAT_BIN=")
		if !ok {
			continue
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", true, err
		}
		if strings.TrimSpace(value) == "" {
			return "", true, fmt.Errorf("empty path")
		}
		return value, true, nil
	}
	return "", false, nil
}
