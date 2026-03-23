# ADR-003: WebSocket Relay Protocol for Remote Agent Control

**Date:** 2026-03-03
**Status:** Accepted

## Context

Huginn's long-term roadmap includes a mobile app where users tap "New Chat" to open a session on their local machine. Multiple machines are supported. The transport must handle streaming tokens, tool call events, permission prompts, and bidirectional control signals between a mobile client and local Huginn instances.

## Decision

Huginn will use a WebSocket relay protocol. The relay hub runs on AWS (API Gateway WebSocket or a thin Go binary on ECS). Each message is a JSON envelope with a `type` field:

- `token` — streamed response token from the agent
- `tool_call` — agent is about to execute a tool (name + args)
- `tool_result` — tool execution complete (name + result)
- `permission_request` — agent needs user approval for a PermWrite/PermExec tool
- `permission_response` — user grants or denies the request
- `done` — agent turn complete

Machine-to-relay auth: API key per machine stored in `~/.huginn/relay.json`. Mobile-to-relay auth: JWT. Machines are identified by `MachineID` (ADR-004).

## Consequences

- Streaming token delivery is natural over WebSocket (no polling).
- Permission flows are fully asynchronous and decoupled from HTTP request-response.
- WebSocket is native on iOS (URLSessionWebSocketTask) and Android (OkHttp).
- Phase 1 (current): relay is not implemented; all agents run in-process.
- Phase 2: relay binary is a separate deployable; Huginn CLI gains `huginn relay register` command.
