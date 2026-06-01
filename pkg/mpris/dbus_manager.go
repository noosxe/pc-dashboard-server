package mpris

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
	extractor      *ArtworkExtractor
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
		extractor:      NewArtworkExtractor(logger),
	}, nil
}

// Start begins dynamic D-Bus session eavesdropping for media players.
func (m *DbusMPRISManager) Start(ctx context.Context) (<-chan MediaEvent, error) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return m.eventsChan, nil
	}

	m.eventsChan = make(chan MediaEvent, 50)
	m.started = true
	m.mu.Unlock()

	monitorConn, err := dbus.ConnectSessionBus()
	if err != nil {
		m.mu.Lock()
		m.started = false
		m.eventsChan = nil
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to connect to session bus for MPRIS monitoring: %w", err)
	}

	// Register match signals to listen for name owner shifts and properties changes
	obj := monitorConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	err = obj.Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged'").Err
	if err != nil {
		monitorConn.Close()
		m.mu.Lock()
		m.started = false
		m.eventsChan = nil
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to add NameOwnerChanged match signal: %w", err)
	}

	err = obj.Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='/org/mpris/MediaPlayer2'").Err
	if err != nil {
		monitorConn.Close()
		m.mu.Lock()
		m.started = false
		m.eventsChan = nil
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to add PropertiesChanged match signal: %w", err)
	}

	ch := make(chan *dbus.Signal, 100)
	monitorConn.Signal(ch)

	// Bootstrap currently active players (runs safely now since m.mu is unlocked)
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
			metadata = m.parseMetadata(rawMeta)
		}
	}

	// Read Identity
	identity := ""
	identityVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.Identity")
	if err == nil {
		identity, _ = identityVar.Value().(string)
	}

	// Read DesktopEntry
	desktopEntry := ""
	desktopVar, err := playerObj.GetProperty("org.mpris.MediaPlayer2.DesktopEntry")
	if err == nil {
		desktopEntry, _ = desktopVar.Value().(string)
	}

	playerName := strings.TrimPrefix(serviceName, "org.mpris.MediaPlayer2.")

	// Multi-tier Resolution Fallbacks
	friendlyName := identity
	lowerFriendly := strings.ToLower(friendlyName)

	if friendlyName == "" || lowerFriendly == "firefox" || lowerFriendly == "chromium" || lowerFriendly == "chrome" {
		if resolvedName := m.getFriendlyNameFromDesktopEntry(desktopEntry); resolvedName != "" {
			friendlyName = resolvedName
		} else if pidName := m.getExecutableNameFromOwner(owner); pidName != "" {
			if resolvedName := m.getFriendlyNameFromDesktopEntry(pidName); resolvedName != "" {
				friendlyName = resolvedName
			} else {
				friendlyName = strings.Title(pidName)
			}
		}
	}

	// Ensure desktopEntry falls back to playerName (without instance suffix) if it remains empty
	if desktopEntry == "" {
		parts := strings.Split(playerName, ".")
		if len(parts) > 0 {
			desktopEntry = parts[0]
		}
	}

	// If friendlyName is still empty, fall back to capitalized desktopEntry
	if friendlyName == "" {
		friendlyName = strings.Title(desktopEntry)
	}

	m.mu.Lock()
	m.activePlayers[owner] = &PlayerState{
		PlayerName:     playerName,
		Identity:       friendlyName,
		DesktopEntry:   desktopEntry,
		PlaybackStatus: status,
		Volume:         volume,
		PositionMicro:  position,
		Metadata:       metadata,
	}
	m.ownerToService[owner] = serviceName
	m.mu.Unlock()

	m.logger.Info("Registered active MPRIS player", "name", playerName, "friendly_name", friendlyName, "owner", owner, "status", status)
}

// getFriendlyNameFromDesktopEntry scans XDG directories to parse the .desktop file and extract its Name= property.
func (m *DbusMPRISManager) getFriendlyNameFromDesktopEntry(desktopEntry string) string {
	if desktopEntry == "" {
		return ""
	}

	xdgDataDirs := os.Getenv("XDG_DATA_DIRS")
	var searchDirs []string
	if xdgDataDirs != "" {
		searchDirs = strings.Split(xdgDataDirs, ":")
	} else {
		searchDirs = []string{"/usr/local/share", "/usr/share"}
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		searchDirs = append([]string{filepath.Join(homeDir, ".local/share")}, searchDirs...)
	}

	for _, dir := range searchDirs {
		desktopPath := filepath.Join(dir, "applications", desktopEntry+".desktop")
		info, err := os.Stat(desktopPath)
		if err != nil {
			continue
		}

		// Security constraint: Do not parse files larger than 64KB
		if info.Size() > 65536 {
			m.logger.Warn("Skipping excessively large desktop file for security limits", "path", desktopPath, "size", info.Size())
			continue
		}

		file, err := os.Open(desktopPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		inDesktopEntrySection := false
		foundName := ""

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "[Desktop Entry]" {
				inDesktopEntrySection = true
				continue
			} else if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				inDesktopEntrySection = false
				continue
			}

			if inDesktopEntrySection && strings.HasPrefix(line, "Name=") {
				foundName = strings.TrimPrefix(line, "Name=")
				break
			}
		}
		file.Close()

		if foundName != "" {
			return foundName
		}
	}
	return ""
}

