package backend

import "context"

// ManagedBackend is a Backend backed by a huginn-managed llama-server subprocess.
// It embeds ExternalBackend pointed at the subprocess's local endpoint.
type ManagedBackend struct {
	ExternalBackend
	shutdown func(ctx context.Context) error
}

// NewManagedBackend creates a ManagedBackend wrapping endpoint, with a shutdown hook.
func NewManagedBackend(endpoint string, shutdownFn func(context.Context) error) *ManagedBackend {
	return &ManagedBackend{
		ExternalBackend: *NewExternalBackend(endpoint),
		shutdown:        shutdownFn,
	}
}

// Shutdown calls the provided shutdown function to stop llama-server.
func (b *ManagedBackend) Shutdown(ctx context.Context) error {
	if b.shutdown != nil {
		return b.shutdown(ctx)
	}
	return nil
}
