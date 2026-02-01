# VHS Demo Tape for Claude Code Orchestrator
# Run with: vhs docs/demo.tape

Output docs/images/demo.gif

Set Shell "zsh"
Set FontSize 15
Set Width 900
Set Height 550
Set Theme "Catppuccin Mocha"
Set Padding 25
Set TypingSpeed 50ms
Set CursorBlink false

# CLI overview
Sleep 300ms
Type "./bin/orchestrator --help"
Enter
Sleep 3s

Ctrl+L
Sleep 300ms

# Show task file
Type "cat examples/smoke-test-discovery.yaml"
Enter
Sleep 4s

Ctrl+L
Sleep 300ms

# Run the workflow
Type "./bin/orchestrator run -f examples/smoke-test-discovery.yaml"
Enter
Sleep 2s

Ctrl+L
Sleep 300ms

# Check status
Type "./bin/orchestrator status --workflow-id transform-smoke-test-discovery"
Enter
Sleep 2s

# Hide recording while we wait for workflow to complete
Hide
Sleep 35s
Show

Ctrl+L
Sleep 300ms

# Get the report
Type "./bin/orchestrator reports transform-smoke-test-discovery -o json"
Enter
Sleep 3s

Sleep 2s
