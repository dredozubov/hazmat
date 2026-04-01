package main

import (
	"fmt"
	"time"
)

type HarnessID string

const (
	HarnessClaude               HarnessID = "claude"
	HarnessCodex                HarnessID = "codex"
	HarnessOpenCode             HarnessID = "opencode"
	claudeHarnessStateVersion             = "1"
	codexHarnessStateVersion              = "1"
	opencodeHarnessStateVersion           = "1"
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
type CodexHarness struct{}
type OpenCodeHarness struct{}

var claudeCodeHarness = ClaudeHarness{}
var codexHarness = CodexHarness{}
var openCodeHarness = OpenCodeHarness{}

func (ClaudeHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessClaude,
		DisplayName:  "Claude Code",
		StateVersion: claudeHarnessStateVersion,
	}
}

func (CodexHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessCodex,
		DisplayName:  "Codex",
		StateVersion: codexHarnessStateVersion,
	}
}

func (OpenCodeHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessOpenCode,
		DisplayName:  "OpenCode",
		StateVersion: opencodeHarnessStateVersion,
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
	return recordHarnessInstalled(h.Spec())
}

func (h ClaudeHarness) RecordBasicsImported() error {
	return recordHarnessImportRun(h.Spec())
}

func (h CodexHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runCodexBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h CodexHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h OpenCodeHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runOpenCodeBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h OpenCodeHarness) ImportBasics(ui *UI, r *Runner, env opencodeImportEnv, opts opencodeImportOptions) error {
	if err := runOpenCodeBasicsImport(ui, r, env, opts); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordBasicsImported(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s import state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h OpenCodeHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h OpenCodeHarness) RecordBasicsImported() error {
	return recordHarnessImportRun(h.Spec())
}

func recordHarnessInstalled(spec HarnessSpec) error {
	return updateHarnessState(spec.ID, func(state HarnessState) HarnessState {
		state.StateVersion = spec.StateVersion
		return state
	})
}

func recordHarnessImportRun(spec HarnessSpec) error {
	return updateHarnessState(spec.ID, func(state HarnessState) HarnessState {
		state.StateVersion = spec.StateVersion
		state.LastImportRunAt = time.Now().UTC().Format(time.RFC3339)
		return state
	})
}
