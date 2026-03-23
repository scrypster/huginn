# Huginn Relay Protocol

This document specifies the wire protocol between a Huginn satellite (local
machine) and HuginnCloud over a persistent WebSocket connection.

---

## 1. Overview

A **satellite** is a registered Huginn instance running on a user's machine.
It maintains a single long-lived WebSocket connection to HuginnCloud. All
traffic flows satellite → cloud or cloud → satellite through this one
connection. HuginnCloud acts as a relay: it forwards commands, session
controls, and chat messages to the satellite, and routes results back to the
originating remote client.

```
Remote User ── HuginnCloud ──(WebSocket)── Satellite (local Huginn)
```

The satellite is always the dialing side. HuginnCloud never initiates a
connection to a satellite.

---

## 2. Connection Lifecycle

```
Satellite                                  HuginnCloud
   |                                            |
   |-- GET /satellite?machine_id=... HTTP/1.1 ->|
   |   Authorization: Bearer <JWT>              |
   |<-- 101 Switching Protocols ---------------|
   |                                            |
   |-- satellite_hello ------------------------>|
   |                                            |
   |   [readPump / writeLoop / pingLoop running]|
   |                                            |
   |<-- (cloud messages) ----------------------|
   |-- (satellite messages) ------------------>|
   |                                            |
   |   [connection drops]                       |
   |                                            |
   |   [exponential backoff: 1s → 30s]          |
   |-- GET /satellite?machine_id=... HTTP/1.1 ->|
   |   Authorization: Bearer <JWT>              |
   |<-- 101 Switching Protocols ---------------|
   |-- satellite_hello ------------------------>|
   |                                            |
```

After `Connect()` succeeds, three goroutines run concurrently on each
connection:

| Goroutine   | Role                                                          |
|-------------|---------------------------------------------------------------|
| `readPump`  | Reads inbound frames; dispatches to `onMessage`; triggers reconnect on error |
| `writeLoop` | Drains `writeCh` and calls `WriteMessage`; sole writer on conn |
| `pingLoop`  | Sends a WebSocket ping every `wsPingInterval` (30 s)          |

---

## 3. Message Envelope

Every message in both directions is a JSON text frame:

```json
{
  "type": "<MessageType>",
  "machine_id": "<string, omitempty>",
  "payload": { "<key>": "<value>" }
}
```

| Field        | Type   | Description                                                                 |
|--------------|--------|-----------------------------------------------------------------------------|
| `type`       | string | Message type constant (see §8)                                              |
| `machine_id` | string | Set by satellite in `satellite_hello`; set by cloud when routing to a specific satellite; omitted otherwise |
| `payload`    | object | Message-specific data (may be absent for messages with no fields)           |

---

## 4. `satellite_hello` (satellite → cloud)

Sent immediately after the WebSocket connection is established — including
after every reconnect — before any other message.

```json
{
  "type": "satellite_hello",
  "machine_id": "mymachine-a1b2c3d4",
  "payload": {
    "version": "0.2.0",
    "active_sessions": [
      { "id": "1710000000000000000-deadbeef", "status": "idle", "last_seq": 42 }
    ]
  }
}
```

| Payload field     | Type            | Description                                                   |
|-------------------|-----------------|---------------------------------------------------------------|
| `version`         | string          | Huginn binary version string                                  |
| `active_sessions` | array of objects | Sessions currently tracked in the local `SessionStore`. Omitted if the store is unavailable or empty. Each entry contains at minimum `id`, `status`, and `last_seq`. |

`machine_id` is the stable identifier produced by `relay.GetMachineID()`:
`<sanitized-hostname>-<8hex>`.

---

## 5. Reconnect and Exponential Backoff

When the connection drops (`readPump` receives an error), `reconnect()` is
called. It loops with exponential backoff until a new connection is established
or `Close()` is called.

**Backoff schedule** (`wsInitDelay = 1s`, `wsMaxDelay = 30s`):

| Attempt | Nominal wait | With 10% jitter (max) |
|---------|--------------|-----------------------|
| 1       | 1 s          | 1.1 s                 |
| 2       | 2 s          | 2.2 s                 |
| 3       | 4 s          | 4.4 s                 |
| 4       | 8 s          | 8.8 s                 |
| 5       | 16 s         | 17.6 s                |
| 6+      | 30 s (cap)   | 33 s                  |

