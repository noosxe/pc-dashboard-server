package power

import "context"

// PowerProfile holds the name of a power profile.
type PowerProfile struct {
	Profile string `json:"profile"`
}

// PowerProfileState holds the current power profile and available profiles.
type PowerProfileState struct {
	ActiveProfile     string         `json:"active_profile"`
	AvailableProfiles []PowerProfile `json:"available_profiles"`
}

// PowerProfilesManager defines the contract for querying and controlling power profiles.
type PowerProfilesManager interface {
	// Start begins monitoring the power profiles and pushes state updates.
	Start(ctx context.Context) (<-chan PowerProfileState, error)

	// SetPowerProfile writes a new active power profile name to D-Bus.
	SetPowerProfile(ctx context.Context, profile string) error
}
