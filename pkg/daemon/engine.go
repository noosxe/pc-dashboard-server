package daemon

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/noosxe/pc-dashboard-server/pkg/adb"
	"github.com/noosxe/pc-dashboard-server/pkg/config"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"github.com/noosxe/pc-dashboard-server/pkg/websocket"
)

// TelemetryPayload outlines the outer JSON wrapper broadcasted to clients.
type TelemetryPayload struct {
	Type      string                `json:"type"`
	Timestamp int64                 `json:"timestamp"`
	Data      metrics.SystemMetrics `json:"data"`
}

// NotificationEventPayload outlines the outer JSON wrapper broadcasted to clients for intercepted events.
type NotificationEventPayload struct {
	Type      string                          `json:"type"`
	Timestamp int64                           `json:"timestamp"`
	Data      notifications.NotificationEvent `json:"data"`
}

// MediaStatePayload outlines the outer JSON wrapper broadcasted to clients for MPRIS states.
type MediaStatePayload struct {
	Type      string           `json:"type"`
	Timestamp int64            `json:"timestamp"`
	Data      mpris.MediaEvent `json:"data"`
}

// SessionLockPayload outlines the outer JSON wrapper broadcasted to clients for lock/unlock states.
type SessionLockPayload struct {
	Type      string                `json:"type"`
	Timestamp int64                 `json:"timestamp"`
	Data      lock.SessionLockEvent `json:"data"`
}

// Engine coordinates telemetry polling, ADB physical monitoring, and WebSocket distribution.
type Engine struct {
	logger          *slog.Logger
	cfg             *config.Config
	metrics         metrics.MetricsReader
	adbClient       adb.ADBClient
	notificationMgr notifications.NotificationManager
	mprisMgr        mpris.MPRISManager
	lockMgr         lock.LockManager
	pool            *websocket.ConnectionPool
	wsServer        *websocket.Server
	intervalMu      sync.Mutex
	interval        time.Duration
	resetChan       chan time.Duration
	activeSerials   map[string]bool
	serialsMu       sync.RWMutex
	lockStateMu     sync.RWMutex
	lastLockState   *lock.SessionLockEvent
}

// NewEngine constructs a central Orchestrator.
func NewEngine(cfg *config.Config, mr metrics.MetricsReader, ac adb.ADBClient, nm notifications.NotificationManager, mm mpris.MPRISManager, lm lock.LockManager, daemonLogger *slog.Logger, websocketLogger *slog.Logger) *Engine {
	e := &Engine{
		logger:          daemonLogger,
		cfg:             cfg,
		metrics:         mr,
		adbClient:       ac,
		notificationMgr: nm,
		mprisMgr:        mm,
		lockMgr:         lm,
		interval:        time.Duration(cfg.Daemon.UpdateIntervalMS) * time.Millisecond,
		resetChan:       make(chan time.Duration, 5),
		activeSerials:   make(map[string]bool),
	}

	// Wire callbacks into the WebSocket connection pool
	e.pool = websocket.NewConnectionPool(websocketLogger, e.handleConfigChange, e.handleAction, e.handleNotificationCommand, e.handleMediaCommand, e.handleClientConnect)
	e.wsServer = websocket.NewServer(cfg.Server.Host, cfg.Server.Port, e.pool, websocketLogger)

	return e
}

// Start boots all concurrent daemon threads and blocks until context cancellation.
func (e *Engine) Start(ctx context.Context) error {
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 5)

	// 1. Boot WebSocket HTTP listener thread
	go func() {
		if err := e.wsServer.Start(gCtx); err != nil {
			e.logger.Error("WebSocket server terminated with error", "error", err)
			errChan <- err
		}
	}()

	// 2. Boot ADB Hotplug tracking thread
	go func() {
		if err := e.runADBTracker(gCtx); err != nil {
			e.logger.Error("ADB device tracking terminated", "error", err)
			errChan <- err
		}
	}()

	// 3. Boot Telemetry polling loop thread
	go func() {
		e.runTelemetryLoop(gCtx)
	}()

	// 4. Boot Notification monitoring thread
	go func() {
		if err := e.runNotificationMonitor(gCtx); err != nil {
			e.logger.Error("Notification monitor terminated", "error", err)
			errChan <- err
		}
	}()

	// 5. Boot MPRIS monitoring thread
	go func() {
		if err := e.runMPRISMonitor(gCtx); err != nil {
			e.logger.Error("MPRIS monitor terminated", "error", err)
			errChan <- err
		}
	}()

	// 6. Boot Session Lock monitoring thread
	go func() {
		if err := e.runLockMonitor(gCtx); err != nil {
			e.logger.Error("Session lock monitor terminated", "error", err)
			errChan <- err
		}
	}()

	// 7. Boot Local UDS Command Socket listener thread
	go func() {
		if err := e.runCommandSocket(gCtx); err != nil {
			e.logger.Error("Local command socket listener terminated with error", "error", err)
			errChan <- err
		}
	}()

	e.logger.Info("PC Dashboard core engine successfully booted")

	// Wait for termination signal or errors
	select {
	case err := <-errChan:
		cancel()
		e.cleanupDevices()
		return err
	case <-ctx.Done():
		e.logger.Info("Shutting down core engine")
		e.cleanupDevices()
		return nil
	}
}

