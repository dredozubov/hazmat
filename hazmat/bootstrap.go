package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	claudeInstallerURL    = "https://claude.ai/install.sh"
	claudeInstallerSHA256 = "b315b46925a9bfb9422f2503dd5aa649f680832f4c076b22d87c39d578c3d830"
)

func findInstalledClaudeBinary() (string, bool) {
	return findInstalledClaudeBinaryWith(asAgentOutput)
}

func findInstalledClaudeBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + "/.local/bin/claude"
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func claudeInstallScript() string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
installer=$(mktemp "${TMPDIR:-/tmp}/claude-install.XXXXXX")
cleanup() { rm -f "$installer"; }
trap cleanup EXIT
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$installer"
actual=$(shasum -a 256 "$installer" | awk '{print $1}')
expected=%q
if [[ "$actual" != "$expected" ]]; then
  echo "Claude installer checksum mismatch: expected $expected, got $actual" >&2
  exit 1
fi
bash "$installer" latest
test -x "$HOME/.local/bin/claude"
`, claudeInstallerURL, claudeInstallerSHA256)
}

// agentSettingsJSON is the default Claude Code settings written to the agent
// user's ~/.claude/settings.json during bootstrap.
//
// The allow list is intentionally empty: Claude Code permits all tools by
// default, and the seatbelt profile is the primary enforcement boundary.
// The deny list adds a software-layer defence-in-depth against the most
// commonly abused patterns.
//
// Edit after bootstrap to tighten or loosen rules.  The deny rules use
// prefix-match patterns, so "Bash(sudo*)" blocks "sudo", "sudo -i", etc.
//
// This file is written once and never overwritten by subsequent bootstrap
// runs — edit it freely.
const agentSettingsJSON = `{
  "permissions": {
    "allow": [],
    "deny": [
      "Bash(sudo*)",
      "Bash(su *)",
      "Bash(* | bash)",
      "Bash(* | sh)",
      "Bash(* | zsh)",
      "Bash(bash <*)",
      "Bash(sh <*)",
      "Bash(curl * | *)",
      "Bash(wget * | *)",
      "Bash(rm -rf /*)",
      "Bash(rm -rf /)"
    ]
  }
}
`

// agentPreToolUseHook is the template written to
// ~/.claude/hooks/pre-tool-use.sh during bootstrap.
//
// The block between the managed markers is the framework section updated by
// 'hazmat init'.  Add your custom rules BELOW the END marker — they
// will not be touched on reruns.
const agentPreToolUseHook = `#!/bin/bash
# [BEGIN hazmat-bootstrap managed] ──────────────────────────────────────────
# PreToolUse hook — runs before every Claude Code tool invocation.
# Receives tool details in $HOOK_INPUT (JSON).
# Exit 0 to allow.  Exit 2 with a message on stdout to block.
#
# Example: block any Bash command that starts with "sudo"
#
#   if [ "$(echo "$HOOK_INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_name',''))")" = "Bash" ]; then
#     cmd=$(echo "$HOOK_INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_input',{}).get('command',''))")
#     if [[ "$cmd" == sudo* ]]; then
#       echo "sudo is blocked in the agent sandbox"
#       exit 2
#     fi
#   fi
# [END hazmat-bootstrap managed] ────────────────────────────────────────────

# Add custom rules here (not overwritten by hazmat init):

exit 0
`

func newBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install or update a harness tool for the agent user",
		Long: `Install or update a supported harness tool in the agent environment.

Without a subcommand, 'hazmat bootstrap' installs Claude Code for backward
compatibility.

Subcommands:
  hazmat bootstrap claude
  hazmat bootstrap codex
  hazmat bootstrap opencode
  hazmat bootstrap gemini`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return claudeCodeHarness.Bootstrap(ui, r)
		},
	}
	cmd.AddCommand(newBootstrapClaudeCmd())
	cmd.AddCommand(newBootstrapCodexCmd())
	cmd.AddCommand(newBootstrapOpenCodeCmd())
	cmd.AddCommand(newBootstrapGeminiCmd())
	return cmd
}

func newBootstrapClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Install or update Claude Code for the agent user and write default settings",
		Long: `Install or update Claude Code for the agent user, write a default settings.json with
allow/deny rules, and create a PreToolUse hook skeleton.

Run once after 'hazmat init'. Uses the passwordless sudo rule configured
during init for the Hazmat helper. Hazmat-owned bootstrap steps run through
that narrow helper path; the broader optional agent-maintenance sudoers rule
is only for manual generic 'sudo -u agent ...' commands.

Steps:
  1. Verify the agent user exists (run 'hazmat init' first if not)
  2. Install or update Claude Code under ~/.local/bin/claude for the agent user
  3. Write ~/.claude/settings.json (0600) if not already present
  4. Create ~/.claude/hooks/ (0700) and a PreToolUse hook (0700) if absent

This command refreshes the harness binary and leaves existing settings/hooks alone.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return claudeCodeHarness.Bootstrap(ui, r)
		},
	}
}

