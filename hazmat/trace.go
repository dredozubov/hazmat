//go:build hazmat_debug

package main

import (
	"path/filepath"

	"hazmat/internal/debugtrace"

	"github.com/spf13/cobra"
)

func newTraceCmd() *cobra.Command {
	return debugtrace.NewCommand(traceEnv(), supportedTraceHarnessSpecs())
}

func traceEnv() debugtrace.Env {
	return debugtrace.Env{
		AgentHome:         agentHome,
		AgentUser:         agentUser,
		DefaultAgentPath:  defaultAgentPath,
		HostLsPath:        hostLsPath,
		HostLogPath:       hostLogPath,
		HostScriptPath:    hostScriptPath,
		HostSudoPath:      hostSudoPath,
		HostUnamePath:     hostUnamePath,
		ExpandTilde:       expandTilde,
		RunSessionCommand: runSessionCommand,
	}
}

func supportedTraceHarnessSpecs() []debugtrace.HarnessSpec {
	return []debugtrace.HarnessSpec{
		{
			ID:               debugtrace.HarnessID(HarnessClaude),
			DisplayName:      "Claude Code",
			CommandName:      "claude",
			LaunchCommand:    "hazmat claude",
			BootstrapCommand: "hazmat bootstrap claude",
			Installed:        installedTraceHarness(HarnessClaude),
			Explain:          traceExplain("claude", parseClaudeArgs),
			ProcessFilters: []string{
				"claude",
				"2.1.",
				"com.anthropic",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".claude"),
				filepath.Join(agentHome, ".claude.json"),
				filepath.Join(agentHome, ".local", "share", "claude"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/claude",
			},
			SampleArgs: []string{"-p", "say ok"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessCodex),
			DisplayName:      "Codex",
			CommandName:      "codex",
			LaunchCommand:    "hazmat codex",
			BootstrapCommand: "hazmat bootstrap codex",
			Installed:        installedTraceHarness(HarnessCodex),
			Explain:          traceExplain("codex", parseHarnessArgs),
			ProcessFilters: []string{
				"codex",
				"com.openai.codex",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".codex"),
				filepath.Join(agentHome, ".agents"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/codex",
			},
			SampleArgs: []string{"exec", "say ok"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessOpenCode),
			DisplayName:      "OpenCode",
			CommandName:      "opencode",
			LaunchCommand:    "hazmat opencode",
			BootstrapCommand: "hazmat bootstrap opencode",
			Installed:        installedTraceHarness(HarnessOpenCode),
			Explain:          traceExplain("opencode", parseHarnessArgs),
			ProcessFilters: []string{
				"opencode",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".opencode"),
				filepath.Join(agentHome, ".config", "opencode"),
				filepath.Join(agentHome, ".local", "share", "opencode"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/opencode",
			},
			SampleArgs: []string{"run", "say ok"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessGemini),
			DisplayName:      "Gemini",
			CommandName:      "gemini",
			LaunchCommand:    "hazmat gemini",
			BootstrapCommand: "hazmat bootstrap gemini",
			Installed:        installedTraceHarness(HarnessGemini),
			Explain:          traceExplain("gemini", parseHarnessArgs),
			ProcessFilters: []string{
				"gemini",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".gemini"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/gemini",
			},
			SampleArgs: []string{"-p", "say ok"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessHermes),
			DisplayName:      "Hermes",
			CommandName:      "hermes",
			LaunchCommand:    "hazmat hermes",
			BootstrapCommand: "hazmat bootstrap hermes",
			Installed:        installedTraceHarness(HarnessHermes),
			Explain:          traceExplain("hermes", parseHarnessArgs),
			ProcessFilters: []string{
				"hermes",
			},
			AgentStatePaths: []string{
				hermesStateDir(),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/providers",
			},
			SampleArgs: []string{"--version"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessQwen),
			DisplayName:      "Qwen Code",
			CommandName:      "qwen",
			LaunchCommand:    "hazmat qwen",
			BootstrapCommand: "hazmat bootstrap qwen",
			Installed:        installedTraceHarness(HarnessQwen),
			Explain:          traceExplain("qwen", parseHarnessArgs),
			ProcessFilters: []string{
				"qwen",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".qwen"),
			},
			HostStatePaths: []string{
				"~/.hazmat/harness-assets.json",
			},
			SampleArgs: []string{"-p", "say ok"},
		},
		{
			ID:               debugtrace.HarnessID(HarnessCursorAgent),
			DisplayName:      "Cursor Agent",
			CommandName:      "cursor-agent",
			LaunchCommand:    "hazmat cursor-agent",
			BootstrapCommand: "hazmat bootstrap cursor-agent",
			Installed:        installedTraceHarness(HarnessCursorAgent),
			Explain:          traceExplain("cursor-agent", parseHarnessArgs),
			ProcessFilters: []string{
				"cursor-agent",
				"cursor",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".cursor"),
				filepath.Join(agentHome, ".config", "cursor"),
			},
			SampleArgs: []string{"--", "--version"},
		},
	}
}

func traceHarnessSpecByID(id HarnessID) (debugtrace.HarnessSpec, bool) {
	return debugtrace.HarnessSpecByID(supportedTraceHarnessSpecs(), debugtrace.HarnessID(id))
}

func traceExplain(commandName string, parser harnessArgsParser) func([]string) (any, error) {
	return func(forwarded []string) (any, error) {
		opts, _, err := parser(forwarded)
		if err != nil {
			return nil, err
		}
		opts.planOnly = true
		cfg, mode, err := resolveExplainSession(commandName, opts)
		if err != nil {
			return nil, err
		}
		return buildExplainJSON(commandName, cfg, mode, opts.noBackup), nil
	}
}

func installedTraceHarness(id HarnessID) func() bool {
	return func() bool {
		for _, managed := range managedHarnessRegistry {
			if managed.Spec.ID == id && managed.Installed != nil {
				return managed.Installed()
			}
		}
		return false
	}
}
