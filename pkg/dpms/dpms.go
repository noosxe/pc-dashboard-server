package dpms

import "context"

// DpmsEvent represents a display power state change.
type DpmsEvent struct {
	State string `json:"state"` // "on" or "off"
}

// DpmsManager defines the contract for monitoring display power (DPMS) events.
type DpmsManager interface {
	// Start begins monitoring the display power status and streams state updates.
	Start(ctx context.Context) (<-chan DpmsEvent, error)
}