// runNotificationMonitor listens to intercepted desktop notifications and broadcasts them to clients.
func (e *Engine) runNotificationMonitor(ctx context.Context) error {
	events, err := e.notificationMgr.Start(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}

			payload := NotificationEventPayload{
				Type:      "notification_event",
				Timestamp: time.Now().Unix(),
				Data:      ev,
			}
			e.pool.Broadcast(payload)
		}
	}
}

// handleNotificationCommand processes triggers received from companion apps.
func (e *Engine) handleNotificationCommand(req notifications.NotificationRequest) (uint32, error) {
	e.logger.Info("Executing notification command via WebSocket", "summary", req.Summary)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return e.notificationMgr.SendNotification(ctx, req)
}

// runADBTracker monitors ADB connection stream and bootstraps new devices.
func (e *Engine) runADBTracker(ctx context.Context) error {
	events, err := e.adbClient.TrackDevices(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}

			if ev.State == adb.StateOnline {
				e.logger.Info("USB device connected. Initiating bootstrap", "serial", ev.Serial)
				e.serialsMu.Lock()
				e.activeSerials[ev.Serial] = true
				e.serialsMu.Unlock()
				go e.bootstrapDevice(ctx, ev.Serial)
			} else {
				e.logger.Info("USB device disconnected", "serial", ev.Serial)
				e.serialsMu.Lock()
				delete(e.activeSerials, ev.Serial)
				e.serialsMu.Unlock()
			}
		}
	}
}

// bootstrapDevice executes wakeup, app launch, and reverse port tunneling.
func (e *Engine) bootstrapDevice(ctx context.Context, serial string) {
	if e.cfg.ADB.NoAppControl {
		e.logger.Info("Skipping wakeup and companion app launch per no-app-control configuration", "serial", serial)
	} else {
		// 1. Wake screen
		if err := e.adbClient.WakeDevice(ctx, serial); err != nil {
			e.logger.Error("Failed to wake screen", "serial", serial, "error", err)
		}

		// 2. Launch Android Companion App
		pkg := e.cfg.ADB.TargetPackage
		act := e.cfg.ADB.TargetActivity
		if err := e.adbClient.LaunchApp(ctx, serial, pkg, act); err != nil {
			e.logger.Error("Failed to launch companion app", "serial", serial, "error", err)
		}
	}

	// 3. Reverse Tunneling
	localPort := e.cfg.Server.Port
	devicePort := e.cfg.Server.Port
	if err := e.adbClient.ReversePort(ctx, serial, localPort, devicePort); err != nil {
		e.logger.Error("Failed to configure reverse port tunnel", "serial", serial, "error", err)
	} else {
		e.logger.Info("Successfully configured reverse port tunnel", "serial", serial)
	}
}

// runTelemetryLoop polls host metrics at configured interval rates.
func (e *Engine) runTelemetryLoop(ctx context.Context) {
	e.intervalMu.Lock()
	currInterval := e.interval
	e.intervalMu.Unlock()

	ticker := time.NewTicker(currInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case newInterval := <-e.resetChan:
			ticker.Reset(newInterval)
		case <-ticker.C:
			// Read performance telemetry
			cpuMetrics, err := e.metrics.ReadCPU()
			if err != nil {
				e.logger.Error("Error collecting CPU metrics", "error", err)
			}

			ramMetrics, err := e.metrics.ReadRAM()
			if err != nil {
				e.logger.Error("Error collecting RAM metrics", "error", err)
			}

			gpuMetrics, err := e.metrics.ReadGPU()
			if err != nil {
				e.logger.Error("Error collecting GPU metrics", "error", err)
			}

			sysMetrics := metrics.SystemMetrics{
				CPU: cpuMetrics,
				RAM: ramMetrics,
				GPU: gpuMetrics,
			}

			payload := TelemetryPayload{
				Type:      "telemetry",
				Timestamp: time.Now().Unix(),
				Data:      sysMetrics,
			}

			// Broadcast payload to all connected clients
			e.pool.Broadcast(payload)
		}
	}
}

// handleConfigChange handles settings modifications requested by clients.
func (e *Engine) handleConfigChange(intervalMs int) {
	e.logger.Info("Update interval changed", "interval_ms", intervalMs)
	duration := time.Duration(intervalMs) * time.Millisecond

	e.intervalMu.Lock()
	e.interval = duration
	e.intervalMu.Unlock()

	// Non-blocking write to resetChan. If a reset is already pending,
	// we drain the channel first so that the ticker always gets the most recent interval.
	select {
	case e.resetChan <- duration:
	default:
		// Drain old value to make space
		select {
		case <-e.resetChan:
		default:
		}
		// Send the new value
		select {
		case e.resetChan <- duration:
		default:
		}
	}
}

