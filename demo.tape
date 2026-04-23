# NEXUS TUI Demo
# Generates demo.gif — runs automatically via GitHub Actions on push to main.
# Manual: vhs demo.tape (requires VHS, Go 1.25+, nexus binary in PATH)

Require nexus

Output demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 800
Set Height 500
Set Padding 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

Type "nexus"
Enter
Sleep 3s

# Navigate down through all menu items
Type "j"
Sleep 600ms
Type "j"
Sleep 600ms
Type "j"
Sleep 600ms

# Navigate back up
Type "k"
Sleep 600ms
Type "k"
Sleep 600ms
Type "k"
Sleep 800ms

# Open Configure screen
Type "j"
Sleep 400ms
Enter
Sleep 2s

# Navigate config items
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms

# Back to menu
Escape
Sleep 1.5s

# Open Health Check
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Enter
Sleep 3s

# Back to menu
Escape
Sleep 1s

# Quit
Type "q"
Sleep 1s
