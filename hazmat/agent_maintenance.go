package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func fileModeString(mode os.FileMode) string {
	return fmt.Sprintf("%04o", uint32(mode)&0o7777)
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

func agentMkdirAllForFile(path string, mode os.FileMode) error {
	return agentEnsureDir(filepath.Dir(path), mode)
}
