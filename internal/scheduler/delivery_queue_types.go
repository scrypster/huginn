// internal/scheduler/delivery_queue_types.go
package scheduler

import (
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/notification"
)

// DeliveryQueuePayload is the JSON blob stored in delivery_queue.payload.
// It captures everything needed to re-attempt a delivery without re-running
// the workflow.
type DeliveryQueuePayload struct {
	Notification notification.Notification `json:"notification"`
	Target       NotificationDelivery      `json:"target"`
	WorkflowName string                    `json:"workflow_name,omitempty"`
}

// DeliveryQueueEntry is one row in the delivery_queue table.
type DeliveryQueueEntry struct {
	ID            string     `json:"id"`
	WorkflowID    string     `json:"workflow_id"`
	RunID         string     `json:"run_id"`
	Endpoint      string     `json:"endpoint"`
	Channel       string     `json:"channel"`        // "webhook" | "email"
	Payload       string     `json:"payload"`        // JSON-encoded DeliveryQueuePayload
	Status        string     `json:"status"`         // pending|retrying|delivered|failed|superseded
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	RetryWindowS  int        `json:"retry_window_s"`
	NextRetryAt   time.Time  `json:"next_retry_at"`
	CreatedAt     time.Time  `json:"created_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// EndpointHealth is one row in the endpoint_health table.
// Scoped per (WorkflowID, Endpoint) — isolated per workflow.
type EndpointHealth struct {
	WorkflowID          string     `json:"workflow_id"`
	Endpoint            string     `json:"endpoint"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CircuitState        string     `json:"circuit_state"` // "closed" | "open"
	OpenedAt            *time.Time `json:"opened_at,omitempty"`
	LastProbeAt         *time.Time `json:"last_probe_at,omitempty"`
}

const (
	circuitBreakThreshold = 5 // open circuit after this many consecutive failures
)

// endpointKey returns a stable, credential-free string that uniquely identifies
// a delivery target for circuit-breaker and dedup keying.
func endpointKey(target NotificationDelivery) string {
	switch target.Type {
	case "webhook":
		return target.To
	case "email":
		if target.Connection != "" {
			return fmt.Sprintf("smtp-connection://%s", target.Connection)
		}
		return fmt.Sprintf("smtp://%s@%s", target.SMTPUser, target.SMTPHost)
	default:
		return fmt.Sprintf("%s:%s", target.Type, target.To)
	}
}
