// Package relay defines the wire protocol and Hub interface for the Huginn relay.
// Current status: scaffolded. InProcessHub is active (current behavior).
// WebSocketHub compiles but is only activated via `huginn relay start`.
package relay

// MessageType identifies the semantic type of a relay message.
type MessageType string

const (
	MsgToken          MessageType = "token"
	MsgToolCall       MessageType = "tool_call"
	MsgToolResult     MessageType = "tool_result"
	MsgPermissionReq  MessageType = "permission_request"
	MsgPermissionResp MessageType = "permission_response"
	MsgDone           MessageType = "done"
	MsgWarning        MessageType = "warning" // satellite → cloud: non-fatal warning during session
	MsgStatus         MessageType = "status"  // satellite → cloud: transient status (e.g. "Loading model…")

	// Phase 2 — Satellite → HuginnCloud push events
	// Emitted after every local session turn completes (WS chat, headless routine,
	// or relay-initiated). HuginnCloud uses this to deliver iOS/push notifications
	// so the user knows a session needs attention even when the browser is closed.
	MsgSessionDone MessageType = "session_done_notify"

	// Emitted when a new Inbox notification is stored in Pebble (routine/workflow
	// completion). HuginnCloud forwards this as an iOS badge + push notification.
	MsgNotificationSync       MessageType = "notification_sync"
	MsgNotificationUpdate     MessageType = "notification_update"
	MsgSatelliteHello         MessageType = "satellite_hello"
	MsgSatelliteHeartbeat     MessageType = "satellite_heartbeat"
	MsgSatelliteReconnect     MessageType = "satellite_reconnect"

	// Phase 2 — HuginnCloud → Satellite
	MsgNotificationActionRequest MessageType = "notification_action_request"
	MsgNotificationActionResult  MessageType = "notification_action_result"

	// Phase 3 — HuginnCloud → Satellite (remote agent execution)
	// HuginnCloud sends these to trigger local agent runs on behalf of a remote user.
	MsgRunAgent      MessageType = "run_agent"       // kick off an agent session remotely
	MsgCancelSession MessageType = "cancel_session"  // cancel an in-progress remote session
	MsgAgentResult   MessageType = "agent_result"    // satellite → cloud: outcome of a run_agent

	// Phase 3 — HuginnCloud → Satellite (chat / session management)
	MsgChatMessage         MessageType = "chat_message"          // cloud → satellite: user chat input
	MsgSessionStart        MessageType = "session_start"         // cloud → satellite: start a new session
	MsgSessionStartAck     MessageType = "session_start_ack"     // satellite → cloud: session created
	MsgSessionResume       MessageType = "session_resume"        // cloud → satellite: re-attach to interrupted session
	MsgSessionResumeAck    MessageType = "session_resume_ack"    // satellite → cloud: resume accepted
	MsgSessionListRequest  MessageType = "session_list_request"  // cloud → satellite: enumerate sessions
	MsgSessionListResult   MessageType = "session_list_result"   // satellite → cloud: session list response
	MsgModelListRequest    MessageType = "model_list_request"    // cloud → satellite: enumerate models
	MsgModelListResult     MessageType = "model_list_result"     // satellite → cloud: model list response

	// Generic HTTP proxy — cloud → satellite REST passthrough
	// Allows the browser app to call any satellite REST endpoint without
	// custom message types. request_id ties request to response.
	MsgHTTPRequest  MessageType = "http_request"
	MsgHTTPResponse MessageType = "http_response"

	// PTY shell — remote terminal over relay
	MsgShellStart  MessageType = "shell_start"
	MsgShellReady  MessageType = "shell_ready"
	MsgShellInput  MessageType = "shell_input"
	MsgShellOutput MessageType = "shell_output"
	MsgShellResize MessageType = "shell_resize"
	MsgShellExit   MessageType = "shell_exit"

	MsgShellEchoOff MessageType = "shell_echo_off"
	MsgShellEchoOn  MessageType = "shell_echo_on"

	// Phase 3 WebRTC stubs — protocol constants only, no implementation yet
	MsgWebRTCOffer  MessageType = "webrtc_offer"
	MsgWebRTCAnswer MessageType = "webrtc_answer"
	MsgWebRTCICE    MessageType = "webrtc_ice_candidate"

	// Model provider configuration — HuginnCloud ↔ Satellite
	MsgModelProviderListRequest MessageType = "model_provider_list_request"
	MsgModelProviderListResult  MessageType = "model_provider_list_result"
	MsgModelConfigGetRequest    MessageType = "model_config_get_request"
	MsgModelConfigGetResult     MessageType = "model_config_get_result"
	MsgModelConfigUpdateRequest MessageType = "model_config_update_request"
	MsgModelConfigUpdateResult  MessageType = "model_config_update_result"
	MsgModelPullRequest         MessageType = "model_pull_request"
	MsgModelPullResult          MessageType = "model_pull_result"

	// CRUD mutation events — satellite → cloud (triggers UI re-fetch)
	MsgAgentChanged    MessageType = "agent_changed"
	MsgSkillChanged    MessageType = "skill_changed"
	MsgWorkflowChanged MessageType = "workflow_changed"
	MsgConfigChanged   MessageType = "config_changed"
)

// Message is the envelope for all relay protocol messages.
type Message struct {
	Type      MessageType    `json:"type"`
	MachineID string         `json:"machine_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	SpaceID   string         `json:"space_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	// Priority controls delivery order in the durable outbox under backpressure.
	// Zero value (PriorityNormal) is the default for all existing messages.
	// The outbox key encodes (255 - Priority) so that higher Priority values
	// sort earlier in Pebble's lexicographic order:
	//   PriorityHigh (255) → key prefix \x00 → drains first
	//   PriorityNormal (0) → key prefix \xff → drains last
	Priority uint8 `json:"priority,omitempty"`
}

// Priority constants for Message.Priority.
// Zero value (PriorityNormal) is the default — existing code that doesn't
// set Priority behaves identically to before (normal-priority FIFO).
const (
	// PriorityNormal (0) is the default for all bulk / data-plane messages.
	PriorityNormal uint8 = 0
	// PriorityHigh (255) is used for control-plane messages that must drain
	// before normal traffic under backpressure (e.g. MsgAgentResult, MsgPing).
	PriorityHigh uint8 = 255
)

// Hub routes messages to remote machines.
type Hub interface {
	Send(machineID string, msg Message) error
	Close(machineID string)
}
