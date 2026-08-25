// Package claudecode bridges Claude Code terminal sessions into Huginn.
//
// Claude Code appends each session to a JSONL transcript under
// ~/.claude/projects/<slugified-cwd>/<session-uuid>.jsonl. This package
// parses those transcripts, maps them onto Huginn session messages, and
// tails them live.
package claudecode

import "encoding/json"

// Line is one parsed transcript line. Only fields the bridge consumes are
// modelled; Claude Code adds fields freely and unknown ones are ignored.
type Line struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	ParentUUID    string          `json:"parentUuid"`
	SessionID     string          `json:"sessionId"`
	CWD           string          `json:"cwd"`
	Timestamp     string          `json:"timestamp"`
	GitBranch     string          `json:"gitBranch"`
	CustomTitle   string          `json:"customTitle"`
	IsSidechain   bool            `json:"isSidechain"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

// contentTypes is an allowlist. Claude Code interleaves many metadata line
// types with conversation content, and new ones appear across versions.
// Allowlisting means an unrecognised future type is dropped rather than
// leaking into the timeline as a junk message.
var contentTypes = map[string]bool{
	"user":      true,
	"assistant": true,
}

// IsContentType reports whether a transcript line type carries conversation
// content the bridge should ingest.
func IsContentType(t string) bool { return contentTypes[t] }

// ParseLine parses one transcript line. ok is false when the line is not
// valid JSON or is not a content-bearing type; callers skip those.
// A parse failure is never fatal to the rest of the file.
func ParseLine(b []byte) (Line, bool) {
	var l Line
	if len(b) == 0 {
		return Line{}, false
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return Line{}, false
	}
	if !IsContentType(l.Type) {
		return Line{}, false
	}
	return l, true
}

// ParseTitle extracts a custom title from a line if it carries one. It is
// separate from ParseLine because "custom-title" is a metadata type that
// ParseLine deliberately rejects, but whose payload the ingester still wants.
func ParseTitle(b []byte) (string, bool) {
	var l Line
	if err := json.Unmarshal(b, &l); err != nil {
		return "", false
	}
	if l.Type != "custom-title" || l.CustomTitle == "" {
		return "", false
	}
	return l.CustomTitle, true
}
