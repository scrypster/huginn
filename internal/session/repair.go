package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// repairJSONL scans the file and truncates at the last valid JSON line.
// Handles partial writes from crashes (SIGKILL mid-write).
func repairJSONL(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var validEnd int
	offset := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineLen := len(scanner.Bytes()) + 1 // +1 for newline
		if line == "" {
			offset += lineLen
			continue
		}
		if json.Valid([]byte(line)) {
			validEnd = offset + lineLen
		}
		offset += lineLen
	}

	if validEnd < len(data) {
		return os.Truncate(path, int64(validEnd))
	}
	return nil
}

// validateRepaired filters a slice of reconstructed/repaired messages, discarding
// entries that are structurally invalid (empty role, no content without a tool
// reference) while preserving cost records and tool messages.
func validateRepaired(msgs []SessionMessage) []SessionMessage {
	out := make([]SessionMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "" {
			continue
		}
		// Cost records pass through regardless of content.
		if m.Type == "cost" || m.Role == "cost" {
			out = append(out, m)
			continue
		}
		// Messages must have content OR a tool reference.
		if m.Content == "" && m.ToolName == "" && m.ToolCallID == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}