// handleAction executes commands requested by companion dashboards.
func (e *Engine) handleAction(command string) {
	e.logger.Info("Control action requested", "command", command)

	switch command {
	case "suspend":
		e.logger.Info("Executing local system suspend")
		// Security: Rigid execution using hardcoded path and static argument.
		cmd := exec.Command("systemctl", "suspend")
		if err := cmd.Run(); err != nil {
			e.logger.Error("System suspend execution failed", "error", err)
		}
	case "disconnect":
		e.logger.Info("Companion disconnect requested. Tearing down active telemetry streaming.")
		// The client closes its connection, which will trigger pool cleanup automatically.
	default:
		e.logger.Warn("Unknown control action", "command", command)
	}
}

// cleanupDevices stops/kills the companion app on all active devices before exit.
func (e *Engine) cleanupDevices() {
	if e.cfg.ADB.NoAppControl {
		e.logger.Info("Skipping companion app close on exit per no-app-control configuration")
		return
	}

	e.serialsMu.RLock()
	serials := make([]string, 0, len(e.activeSerials))
	for serial := range e.activeSerials {
		serials = append(serials, serial)
	}
	e.serialsMu.RUnlock()

	if len(serials) == 0 {
		return
	}

	e.logger.Info("Shutting down daemon: Closing companion app on active devices", "count", len(serials))

	// Use a short, independent context timeout to ensure cleanup commands execute even if
	// the parent context was cancelled.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, serial := range serials {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			pkg := e.cfg.ADB.TargetPackage
			if err := e.adbClient.CloseApp(cleanupCtx, s, pkg); err != nil {
				e.logger.Error("Failed to close companion app on exit", "serial", s, "error", err)
			} else {
				e.logger.Info("Successfully closed companion app on exit", "serial", s)
			}
		}(serial)
	}
	wg.Wait()
}

// runMPRISMonitor monitors active media players and broadcasts status changes to WebSocket clients.
func (e *Engine) runMPRISMonitor(ctx context.Context) error {
	events, err := e.mprisMgr.Start(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}

			payload := MediaStatePayload{
				Type:      "media_state",
				Timestamp: time.Now().Unix(),
				Data:      ev,
			}
			e.pool.Broadcast(payload)
		}
	}
}

// runLockMonitor monitors session lock events and broadcasts status changes to WebSocket clients.
func (e *Engine) runLockMonitor(ctx context.Context) error {
	events, err := e.lockMgr.Start(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}

			// Cache the last session lock event
			e.lockStateMu.Lock()
			e.lastLockState = &ev
			e.lockStateMu.Unlock()

			payload := SessionLockPayload{
				Type:      "session_lock",
				Timestamp: time.Now().Unix(),
				Data:      ev,
			}
			e.pool.Broadcast(payload)

			// Handle companion app screen wake/sleep via ADB
			if !e.cfg.ADB.NoAppControl {
				e.serialsMu.RLock()
				serials := make([]string, 0, len(e.activeSerials))
				for serial := range e.activeSerials {
					serials = append(serials, serial)
				}
				e.serialsMu.RUnlock()

				for _, serial := range serials {
					go func(s string, locked bool) {
						adbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
						defer cancel()

						if locked {
							e.logger.Info("Power event (DPMS off/Lock): putting Android screen to sleep", "serial", s)
							if err := e.adbClient.SleepDevice(adbCtx, s); err != nil {
								e.logger.Error("Failed to sleep screen via ADB", "serial", s, "error", err)
							}
						} else {
							e.logger.Info("Power event (DPMS on/Unlock): waking Android screen", "serial", s)
							if err := e.adbClient.WakeDevice(adbCtx, s); err != nil {
								e.logger.Error("Failed to wake screen via ADB", "serial", s, "error", err)
							}
						}
					}(serial, ev.Locked)
				}
			}
		}
	}
}

// handleClientConnect pushes the cached session lock state to a newly connected client.
func (e *Engine) handleClientConnect(conn *websocket.ClientConn) {
	e.lockStateMu.RLock()
	state := e.lastLockState
	e.lockStateMu.RUnlock()

	if state != nil {
		payload := SessionLockPayload{
			Type:      "session_lock",
			Timestamp: time.Now().Unix(),
			Data:      *state,
		}
		if err := conn.WriteJSON(payload); err != nil {
			e.logger.Error("Failed to send initial session lock state to client", "error", err)
		} else {
			e.logger.Info("Sent initial session lock state to client", "locked", state.Locked)
		}
	} else {
		e.logger.Debug("No initial session lock state cached, skipping push")
	}
}

// handleMediaCommand processes media control requests routed from WebSocket clients.
func (e *Engine) handleMediaCommand(playerName string, command string, args map[string]interface{}) error {
	e.logger.Info("Executing media command via WebSocket", "player", playerName, "command", command)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return e.mprisMgr.SendCommand(ctx, playerName, command, args)
}
