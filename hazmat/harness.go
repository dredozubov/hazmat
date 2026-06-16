package hazmat

import (
	"fmt"
	"hazmat/harnesses"
	"hazmat/internal/harnessruntime"
)

type HarnessID = harnesses.ID

const (
	HarnessClaude                  HarnessID = harnesses.Claude
	HarnessCodex                   HarnessID = harnesses.Codex
	HarnessOpenCode                HarnessID = harnesses.OpenCode
	HarnessGemini                  HarnessID = harnesses.Gemini
	HarnessHermes                  HarnessID = harnesses.Hermes
	HarnessQwen                    HarnessID = harnesses.Qwen
	HarnessCursorAgent             HarnessID = harnesses.CursorAgent
	HarnessPi                      HarnessID = harnesses.Pi
	claudeHarnessStateVersion                = harnesses.ClaudeStateVersion
	codexHarnessStateVersion                 = harnesses.CodexStateVersion
	opencodeHarnessStateVersion              = harnesses.OpenCodeStateVersion
	geminiHarnessStateVersion                = harnesses.GeminiStateVersion
	hermesHarnessStateVersion                = harnesses.HermesStateVersion
	qwenHarnessStateVersion                  = harnesses.QwenStateVersion
	cursorAgentHarnessStateVersion           = harnesses.CursorAgentStateVersion
	piHarnessStateVersion                    = harnesses.PiStateVersion
)

type HarnessSpec = harnesses.Spec

type harnessImportPolicy = harnesses.ImportPolicy

type ManagedHarness struct {
	Spec                 HarnessSpec
	LaunchCommand        string
	BootstrapCommand     string
	ImportPolicy         harnessImportPolicy
	Installed            func() bool
	Probe                func(read func(args ...string) (string, error)) harnessProbe
	ManagedCodeArtifacts func() []harnessManagedArtifact
	PreservedArtifacts   []string
	Bootstrap            func(ui *UI, r *Runner) error
}

type ClaudeHarness struct{}
type CodexHarness struct{}
type OpenCodeHarness struct{}
type GeminiHarness struct{}
type HermesHarness struct{}
type QwenHarness struct{}
type CursorAgentHarness struct{}
type PiHarness struct{}

var claudeCodeHarness = ClaudeHarness{}
var codexHarness = CodexHarness{}
var openCodeHarness = OpenCodeHarness{}
var geminiHarness = GeminiHarness{}
var hermesHarness = HermesHarness{}
var qwenHarness = QwenHarness{}
var cursorAgentHarness = CursorAgentHarness{}
var piHarness = PiHarness{}

func harnessMetadata(id HarnessID) harnesses.Metadata {
	return harnesses.MustMetadata(id)
}

