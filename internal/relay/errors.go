package relay

import "errors"

// ErrOutboxNearFull is returned by Enqueue when the outbox exceeds the reject
// threshold (90% capacity). The message was not enqueued. Callers should treat
// this as an early-warning signal and may log or surface the pressure upstream.
var ErrOutboxNearFull = errors.New("relay: outbox near full")

// ErrOutboxFull is returned by Enqueue when the outbox is at maximum capacity.
// The message was not enqueued. This is a hard limit; callers must discard.
var ErrOutboxFull = errors.New("relay: outbox full")
