package conntools

import (
	"log/slog"

	"github.com/scrypster/huginn/internal/tools"
)

// strictInject registers a connection tool using StrictRegister and logs a
// warning if a name collision occurs. All Inject* functions use this helper
// so that shadowing of built-in tools is surfaced rather than silently overwriting.
func strictInject(reg *tools.Registry, t tools.Tool) {
	if err := reg.StrictRegister(t); err != nil {
		slog.Warn("connections: tool registration conflict", "tool", t.Name(), "err", err)
	}
}
