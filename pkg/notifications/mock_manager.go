package notifications

import (
	"context"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"
)

// MockNotificationManager generates simulated system events and tracks mock publication commands.
type MockNotificationManager struct {
	logger *slog.Logger
	nextID uint32
}

// NewMockNotificationManager creates a simulation driver for notifications.
func NewMockNotificationManager(logger *slog.Logger) *MockNotificationManager {
	return &MockNotificationManager{
		logger: logger,
		nextID: 100,
	}
}

// Start spawns a background tick generator emitting mock notification events every 8 seconds.
func (m *MockNotificationManager) Start(ctx context.Context) (<-chan NotificationEvent, error) {
	out := make(chan NotificationEvent, 10)
	m.logger.Info("Mock notification monitoring successfully initialized")

	mockApps := []string{"Slack", "Discord", "Thunderbird", "Spotify", "Systemd"}
	mockSummaries := []string{
		"New direct message",
		"Server Alert",
		"Update available",
		"Now playing",
		"Battery low",
	}
	mockBodies := []string{
		"Alice: Let's sync up later today.",
		"High CPU load detected on host server.",
		"Version 1.26.4 is ready for download.",
		"Stayin' Alive - Bee Gees",
		"Battery is currently at 12%. Connect charger.",
	}

	go func() {
		defer close(out)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()

		// Send initial event quickly for immediate testing feedback
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			idx := rand.Intn(len(mockApps))
			out <- NotificationEvent{
				AppName:       mockApps[idx],
				ReplacesID:    0,
				AppIcon:       "dialog-information",
				Summary:       mockSummaries[idx],
				Body:          mockBodies[idx],
				Actions:       []string{"default", "Dismiss"},
				Hints:         map[string]interface{}{"urgency": 1},
				ExpireTimeout: 5000,
			}
		}

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopping mock notification generator")
				return
			case <-ticker.C:
				idx := rand.Intn(len(mockApps))
				out <- NotificationEvent{
					AppName:       mockApps[idx],
					ReplacesID:    0,
					AppIcon:       "dialog-information",
					Summary:       mockSummaries[idx],
					Body:          mockBodies[idx],
					Actions:       []string{"default", "Dismiss"},
					Hints:         map[string]interface{}{"urgency": 1},
					ExpireTimeout: 5000,
				}
			}
		}
	}()

	return out, nil
}

// SendNotification logs the simulated trigger and returns a unique simulated notification ID.
func (m *MockNotificationManager) SendNotification(ctx context.Context, req NotificationRequest) (uint32, error) {
	id := atomic.AddUint32(&m.nextID, 1)

	// Apply sanitization similar to production for consistent mock verification
	if len(req.Summary) > 512 {
		req.Summary = req.Summary[:512]
	}
	if len(req.Body) > 2048 {
		req.Body = req.Body[:2048]
	}
	req.Summary = sanitizeHTML(req.Summary)
	req.Body = sanitizeHTML(req.Body)

	m.logger.Info("Mock triggered notification popup on host desktop",
		"id", id,
		"app_name", req.AppName,
		"summary", req.Summary,
		"body", req.Body,
		"expire_timeout", req.ExpireTimeout,
	)

	return id, nil
}
