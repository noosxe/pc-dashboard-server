package dpms

import (
	"context"
	"log/slog"
	"time"
)

// MockDpmsManager generates simulated display power events on a periodic ticker.
type MockDpmsManager struct {
	logger *slog.Logger
}

// NewMockDpmsManager creates a simulation driver for display power status.
func NewMockDpmsManager(logger *slog.Logger) *MockDpmsManager {
	return &MockDpmsManager{logger: logger}
}

// Start begins a mock status toggling loop emitting DPMS events.
func (m *MockDpmsManager) Start(ctx context.Context) (<-chan DpmsEvent, error) {
	out := make(chan DpmsEvent, 10)
	m.logger.Info("Mock display power (DPMS) monitoring successfully initialized")

	go func() {
		defer close(out)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		state := "on"

		// Send initial DPMS event for testing feedback after a brief delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
			state = "off"
			m.logger.Info("Mock DPMS event generated", "state", state)
			out <- DpmsEvent{State: state}
		}

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopping mock DPMS generator")
				return
			case <-ticker.C:
				if state == "on" {
					state = "off"
				} else {
					state = "on"
				}
				m.logger.Info("Mock DPMS event generated", "state", state)
				out <- DpmsEvent{State: state}
			}
		}
	}()

	return out, nil
}
