# Hazmat quickstart: install → preview → init with Claude → first contained session
#
# Shows the complete first-time experience. Someone watching this should
# think "I could do that in 10 minutes."
#
# Prerequisites:
#   - macOS with Homebrew
#   - hazmat NOT initialized (reset manually with approved rollback command)
#   - hazmat NOT installed via brew (run: brew uninstall hazmat 2>/dev/null)
#   - ~/workspace/demo-project exists with some source code (ideally with go.mod
#     or package.json so an integration auto-resolves)
#   - explicit approval to run setup; do not run this from an agent workflow
#     without asking for the exact sudo-adjacent recording command
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

Source assets/config.tape

Hide
Type "clear"
Enter
Sleep 500ms
Show

# ── Scene 1: Install ──────────────────────────────────────────────

Type "brew install dredozubov/tap/hazmat"
Enter
Wait+Line /hazmat was successfully installed/
Sleep 1s

# ── Scene 2: Preview before host mutation ─────────────────────────

Type "hazmat init --dry-run"
Enter
Sleep 3s

# ── Scene 3: One-time setup with Claude bootstrap ─────────────────

Type "hazmat init --bootstrap-agent claude"
Enter
Sleep 4s                          # TIMING: banner + intro text

# "Proceed with setup? [y/N]"
Type "y"
Enter
Sleep 3s                          # TIMING: pre-flight checks

# Setup steps run sequentially (user, group, ACLs, snapshots,
# hardening, seatbelt, wrappers, launch helper, sudoers, pf,
# DNS blocklist, launch daemon, harness bootstrap, verify)
Wait+Line /API key/
Sleep 500ms

# "API key:" — Enter to skip (agent is already logged in via subscription)
Enter
Sleep 1s

# "Name [<default>] (from host git config, Enter to accept):"
Wait+Line /Name/
Sleep 500ms
Enter

# "Email [<default>] (from host git config, Enter to accept):"
Wait+Line /Email/
Sleep 500ms
Enter

# "Import Claude basics from your host setup? [y/N]"
Wait+Line /Import Claude basics/
Sleep 500ms
Type "y"
Enter
Wait+Line /Setup complete/
Sleep 1s

# ── Scene 4: First contained session ──────────────────────────────
# The session contract prints automatically — Mode, Project,
# Integrations, read-only paths, snapshot config.
#
# The agent user must already be logged in to Claude via subscription.
# Before recording:
#   hazmat init --yes → cd ~/workspace/demo-project → hazmat claude
#   → /login → complete browser auth → exit → sudo hazmat rollback
# Plain rollback keeps /Users/agent (and Claude auth) intact.

Type "cd ~/workspace/demo-project"
Enter
Sleep 500ms

Type "hazmat explain"
Enter
Wait+Line /hazmat: session/
Sleep 1s

Type "hazmat claude"
Enter
Sleep 5s                          # TIMING: snapshot + seatbelt + session contract + launch

Type@70ms "What does this project do? One sentence."
Enter
Sleep 8s                          # TIMING: depends on Claude response speed

Ctrl+C
Sleep 1.5s

# ── Scene 5: All green ────────────────────────────────────────────

Type "hazmat --help"
Enter
Sleep 4s
