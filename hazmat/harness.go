package main

import (
	"fmt"
	"time"
)

type HarnessID string

const (
	HarnessClaude             HarnessID = "claude"
	claudeHarnessStateVersion           = "1"
)

type HarnessSpec struct {
	ID           HarnessID
	DisplayName  string
	StateVersion string
}

type HarnessState struct {
	StateVersion    string `json:"state_version,omitempty"`
	LastImportRunAt string `json:"last_import_run_at,omitempty"`
}

type ClaudeHarness struct{}

var claudeCodeHarness = ClaudeHarness{}

func (ClaudeHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessClaude,
		DisplayName:  "Claude Code",
		StateVersion: claudeHarnessStateVersion,
	}
}

func (h ClaudeHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h ClaudeHarness) ImportBasics(ui *UI, r *Runner, env claudeImportEnv, opts claudeImportOptions) error {
	if err := runClaudeBasicsImport(ui, r, env, opts); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordBasicsImported(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s import state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h ClaudeHarness) RecordInstalled() error {
	spec := h.Spec()
	return updateHarnessState(spec.ID, func(state HarnessState) HarnessState {
		state.StateVersion = spec.StateVersion
		return state
	})
}

func (h ClaudeHarness) RecordBasicsImported() error {
	spec := h.Spec()
	return updateHarnessState(spec.ID, func(state HarnessState) HarnessState {
		state.StateVersion = spec.StateVersion
		state.LastImportRunAt = time.Now().UTC().Format(time.RFC3339)
		return state
	})
}
