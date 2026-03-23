package tui

// appState tracks the modal / overlay state of the chat screen.
// It is distinct from appScreen, which tracks top-level navigation.
type appState int

const (
	stateChat        appState = iota
	stateWizard
	stateFilePicker
	stateStreaming
	statePermAwait        // waiting for user to allow/deny a tool permission request
	stateWriteAwait       // waiting for user to allow/deny a file write
	stateSessionPicker    // session resume picker overlay
	stateSwarm            // showing swarm agent progress view
	stateAgentWizard      // agent creation wizard overlay
	stateArtifactView     // full-screen artifact overlay (ctrl+o on artifact line)
	stateThreadOverlay    // full-screen thread overlay (ctrl+t)
	stateObservationDeck  // narrated walkthrough of a thread (ctrl+e)
)

// Layout constants — every line must be accounted for.
const (
	dividerLines  = 1 // separator above input
	inputLines    = 3 // bordered input box (border top + content + border bottom)
	footerLines   = 2 // two-row footer: auto-run + model info
	reservedLines = dividerLines + inputLines + footerLines
	wizardLines   = 8 // wizard: 1 hint + up to 6 commands + padding

	// Maximum lines of tool output to show before truncating.
	toolOutputMaxLines = 8
)
