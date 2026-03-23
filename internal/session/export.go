package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExportMarkdown converts session messages to a readable Markdown format.
func ExportMarkdown(msgs []SessionMessage, title string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(fmt.Sprintf("_Exported %s_\n\n---\n\n", time.Now().Format("2006-01-02 15:04")))

	for _, m := range msgs {
		switch m.Role {
		case "user":
			sb.WriteString("**User**\n\n")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n---\n\n")
		case "assistant":
			sb.WriteString("**Assistant**\n\n")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n---\n\n")
		}
	}
	return sb.String()
}

// ExportJSON converts session messages to a JSON array.
func ExportJSON(msgs []SessionMessage) (string, error) {
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
