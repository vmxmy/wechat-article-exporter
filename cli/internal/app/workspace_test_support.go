package app

// ForceWorkspaceForTesting bypasses the entry-point TTY probe for native PTY
// acceptance harnesses. Bubble Tea still reads and writes through the supplied
// pseudo-terminal streams, so navigation, resize handling, rendering, and
// clean shutdown exercise the production workspace.
func (a *App) ForceWorkspaceForTesting() {
	if a != nil {
		a.forceWorkspace = true
	}
}
