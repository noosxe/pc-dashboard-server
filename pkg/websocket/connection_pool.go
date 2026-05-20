package websocket

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
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
	mu             sync.RWMutex
	connections    map[*ClientConn]bool
	onConfigChange func(intervalMs int)
	onAction       func(command string)
}

// NewConnectionPool instantiates an active client pool.
func NewConnectionPool(onConfigChange func(intervalMs int), onAction func(command string)) *ConnectionPool {
	return &ConnectionPool{
		connections:    make(map[*ClientConn]bool),
		onConfigChange: onConfigChange,
		onAction:       onAction,
	}
}

// Add inserts a wrapper connection into the registry.
func (p *ConnectionPool) Add(conn *ClientConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connections[conn] = true
	log.Printf("[WebSocket] Client connected. Active clients: %d", len(p.connections))
}

// Remove deletes a client from the registry and closes it.
func (p *ConnectionPool) Remove(conn *ClientConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connections[conn] {
		delete(p.connections, conn)
		conn.ws.Close()
		log.Printf("[WebSocket] Client disconnected. Active clients: %d", len(p.connections))
	}
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
			log.Printf("[WebSocket] Broadcast send failure (removing client): %v", err)
			p.Remove(conn)
		}
	}
}

// InboundMessage outlines the base schema for all client requests.
type InboundMessage struct {
	Type     string           `json:"type"`
	Command  string           `json:"command,omitempty"`
	Settings *ElementSettings `json:"settings,omitempty"`
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
			log.Printf("[WebSocket] Parse error on inbound message: %v", err)
			continue
		}

		switch inbound.Type {
		case "ping":
			// Fast response keepalive
			if err := client.WriteJSON(map[string]string{"type": "pong"}); err != nil {
				log.Printf("[WebSocket] Failed to send pong: %v", err)
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
		}
	}
}
