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
	HarnessGemini               HarnessID = "gemini"
	claudeHarnessStateVersion             = "1"
	codexHarnessStateVersion              = "1"
	opencodeHarnessStateVersion           = "1"
	geminiHarnessStateVersion             = "1"
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

type ManagedHarness struct {
	Spec             HarnessSpec
	LaunchCommand    string
	BootstrapCommand string
	Installed        func() bool
	Bootstrap        func(ui *UI, r *Runner) error
}

type ClaudeHarness struct{}
type CodexHarness struct{}
type OpenCodeHarness struct{}
type GeminiHarness struct{}

var claudeCodeHarness = ClaudeHarness{}
var codexHarness = CodexHarness{}
var openCodeHarness = OpenCodeHarness{}
var geminiHarness = GeminiHarness{}

var managedHarnessRegistry = []ManagedHarness{
	{
		Spec:             claudeCodeHarness.Spec(),
		LaunchCommand:    "hazmat claude",
		BootstrapCommand: "hazmat bootstrap claude",
		Installed: func() bool {
			_, ok := findInstalledClaudeBinary()
			return ok
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return claudeCodeHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             codexHarness.Spec(),
		LaunchCommand:    "hazmat codex",
		BootstrapCommand: "hazmat bootstrap codex",
		Installed: func() bool {
			_, ok := findInstalledCodexBinary()
			return ok
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return codexHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             openCodeHarness.Spec(),
		LaunchCommand:    "hazmat opencode",
		BootstrapCommand: "hazmat bootstrap opencode",
		Installed: func() bool {
			_, ok := findInstalledOpenCodeBinary()
			return ok
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return openCodeHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             geminiHarness.Spec(),
		LaunchCommand:    "hazmat gemini",
		BootstrapCommand: "hazmat bootstrap gemini",
		Installed: func() bool {
			_, ok := findInstalledGeminiBinary()
			return ok
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return geminiHarness.Bootstrap(ui, r)
		},
	},
}

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

func (GeminiHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessGemini,
		DisplayName:  "Gemini",
		StateVersion: geminiHarnessStateVersion,
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

func (h CodexHarness) ImportBasics(ui *UI, r *Runner, env codexImportEnv, opts codexImportOptions) error {
	if err := runCodexBasicsImport(ui, r, env, opts); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordBasicsImported(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s import state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h CodexHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h CodexHarness) RecordBasicsImported() error {
	return recordHarnessImportRun(h.Spec())
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

func (h GeminiHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runGeminiBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h GeminiHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func managedHarnesses() []ManagedHarness {
	harnesses := make([]ManagedHarness, len(managedHarnessRegistry))
	copy(harnesses, managedHarnessRegistry)
	return harnesses
}

func managedHarnessByID(id HarnessID) (ManagedHarness, bool) {
	for _, harness := range managedHarnessRegistry {
		if harness.Spec.ID == id {
			return harness, true
		}
	}
	return ManagedHarness{}, false
}

func isManagedHarnessInstalled(id HarnessID) bool {
	harness, ok := managedHarnessByID(id)
	return ok && harness.Installed()
}

func installedManagedHarnesses() []ManagedHarness {
	var installed []ManagedHarness
	for _, harness := range managedHarnessRegistry {
		if harness.Installed() {
			installed = append(installed, harness)
		}
	}
	return installed
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
