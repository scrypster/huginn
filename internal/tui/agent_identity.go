package tui

import (
	"hash/fnv"
	"strings"
)

// agentPalette is a fixed set of terminal-friendly colors used to assign a
// deterministic color to each agent name.
var agentPalette = []string{
	"#58A6FF", // blue
	"#3FB950", // green
	"#FF7B72", // red/coral
	"#D2A8FF", // lavender
	"#FFA657", // orange
	"#79C0FF", // sky blue
}

// agentColorFromName returns a deterministic palette color for the given agent
// name by hashing it with FNV-32a.
func agentColorFromName(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return agentPalette[int(h.Sum32())%len(agentPalette)]
}

// agentIconFromName returns a single upper-case character derived from the
// first rune of the agent name, used as the avatar icon in the chat header.
func agentIconFromName(name string) string {
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}
