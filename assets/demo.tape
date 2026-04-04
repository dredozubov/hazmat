# Hazmat demo: containment in action
# Prerequisites: hazmat init already run, ~/.ssh/id_rsa exists on host user

Output assets/demo.gif
Output assets/demo.webm

Require hazmat

Source assets/config.tape
Set TypingSpeed 60ms

# Scene 1: Enter containment
Type "cd ~/workspace/demo-project"
Enter
Sleep 500ms

Type "hazmat shell"
Enter
Sleep 1.5s

# Scene 2: Try to read credentials — seatbelt denies
Type "cat ~/.ssh/id_rsa"
Enter
Sleep 1.5s

Type "cat ~/.aws/credentials"
Enter
Sleep 1.5s

# Scene 3: Try exfiltration — pf firewall blocks
Type "curl -s --connect-timeout 2 smtp://mail.example.com"
Enter
Sleep 3s

# Scene 4: Normal work still works
Type "echo 'hello from containment' > test.txt && cat test.txt"
Enter
Sleep 1.5s

Type "exit"
Enter
Sleep 1s

# Scene 5: Status check — all green
Type "hazmat status"
Enter
Sleep 3s
