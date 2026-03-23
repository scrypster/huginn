package server

// Local WebSocket event types — used in WSMessage.Type for messages pushed from
// the satellite to connected browser clients over ws://localhost.
//
// These are distinct from relay.Msg* constants, which are the satellite→cloud
// protocol types used in relay.Message.Type.
const (
	WSEventNotificationNew    = "notification_new"
	WSEventNotificationUpdate = "notification_update"
	WSEventInboxBadge         = "inbox_badge"
)