Jitter is applied as a uniform random value in `[0, delay/10)` added to the
delay (positive-only, prevents thundering herd). On a successful reconnect
the delay resets to `wsInitDelay` and `satellite_hello` is sent automatically.

Reconnect is also triggered when a ping write fails (see §6).

The loop exits only when the `done` channel is closed (i.e. `Close()` was
called or the context was cancelled).

---

## 6. WebSocket Ping/Pong

The satellite sends a WebSocket-level ping every `wsPingInterval = 30 s` via
`pingLoop`. The server must respond with a pong within `wsPongTimeout = 10 s`.

**Pong handler behaviour:** on each received pong, the read deadline is reset
to `now + wsPingInterval + wsPongTimeout` (40 s). This keeps the read deadline
rolling forward as long as the server is alive.

**Initial read deadline:** set to `wsPingInterval + wsPongTimeout` (40 s)
immediately after dialing, before the first ping is sent.

**Ping failure:** if `WriteControl(PingMessage)` returns an error, `pingLoop`
closes the connection, which causes `readPump` to detect an error and begin
the reconnect sequence.

---

## 7. `satellite_heartbeat` (satellite → cloud)

Sent by `Heartbeater` on a fixed interval (default 60 s, configurable via
`HeartbeatConfig.Interval`). Used by HuginnCloud for satellite health
monitoring.

```json
{
  "type": "satellite_heartbeat",
  "machine_id": "mymachine-a1b2c3d4",
  "payload": {
    "machine_id": "mymachine-a1b2c3d4",
    "uptime_seconds": 3661,
    "os": "darwin",
    "arch": "arm64",
    "available_disk_gb": 128.4
  }
}
```

| Payload field       | Type   | Description                                                         |
|---------------------|--------|---------------------------------------------------------------------|
| `machine_id`        | string | Same as the envelope `machine_id`                                   |
| `uptime_seconds`    | int64  | Seconds since the relay process started                             |
| `os`                | string | `runtime.GOOS` (e.g. `"darwin"`, `"linux"`)                        |
| `arch`              | string | `runtime.GOARCH` (e.g. `"arm64"`, `"amd64"`)                       |
| `available_disk_gb` | float64 | Free disk space in GB for `~/.huginn`. Omitted if the check fails. |

---

## 8. `session_resume` / `session_resume_ack` Round Trip

When a remote client needs to re-attach to an interrupted session, HuginnCloud
sends `session_resume` to the satellite. The satellite looks up the session in
its local `SessionStore` and replies with `session_resume_ack`.

**Cloud → Satellite (`session_resume`):**

```json
{
  "type": "session_resume",
  "payload": {
    "session_id": "1710000000000000000-deadbeef"
  }
}
```

| Payload field | Type   | Required | Description                        |
|---------------|--------|----------|------------------------------------|
| `session_id`  | string | yes      | ID of the session to re-attach to  |

**Satellite → Cloud (`session_resume_ack`):**

```json
{
  "type": "session_resume_ack",
  "payload": {
    "session_id": "1710000000000000000-deadbeef",
    "status": "idle",
    "last_seq": 42
  }
}
```

| Payload field | Type   | Description                                             |
|---------------|--------|---------------------------------------------------------|
| `session_id`  | string | Echo of the requested session ID                        |
| `status`      | string | Current session status as stored in `SessionStore`      |
| `last_seq`    | int    | Last sequence number seen in this session               |

If `session_id` is missing from the payload, or the session is not found in
the `SessionStore`, the satellite logs a warning and sends no ack. HuginnCloud
may retry.

---

## 9. Message Type Reference

All `MessageType` constants defined in `internal/relay/relay.go`:

