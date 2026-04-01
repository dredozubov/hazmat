# Hazmat hero GIF: full autonomy, controlled environment
#
# Shows the daily workflow: enter containment, work normally, exit.
# The isolation proof is brief — the focus is "this is how I work all day."
#
# Prerequisites: hazmat init already completed

Output assets/hero.gif
Output assets/hero.webm

Require hazmat

Set Shell "zsh"
Set FontSize 16
Set FontFamily "JetBrains Mono"
Set Width 1000
Set Height 550
Set Padding 15
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 10
Set MarginFill "#1e1e2e"

# Hidden setup: navigate to a real project
Hide
Type "cd ~/workspace/demo-project && clear"
Enter
Sleep 1s
Show

# Scene 1: Enter containment — one command
Type "hazmat claude"
Enter
Sleep 3s

# Scene 2: Show Claude Code is running, do a quick task
Type@70ms "What does main.go do? Keep it to 2 sentences."
Enter
Sleep 6s

# Scene 3: Exit Claude, check what changed
Ctrl+C
Sleep 1s

Type "hazmat diff"
Enter
Sleep 3s

# Scene 4: Everything is fine, back to normal
Type "hazmat snapshots"
Enter
Sleep 3s