func runBootstrap(ui *UI, r *Runner) error {
	// ── Step 1: verify agent user ─────────────────────────────────────────────
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := requireAgentUser(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	// ── Step 2: install/update Claude Code ────────────────────────────────────
	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "Claude Code",
		TempPattern:   "hazmat-claude-bootstrap-*.sh",
		InstallReason: "download, verify, and install or update Claude Code as agent user",
		BuildScript: func(bool) (string, error) {
			return claudeInstallScript(), nil
		},
		FindExisting: findInstalledClaudeBinaryWith,
	}); err != nil {
		return err
	}

	// ── Step 3: write settings.json ───────────────────────────────────────────
	ui.Step("Write agent Claude settings")
	claudeDir := agentHome + "/.claude"
	settingsPath := claudeDir + "/settings.json"

	if err := agentEnsureSharedDir(claudeDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", claudeDir, err)
	}
	projectsDir := claudeDir + "/projects"
	if err := agentEnsureSharedDir(projectsDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", projectsDir, err)
	}

	if _, err := r.AgentOutput("test", "-f", settingsPath); err == nil {
		ui.SkipDone(settingsPath + " already present (not overwritten)")
	} else {
		if err := agentWriteSharedFile(settingsPath, []byte(agentSettingsJSON), 0o660); err != nil {
			return fmt.Errorf("write settings.json: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0660)", settingsPath))
	}

	// ── Step 4: create hooks skeleton ─────────────────────────────────────────
	ui.Step("Create hooks skeleton")
	hooksDir := claudeDir + "/hooks"
	hookScript := hooksDir + "/pre-tool-use.sh"

	if err := agentEnsureSharedDir(hooksDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", hooksDir, err)
	}

	// Check the hook script specifically — not just the directory.  This
	// handles the case where the directory exists but the script was never
	// written (partial run) or was deleted by the user.
	if _, err := r.AgentOutput("test", "-f", hookScript); err == nil {
		ui.SkipDone(hookScript + " already present (not overwritten)")
	} else {
		if err := agentWriteSharedFile(hookScript, []byte(agentPreToolUseHook), 0o770); err != nil {
			return fmt.Errorf("write hook script: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0770)", hookScript))
	}

	// ── Step 5: supply chain hardening ───────────────────────────────────────
	// Block lifecycle scripts in package managers to prevent supply chain
	// attacks like the axios compromise (axios/axios#10604) where a malicious
	// postinstall hook delivered a RAT within 2 seconds of npm install.
	ui.Step("Supply chain hardening (package manager scripts)")

	npmrc := agentHome + "/.npmrc"
	if current, err := r.AgentOutput("cat", npmrc); err == nil {
		// Check if ignore-scripts is already set.
		if strings.Contains(current, "ignore-scripts") {
			ui.SkipDone("npm ignore-scripts already configured")
		} else {
			updated := current
			if updated != "" && !strings.HasSuffix(updated, "\n") {
				updated += "\n"
			}
			updated += "ignore-scripts=true\n"
			if err := agentWriteFile(npmrc, []byte(updated), 0o644); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", npmrc, err))
			} else {
				ui.Ok("npm: ignore-scripts=true appended to " + npmrc)
			}
		}
	} else {
		npmrcContent := `# Managed by hazmat — supply chain hardening.
#
# Blocks npm lifecycle scripts (preinstall, postinstall, etc.) to prevent
# supply chain attacks. The axios compromise (2026) delivered a RAT entirely
# through a postinstall hook that executed in 2 seconds — before detection
# was possible.
#
# To allow scripts for a specific package:
#   npm install --ignore-scripts=false sharp
#
# CVE references: axios/axios#10604, ua-parser-js (2021), event-stream (2018)
ignore-scripts=true
`
		if err := agentWriteFile(npmrc, []byte(npmrcContent), 0o644); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not write %s: %v", npmrc, err))
		} else {
			ui.Ok("npm: ignore-scripts=true (blocks postinstall supply chain attacks)")
		}
	}

	// pip: use --no-input and recommend uv for safer installs.
	// pip's setup.py runs arbitrary code at install time — same attack class
	// as npm postinstall. Unlike npm, pip has no global ignore-scripts flag,
	// but we can set safer defaults.
	pipConf := agentHome + "/.config/pip/pip.conf"
	if _, err := r.AgentOutput("test", "-f", pipConf); err == nil {
		ui.SkipDone("pip.conf already configured")
	} else {
		pipConfContent := `# Managed by hazmat — supply chain hardening.
#
# pip's setup.py mechanism runs arbitrary code at install time.
# These settings reduce the attack surface but cannot fully prevent it.
# Prefer uv (https://github.com/astral-sh/uv) for safer dependency installation.
[global]
no-input = true
disable-pip-version-check = true
`
		pipConfDir := agentHome + "/.config/pip"
		if err := agentEnsureDir(pipConfDir, 0o755); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not create %s: %v", pipConfDir, err))
		} else if err := agentWriteFile(pipConf, []byte(pipConfContent), 0o644); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not write %s: %v", pipConf, err))
		} else {
			ui.Ok("pip: safer defaults configured")
		}
	}

	// ── Done ──────────────────────────────────────────────────────────────────
	fmt.Println()
	cGreen.Println("━━━ Bootstrap complete ━━━")
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println()
	cBold.Println("  1. Configure agent credentials:")
	fmt.Println("       hazmat config agent")
	fmt.Println()
	cBold.Println("  2. (Optional) Review and customize:")
	fmt.Println("       " + settingsPath + "  — allow/deny rules")
	fmt.Println("       " + hookScript + "  — PreToolUse guard")
	fmt.Println()
	cBold.Println("  3. Test a session:")
	fmt.Println("       cd your-project")
	fmt.Println("       hazmat claude")
	fmt.Println()

	return nil
}
