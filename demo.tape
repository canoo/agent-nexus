# NEXUS TUI Demo
# Run: vhs demo.tape

Output demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 900
Set Height 600
Set Padding 20
Set Theme "Catppuccin Mocha"

# Build and launch
Type "cd tools/tui && go build -o nexus . && ./nexus"
Enter
Sleep 2s

# Show menu
Sleep 1s

# Navigate to Configure
Type "j"
Sleep 500ms
Enter
Sleep 1s

# Show configure screen
Sleep 1.5s

# Go back
Escape
Sleep 1s

# Navigate to Health Check
Type "jj"
Sleep 500ms
Enter
Sleep 2s

# Go back
Escape
Sleep 1s

# Show Install option
Enter
Sleep 2s

# Go back and quit
Escape
Sleep 500ms
Type "q"
Sleep 500ms
