package main

import (
	"fmt"
	"os/user"

	"github.com/spf13/cobra"
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
// 'sandbox bootstrap'.  Add your custom rules BELOW the END marker — they
// will not be touched on reruns.
const agentPreToolUseHook = `#!/bin/bash
# [BEGIN sandbox-bootstrap managed] ──────────────────────────────────────────
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
# [END sandbox-bootstrap managed] ────────────────────────────────────────────

# Add custom rules here (not overwritten by sandbox bootstrap):

exit 0
`

func newBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Install Claude Code for the agent user and write default settings",
		Long: `Install Claude Code for the agent user, write a default settings.json with
allow/deny rules, and create a PreToolUse hook skeleton.

Run once after 'sandbox setup'. All steps require your password (sudo).
After bootstrap, 'sandbox claude' runs without any password prompt.

Steps:
  1. Verify the agent user exists (run 'sandbox setup' first if not)
  2. Install Claude Code under ~/.local/bin/claude for the agent user
  3. Write ~/.claude/settings.json (0600) if not already present
  4. Create ~/.claude/hooks/ (0700) and a PreToolUse hook (0700) if absent

This command is idempotent: steps already completed are skipped.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return runBootstrap(ui, r)
		},
	}
}

func runBootstrap(ui *UI, r *Runner) error {
	// ── Step 1: verify agent user ─────────────────────────────────────────────
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user %q not found — run 'sandbox setup' first", agentUser)
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	// ── Step 2: install Claude Code ───────────────────────────────────────────
	ui.Step("Install Claude Code for agent user")
	claudeBin := agentHome + "/.local/bin/claude"
	// Check via root stat (dr cannot read /Users/agent directly).
	if _, err := sudoOutput("test", "-x", claudeBin); err == nil {
		ui.SkipDone("Claude Code already installed")
	} else {
		ui.WarnMsg("Your sudo password is required to install Claude Code as the agent user.")
		// r.Interactive connects stdin/stdout/stderr so the curl installer's
		// prompts are fully visible.
		if err := r.Interactive("sudo", "-u", agentUser, "-i",
			"bash", "-c", "curl -fsSL https://claude.ai/install.sh | bash"); err != nil {
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
	if err := r.Sudo("install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", claudeDir); err != nil {
		return fmt.Errorf("ensure %s: %w", claudeDir, err)
	}

	if _, err := sudoOutput("test", "-f", settingsPath); err == nil {
		ui.SkipDone(settingsPath + " already present (not overwritten)")
	} else {
		if err := r.SudoWriteFile(settingsPath, agentSettingsJSON); err != nil {
			return fmt.Errorf("write settings.json: %w", err)
		}
		if err := r.Sudo("chown", agentUser+":staff", settingsPath); err != nil {
			return fmt.Errorf("chown settings.json: %w", err)
		}
		if err := r.Sudo("chmod", "0600", settingsPath); err != nil {
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
	if err := r.Sudo("install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", hooksDir); err != nil {
		return fmt.Errorf("ensure %s: %w", hooksDir, err)
	}

	// Check the hook script specifically — not just the directory.  This
	// handles the case where the directory exists but the script was never
	// written (partial run) or was deleted by the user.
	if _, err := sudoOutput("test", "-f", hookScript); err == nil {
		ui.SkipDone(hookScript + " already present (not overwritten)")
	} else {
		if err := r.SudoWriteFile(hookScript, agentPreToolUseHook); err != nil {
			return fmt.Errorf("write hook script: %w", err)
		}
		if err := r.Sudo("chown", agentUser+":staff", hookScript); err != nil {
			return fmt.Errorf("chown hook script: %w", err)
		}
		if err := r.Sudo("chmod", "0700", hookScript); err != nil {
			return fmt.Errorf("chmod hook script: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0700)", hookScript))
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
	fmt.Println("       cd ~/workspace/my-project")
	fmt.Println("       sandbox claude")
	fmt.Println()

	return nil
}