| Constant                    | Wire value                    | Direction          | Description                                              |
|-----------------------------|-------------------------------|--------------------|----------------------------------------------------------|
| `MsgToken`                  | `token`                       | satellite → cloud  | Streaming model output token                             |
| `MsgToolCall`               | `tool_call`                   | satellite → cloud  | Model requests a tool invocation                         |
| `MsgToolResult`             | `tool_result`                 | satellite → cloud  | Result of a tool invocation                              |
| `MsgPermissionReq`          | `permission_request`          | satellite → cloud  | Request remote user approval for a tool                  |
| `MsgPermissionResp`         | `permission_response`         | cloud → satellite  | Approval or denial for a pending permission request      |
| `MsgDone`                   | `done`                        | satellite → cloud  | Agentic loop turn complete                               |
| `MsgSessionDone`            | `session_done_notify`         | satellite → cloud  | Session turn done; triggers iOS push notification        |
| `MsgNotificationSync`       | `notification_sync`           | satellite → cloud  | Full notification sync                                   |
| `MsgNotificationUpdate`     | `notification_update`         | satellite → cloud  | New inbox notification stored; triggers push badge       |
| `MsgSatelliteHello`         | `satellite_hello`             | satellite → cloud  | Initial handshake after connect/reconnect (see §4)       |
| `MsgSatelliteHeartbeat`     | `satellite_heartbeat`         | satellite → cloud  | Periodic health report (see §7)                          |
| `MsgSatelliteReconnect`     | `satellite_reconnect`         | satellite → cloud  | Satellite-initiated reconnect notification               |
| `MsgNotificationActionRequest` | `notification_action_request` | cloud → satellite | Cloud requests action on a notification                 |
| `MsgNotificationActionResult`  | `notification_action_result`  | satellite → cloud | Result of a notification action                         |
| `MsgRunAgent`               | `run_agent`                   | cloud → satellite  | Kick off a remote agent session (Phase 3)                |
| `MsgCancelSession`          | `cancel_session`              | cloud → satellite  | Cancel an in-progress remote session (Phase 3)           |
| `MsgAgentResult`            | `agent_result`                | satellite → cloud  | Outcome of a `run_agent` execution (Phase 3)             |
| `MsgChatMessage`            | `chat_message`                | cloud → satellite  | User chat input for a remote session (Phase 3)           |
| `MsgSessionStart`           | `session_start`               | cloud → satellite  | Start a new session remotely (Phase 3)                   |
| `MsgSessionResume`          | `session_resume`              | cloud → satellite  | Re-attach to an interrupted session (see §8)             |
| `MsgSessionResumeAck`       | `session_resume_ack`          | satellite → cloud  | Resume accepted; returns session metadata (see §8)       |
| `MsgSessionListRequest`     | `session_list_request`        | cloud → satellite  | Enumerate sessions on satellite (Phase 3)                |
| `MsgSessionListResult`      | `session_list_result`         | satellite → cloud  | Session list response (Phase 3)                          |
| `MsgModelListRequest`       | `model_list_request`          | cloud → satellite  | Enumerate configured models (Phase 3)                    |
| `MsgModelListResult`        | `model_list_result`           | satellite → cloud  | Model list response (Phase 3)                            |

Phase 3 messages are defined and parsed but their handler logic is not yet
implemented in the dispatcher. The dispatcher logs receipt and takes no action.

---

## 10. Write Buffer

`WebSocketHub` uses a single `writeCh` channel (buffered, size `wsWriteBufSize = 256`)
shared across reconnections. All callers serialize through `Send()`.

| Error                | Meaning                                                                  |
|----------------------|--------------------------------------------------------------------------|
| `ErrNotActivated`    | `Send()` called before `Connect()`, or after `Close()` (`conn == nil`)  |
| `ErrWriteBufferFull` | The 256-slot channel is full; the message is dropped and the error returned. Indicates heavy backpressure; messages should be logged and discarded by the caller. |

Because `writeCh` is allocated once at hub construction and outlives individual
connections, messages enqueued before a reconnect are delivered to the next
connection's `writeLoop` without loss.

---

## 11. Authentication

The satellite presents a JWT machine token in the HTTP `Authorization` header
during the WebSocket upgrade:

```
GET /satellite?machine_id=mymachine-a1b2c3d4 HTTP/1.1
Host: relay.huginncloud.com
Upgrade: websocket
Authorization: Bearer <JWT>
```

The JWT is stored in the OS keyring (`huginn` service, `relay/machine-token`
key). HuginnCloud rejects connections with a missing or invalid token with
`HTTP 401`.

---

## 12. Error Handling

| Condition                   | Behaviour                                                         |
|-----------------------------|-------------------------------------------------------------------|
| Read error on conn          | `readPump` triggers `reconnect()`                                 |
| Write error in `writeLoop`  | `writeLoop` exits; `readPump` detects dead conn and reconnects    |
| Ping write failure          | `pingLoop` closes conn; `readPump` detects and reconnects         |
| JSON parse error (inbound)  | Message silently dropped; connection kept open                    |
| Unknown `type` value        | Passed to `onMessage` unchanged; callers should ignore for forward compatibility |
| Wrong `machine_id` on msg   | Dispatcher drops with warning log                                 |
| `session_resume` unknown ID | Dispatcher logs warning; no ack sent; cloud may retry             |
