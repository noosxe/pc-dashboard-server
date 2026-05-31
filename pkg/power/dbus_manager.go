package power

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
)

// DbusPowerProfilesManager implements PowerProfilesManager using native system D-Bus.
type DbusPowerProfilesManager struct {
	logger        *slog.Logger
	conn          *dbus.Conn
	mu            sync.RWMutex
	activeProfile string
	profiles      []PowerProfile
	eventsChan    chan PowerProfileState
	started       bool
}

// NewDbusPowerProfilesManager connects to the system bus.
func NewDbusPowerProfilesManager(logger *slog.Logger) (*DbusPowerProfilesManager, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	return &DbusPowerProfilesManager{
		logger: logger,
		conn:   conn,
	}, nil
}

// Start begins listening to active profile changes and fetches initial properties.
func (m *DbusPowerProfilesManager) Start(ctx context.Context) (<-chan PowerProfileState, error) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return m.eventsChan, nil
	}

	m.eventsChan = make(chan PowerProfileState, 10)
	m.started = true
	m.mu.Unlock()

	// Register D-Bus match rule to listen for PropertiesChanged signals on power profiles daemon
	obj := m.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	rule := "type='signal',sender='net.hadess.PowerProfiles',path='/net/hadess/PowerProfiles',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'"
	err := obj.Call("org.freedesktop.DBus.AddMatch", 0, rule).Err
	if err != nil {
		m.mu.Lock()
		m.started = false
		m.eventsChan = nil
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to add D-Bus match rule for power profiles: %w", err)
	}

	ch := make(chan *dbus.Signal, 50)
	m.conn.Signal(ch)

	// Fetch initial properties from net.hadess.PowerProfiles
	m.fetchInitialState()

	m.logger.Info("D-Bus Power Profiles manager successfully initialized")

	// Start signal eavesdropping loop
	go func() {
		defer func() {
			m.mu.Lock()
			if m.eventsChan != nil {
				close(m.eventsChan)
				m.eventsChan = nil
			}
			m.mu.Unlock()
			m.conn.Close()
		}()

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Shutting down D-Bus Power Profiles monitor")
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				m.handleDbusSignal(sig)
			}
		}
	}()

	return m.eventsChan, nil
}

// SetPowerProfile transitions the system power profile on the D-Bus interface.
func (m *DbusPowerProfilesManager) SetPowerProfile(ctx context.Context, profile string) error {
	m.mu.RLock()
	available := m.profiles
	m.mu.RUnlock()

	// 1. Strict Input Sanitization: Validate against read-only available profiles list
	valid := false
	for _, p := range available {
		if p.Profile == profile {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("invalid power profile: %s (not in available profiles)", profile)
	}

	m.logger.Info("Setting active power profile", "profile", profile)
	obj := m.conn.Object("net.hadess.PowerProfiles", "/net/hadess/PowerProfiles")

	err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0,
		"net.hadess.PowerProfiles",
		"ActiveProfile",
		dbus.MakeVariant(profile),
	).Err
	if err != nil {
		return fmt.Errorf("failed to set ActiveProfile property: %w", err)
	}

	return nil
}

// fetchInitialState queries the daemon on startup.
func (m *DbusPowerProfilesManager) fetchInitialState() {
	obj := m.conn.Object("net.hadess.PowerProfiles", "/net/hadess/PowerProfiles")

	// 1. Fetch ActiveProfile
	activeVar, err := obj.GetProperty("net.hadess.PowerProfiles.ActiveProfile")
	if err == nil {
		if s, ok := activeVar.Value().(string); ok {
			m.activeProfile = s
		}
	} else {
		m.logger.Warn("Failed to query ActiveProfile property on startup", "error", err)
	}

	// 2. Fetch Available Profiles
	profilesVar, err := obj.GetProperty("net.hadess.PowerProfiles.Profiles")
	var parsedProfiles []PowerProfile
	if err == nil {
		if rawSlice, ok := profilesVar.Value().([]map[string]dbus.Variant); ok {
			for _, m := range rawSlice {
				if pVar, ok := m["Profile"]; ok {
					if pStr, ok := pVar.Value().(string); ok {
						parsedProfiles = append(parsedProfiles, PowerProfile{Profile: pStr})
					}
				}
			}
		} else if rawSliceInterface, ok := profilesVar.Value().([]interface{}); ok {
			for _, item := range rawSliceInterface {
				if m, ok := item.(map[string]dbus.Variant); ok {
					if pVar, ok := m["Profile"]; ok {
						if pStr, ok := pVar.Value().(string); ok {
							parsedProfiles = append(parsedProfiles, PowerProfile{Profile: pStr})
						}
					}
				}
			}
		}
	} else {
		m.logger.Warn("Failed to query Profiles property on startup", "error", err)
	}

	m.mu.Lock()
	m.profiles = parsedProfiles
	m.mu.Unlock()

	m.logger.Info("Fetched initial power profiles state", "active", m.activeProfile, "count", len(m.profiles))
	m.broadcastState()
}

// handleDbusSignal routes and parses PropertiesChanged signals.
func (m *DbusPowerProfilesManager) handleDbusSignal(sig *dbus.Signal) {
	m.logger.Debug("Received D-Bus signal in Power Profiles manager", "sender", sig.Sender, "path", sig.Path, "name", sig.Name, "body", sig.Body)
	if sig.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" {
		if len(sig.Body) >= 2 {
			if iface, ok := sig.Body[0].(string); ok && iface == "net.hadess.PowerProfiles" {
				if changedProps, ok := sig.Body[1].(map[string]dbus.Variant); ok {
					if activeVal, ok := changedProps["ActiveProfile"]; ok {
						if activeStr, ok := activeVal.Value().(string); ok {
							m.mu.Lock()
							m.activeProfile = activeStr
							m.mu.Unlock()
							m.logger.Info("Power profile changed", "profile", activeStr)
							m.broadcastState()
						}
					}
				}
			}
		}
	}
}

// broadcastState pushes state updates.
func (m *DbusPowerProfilesManager) broadcastState() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventsChan == nil {
		return
	}

	state := PowerProfileState{
		ActiveProfile:     m.activeProfile,
		AvailableProfiles: m.profiles,
	}

	select {
	case m.eventsChan <- state:
	default:
		// Non-blocking fallback
	}
}
