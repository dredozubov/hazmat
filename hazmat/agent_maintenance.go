package hazmat

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	agentPathProbe     = defaultAgentPathProbe
	agentReadDirNames  = defaultAgentReadDirNames
	agentReadFile      = defaultAgentReadFile
	agentReadFileBytes = defaultAgentReadFileBytes
)

func fileModeString(mode os.FileMode) string {
	return fmt.Sprintf("%04o", uint32(mode)&0o7777)
}

func agentPathExists(path string) (bool, error) {
	return agentPathProbe("-e", path)
}

func agentPathIsDir(path string) (bool, error) {
	return agentPathProbe("-d", path)
}

func agentPathIsExecutable(path string) (bool, error) {
	return agentPathProbe("-x", path)
}

func agentPathIsSymlink(path string) (bool, error) {
	return agentPathProbe("-L", path)
}

func defaultAgentPathProbe(testFlag, path string) (bool, error) {
	const script = `case "$1" in
  -e|-f|-d|-x|-L) ;;
  *) exit 64 ;;
esac
if test "$1" "$2"; then
  printf yes
else
  printf no
fi`
	out, err := asAgentOutput("/bin/sh", "-c", script, "hazmat-agent-path-probe", testFlag, path)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(out) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected agent path probe output for %s %s: %q", testFlag, path, out)
	}
}

func defaultAgentReadFile(path string) ([]byte, error) {
	out, err := asAgentOutput("/bin/cat", path)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func defaultAgentReadFileBytes(path string) ([]byte, error) {
	cmd := newAgentCommand("/bin/cat", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func defaultAgentReadDirNames(path string) ([]string, error) {
	const script = `dir=$1
if [ ! -d "$dir" ]; then
  exit 0
fi
for entry in "$dir"/*; do
  [ -d "$entry" ] || continue
  name=${entry##*/}
  printf '%s\n' "$name"
done`

	out, err := asAgentOutput("/bin/sh", "-c", script, "hazmat-agent-read-dir-names", path)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var names []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func agentEnsureDir(path string, mode os.FileMode) error {
	if err := asAgentQuiet("/usr/bin/install", "-d", "-m", fileModeString(mode), path); err != nil {
		return fmt.Errorf("ensure %s: %w", path, err)
	}
	return nil
}

func agentMkdirAll(path string) error {
	if err := asAgentQuiet("/bin/mkdir", "-p", path); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	return nil
}

func agentEnsureSharedDir(path string, mode os.FileMode) error {
	if err := agentEnsureDir(path, mode); err != nil {
		return err
	}
	return agentSetSharedGroup(path, mode)
}

func agentSetSharedGroup(path string, mode os.FileMode) error {
	if err := asAgentQuiet("/usr/bin/chgrp", sharedGroup, path); err != nil {
		return fmt.Errorf("set group on %s: %w", path, err)
	}
	if err := asAgentQuiet("/bin/chmod", fileModeString(mode), path); err != nil {
		return fmt.Errorf("set mode on %s: %w", path, err)
	}
	return nil
}

func agentWriteFile(path string, content []byte, mode os.FileMode) error {
	cmd := newAgentCommand("/usr/bin/tee", path)
	cmd.Stdin = bytes.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("write %s: %s", path, msg)
	}
	if err := asAgentQuiet("/bin/chmod", fileModeString(mode), path); err != nil {
		return fmt.Errorf("set mode on %s: %w", path, err)
	}
	return nil
}

func agentWriteSharedFile(path string, content []byte, mode os.FileMode) error {
	if err := agentWriteFile(path, content, mode); err != nil {
		return err
	}
	return agentSetSharedGroup(path, mode)
}
