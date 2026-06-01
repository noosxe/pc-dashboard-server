package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	"github.com/godbus/dbus/v5"
)

var htmlTagRegexp = regexp.MustCompile("<[^>]*>")

// sanitizeHTML strips all HTML tags to prevent markup injection.
func sanitizeHTML(s string) string {
	return htmlTagRegexp.ReplaceAllString(s, "")
}

// DbusNotificationManager coordinates monitoring and publishing via native Linux D-Bus session bus.
type DbusNotificationManager struct {
	logger    *slog.Logger
	sendConn  *dbus.Conn
	mu        sync.Mutex
	extractor *IconExtractor
}

// NewDbusNotificationManager instantiates the production manager, connecting to the D-Bus session.
func NewDbusNotificationManager(logger *slog.Logger) (*DbusNotificationManager, error) {
	sendConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus for publishing: %w", err)
	}

	return &DbusNotificationManager{
		logger:    logger,
		sendConn:  sendConn,
		extractor: NewIconExtractor(logger),
	}, nil
}

// Start registers monitor eavesdropping and streams caught notifications via an event channel.
func (m *DbusNotificationManager) Start(ctx context.Context) (<-chan NotificationEvent, error) {
	monitorConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus for monitoring: %w", err)
	}

	rules := []string{"type='method_call',interface='org.freedesktop.Notifications',member='Notify'"}
	obj := monitorConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	err = obj.Call("org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, rules, uint32(0)).Err
	if err != nil {
		monitorConn.Close()
		return nil, fmt.Errorf("failed to become monitor: %w", err)
	}

	ch := make(chan *dbus.Message, 100)
	monitorConn.Eavesdrop(ch)

	out := make(chan NotificationEvent, 50)
	m.logger.Info("D-Bus notification monitoring successfully initialized")

	go func() {
		defer close(out)
		defer monitorConn.Close()

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Shutting down D-Bus notification monitor")
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				m.logger.Debug("Received D-Bus message in Notifications manager", "type", msg.Type, "headers", msg.Headers, "body_len", len(msg.Body))
				if len(msg.Body) < 8 {
					continue
				}

				appName, _ := msg.Body[0].(string)
				replacesId, _ := msg.Body[1].(uint32)
				appIcon, _ := msg.Body[2].(string)
				summary, _ := msg.Body[3].(string)
				body, _ := msg.Body[4].(string)
				actions, _ := msg.Body[5].([]string)
				if actions == nil {
					actions = []string{}
				}
				hintsRaw, _ := msg.Body[6].(map[string]dbus.Variant)
				expireTimeout, _ := msg.Body[7].(int32)

				m.logger.Debug("Parsed notification from D-Bus",
					"app_name", appName,
					"replaces_id", replacesId,
					"summary", summary,
					"body", body,
				)

				hints := make(map[string]interface{})
				for k, v := range hintsRaw {
					hints[k] = v.Value()
				}

				// Asynchronously extract the app icon in a background goroutine
				// to prevent disk/image I/O from blocking the D-Bus signal receiver loop.
				go func(appName, appIcon, summary, body string, replacesId uint32, actions []string, hints map[string]interface{}, expireTimeout int32, hintsRaw map[string]dbus.Variant) {
					base64Icon := m.extractor.Extract(appName, appIcon, hintsRaw)

					m.logger.Debug("Dispatched processed notification event",
						"app_name", appName,
						"replaces_id", replacesId,
						"has_base64_icon", base64Icon != "",
					)

					select {
					case out <- NotificationEvent{
						AppName:       appName,
						ReplacesID:    replacesId,
						AppIcon:       appIcon,
						AppIconBase64: base64Icon,
						Summary:       summary,
						Body:          body,
						Actions:       actions,
						Hints:         hints,
						ExpireTimeout: expireTimeout,
					}:
					case <-ctx.Done():
					}
				}(appName, appIcon, summary, body, replacesId, actions, hints, expireTimeout, hintsRaw)
			}
		}
	}()

	return out, nil
}

// SendNotification publishes a desktop notification to the host session.
func (m *DbusNotificationManager) SendNotification(ctx context.Context, req NotificationRequest) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Safety Enforcements & Sanitizations
	if len(req.Summary) > 512 {
		req.Summary = req.Summary[:512]
	}
	if len(req.Body) > 2048 {
		req.Body = req.Body[:2048]
	}
	req.Summary = sanitizeHTML(req.Summary)
	req.Body = sanitizeHTML(req.Body)

	// Restrict hints to standard, safe primitives
	allowedHints := map[string]bool{
		"urgency":        true,
		"category":       true,
		"transient":      true,
		"resident":       true,
		"suppress-sound": true,
		"sound-file":     true,
		"sound-name":     true,
	}

	cleanHints := make(map[string]dbus.Variant)
	for k, v := range req.Hints {
		if !allowedHints[k] {
			continue // Filter out unknown/unverified hints to prevent security/serialization exploits
		}
		switch val := v.(type) {
		case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			cleanHints[k] = dbus.MakeVariant(val)
		}
	}

	obj := m.sendConn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")

	var notificationID uint32
	err := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		req.AppName,
		req.ReplacesID,
		req.AppIcon,
		req.Summary,
		req.Body,
		req.Actions,
		cleanHints,
		req.ExpireTimeout,
	).Store(&notificationID)

	if err != nil {
		return 0, fmt.Errorf("failed to trigger notification via D-Bus: %w", err)
	}

	return notificationID, nil
}

// Close terminates the permanent publisher connection cleanly.
func (m *DbusNotificationManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendConn != nil {
		return m.sendConn.Close()
	}
	return nil
}
