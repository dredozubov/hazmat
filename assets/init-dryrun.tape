# Hazmat init dry-run GIF: see what setup does before committing
#
# Shows transparency — every step explained, nothing hidden.
# Reassures people that `hazmat init` is not magic.
#
# Prerequisites: none (dry-run doesn't require prior setup)

Output assets/init-dryrun.gif
Output assets/init-dryrun.webm

Require hazmat

Set Shell "zsh"
Set FontSize 14
Set FontFamily "JetBrains Mono"
Set Width 1100
Set Height 650
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

# The whole point: preview everything without touching the system
Type "hazmat init --dry-run"
Enter

# Init output is multi-step — give it time to render
Sleep 8s

# Scroll through if needed
Sleep 3s
