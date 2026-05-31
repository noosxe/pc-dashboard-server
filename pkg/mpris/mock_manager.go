package mpris

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// MockMPRISManager simulates active media player states and progress transitions.
type MockMPRISManager struct {
	logger        *slog.Logger
	mu            sync.Mutex
	status        PlaybackStatus
	volume        float64
	positionMicro int64
	trackIndex    int
	tracks        []PlayerMetadata
	eventsChan    chan MediaEvent
	started       bool
}

// NewMockMPRISManager instantiates a simulated media controller.
func NewMockMPRISManager(logger *slog.Logger) *MockMPRISManager {
	tracks := []PlayerMetadata{
		{
			TrackID:     "spotify:track:4PTG3Z6ehGkBFm5zOHYGaS",
			Title:       "Stayin' Alive",
			Artist:      []string{"Bee Gees"},
			Album:       "Saturday Night Fever",
			ArtURL:      "https://images.example.com/stayinalive.jpg",
			LengthMicro: 284000000, // 284 seconds
		},
		{
			TrackID:     "spotify:track:512G3Z6ehGkBFm5zOHYGaS",
			Title:       "Billie Jean",
			Artist:      []string{"Michael Jackson"},
			Album:       "Thriller",
			ArtURL:      "https://images.example.com/billiejean.jpg",
			LengthMicro: 294000000, // 294 seconds
		},
		{
			TrackID:     "spotify:track:615G3Z6ehGkBFm5zOHYGaS",
			Title:       "Take On Me",
			Artist:      []string{"a-ha"},
			Album:       "Hunting High and Low",
			ArtURL:      "https://images.example.com/takeonme.jpg",
			LengthMicro: 225000000, // 225 seconds
		},
	}

	return &MockMPRISManager{
		logger:        logger,
		status:        StatusPlaying,
		volume:        0.85,
		positionMicro: 45000000, // starts 45 seconds in
		trackIndex:    0,
		tracks:        tracks,
	}
}

// Start boots the background simulation tick and returns the media event stream.
func (m *MockMPRISManager) Start(ctx context.Context) (<-chan MediaEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return m.eventsChan, nil
	}

	m.eventsChan = make(chan MediaEvent, 50)
	m.started = true
	m.logger.Info("Mock MPRIS manager successfully initialized")

	// Helper to send initial payload
	go func() {
		m.mu.Lock()
		m.broadcastStateLocked()
		m.mu.Unlock()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopping mock MPRIS simulation ticker")
				return
			case <-ticker.C:
				m.mu.Lock()
				if m.status == StatusPlaying {
					m.positionMicro += 1000000 // increment by 1 second

					currTrack := m.tracks[m.trackIndex]
					if m.positionMicro >= currTrack.LengthMicro {
						// transition to next track
						m.trackIndex = (m.trackIndex + 1) % len(m.tracks)
						m.positionMicro = 0
						m.logger.Info("Mock track completed, transitioning", "title", m.tracks[m.trackIndex].Title)
					}
					m.broadcastStateLocked()
				}
				m.mu.Unlock()
			}
		}
	}()

	return m.eventsChan, nil
}

// SendCommand executes a mock state update based on inbound instructions.
func (m *MockMPRISManager) SendCommand(ctx context.Context, playerName string, command string, args map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("mock MPRIS manager not started")
	}

	m.logger.Info("Processing mock media command", "player", playerName, "command", command, "args", args)

	switch command {
	case "play":
		m.status = StatusPlaying
	case "pause":
		m.status = StatusPaused
	case "play_pause":
		if m.status == StatusPlaying {
			m.status = StatusPaused
		} else {
			m.status = StatusPlaying
		}
	case "stop":
		m.status = StatusStopped
		m.positionMicro = 0
	case "next":
		m.trackIndex = (m.trackIndex + 1) % len(m.tracks)
		m.positionMicro = 0
	case "previous":
		m.trackIndex = (m.trackIndex - 1 + len(m.tracks)) % len(m.tracks)
		m.positionMicro = 0
	case "seek":
		if args != nil {
			if offsetVal, ok := args["offset_microseconds"]; ok {
				var offset int64
				switch v := offsetVal.(type) {
				case int64:
					offset = v
				case float64:
					offset = int64(v)
				case int:
					offset = int64(v)
				}
				m.positionMicro += offset
				currTrack := m.tracks[m.trackIndex]
				if m.positionMicro < 0 {
					m.positionMicro = 0
				} else if m.positionMicro > currTrack.LengthMicro {
					m.positionMicro = currTrack.LengthMicro
				}
			}
		}
	case "set_position":
		if args != nil {
			if posVal, ok := args["position_microseconds"]; ok {
				var pos int64
				switch v := posVal.(type) {
				case int64:
					pos = v
				case float64:
					pos = int64(v)
				case int:
					pos = int64(v)
				}
				currTrack := m.tracks[m.trackIndex]
				if pos < 0 {
					pos = 0
				} else if pos > currTrack.LengthMicro {
					pos = currTrack.LengthMicro
				}
				m.positionMicro = pos
			}
		}
	case "set_volume":
		if args != nil {
			if volVal, ok := args["volume"]; ok {
				var vol float64
				switch v := volVal.(type) {
				case float64:
					vol = v
				case float32:
					vol = float64(v)
				case int:
					vol = float64(v)
				}
				if vol < 0.0 {
					vol = 0.0
				} else if vol > 1.0 {
					vol = 1.0
				}
				m.volume = vol
			}
		}
	default:
		return fmt.Errorf("unsupported mock command: %s", command)
	}

	// Broadcast the state update immediately upon receiving any command
	m.broadcastStateLocked()
	return nil
}

// broadcastStateLocked constructs and pushes the current active players state.
func (m *MockMPRISManager) broadcastStateLocked() {
	if m.eventsChan == nil {
		return
	}

	state := PlayerState{
		PlayerName:     "spotify",
		Identity:       "Spotify",
		DesktopEntry:   "spotify",
		PlaybackStatus: m.status,
		Volume:         m.volume,
		PositionMicro:  m.positionMicro,
		Metadata:       m.tracks[m.trackIndex],
	}

	event := MediaEvent{
		ActivePlayers: []PlayerState{state},
	}

	select {
	case m.eventsChan <- event:
	default:
		// Drop frame if buffer is full to prevent blocking locks
	}
}
