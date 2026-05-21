package daemon

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/noosxe/pc-dashboard-server/pkg/adb"
	"github.com/noosxe/pc-dashboard-server/pkg/config"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/websocket"
)

// TelemetryPayload outlines the outer JSON wrapper broadcasted to clients.
type TelemetryPayload struct {
	Type      string                `json:"type"`
	Timestamp int64                 `json:"timestamp"`
	Data      metrics.SystemMetrics `json:"data"`
}

// Engine coordinates telemetry polling, ADB physical monitoring, and WebSocket distribution.
type Engine struct {
	logger        *slog.Logger
	cfg           *config.Config
	metrics       metrics.MetricsReader
	adbClient     adb.ADBClient
	pool          *websocket.ConnectionPool
	wsServer      *websocket.Server
	intervalMu    sync.Mutex
	interval      time.Duration
	resetChan     chan time.Duration
	activeSerials map[string]bool
	serialsMu     sync.RWMutex
}

// NewEngine constructs a central Orchestrator.
func NewEngine(cfg *config.Config, mr metrics.MetricsReader, ac adb.ADBClient, daemonLogger *slog.Logger, websocketLogger *slog.Logger) *Engine {
	e := &Engine{
		logger:        daemonLogger,
		cfg:           cfg,
		metrics:       mr,
		adbClient:     ac,
		interval:      time.Duration(cfg.Daemon.UpdateIntervalMS) * time.Millisecond,
		resetChan:     make(chan time.Duration, 5),
		activeSerials: make(map[string]bool),
	}

	// Wire callbacks into the WebSocket connection pool
	e.pool = websocket.NewConnectionPool(websocketLogger, e.handleConfigChange, e.handleAction)
	e.wsServer = websocket.NewServer(cfg.Server.Host, cfg.Server.Port, e.pool, websocketLogger)

	return e
}

// Start boots all concurrent daemon threads and blocks until context cancellation.
func (e *Engine) Start(ctx context.Context) error {
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 3)

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
