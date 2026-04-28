// internal/agents/heartbeat_yaml.go
package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

const managedHeader = "# MANAGED BY HUGINN"

const defaultHeartbeatCron = "0 */4 * * *"

// HeartbeatYAMLPath returns the path to the heartbeat workflow file for an agent.
// Exported for use in tests and the server layer.
func HeartbeatYAMLPath(baseDir, agentName string) string {
	return filepath.Join(baseDir, "workflows", "heartbeat-"+sanitizeAgentName(agentName)+".yaml")
}

// IsManaged returns true if the file at path begins with the managed header comment.
// Returns false for nonexistent files.
func IsManaged(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(managedHeader))
	n, _ := f.Read(buf)
	return string(buf[:n]) == managedHeader
}

// generateHeartbeatYAML produces the YAML content for a heartbeat workflow file.
func generateHeartbeatYAML(name string, cron string, enabled bool) string {
	safeName := sanitizeAgentName(name)
	return fmt.Sprintf(
		"# MANAGED BY HUGINN — changes to cron/enabled will be overwritten by the UI.\n"+
			"# To customize fully: copy to a new filename (e.g. my-%s-heartbeat.yaml) and Huginn will stop managing it.\n"+
			"name: \"Heartbeat: %s\"\n"+
			"description: \"Auto-generated heartbeat for %s\"\n"+
			"enabled: %v\n"+
			"schedule: \"%s\"\n"+
			"notification:\n"+
			"  on_success: true\n"+
			"  on_failure: true\n"+
			"  severity: info\n"+
			"  deliver_to:\n"+
			"    - type: agent_dm\n"+
			"      from: \"%s\"\n"+
			"steps:\n"+
			"  - name: \"Check in\"\n"+
			"    agent: \"%s\"\n"+
			"    prompt: |\n"+
			"      You are checking in with your user. Use your tools and memory to assess whether\n"+
			"      anything warrants their attention right now.\n"+
			"\n"+
			"      Respond as you would in a direct message to a colleague — conversational, direct, 2-4 sentences.\n"+
			"      Do not use bullet points, markdown tables, headers, or report formatting.\n"+
			"      Do not say \"Heartbeat:\" or \"Status update:\" or anything that sounds like a log entry.\n"+
			"      If there is nothing to report, say so in one sentence and stop.\n"+
			"\n"+
			"      Good: \"Nothing unusual in your repos today. The PR you opened yesterday is still waiting on review.\"\n"+
			"      Bad: \"**Heartbeat Report**\\n- Repos: OK\\n- PRs: 1 open\\n- Actions: None required\"\n"+
			"    position: 0\n"+
			"    on_failure: stop\n",
		safeName, name, name, enabled, cron, name, name,
	)
}

func writeHeartbeatYAML(baseDir string, def AgentDef) error {
	workflowsDir := filepath.Join(baseDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o750); err != nil {
		return fmt.Errorf("mkdir workflows: %w", err)
	}
	cron := def.HeartbeatCron
	if cron == "" {
		cron = defaultHeartbeatCron
	}
	content := generateHeartbeatYAML(def.Name, cron, def.HeartbeatEnabled)
	return os.WriteFile(HeartbeatYAMLPath(baseDir, def.Name), []byte(content), 0o600)
}

// SyncHeartbeatYAMLDefault ensures the heartbeat YAML file matches the agent's config:
//   - HeartbeatEnabled=true:  creates/updates the file with enabled=true
//   - HeartbeatEnabled=false, managed file exists: updates file with enabled=false (preserves cron on re-enable)
//   - HeartbeatEnabled=false, no file: no-op (avoids creating disabled skeleton files)
func SyncHeartbeatYAMLDefault(def AgentDef) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	path := HeartbeatYAMLPath(baseDir, def.Name)
	if def.HeartbeatEnabled || IsManaged(path) {
		return writeHeartbeatYAML(baseDir, def)
	}
	return nil
}

// DeleteHeartbeatYAMLDefault removes the managed heartbeat workflow for an agent.
// No-op if the file does not exist or is not managed (user-customized files are not touched).
func DeleteHeartbeatYAMLDefault(agentName string) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	path := HeartbeatYAMLPath(baseDir, agentName)
	if !IsManaged(path) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RenameHeartbeatYAMLDefault handles heartbeat file lifecycle when an agent is renamed.
// Removes the old managed file and writes a new file (if enabled).
func RenameHeartbeatYAMLDefault(oldName string, newDef AgentDef) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	// Remove old managed file. Best effort: if the remove fails, the new file is still
	// written successfully. Surfacing this error would leave the rename half-done with
	// no clear recovery path, so we intentionally discard it (unlike DeleteHeartbeatYAMLDefault
	// which operates standalone and can safely propagate errors).
	oldPath := HeartbeatYAMLPath(baseDir, oldName)
	if IsManaged(oldPath) {
		_ = os.Remove(oldPath)
	}
	// Write new file only if heartbeat is enabled
	if newDef.HeartbeatEnabled {
		return writeHeartbeatYAML(baseDir, newDef)
	}
	return nil
}

// HeartbeatCronOrDefault returns the cron string from the def, or the default if empty.
// Exported for use in the web layer when displaying next-run time.
func HeartbeatCronOrDefault(def AgentDef) string {
	if def.HeartbeatCron != "" {
		return def.HeartbeatCron
	}
	return defaultHeartbeatCron
}
