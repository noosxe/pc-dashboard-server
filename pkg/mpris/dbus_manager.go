package mpris

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// DbusMPRISManager coordinates native D-Bus monitoring and command execution.
type DbusMPRISManager struct {
	logger         *slog.Logger
	sendConn       *dbus.Conn
	mu             sync.RWMutex
	activePlayers  map[string]*PlayerState // keyed by unique owner name (e.g. ":1.123")
	ownerToService map[string]string       // maps unique owner to well-known service name
	eventsChan     chan MediaEvent
	started        bool
}

// NewDbusMPRISManager connects to the session bus for sending commands.
func NewDbusMPRISManager(logger *slog.Logger) (*DbusMPRISManager, error) {
	sendConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus for MPRIS commands: %w", err)
	}

	return &DbusMPRISManager{
		logger:         logger,
		sendConn:       sendConn,
		activePlayers:  make(map[string]*PlayerState),
		ownerToService: make(map[string]string),
	}, nil
}

// Start begins dynamic D-Bus session eavesdropping for media players.
func (m *DbusMPRISManager) Start(ctx context.Context) (<-chan MediaEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return m.eventsChan, nil
	}

	monitorConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus for MPRIS monitoring: %w", err)
	}

	// Register match signals to listen for name owner shifts and properties changes
	obj := monitorConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	err = obj.Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged'").Err
	if err != nil {
		monitorConn.Close()
		return nil, fmt.Errorf("failed to add NameOwnerChanged match signal: %w", err)
	}

	err = obj.Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='/org/mpris/MediaPlayer2'").Err
	if err != nil {
		monitorConn.Close()
		return nil, fmt.Errorf("failed to add PropertiesChanged match signal: %w", err)
	}

	ch := make(chan *dbus.Signal, 100)
	monitorConn.Signal(ch)

	m.eventsChan = make(chan MediaEvent, 50)
	m.started = true

	// Bootstrap currently active players
	m.bootstrapActivePlayers()

	m.logger.Info("D-Bus MPRIS player monitoring successfully initialized")

	// 1. Start Signal Eavesdropping Loop
	go func() {
		defer close(m.eventsChan)
		defer monitorConn.Close()

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Shutting down D-Bus MPRIS signal monitor")
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				m.handleDbusSignal(sig)
			}
		}
	}()

	// 2. Start Periodic Position Update Ticker Loop
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.pollActivePlayerPositions()
			}
		}
	}()

	return m.eventsChan, nil
}

// SendCommand dispatches control commands directly to standard D-Bus MPRIS endpoints.
func (m *DbusMPRISManager) SendCommand(ctx context.Context, playerName string, command string, args map[string]interface{}) error {
	m.mu.RLock()
	// Attempt to locate well-known service name from active players
	var serviceName string
	for owner, state := range m.activePlayers {
		if state.PlayerName == playerName {
			serviceName = m.ownerToService[owner]
			break
		}
	}
	m.mu.RUnlock()

	// Fallback to standard MPRIS naming prefix if player not currently registered
	if serviceName == "" {
		if strings.HasPrefix(playerName, "org.mpris.MediaPlayer2.") {
			serviceName = playerName
		} else {
			serviceName = "org.mpris.MediaPlayer2." + playerName
		}
	}

	m.logger.Info("Sending command to D-Bus MPRIS player", "service", serviceName, "command", command)
	obj := m.sendConn.Object(serviceName, "/org/mpris/MediaPlayer2")

	switch command {
	case "play":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Play", 0).Err
	case "pause":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Pause", 0).Err
	case "play_pause":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.PlayPause", 0).Err
	case "stop":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Stop", 0).Err
	case "next":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Next", 0).Err
	case "previous":
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Previous", 0).Err
	case "seek":
		if args == nil {
			return fmt.Errorf("seek offset argument is required")
		}
		offsetVal, ok := args["offset_microseconds"]
		if !ok {
			return fmt.Errorf("seek offset_microseconds argument is required")
		}
		var offset int64
		switch v := offsetVal.(type) {
		case int64:
			offset = v
		case float64:
			offset = int64(v)
		case int:
			offset = int64(v)
		}
		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.Seek", 0, offset).Err
	case "set_position":
		if args == nil {
			return fmt.Errorf("set_position arguments are required")
		}
		posVal, ok := args["position_microseconds"]
		if !ok {
			return fmt.Errorf("set_position position_microseconds argument is required")
		}
		var pos int64
		switch v := posVal.(type) {
		case int64:
			pos = v
		case float64:
			pos = int64(v)
		case int:
			pos = int64(v)
		}
		if pos < 0 {
			pos = 0
		}

		trackIDVal, ok := args["track_id"]
		var trackID string
		if ok {
			trackID, _ = trackIDVal.(string)
		}
		trackPath := dbus.ObjectPath(trackID)

		return obj.CallWithContext(ctx, "org.mpris.MediaPlayer2.Player.SetPosition", 0, trackPath, pos).Err
	case "set_volume":
		if args == nil {
			return fmt.Errorf("set_volume arguments are required")
		}
		volVal, ok := args["volume"]
		if !ok {
			return fmt.Errorf("set_volume volume argument is required")
		}
		var vol float64
		switch v := volVal.(type) {
		case float64:
			vol = v
		case float32:
			vol = float64(v)
		case int:
			vol = float64(v)
		}
		// Clamping volume to prevent security/malicious out-of-range inputs
		if vol < 0.0 {
			vol = 0.0
		} else if vol > 1.0 {
			vol = 1.0
		}

		return m.sendConn.Object(serviceName, "/org/mpris/MediaPlayer2").CallWithContext(ctx,
			"org.freedesktop.DBus.Properties.Set", 0,
			"org.mpris.MediaPlayer2.Player",
			"Volume",
			dbus.MakeVariant(vol),
		).Err
	default:
		return fmt.Errorf("unsupported MPRIS command: %s", command)
	}
}

