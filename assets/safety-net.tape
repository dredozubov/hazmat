# Hazmat safety net GIF: snapshot, diff, restore
#
# Shows the "undo button" — autonomous mode is low-stakes because
# you can always roll back.
#
# Prerequisites: hazmat init completed, a project with recent hazmat session

Output assets/safety-net.gif
Output assets/safety-net.webm

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

Hide
Type "cd ~/workspace/demo-project && clear"
Enter
Sleep 1s
Show

# Scene 1: Run something in containment that modifies files
Type "hazmat exec -- sh -c 'echo \"// refactored\" >> main.go'"
Enter
Sleep 2s

# Scene 2: See what the agent changed
Type "hazmat diff"
Enter
Sleep 3s

# Scene 3: Don't like it? Restore.
Type "hazmat restore"
Enter
Sleep 2s

# Scene 4: Verify — diff is clean
Type "hazmat diff"
Enter
Sleep 2s
