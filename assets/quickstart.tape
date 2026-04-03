# Hazmat quickstart: install → init → first contained session
#
# Shows the complete first-time experience. Someone watching this should
# think "I could do that in 10 minutes."
#
# Prerequisites:
#   - macOS with Homebrew
#   - hazmat NOT initialized (reset with: hazmat rollback --delete-user --delete-group)
#   - ~/workspace/demo-project exists with some source code
#   - ANTHROPIC_API_KEY exported in host shell (init will offer to copy it)
#   - sudo cached: run `sudo -v` immediately before `vhs assets/quickstart.tape`
#
# Timing: Sleep values are estimates for an M-series Mac with a warm
# Homebrew cache. If a prompt hasn't appeared when the next Type fires,
# increase the preceding Sleep. Search "TIMING" for sensitive waits.
#
# Pre-cache the tap to keep brew install fast:
#   brew tap dredozubov/tap

Output assets/quickstart.gif
Output assets/quickstart.webm

Require brew

Set Shell "zsh"
Set FontSize 15
Set FontFamily "JetBrains Mono"
Set Width 1100
Set Height 600
Set Padding 15
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 10
Set MarginFill "#1e1e2e"

Hide
Type "clear"
Enter
Sleep 500ms
Show

# ── Scene 1: Install ──────────────────────────────────────────────

Type "brew install dredozubov/tap/hazmat"
Enter
Sleep 20s                         # TIMING: pre-cache the tap to keep this fast

# ── Scene 2: Pre-init status — nothing configured yet ─────────────

Type "hazmat"
Enter
Sleep 3s

# ── Scene 3: One-time setup ───────────────────────────────────────

Type "hazmat init"
Enter
Sleep 4s                          # TIMING: banner + intro text

# "Proceed with setup? [y/N]"
Type "y"
Enter
Sleep 3s                          # TIMING: pre-flight checks

# Steps 1–12 run sequentially (user, group, ACLs, snapshots,
# seatbelt, wrappers, sudoers, pf, DNS blocklist, launchdaemon, verify)
Sleep 50s                         # TIMING: most sensitive — the full setup sequence

# "Found ANTHROPIC_API_KEY in your environment ... Copy this key? [y/N]"
Type "y"
Enter
Sleep 2s

# "Name [Claude Code] (Enter to accept):"
Enter
Sleep 1s

# "Email [you@example.com] (Enter to accept):"
Enter
Sleep 2s

# "Import Claude portable config? [y/N]"
Type "y"
Enter
Sleep 5s                          # TIMING: import + final verify + "Setup complete"

# ── Scene 4: First contained session ──────────────────────────────

Type "cd ~/workspace/demo-project"
Enter
Sleep 500ms

Type "hazmat claude"
Enter
Sleep 5s                          # TIMING: snapshot + seatbelt generation + launch

Type@70ms "What does this project do? One sentence."
Enter
Sleep 8s                          # TIMING: depends on Claude response speed

Ctrl+C
Sleep 1.5s

# ── Scene 5: All green ───────────────────────────────────────────

Type "hazmat"
Enter
Sleep 4s
