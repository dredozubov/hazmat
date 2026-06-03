package hazmat

import (
	"fmt"
	"hazmat/credentials"
	"time"
)

type HarnessID = credentials.HarnessID

const (
	HarnessClaude                  HarnessID = credentials.HarnessClaude
	HarnessCodex                   HarnessID = credentials.HarnessCodex
	HarnessOpenCode                HarnessID = credentials.HarnessOpenCode
	HarnessGemini                  HarnessID = credentials.HarnessGemini
	HarnessHermes                  HarnessID = credentials.HarnessHermes
	HarnessQwen                    HarnessID = credentials.HarnessQwen
	HarnessCursorAgent             HarnessID = credentials.HarnessCursorAgent
	claudeHarnessStateVersion                = "1"
	codexHarnessStateVersion                 = "1"
	opencodeHarnessStateVersion              = "1"
	geminiHarnessStateVersion                = "1"
	hermesHarnessStateVersion                = "1"
	qwenHarnessStateVersion                  = "1"
	cursorAgentHarnessStateVersion           = "1"
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

type harnessImportPolicy struct {
	Supported bool
	Boundary  string
}

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

var claudeCodeHarness = ClaudeHarness{}
var codexHarness = CodexHarness{}
var openCodeHarness = OpenCodeHarness{}
var geminiHarness = GeminiHarness{}
var hermesHarness = HermesHarness{}
var qwenHarness = QwenHarness{}
var cursorAgentHarness = CursorAgentHarness{}

var managedHarnessRegistry = []ManagedHarness{
	{
		Spec:             claudeCodeHarness.Spec(),
		LaunchCommand:    "hazmat claude",
		BootstrapCommand: "hazmat bootstrap claude",
		ImportPolicy: harnessImportPolicy{
			Supported: true,
			Boundary:  "portable Claude auth, settings, hooks, and project basics",
		},
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
		Spec:             codexHarness.Spec(),
		LaunchCommand:    "hazmat codex",
		BootstrapCommand: "hazmat bootstrap codex",
		ImportPolicy: harnessImportPolicy{
			Supported: true,
			Boundary:  "portable Codex auth, config, prompts, and session basics",
		},
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
		Spec:             openCodeHarness.Spec(),
		LaunchCommand:    "hazmat opencode",
		BootstrapCommand: "hazmat bootstrap opencode",
		ImportPolicy: harnessImportPolicy{
			Supported: true,
			Boundary:  "portable OpenCode auth, config, command, and agent basics",
		},
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
		Spec:             geminiHarness.Spec(),
		LaunchCommand:    "hazmat gemini",
		BootstrapCommand: "hazmat bootstrap gemini",
		ImportPolicy: harnessImportPolicy{
			Supported: true,
			Boundary:  "file-backed Gemini OAuth, accounts, settings, and memory basics; Keychain OAuth remains external",
		},
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
		Spec:             hermesHarness.Spec(),
		LaunchCommand:    "hazmat hermes",
		BootstrapCommand: "hazmat bootstrap hermes",
		ImportPolicy: harnessImportPolicy{
			Supported: false,
			Boundary:  "Hermes v1 has no curated import; manual executable, profile roots, and provider state are preserved",
		},
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
		Spec:             qwenHarness.Spec(),
		LaunchCommand:    "hazmat qwen",
		BootstrapCommand: "hazmat bootstrap qwen",
		ImportPolicy: harnessImportPolicy{
			Supported: false,
			Boundary:  "Qwen v1 has no curated import; contained profile state and host asset sync boundaries are preserved",
		},
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
		Spec:             cursorAgentHarness.Spec(),
		LaunchCommand:    "hazmat cursor-agent",
		BootstrapCommand: "hazmat bootstrap cursor-agent",
		ImportPolicy: harnessImportPolicy{
			Supported: false,
			Boundary:  "Cursor Agent v1 has no curated import; host Cursor IDE state, host ~/.cursor profile state, and host auth settings are not imported",
		},
		Installed: func() bool {
			_, ok := findInstalledCursorAgentBinary()
			return ok
		},
		Probe:                probeCursorAgentHarness,
		ManagedCodeArtifacts: cursorAgentHarnessManagedCodeArtifacts,
		PreservedArtifacts: []string{
			agentHome + cursorAgentBinRel + " manual Cursor Agent executable",
			agentHome + "/.cursor contained Cursor Agent profile state",
			"host Cursor IDE state and auth settings are not imported",
		},
		Bootstrap: func(ui *UI, r *Runner) error {
			return cursorAgentHarness.Bootstrap(ui, r)
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

func (HermesHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessHermes,
		DisplayName:  "Hermes",
		StateVersion: hermesHarnessStateVersion,
	}
}

func (QwenHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessQwen,
		DisplayName:  "Qwen Code",
		StateVersion: qwenHarnessStateVersion,
	}
}

func (CursorAgentHarness) Spec() HarnessSpec {
	return HarnessSpec{
		ID:           HarnessCursorAgent,
		DisplayName:  "Cursor Agent",
		StateVersion: cursorAgentHarnessStateVersion,
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

func harnessStateCurrent(state HarnessState, spec HarnessSpec) bool {
	return state.StateVersion == spec.StateVersion
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
	if !harnessStateCurrent(recorded, harness.Spec) {
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
