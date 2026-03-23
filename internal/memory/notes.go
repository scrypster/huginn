package memory

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	notesMaxWarningLines = 100
	dirPerm              = 0o755 // Directory permission
	filePerm             = 0o644 // Regular file permission
)

// notesFilePath returns the path to an agent's context notes file.
// Convention: ~/.huginn/agents/{name}.memory.md
func notesFilePath(huginnHome, agentName string) string {
	return filepath.Join(huginnHome, "agents", agentName+".memory.md")
}

// seedNotes is the initial content written when the file is first created.
const seedNotes = `# Agent Memory

<!-- This file is read at the start of every conversation and can be updated by the agent. -->
<!-- You can edit it directly. Keep it under 100 lines. -->
`

// ReadNotes returns the content of the agent's memory file.
// Returns ("", nil) if the file does not exist.
func ReadNotes(huginnHome, agentName string) (string, error) {
	p := notesFilePath(huginnHome, agentName)
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("notes: read %q: %w", p, err)
	}
	return string(data), nil
}

// EnsureNotes creates the memory file with seed content if it doesn't exist.
func EnsureNotes(huginnHome, agentName string) error {
	p := notesFilePath(huginnHome, agentName)
	if err := os.MkdirAll(filepath.Dir(p), dirPerm); err != nil {
		return err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return os.WriteFile(p, []byte(seedNotes), filePerm)
	}
	return nil
}

// AppendNotes appends content to the agent's memory file, creating it if needed.
func AppendNotes(huginnHome, agentName, content string) error {
	p := notesFilePath(huginnHome, agentName)
	if err := os.MkdirAll(filepath.Dir(p), dirPerm); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("notes: open %q: %w", p, err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n%s", content)
	return err
}

// RewriteNotes replaces the entire memory file with new content.
func RewriteNotes(huginnHome, agentName, content string) error {
	p := notesFilePath(huginnHome, agentName)
	if err := os.MkdirAll(filepath.Dir(p), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), filePerm)
}

// NotesStats returns stats about the memory file.
type NotesStats struct {
	Exists bool
	Lines  int
	Bytes  int
	Path   string
}

// StatsNotes returns stats for the agent's memory file.
func StatsNotes(huginnHome, agentName string) NotesStats {
	p := notesFilePath(huginnHome, agentName)
	info, err := os.Stat(p)
	if err != nil {
		return NotesStats{Exists: false, Path: p}
	}
	// Count lines
	f, err := os.Open(p)
	lines := 0
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines++
		}
		f.Close()
	}
	return NotesStats{
		Exists: true,
		Lines:  lines,
		Bytes:  int(info.Size()),
		Path:   p,
	}
}

// NotesPromptBlock returns the system prompt block to inject when context notes are enabled.
// Returns "" if the file is empty or doesn't exist.
func NotesPromptBlock(huginnHome, agentName string) string {
	content, err := ReadNotes(huginnHome, agentName)
	if err != nil || strings.TrimSpace(content) == "" {
		return ""
	}
	lines := strings.Count(content, "\n")
	warning := ""
	if lines > notesMaxWarningLines {
		warning = fmt.Sprintf("\n\nWARNING: Your memory file is %d lines long. Use update_memory with action \"rewrite\" to consolidate and remove stale entries. Target under 100 lines.", lines)
	}
	return fmt.Sprintf("<context-notes>\n%s\n</context-notes>\n\nThe above are your persistent notes from previous conversations. Reference them when relevant. Update them using the update_memory tool when you learn something worth remembering.%s", strings.TrimSpace(content), warning)
}
