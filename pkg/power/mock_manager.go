package power

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// MockPowerProfilesManager implements PowerProfilesManager for emulation.
type MockPowerProfilesManager struct {
	logger        *slog.Logger
	mu            sync.Mutex
	activeProfile string
	profiles      []PowerProfile
	eventsChan    chan PowerProfileState
	started       bool
}

// NewMockPowerProfilesManager instantiates a mock manager.
func NewMockPowerProfilesManager(logger *slog.Logger) *MockPowerProfilesManager {
	return &MockPowerProfilesManager{
		logger:        logger,
		activeProfile: "balanced",
		profiles: []PowerProfile{
			{Profile: "power-saver"},
			{Profile: "balanced"},
			{Profile: "performance"},
		},
	}
}

// Start initiates the mock monitoring loop and immediately pushes the default state.
func (m *MockPowerProfilesManager) Start(ctx context.Context) (<-chan PowerProfileState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return m.eventsChan, nil
	}

	m.eventsChan = make(chan PowerProfileState, 10)
	m.started = true

	// Immediately broadcast the initial mock state
	state := PowerProfileState{
		ActiveProfile:     m.activeProfile,
		AvailableProfiles: m.profiles,
	}
	m.eventsChan <- state

	m.logger.Info("Mock Power Profiles manager successfully initialized")
	return m.eventsChan, nil
}

// SetPowerProfile simulates a transition in the active power profile.
func (m *MockPowerProfilesManager) SetPowerProfile(ctx context.Context, profile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	valid := false
	for _, p := range m.profiles {
		if p.Profile == profile {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("invalid power profile: %s", profile)
	}

	m.activeProfile = profile
	m.logger.Info("Power profile transitioned (Mock)", "profile", profile)

	if m.started && m.eventsChan != nil {
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

	return nil
}
