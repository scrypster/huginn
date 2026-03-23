package memory

// RegisterMemoryCmds previously registered the `memory` subcommand tree (status,
// auth, list, search, forget, import, export) backed by the Go SDK MemoryClient.
// Those commands have been removed as part of the MCP migration (Task 5).
// MuninnDB is now accessed exclusively via the per-session MCP connection.