// getExecutableNameFromOwner resolves the Unix process ID of the connection and returns the base binary name.
func (m *DbusMPRISManager) getExecutableNameFromOwner(owner string) string {
	if owner == "" {
		return ""
	}

	obj := m.sendConn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var pid uint32
	err := obj.Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid)
	if err != nil {
		m.logger.Debug("Failed to fetch process ID for D-Bus connection owner", "owner", owner, "error", err)
		return ""
	}

	pidStr := strconv.Itoa(int(pid))

	exePath, err := os.Readlink("/proc/" + pidStr + "/exe")
	if err == nil {
		return filepath.Base(exePath)
	}

	commBytes, err := os.ReadFile("/proc/" + pidStr + "/comm")
	if err == nil {
		return strings.TrimSpace(string(commBytes))
	}

	return ""
}

// handleDbusSignal routes and parses D-Bus event frames.
func (m *DbusMPRISManager) handleDbusSignal(sig *dbus.Signal) {
	m.logger.Debug("Received D-Bus signal in MPRIS manager", "sender", sig.Sender, "path", sig.Path, "name", sig.Name, "body", sig.Body)
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
		m.logger.Info("Received D-Bus PropertiesChanged signal", "sender", sig.Sender)
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
			knownPlayers := make([]string, 0, len(m.activePlayers))
			for owner, p := range m.activePlayers {
				knownPlayers = append(knownPlayers, fmt.Sprintf("%s (%s/%s)", owner, p.PlayerName, p.Identity))
			}
			m.mu.Unlock()
			m.logger.Info("PropertiesChanged received but player not registered in activePlayers",
				"sender", sig.Sender, "known_players", knownPlayers)
			return
		}

		updated := false
		if val, ok := changedProps["PlaybackStatus"]; ok {
			if s, ok := val.Value().(string); ok {
				player.PlaybackStatus = PlaybackStatus(s)
				updated = true
				m.logger.Debug("MPRIS player PlaybackStatus changed", "player", player.PlayerName, "status", s)
			}
		}
		if val, ok := changedProps["Volume"]; ok {
			if v, ok := val.Value().(float64); ok {
				player.Volume = v
				updated = true
				m.logger.Debug("MPRIS player Volume changed", "player", player.PlayerName, "volume", v)
			}
		}
		if val, ok := changedProps["Metadata"]; ok {
			m.logger.Info("D-Bus signal changedProps contains Metadata")
			if rawMeta, ok := val.Value().(map[string]dbus.Variant); ok {
				newMeta := m.parseMetadata(rawMeta)
				// Preserve existing base64 artwork if the new metadata update lacks it but is the same track/title
				if newMeta.ArtURL == "" && player.Metadata.ArtURL != "" && newMeta.Title == player.Metadata.Title {
					m.logger.Info("Preserving existing processed artwork for the same track", "title", newMeta.Title)
					newMeta.ArtURL = player.Metadata.ArtURL
				}
				player.Metadata = newMeta
				updated = true
				m.logger.Info("MPRIS player Metadata changed and parsed successfully", "player", player.PlayerName, "title", player.Metadata.Title, "artist", player.Metadata.Artist, "album", player.Metadata.Album)
			} else {
				m.logger.Warn("Metadata key present in signal, but value is not of map[string]dbus.Variant type", "type", fmt.Sprintf("%T", val.Value()))
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
func (m *DbusMPRISManager) parseMetadata(metaMap map[string]dbus.Variant) PlayerMetadata {
	meta := PlayerMetadata{
		Artist: []string{},
	}

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
		if rawURL, ok := v.Value().(string); ok {
			m.logger.Info("MPRIS metadata raw artUrl key found", "url", rawURL)
			meta.ArtURL = m.extractor.Extract(rawURL)
			m.logger.Info("MPRIS metadata processed artUrl output", "has_artwork", meta.ArtURL != "", "len", len(meta.ArtURL))
		} else {
			m.logger.Warn("MPRIS metadata artUrl found but value was not a string", "value", v.Value())
		}
	} else {
		m.logger.Info("MPRIS metadata artUrl key was NOT present in metadata map")
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
