package lock

import "context"

// SessionLockEvent represents a change in the user session lock status.
type SessionLockEvent struct {
	Locked bool `json:"locked"`
}

// LockManager defines the contract for monitoring session lock/unlock events.
type LockManager interface {
	// Start begins monitoring the session lock status and streams state updates.
	Start(ctx context.Context) (<-chan SessionLockEvent, error)
}