// Close terminates D-Bus session transport cleanly.
func (m *DbusMPRISManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendConn != nil {
		return m.sendConn.Close()
	}
	return nil
}

// bootstrapActivePlayers queries org.freedesktop.DBus to fetch initially running players.
func (m *DbusMPRISManager) bootstrapActivePlayers() {
	obj := m.sendConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var names []string
	err := obj.Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		m.logger.Error("Failed to list active D-Bus players on startup", "error", err)
		return
	}

	for _, name := range names {
		if strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
			var owner string
			err = obj.Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner)
			if err != nil {
				continue
			}
			m.fetchAndRegisterPlayer(name, owner)
		}
	}
	m.broadcastState()
}

// fetchAndRegisterPlayer extracts properties from the D-Bus object and populates local registries.
func (m *DbusMPRISManager) fetchAndRegisterPlayer(serviceName, owner string) {
	playerObj := m.sendConn.Object(serviceName, "/org/mpris/MediaPlayer2")

	// Read Volume
	volume := 1.0
	volVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Player.Volume")
	if err == nil {
		volume, _ = volVar.Value().(float64)
	}

	// Read PlaybackStatus
	status := StatusStopped
	statusVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Player.PlaybackStatus")
	if err == nil {
		if s, ok := statusVar.Value().(string); ok {
			status = PlaybackStatus(s)
		}
	}

	// Read Position
	var position int64
	posVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Player.Position")
	if err == nil {
		position, _ = posVar.Value().(int64)
	}

	// Read Metadata
	var metadata PlayerMetadata
	metaVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Player.Metadata")
	if err == nil {
		if rawMeta, ok := metaVar.Value().(map[string]dbus.Variant); ok {
			metadata = parseMetadata(rawMeta)
		}
	}

	playerName := strings.TrimPrefix(serviceName, "org.mpris.MediaPlayer2.")

	m.mu.Lock()
	m.activePlayers[owner] = &PlayerState{
		PlayerName:     playerName,
		PlaybackStatus: status,
		Volume:         volume,
		PositionMicro:  position,
		Metadata:       metadata,
	}
	m.ownerToService[owner] = serviceName
	m.mu.Unlock()

	m.logger.Info("Registered active MPRIS player", "name", playerName, "owner", owner, "status", status)
}

