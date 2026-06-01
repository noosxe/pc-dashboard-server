package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/noosxe/pc-dashboard-server/pkg/dpms"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"github.com/noosxe/pc-dashboard-server/pkg/power"
)

// UDSRequest represents an incoming trigger request via local Unix socket.
type UDSRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// UDSResponse represents the execution feedback returned to the trigger client.
type UDSResponse struct {
	Success     bool   `json:"success"`
	ClientCount int    `json:"client_count"`
	Error       string `json:"error,omitempty"`
}

// runCommandSocket starts the Unix domain socket command listener thread.
func (e *Engine) runCommandSocket(ctx context.Context) error {
	socketPath := e.cfg.Daemon.SocketPath
	if socketPath == "" {
		e.logger.Info("Unix Domain Socket path is empty, command trigger socket disabled")
		return nil
	}

	e.logger.Info("Initializing local command trigger socket", "path", socketPath)

	// Check if socket already exists and try to dial it
	if _, err := os.Stat(socketPath); err == nil {
		conn, dialErr := net.DialTimeout("unix", socketPath, 1*time.Second)
		if dialErr == nil {
			conn.Close()
			return fmt.Errorf("socket file %s is already in use by a running daemon instance", socketPath)
		}
		// Connection failed, so socket is stale. Clean it up.
		e.logger.Warn("Stale socket file detected, removing it", "path", socketPath)
		if removeErr := os.Remove(socketPath); removeErr != nil {
			return fmt.Errorf("failed to remove stale socket file %s: %w", socketPath, removeErr)
		}
	}

	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on Unix domain socket %s: %w", socketPath, err)
	}
	defer func() {
		listener.Close()
		_ = os.Remove(socketPath)
	}()

	// Restrict permissions to owner only
	if err := os.Chmod(socketPath, 0600); err != nil {
		e.logger.Warn("Failed to set strict file permissions 0600 on socket file", "path", socketPath, "error", err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			e.logger.Error("Command socket accept failure", "error", err)
			continue
		}

		go e.handleCommandConnection(conn)
	}
}

// handleCommandConnection processes a single trigger client connection.
func (e *Engine) handleCommandConnection(conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	var req UDSRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		e.logger.Error("Failed to decode UDS request", "error", err)
		e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode request JSON: " + err.Error()})
		return
	}

	var broadcastPayload interface{}

	switch req.Type {
	case "session_lock":
		var ev lock.SessionLockEvent
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode session_lock data: " + err.Error()})
			return
		}
		// Cache the session lock state
		e.lockStateMu.Lock()
		e.lastLockState = &ev
		e.lockStateMu.Unlock()

		broadcastPayload = SessionLockPayload{
			Type:      "session_lock",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "dpms":
		var ev dpms.DpmsEvent
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode dpms data: " + err.Error()})
			return
		}
		// Cache the DPMS state
		e.dpmsStateMu.Lock()
		e.lastDpmsState = &ev
		e.dpmsStateMu.Unlock()

		// Actively wake/sleep physical companion device screen synchronously over ADB if not no-app-control
		if !e.cfg.ADB.NoAppControl {
			e.serialsMu.RLock()
			serials := make([]string, 0, len(e.activeSerials))
			for serial := range e.activeSerials {
				serials = append(serials, serial)
			}
			e.serialsMu.RUnlock()

			for _, serial := range serials {
				go func(s string, state string) {
					adbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()

					if state == "off" {
						e.logger.Info("Power event (DPMS off/Command): putting Android screen to sleep", "serial", s)
						if err := e.adbClient.SleepDevice(adbCtx, s); err != nil {
							e.logger.Error("Failed to sleep screen via ADB", "serial", s, "error", err)
						}
					} else {
						e.logger.Info("Power event (DPMS on/Command): waking Android screen", "serial", s)
						if err := e.adbClient.WakeDevice(adbCtx, s); err != nil {
							e.logger.Error("Failed to wake screen via ADB", "serial", s, "error", err)
						}
					}
				}(serial, ev.State)
			}
		}

		broadcastPayload = DpmsStatePayload{
			Type:      "dpms_state",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "notification_event":
		var ev notifications.NotificationEvent
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode notification_event data: " + err.Error()})
			return
		}
		broadcastPayload = NotificationEventPayload{
			Type:      "notification_event",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "media_state":
		var ev mpris.MediaEvent
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode media_state data: " + err.Error()})
			return
		}
		broadcastPayload = MediaStatePayload{
			Type:      "media_state",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "telemetry":
		var ev metrics.SystemMetrics
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode telemetry data: " + err.Error()})
			return
		}
		broadcastPayload = TelemetryPayload{
			Type:      "telemetry",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "power_profile_state":
		var ev power.PowerProfileState
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode power_profile_state data: " + err.Error()})
			return
		}
		// Cache the power profile state
		e.powerStateMu.Lock()
		e.lastPowerState = &ev
		e.powerStateMu.Unlock()

		broadcastPayload = PowerProfileStatePayload{
			Type:      "power_profile_state",
			Timestamp: time.Now().Unix(),
			Data:      ev,
		}

	case "raw":
		var ev interface{}
		if err := json.Unmarshal(req.Data, &ev); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: "failed to decode raw data: " + err.Error()})
			return
		}
		broadcastPayload = ev

	default:
		// Handle custom/unrecognized trigger types by wrapping them in a standard envelope
		var inner interface{}
		if err := json.Unmarshal(req.Data, &inner); err != nil {
			e.writeUDSResponse(conn, UDSResponse{Success: false, Error: fmt.Sprintf("failed to decode raw %s data: %v", req.Type, err)})
			return
		}
		broadcastPayload = map[string]interface{}{
			"type":      req.Type,
			"timestamp": time.Now().Unix(),
			"data":      inner,
		}
	}

	// Broadcast the constructed payload to all active websocket clients
	e.pool.Broadcast(broadcastPayload)

	e.logger.Info("Successfully processed local trigger request", "type", req.Type, "client_count", e.pool.Size())

	e.writeUDSResponse(conn, UDSResponse{
		Success:     true,
		ClientCount: e.pool.Size(),
	})
}

// writeUDSResponse helper writes the JSON response to the socket.
func (e *Engine) writeUDSResponse(conn net.Conn, resp UDSResponse) {
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(resp); err != nil {
		e.logger.Error("Failed to write UDS response", "error", err)
	}
}