var managedHarnessRegistry = []ManagedHarness{
	{
		Spec:             harnessMetadata(HarnessClaude).Spec,
		LaunchCommand:    harnessMetadata(HarnessClaude).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessClaude).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessClaude).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledClaudeBinary()
			return ok
		},
		Probe:                probeClaudeHarness,
		ManagedCodeArtifacts: claudeHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + "/.claude credentials, settings, hooks, and projects",
			agentHome + "/.claude.json account state",
			"provider credentials in ~/.hazmat/secrets",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return claudeCodeHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessCodex).Spec,
		LaunchCommand:    harnessMetadata(HarnessCodex).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessCodex).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessCodex).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledCodexBinary()
			return ok
		},
		Probe:                probeCodexHarness,
		ManagedCodeArtifacts: codexHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + codexStateDirRel + " auth, config, logs, and sessions",
			agentHome + "/.agents shared skills",
			"provider credentials in ~/.hazmat/secrets",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return codexHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessOpenCode).Spec,
		LaunchCommand:    harnessMetadata(HarnessOpenCode).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessOpenCode).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessOpenCode).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledOpenCodeBinary()
			return ok
		},
		Probe:                probeOpenCodeHarness,
		ManagedCodeArtifacts: openCodeHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + "/.config/opencode config",
			agentHome + "/.local/share/opencode auth and data",
			"provider credentials in ~/.hazmat/secrets",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return openCodeHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessGemini).Spec,
		LaunchCommand:    harnessMetadata(HarnessGemini).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessGemini).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessGemini).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledGeminiBinary()
			return ok
		},
		Probe:                probeGeminiHarness,
		ManagedCodeArtifacts: geminiHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + geminiStateDirRel + " OAuth, accounts, config, and sessions",
			"provider credentials in ~/.hazmat/secrets",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return geminiHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessHermes).Spec,
		LaunchCommand:    harnessMetadata(HarnessHermes).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessHermes).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessHermes).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledHermesBinary()
			return ok
		},
		Probe:                probeHermesHarness,
		ManagedCodeArtifacts: hermesHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + hermesBinRel + " manual Hermes executable",
			hermesStateDir() + " managed profile roots",
			"provider credentials in ~/.hazmat/secrets",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return hermesHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessQwen).Spec,
		LaunchCommand:    harnessMetadata(HarnessQwen).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessQwen).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessQwen).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledQwenBinary()
			return ok
		},
		Probe:                probeQwenHarness,
		ManagedCodeArtifacts: qwenHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + qwenStateDirRel + " auth, settings, extensions, and sessions",
			"host ~/.qwen auth and settings are not imported",
			"provider credentials are configured inside the contained Qwen profile or project .env",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return qwenHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessCursorAgent).Spec,
		LaunchCommand:    harnessMetadata(HarnessCursorAgent).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessCursorAgent).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessCursorAgent).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledCursorAgentBinary()
			return ok
		},
		Probe:                probeCursorAgentHarness,
		ManagedCodeArtifacts: cursorAgentHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + cursorAgentBinRel + " manual Cursor Agent executable",
			agentHome + "/.cursor contained Cursor Agent profile state",
			"host Cursor IDE state, host ~/.cursor, and host auth settings are not imported",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return cursorAgentHarness.Bootstrap(ui, r)
		},
	},
	{
		Spec:             harnessMetadata(HarnessPi).Spec,
		LaunchCommand:    harnessMetadata(HarnessPi).LaunchCommand,
		BootstrapCommand: harnessMetadata(HarnessPi).BootstrapCommand,
		ImportPolicy:     harnessMetadata(HarnessPi).ImportPolicy,
		Installed: func() bool {
			_, ok := findInstalledPiBinary()
			return ok
		},
		Probe:                probePiHarness,
		ManagedCodeArtifacts: piHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + piBinRel + " manual Pi executable",
			agentHome + piStateDirRel + " contained Pi settings, trust decisions, sessions, skills, extensions, and auth",
			"host ~/.pi/agent is not imported",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return piHarness.Bootstrap(ui, r)
		},
	},
}

func (ClaudeHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessClaude)
}

func (CodexHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessCodex)
}

func (OpenCodeHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessOpenCode)
}

func (GeminiHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessGemini)
}

func (HermesHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessHermes)
}

func (QwenHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessQwen)
}

func (CursorAgentHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessCursorAgent)
}

func (PiHarness) Spec() HarnessSpec {
	return harnesses.MustSpec(HarnessPi)
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

func (h GeminiHarness) ImportBasics(ui *UI, r *Runner, env geminiImportEnv, opts geminiImportOptions) error {
	if err := runGeminiBasicsImport(ui, r, env, opts); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordBasicsImported(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s import state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h GeminiHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h GeminiHarness) RecordBasicsImported() error {
	return recordHarnessImportRun(h.Spec())
}

func (h HermesHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runHermesBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h HermesHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h QwenHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runQwenBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h QwenHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h CursorAgentHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runCursorAgentBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h CursorAgentHarness) RecordInstalled() error {
	return recordHarnessInstalled(h.Spec())
}

func (h PiHarness) Bootstrap(ui *UI, r *Runner) error {
	if err := runPiBootstrap(ui, r); err != nil {
		return err
	}
	if r != nil && !r.DryRun {
		if err := h.RecordInstalled(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not record %s harness state: %v", h.Spec().DisplayName, err))
		}
	}
	return nil
}

func (h PiHarness) RecordInstalled() error {
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

func formatInstalledHarnessNameForStatus(harness ManagedHarness, state HazmatState) string {
	name := harness.Spec.DisplayName
	if state.Harnesses == nil {
		return name + " (state missing)"
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || recorded.StateVersion == "" {
		return name + " (state missing)"
	}
	if !harnessruntime.StateCurrent(recorded, harness.Spec) {
		return fmt.Sprintf("%s (state v%s; want v%s)", name, recorded.StateVersion, harness.Spec.StateVersion)
	}
	return name
}

func formatInstalledHarnessNamesForStatus(installed []ManagedHarness, state HazmatState) []string {
	names := make([]string, 0, len(installed))
	for _, harness := range installed {
		names = append(names, formatInstalledHarnessNameForStatus(harness, state))
	}
	return names
}

func recordHarnessInstalled(spec HarnessSpec) error {
	return harnessruntime.RecordInstalled(stateStore(), spec)
}

func recordHarnessImportRun(spec HarnessSpec) error {
	return harnessruntime.RecordImportRun(stateStore(), spec)
}
