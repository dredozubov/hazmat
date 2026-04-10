package main

import "testing"

func TestAgentMaintenanceSudoersDefaultChoiceInteractiveDefaultsToSkip(t *testing.T) {
	if got := agentMaintenanceSudoersDefaultChoice(&UI{}); got != "skip" {
		t.Fatalf("agentMaintenanceSudoersDefaultChoice(interactive) = %q, want skip", got)
	}
}

func TestAgentMaintenanceSudoersDefaultChoiceYesAllDefaultsToInstall(t *testing.T) {
	if got := agentMaintenanceSudoersDefaultChoice(&UI{YesAll: true}); got != "install" {
		t.Fatalf("agentMaintenanceSudoersDefaultChoice(--yes) = %q, want install", got)
	}
}

func TestAgentMaintenanceSudoersDefaultChoiceDryRunYesAllStillInstalls(t *testing.T) {
	if got := agentMaintenanceSudoersDefaultChoice(&UI{DryRun: true, YesAll: true}); got != "install" {
		t.Fatalf("agentMaintenanceSudoersDefaultChoice(--dry-run --yes) = %q, want install", got)
	}
}
