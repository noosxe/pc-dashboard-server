package mpris

import "context"

// PlaybackStatus represents the state of a player.
type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "Playing"
	StatusPaused  PlaybackStatus = "Paused"
	StatusStopped PlaybackStatus = "Stopped"
)

// PlayerMetadata represents the track currently playing.
type PlayerMetadata struct {
	TrackID     string   `json:"track_id"`
	Title       string   `json:"title"`
	Artist      []string `json:"artist"`
	Album       string   `json:"album"`
	ArtURL      string   `json:"art_url"`
	LengthMicro int64    `json:"length_microseconds"` // length in microseconds
}

// PlayerState represents the complete state of a media player.
type PlayerState struct {
	PlayerName     string         `json:"player_name"`           // e.g. "spotify", "vlc"
	PlaybackStatus PlaybackStatus `json:"playback_status"`
	Volume         float64        `json:"volume"`                // 0.0 to 1.0
	PositionMicro  int64          `json:"position_microseconds"` // current playback position
	Metadata       PlayerMetadata `json:"metadata"`
}

// MediaEvent represents a change in the active players or their playback states.
type MediaEvent struct {
	ActivePlayers []PlayerState `json:"active_players"`
}

// MPRISManager defines the contract for monitoring and controlling MPRIS players.
type MPRISManager interface {
	// Start begins monitoring DBus for MPRIS players and pushes state updates.
	Start(ctx context.Context) (<-chan MediaEvent, error)

	// SendCommand issues a control command to a specific active player.
	SendCommand(ctx context.Context, playerName string, command string, args map[string]interface{}) error
}
