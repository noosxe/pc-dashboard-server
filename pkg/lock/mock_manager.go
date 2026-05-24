package lock

import (
	"context"
	"log/slog"
	"time"
)

// MockLockManager generates simulated session lock/unlock events on a periodic ticker.
type MockLockManager struct {
	logger *slog.Logger
}

// NewMockLockManager creates a simulation driver for session lock status.
func NewMockLockManager(logger *slog.Logger) *MockLockManager {
	return &MockLockManager{logger: logger}
}

// Start begins a mock status toggling loop emitting lock events.
func (m *MockLockManager) Start(ctx context.Context) (<-chan SessionLockEvent, error) {
	out := make(chan SessionLockEvent, 10)
	m.logger.Info("Mock session lock monitoring successfully initialized")

	go func() {
		defer close(out)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		locked := false

		// Send initial unlocked event for testing feedback after a brief delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			locked = true
			m.logger.Info("Mock session lock event generated", "locked", locked)
			out <- SessionLockEvent{Locked: locked}
		}

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopping mock session lock generator")
				return
			case <-ticker.C:
				locked = !locked
				m.logger.Info("Mock session lock event generated", "locked", locked)
				out <- SessionLockEvent{Locked: locked}
			}
		}
	}()

	return out, nil
}
