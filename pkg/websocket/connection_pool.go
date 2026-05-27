package websocket

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
)

// ClientConn wraps a raw websocket connection with safety locks.
type ClientConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

// WriteJSON serializes and sends a JSON payload to the client.
func (c *ClientConn) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteJSON(v)
}

// ConnectionPool manages active clients and ensures safe write routing.
type ConnectionPool struct {
	logger                *slog.Logger
	mu                    sync.RWMutex
	connections           map[*ClientConn]bool
	onConfigChange        func(intervalMs int)
	onAction              func(command string)
	onNotificationCommand func(notifications.NotificationRequest) (uint32, error)
	onMediaCommand        func(playerName string, command string, args map[string]interface{}) error
	onConnect             func(conn *ClientConn)
}

// NewConnectionPool instantiates an active client pool.
func NewConnectionPool(
	logger *slog.Logger,
	onConfigChange func(intervalMs int),
	onAction func(command string),
	onNotificationCommand func(notifications.NotificationRequest) (uint32, error),
	onMediaCommand func(playerName string, command string, args map[string]interface{}) error,
	onConnect func(conn *ClientConn),
) *ConnectionPool {
	return &ConnectionPool{
		logger:                logger,
		connections:           make(map[*ClientConn]bool),
		onConfigChange:        onConfigChange,
		onAction:              onAction,
		onNotificationCommand: onNotificationCommand,
		onMediaCommand:        onMediaCommand,
		onConnect:             onConnect,
	}
}

// Add inserts a wrapper connection into the registry.
func (p *ConnectionPool) Add(conn *ClientConn) {
	p.mu.Lock()
	p.connections[conn] = true
	p.logger.Info("Client connected", "active_clients", len(p.connections))
	p.mu.Unlock()

	if p.onConnect != nil {
		p.onConnect(conn)
	}
}

// Remove deletes a client from the registry and closes it.
func (p *ConnectionPool) Remove(conn *ClientConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connections[conn] {
		delete(p.connections, conn)
		conn.ws.Close()
		p.logger.Info("Client disconnected", "active_clients", len(p.connections))
	}
}

// Size returns the count of active client connections in the pool.
func (p *ConnectionPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.connections)
}

// Broadcast sends a message to all connected clients.
func (p *ConnectionPool) Broadcast(message interface{}) {
	p.mu.RLock()
	// Create a copy of connections to minimize lock contention
	conns := make([]*ClientConn, 0, len(p.connections))
	for conn := range p.connections {
		conns = append(conns, conn)
	}
	p.mu.RUnlock()

	for _, conn := range conns {
		if err := conn.WriteJSON(message); err != nil {
			p.logger.Error("Broadcast send failure (removing client)", "error", err)
			p.Remove(conn)
		}
	}
}

// MediaCommandArgs represents parameters for a media command.
type MediaCommandArgs struct {
	OffsetMicroseconds   *int64   `json:"offset_microseconds,omitempty"`
	PositionMicroseconds *int64   `json:"position_microseconds,omitempty"`
	TrackID              *string  `json:"track_id,omitempty"`
	Volume               *float64 `json:"volume,omitempty"`
}

// InboundMessage outlines the base schema for all client requests.
type InboundMessage struct {
	Type     string           `json:"type"`
	Command  string           `json:"command,omitempty"`
	Settings *ElementSettings `json:"settings,omitempty"`

	// Notification request fields (embedded directly at root level)
	AppName       string                 `json:"app_name"`
	ReplacesID    uint32                 `json:"replaces_id"`
	AppIcon       string                 `json:"app_icon"`
	Summary       string                 `json:"summary"`
	Body          string                 `json:"body"`
	Actions       []string               `json:"actions"`
	Hints         map[string]interface{} `json:"hints"`
	ExpireTimeout int32                  `json:"expire_timeout"`

	// Media control fields
	PlayerName string            `json:"player_name,omitempty"`
	Args       *MediaCommandArgs `json:"args,omitempty"`
}

// ElementSettings outlines configuration update payloads.
type ElementSettings struct {
	IntervalMS int `json:"interval_ms"`
}

// HandleClient reads incoming frames in a blocking loop.
func (p *ConnectionPool) HandleClient(wsConn *websocket.Conn) {
	client := &ClientConn{ws: wsConn}
	p.Add(client)
	defer p.Remove(client)

	for {
		_, msgBytes, err := wsConn.ReadMessage()
		if err != nil {
			break
		}

		var inbound InboundMessage
		if err := json.Unmarshal(msgBytes, &inbound); err != nil {
			p.logger.Error("Parse error on inbound message", "error", err)
			continue
		}

		switch inbound.Type {
		case "ping":
			// Fast response keepalive
			if err := client.WriteJSON(map[string]string{"type": "pong"}); err != nil {
				p.logger.Error("Failed to send pong", "error", err)
			}
		case "config":
			if inbound.Settings != nil && p.onConfigChange != nil {
				interval := inbound.Settings.IntervalMS
				// Enforce 100ms - 10000ms boundaries
				if interval < 100 {
					interval = 100
				} else if interval > 10000 {
					interval = 10000
				}
				p.onConfigChange(interval)
			}
		case "action":
			if p.onAction != nil {
				p.onAction(inbound.Command)
			}
		case "notification_command":
			if p.onNotificationCommand != nil {
				req := notifications.NotificationRequest{
					AppName:       inbound.AppName,
					ReplacesID:    inbound.ReplacesID,
					AppIcon:       inbound.AppIcon,
					Summary:       inbound.Summary,
					Body:          inbound.Body,
					Actions:       inbound.Actions,
					Hints:         inbound.Hints,
					ExpireTimeout: inbound.ExpireTimeout,
				}
				// Default AppName and Icon values per schema if empty
				if req.AppName == "" {
					req.AppName = "pc-dashboard"
				}
				if req.AppIcon == "" {
					req.AppIcon = "dialog-information"
				}
				if req.ExpireTimeout == 0 {
					req.ExpireTimeout = -1
				}

				id, err := p.onNotificationCommand(req)
				if err != nil {
					p.logger.Error("Failed to execute notification command", "error", err)
					_ = client.WriteJSON(map[string]interface{}{
						"type":   "notification_response",
						"status": "error",
						"error":  err.Error(),
					})
				} else {
					_ = client.WriteJSON(map[string]interface{}{
						"type":   "notification_response",
						"status": "success",
						"id":     id,
					})
				}
			}
		case "media_command":
			if p.onMediaCommand != nil {
				argsMap := make(map[string]interface{})
				if inbound.Args != nil {
					if inbound.Args.OffsetMicroseconds != nil {
						argsMap["offset_microseconds"] = *inbound.Args.OffsetMicroseconds
					}
					if inbound.Args.PositionMicroseconds != nil {
						argsMap["position_microseconds"] = *inbound.Args.PositionMicroseconds
					}
					if inbound.Args.TrackID != nil {
						argsMap["track_id"] = *inbound.Args.TrackID
					}
					if inbound.Args.Volume != nil {
						argsMap["volume"] = *inbound.Args.Volume
					}
				}

				err := p.onMediaCommand(inbound.PlayerName, inbound.Command, argsMap)
				if err != nil {
					p.logger.Error("Failed to execute media command", "error", err)
					_ = client.WriteJSON(map[string]interface{}{
						"type":   "media_response",
						"status": "error",
						"error":  err.Error(),
					})
				} else {
					_ = client.WriteJSON(map[string]interface{}{
						"type":   "media_response",
						"status": "success",
					})
				}
			}
		}
	}
}
