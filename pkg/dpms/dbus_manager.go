package dpms

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
)

// DbusDpmsManager coordinates native Linux D-Bus monitoring for display power (DPMS) events.
type DbusDpmsManager struct {
	logger      *slog.Logger
	sessionConn *dbus.Conn

	mu           sync.Mutex
	isFirstEvent bool
	lastState    string // "on" or "off"
}

// NewDbusDpmsManager instantiates the production DPMS manager connecting to the Session Bus.
func NewDbusDpmsManager(logger *slog.Logger) (*DbusDpmsManager, error) {
	sessionConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to D-Bus session bus for DPMS tracking: %w", err)
	}

	return &DbusDpmsManager{
		logger:       logger,
		sessionConn:  sessionConn,
		isFirstEvent: true,
	}, nil
}

// Start registers signal match rules for display power state changes and streams them.
func (m *DbusDpmsManager) Start(ctx context.Context) (<-chan DpmsEvent, error) {
	out := make(chan DpmsEvent, 50)
	sessionChan := make(chan *dbus.Signal, 50)

	obj := m.sessionConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

	// Match PropertiesChanged signals on /org/gnome/Mutter/DisplayConfig
	rules := []string{
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='/org/gnome/Mutter/DisplayConfig'",
	}
	for _, rule := range rules {
		if err := obj.Call("org.freedesktop.DBus.AddMatch", 0, rule).Err; err != nil {
			m.logger.Warn("Failed to add D-Bus display config match rule", "rule", rule, "error", err)
		}
	}

	m.sessionConn.Signal(sessionChan)

	go func() {
		defer m.sessionConn.Close()
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Shutting down D-Bus display power monitor")
				return
			case sig, ok := <-sessionChan:
				if !ok {
					return
				}
				m.handleSessionSignal(sig, out)
			}
		}
	}()

	m.logger.Info("D-Bus display power (DPMS) monitoring successfully initialized")
	return out, nil
}

// handleSessionSignal parses incoming D-Bus property changes.
func (m *DbusDpmsManager) handleSessionSignal(sig *dbus.Signal, out chan<- DpmsEvent) {
	m.logger.Debug("Received D-Bus signal in DPMS manager", "sender", sig.Sender, "path", sig.Path, "name", sig.Name, "body", sig.Body)

	if sig.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" && sig.Path == "/org/gnome/Mutter/DisplayConfig" {
		if len(sig.Body) >= 2 {
			if iface, ok := sig.Body[0].(string); ok && iface == "org.gnome.Mutter.DisplayConfig" {
				if changedProps, ok := sig.Body[1].(map[string]dbus.Variant); ok {
					if variant, exists := changedProps["PowerSaveMode"]; exists {
						var mode int64
						val := variant.Value()
						switch v := val.(type) {
						case int32:
							mode = int64(v)
						case int64:
							mode = v
						case uint32:
							mode = int64(v)
						case uint64:
							mode = int64(v)
						case int:
							mode = int64(v)
						default:
							m.logger.Warn("Unexpected PowerSaveMode type received", "type", fmt.Sprintf("%T", val))
							return
						}

						m.logger.Debug("Received GNOME Mutter PowerSaveMode change", "mode", mode)

						// 0 is Normal (ON), anything else (1 = standby, 2 = suspend, 3 = off) represents screen sleeping.
						state := "on"
						if mode > 0 {
							state = "off"
						}

						m.updateState(state, out)
					}
				}
			}
		}
	}
}

// updateState performs thread-safe deduplication and dispatches genuine DPMS transitions.
func (m *DbusDpmsManager) updateState(state string, out chan<- DpmsEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isFirstEvent || m.lastState != state {
		m.isFirstEvent = false
		m.lastState = state
		m.logger.Info("DPMS display power state transitioned", "state", state)

		select {
		case out <- DpmsEvent{State: state}:
		default:
			m.logger.Warn("DPMS state channel full, dropping event", "state", state)
		}
	}
}