// handleDbusSignal routes and parses D-Bus event frames.
func (m *DbusMPRISManager) handleDbusSignal(sig *dbus.Signal) {
	switch sig.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) < 3 {
			return
		}
		serviceName, _ := sig.Body[0].(string)
		oldOwner, _ := sig.Body[1].(string)
		newOwner, _ := sig.Body[2].(string)

		if !strings.HasPrefix(serviceName, "org.mpris.MediaPlayer2.") {
			return
		}

		if oldOwner != "" && newOwner == "" {
			// Player stopped/closed
			m.mu.Lock()
			playerName := ""
			if p, ok := m.activePlayers[oldOwner]; ok {
				playerName = p.PlayerName
			}
			delete(m.activePlayers, oldOwner)
			delete(m.ownerToService, oldOwner)
			m.mu.Unlock()

			m.logger.Info("Unregistered MPRIS player (exited)", "name", playerName, "owner", oldOwner)
			m.broadcastState()
		} else if newOwner != "" {
			// Player started
			m.fetchAndRegisterPlayer(serviceName, newOwner)
			m.broadcastState()
		}

	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if len(sig.Body) < 2 {
			return
		}
		interfaceName, _ := sig.Body[0].(string)
		if interfaceName != "org.mpris.MediaPlayer2.Player" {
			return
		}

		changedProps, _ := sig.Body[1].(map[string]dbus.Variant)

		m.mu.Lock()
		player, exists := m.activePlayers[sig.Sender]
		if !exists {
			m.mu.Unlock()
			return
		}

		updated := false
		if val, ok := changedProps["PlaybackStatus"]; ok {
			if s, ok := val.Value().(string); ok {
				player.PlaybackStatus = PlaybackStatus(s)
				updated = true
			}
		}
		if val, ok := changedProps["Volume"]; ok {
			if v, ok := val.Value().(float64); ok {
				player.Volume = v
				updated = true
			}
		}
		if val, ok := changedProps["Metadata"]; ok {
			if rawMeta, ok := val.Value().(map[string]dbus.Variant); ok {
				player.Metadata = parseMetadata(rawMeta)
				updated = true
			}
		}
		m.mu.Unlock()

		if updated {
			m.broadcastState()
		}
	}
}

// pollActivePlayerPositions queries playing players for updated microsecond progress.
func (m *DbusMPRISManager) pollActivePlayerPositions() {
	m.mu.RLock()
	playingServices := make(map[string]string) // unique name -> well-known service name
	for owner, state := range m.activePlayers {
		if state.PlaybackStatus == StatusPlaying {
			playingServices[owner] = m.ownerToService[owner]
		}
	}
	m.mu.RUnlock()

	if len(playingServices) == 0 {
		return
	}

	updated := false
	for owner, service := range playingServices {
		playerObj := m.sendConn.Object(service, "/org/mpris/MediaPlayer2")
		posVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Player.Position")
		if err == nil {
			if pos, ok := posVar.Value().(int64); ok {
				m.mu.Lock()
				if player, exists := m.activePlayers[owner]; exists {
					player.PositionMicro = pos
					updated = true
				}
				m.mu.Unlock()
			}
		}
	}

	if updated {
		m.broadcastState()
	}
}

// broadcastState sends active players list to the listener channel.
func (m *DbusMPRISManager) broadcastState() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventsChan == nil {
		return
	}

	players := make([]PlayerState, 0, len(m.activePlayers))
	for _, p := range m.activePlayers {
		players = append(players, *p)
	}

	event := MediaEvent{
		ActivePlayers: players,
	}

	select {
	case m.eventsChan <- event:
	default:
		// Drop frame if buffer is full to prevent locks
	}
}

// parseMetadata converts standard D-Bus map variants into clean structures.
func parseMetadata(metaMap map[string]dbus.Variant) PlayerMetadata {
	var meta PlayerMetadata

	if v, ok := metaMap["mpris:trackid"]; ok {
		switch val := v.Value().(type) {
		case string:
			meta.TrackID = val
		case dbus.ObjectPath:
			meta.TrackID = string(val)
		}
	}
	if v, ok := metaMap["xesam:title"]; ok {
		meta.Title, _ = v.Value().(string)
	}
	if v, ok := metaMap["xesam:artist"]; ok {
		switch val := v.Value().(type) {
		case []string:
			meta.Artist = val
		case string:
			meta.Artist = []string{val}
		}
	}
	if v, ok := metaMap["xesam:album"]; ok {
		meta.Album, _ = v.Value().(string)
	}
	if v, ok := metaMap["mpris:artUrl"]; ok {
		meta.ArtURL, _ = v.Value().(string)
	}
	if v, ok := metaMap["mpris:length"]; ok {
		switch val := v.Value().(type) {
		case int64:
			meta.LengthMicro = val
		case uint64:
			meta.LengthMicro = int64(val)
		case int32:
			meta.LengthMicro = int64(val)
		}
	}

	return meta
}
