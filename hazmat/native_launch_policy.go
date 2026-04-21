package main

import (
	"fmt"
	"os"
)

type nativeLaunchPolicyArtifact struct {
	Path    string
	cleanup func()
}

func (a nativeLaunchPolicyArtifact) Cleanup() {
	if a.cleanup != nil {
		a.cleanup()
	}
}

func prepareNativeLaunchPolicy(cfg sessionConfig) (nativeLaunchPolicyArtifact, error) {
	policy := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", os.Getpid())
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return nativeLaunchPolicyArtifact{}, fmt.Errorf("write seatbelt policy: %w", err)
	}
	if err := os.Chmod(policyFile, 0o644); err != nil {
		_ = os.Remove(policyFile)
		return nativeLaunchPolicyArtifact{}, fmt.Errorf("set seatbelt policy mode: %w", err)
	}

	return nativeLaunchPolicyArtifact{
		Path: policyFile,
		cleanup: func() {
			_ = os.Remove(policyFile)
		},
	}, nil
}
