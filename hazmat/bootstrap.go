package main

import (
	"fmt"
	"os"
	"os/user"

	"github.com/spf13/cobra"
)

const (
	claudeInstallerURL    = "https://claude.ai/install.sh"
	claudeInstallerSHA256 = "431889ac7d056f636aaf5b71524666d04c89c45560f80329940846479d484778"
)

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
		Short: "Install a harness tool for the agent user",
		Long: `Install a supported harness tool into the agent environment.

Without a subcommand, 'hazmat bootstrap' installs Claude Code for backward
compatibility.

Subcommands:
  hazmat bootstrap claude
  hazmat bootstrap opencode`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return claudeCodeHarness.Bootstrap(ui, r)
		},
	}
	cmd.AddCommand(newBootstrapClaudeCmd())
	cmd.AddCommand(newBootstrapOpenCodeCmd())
	return cmd
}

func newBootstrapClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Install Claude Code for the agent user and write default settings",
		Long: `Install Claude Code for the agent user, write a default settings.json with
allow/deny rules, and create a PreToolUse hook skeleton.

Run once after 'hazmat init'. Uses the passwordless sudo rule configured
during init — no password prompt.

Steps:
  1. Verify the agent user exists (run 'hazmat init' first if not)
  2. Install Claude Code under ~/.local/bin/claude for the agent user
  3. Write ~/.claude/settings.json (0600) if not already present
  4. Create ~/.claude/hooks/ (0700) and a PreToolUse hook (0700) if absent

This command is idempotent: steps already completed are skipped.`,
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
	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user %q not found — run 'hazmat init' first", agentUser)
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	// ── Step 2: install Claude Code ───────────────────────────────────────────
	ui.Step("Install Claude Code for agent user")
	claudeBin := agentHome + "/.local/bin/claude"
	// Check via root stat (dr cannot read /Users/agent directly).
	if _, err := sudoOutput("test", "-x", claudeBin); err == nil {
		ui.SkipDone("Claude Code already installed")
	} else {
		// Write the installer script to a temp file rather than passing it
		// inline. sudo -i joins arguments into a single string for the login
		// shell, which strips newlines and evaluates $variables prematurely.
		installScript := fmt.Sprintf(`#!/bin/bash
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
bash "$installer"
`, claudeInstallerURL, claudeInstallerSHA256)

		scriptFile, err := os.CreateTemp("/tmp", "hazmat-bootstrap-*.sh")
		if err != nil {
			return fmt.Errorf("create bootstrap script: %w", err)
		}
		defer os.Remove(scriptFile.Name())
		if _, err := scriptFile.WriteString(installScript); err != nil {
			scriptFile.Close()
			return fmt.Errorf("write bootstrap script: %w", err)
		}
		scriptFile.Close()
		os.Chmod(scriptFile.Name(), 0o755)

		if err := r.SudoVisible("download, verify, and install Claude Code as agent user",
			"-u", agentUser, "-H", "bash", scriptFile.Name()); err != nil {
			return fmt.Errorf("install Claude Code: %w", err)
		}
		ui.Ok("Claude Code installed")
	}

	// ── Step 3: write settings.json ───────────────────────────────────────────
	ui.Step("Write agent Claude settings")
	claudeDir := agentHome + "/.claude"
	settingsPath := claudeDir + "/settings.json"

	// Always ensure .claude is agent-owned so the agent process can write
	// to it at runtime.  'install -d' is idempotent: it creates the
	// directory if absent and applies the given owner/mode unconditionally.
	if err := r.Sudo("create agent .claude config directory", "install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", claudeDir); err != nil {
		return fmt.Errorf("ensure %s: %w", claudeDir, err)
	}

	if _, err := sudoOutput("test", "-f", settingsPath); err == nil {
		ui.SkipDone(settingsPath + " already present (not overwritten)")
	} else {
		if err := r.SudoWriteFile("write agent Claude settings", settingsPath, agentSettingsJSON); err != nil {
			return fmt.Errorf("write settings.json: %w", err)
		}
		if err := r.Sudo("set agent settings ownership", "chown", agentUser+":staff", settingsPath); err != nil {
			return fmt.Errorf("chown settings.json: %w", err)
		}
		if err := r.Sudo("set agent settings permissions", "chmod", "0600", settingsPath); err != nil {
			return fmt.Errorf("chmod settings.json: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0600)", settingsPath))
	}

	// ── Step 4: create hooks skeleton ─────────────────────────────────────────
	ui.Step("Create hooks skeleton")
	hooksDir := claudeDir + "/hooks"
	hookScript := hooksDir + "/pre-tool-use.sh"

	// Always ensure hooks/ is agent-owned (same idempotent install -d logic
	// as .claude above).
	if err := r.Sudo("create agent hooks directory", "install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", hooksDir); err != nil {
		return fmt.Errorf("ensure %s: %w", hooksDir, err)
	}

	// Check the hook script specifically — not just the directory.  This
	// handles the case where the directory exists but the script was never
	// written (partial run) or was deleted by the user.
	if _, err := sudoOutput("test", "-f", hookScript); err == nil {
		ui.SkipDone(hookScript + " already present (not overwritten)")
	} else {
		if err := r.SudoWriteFile("write agent pre-tool-use hook", hookScript, agentPreToolUseHook); err != nil {
			return fmt.Errorf("write hook script: %w", err)
		}
		if err := r.Sudo("set hook script ownership", "chown", agentUser+":staff", hookScript); err != nil {
			return fmt.Errorf("chown hook script: %w", err)
		}
		if err := r.Sudo("make hook script executable", "chmod", "0700", hookScript); err != nil {
			return fmt.Errorf("chmod hook script: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0700)", hookScript))
	}

	// ── Step 5: supply chain hardening ───────────────────────────────────────
	// Block lifecycle scripts in package managers to prevent supply chain
	// attacks like the axios compromise (axios/axios#10604) where a malicious
	// postinstall hook delivered a RAT within 2 seconds of npm install.
	ui.Step("Supply chain hardening (package manager scripts)")

	npmrc := agentHome + "/.npmrc"
	if _, err := sudoOutput("test", "-f", npmrc); err == nil {
		// Check if ignore-scripts is already set.
		if out, _ := sudoOutput("grep", "ignore-scripts", npmrc); out != "" {
			ui.SkipDone("npm ignore-scripts already configured")
		} else {
			if err := r.Sudo("append ignore-scripts to npmrc",
				"bash", "-c", fmt.Sprintf("echo 'ignore-scripts=true' >> %s", npmrc)); err != nil {
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
		if err := r.SudoWriteFile("write agent .npmrc with ignore-scripts", npmrc, npmrcContent); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not write %s: %v", npmrc, err))
		} else {
			r.Sudo("set npmrc ownership", "chown", agentUser+":staff", npmrc)
			ui.Ok("npm: ignore-scripts=true (blocks postinstall supply chain attacks)")
		}
	}

	// pip: use --no-input and recommend uv for safer installs.
	// pip's setup.py runs arbitrary code at install time — same attack class
	// as npm postinstall. Unlike npm, pip has no global ignore-scripts flag,
	// but we can set safer defaults.
	pipConf := agentHome + "/.config/pip/pip.conf"
	if _, err := sudoOutput("test", "-f", pipConf); err == nil {
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
		r.Sudo("create pip config directory", "mkdir", "-p", pipConfDir)
		if err := r.SudoWriteFile("write agent pip.conf", pipConf, pipConfContent); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not write %s: %v", pipConf, err))
		} else {
			r.Sudo("set pip config ownership", "chown", "-R", agentUser+":staff", pipConfDir)
			ui.Ok("pip: safer defaults configured")
		}
	}

	// ── Done ──────────────────────────────────────────────────────────────────
	fmt.Println()
	cGreen.Println("━━━ Bootstrap complete ━━━")
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println()
	cBold.Println("  1. Set the Anthropic API key for the agent user:")
	fmt.Printf("       sudo -u %s -i\n", agentUser)
	fmt.Println(`       echo 'export ANTHROPIC_API_KEY="sk-ant-..."' >> ~/.zshrc`)
	fmt.Println()
	cBold.Println("  2. Configure git for the agent user:")
	fmt.Printf(`       sudo -u %s git config --global user.name "Agent"`+"\n", agentUser)
	fmt.Printf(`       sudo -u %s git config --global user.email "agent@localhost"`+"\n", agentUser)
	fmt.Println()
	cBold.Println("  3. (Optional) Review and customize:")
	fmt.Println("       " + settingsPath + "  — allow/deny rules")
	fmt.Println("       " + hookScript + "  — PreToolUse guard")
	fmt.Println()
	cBold.Println("  4. Test a session:")
	fmt.Println("       cd your-project")
	fmt.Println("       hazmat claude")
	fmt.Println()

	return nil
}
