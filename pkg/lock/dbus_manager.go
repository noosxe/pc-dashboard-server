package lock

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
)

// DbusLockManager coordinates native Linux D-Bus monitoring for session lock/unlock events.
type DbusLockManager struct {
	logger      *slog.Logger
	sessionConn *dbus.Conn
	systemConn  *dbus.Conn

	mu           sync.Mutex
	isFirstEvent bool
	lastState    bool
}

// NewDbusLockManager instantiates the production manager, connecting to the D-Bus session & system buses.
func NewDbusLockManager(logger *slog.Logger) (*DbusLockManager, error) {
	sessionConn, sessionErr := dbus.ConnectSessionBus()
	if sessionErr != nil {
		logger.Warn("Failed to connect to D-Bus session bus for screensaver tracking", "error", sessionErr)
	}

	systemConn, systemErr := dbus.ConnectSystemBus()
	if systemErr != nil {
		logger.Warn("Failed to connect to D-Bus system bus for systemd logind tracking", "error", systemErr)
	}

	// If both connections fail, we cannot monitor either bus. Return an error to fallback.
	if sessionErr != nil && systemErr != nil {
		return nil, fmt.Errorf("both D-Bus session and system bus connections failed: session_err=%v, system_err=%v", sessionErr, systemErr)
	}

	return &DbusLockManager{
		logger:       logger,
		sessionConn:  sessionConn,
		systemConn:   systemConn,
		isFirstEvent: true,
	}, nil
}

// Start registers signal match rules on both buses and pipes deduplicated lock events into a single channel.
func (m *DbusLockManager) Start(ctx context.Context) (<-chan SessionLockEvent, error) {
	out := make(chan SessionLockEvent, 50)
	sessionChan := make(chan *dbus.Signal, 50)
	systemChan := make(chan *dbus.Signal, 50)

	var wg sync.WaitGroup

	// 1. Setup Session Bus screensaver listener
	if m.sessionConn != nil {
		obj := m.sessionConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

		rules := []string{
			"type='signal',interface='org.freedesktop.ScreenSaver',member='ActiveChanged'",
			"type='signal',interface='org.gnome.ScreenSaver',member='ActiveChanged'",
		}
		for _, rule := range rules {
			if err := obj.Call("org.freedesktop.DBus.AddMatch", 0, rule).Err; err != nil {
				m.logger.Warn("Failed to add D-Bus screensaver match rule", "rule", rule, "error", err)
			}
		}

		m.sessionConn.Signal(sessionChan)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.sessionConn.Close()

			for {
				select {
				case <-ctx.Done():
					m.logger.Info("Shutting down D-Bus screensaver monitor")
					return
				case sig, ok := <-sessionChan:
					if !ok {
						return
					}
					m.handleSessionSignal(sig, out)
				}
			}
		}()
	}

	// 2. Setup System Bus logind listener
	if m.systemConn != nil {
		obj := m.systemConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

		rules := []string{
			"type='signal',sender='org.freedesktop.login1',interface='org.freedesktop.login1.Session',member='Lock'",
			"type='signal',sender='org.freedesktop.login1',interface='org.freedesktop.login1.Session',member='Unlock'",
		}
		for _, rule := range rules {
			if err := obj.Call("org.freedesktop.DBus.AddMatch", 0, rule).Err; err != nil {
				m.logger.Warn("Failed to add D-Bus logind match rule", "rule", rule, "error", err)
			}
		}

		m.systemConn.Signal(systemChan)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.systemConn.Close()

			for {
				select {
				case <-ctx.Done():
					m.logger.Info("Shutting down D-Bus logind session monitor")
					return
				case sig, ok := <-systemChan:
					if !ok {
						return
					}
					m.handleSystemSignal(sig, out)
				}
			}
		}()
	}

	// 3. Coordinator to clean up the output channel when all watchers finish
	go func() {
		wg.Wait()
		close(out)
		m.logger.Info("D-Bus session lock monitoring fully stopped")
	}()

	m.logger.Info("D-Bus session lock monitoring successfully initialized")
	return out, nil
}

// handleSessionSignal handles incoming screensaver change events.
func (m *DbusLockManager) handleSessionSignal(sig *dbus.Signal, out chan<- SessionLockEvent) {
	if sig.Name == "org.freedesktop.ScreenSaver.ActiveChanged" || sig.Name == "org.gnome.ScreenSaver.ActiveChanged" {
		if len(sig.Body) > 0 {
			if active, ok := sig.Body[0].(bool); ok {
				m.logger.Debug("Received D-Bus screensaver ActiveChanged signal", "active", active)
				m.updateState(active, out)
			}
		}
	}
}

// handleSystemSignal handles incoming systemd session lock/unlock events.
func (m *DbusLockManager) handleSystemSignal(sig *dbus.Signal, out chan<- SessionLockEvent) {
	switch sig.Name {
	case "org.freedesktop.login1.Session.Lock":
		m.logger.Debug("Received systemd Lock signal")
		m.updateState(true, out)
	case "org.freedesktop.login1.Session.Unlock":
		m.logger.Debug("Received systemd Unlock signal")
		m.updateState(false, out)
	}
}

// updateState performs thread-safe deduplication and dispatches genuine lock-state transitions.
func (m *DbusLockManager) updateState(locked bool, out chan<- SessionLockEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isFirstEvent || m.lastState != locked {
		m.isFirstEvent = false
		m.lastState = locked
		m.logger.Info("Session lock state transitioned", "locked", locked)

		select {
		case out <- SessionLockEvent{Locked: locked}:
		default:
			m.logger.Warn("Session lock channel full, dropping event", "locked", locked)
		}
	}
}
