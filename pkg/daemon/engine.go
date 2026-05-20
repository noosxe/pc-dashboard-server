package daemon

import (
	"context"
	"log"
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
	cfg        *config.Config
	metrics    metrics.MetricsReader
	adbClient  adb.ADBClient
	pool       *websocket.ConnectionPool
	wsServer   *websocket.Server
	intervalMu sync.Mutex
	interval   time.Duration
	resetChan  chan time.Duration
}

// NewEngine constructs a central Orchestrator.
func NewEngine(cfg *config.Config, mr metrics.MetricsReader, ac adb.ADBClient) *Engine {
	e := &Engine{
		cfg:       cfg,
		metrics:   mr,
		adbClient: ac,
		interval:  time.Duration(cfg.Daemon.UpdateIntervalMS) * time.Millisecond,
		resetChan: make(chan time.Duration, 5),
	}

	// Wire callbacks into the WebSocket connection pool
	e.pool = websocket.NewConnectionPool(e.handleConfigChange, e.handleAction)
	e.wsServer = websocket.NewServer(cfg.Server.Host, cfg.Server.Port, e.pool)

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
			log.Printf("[Daemon] WebSocket server terminated with error: %v", err)
			errChan <- err
		}
	}()

	// 2. Boot ADB Hotplug tracking thread
	go func() {
		if err := e.runADBTracker(gCtx); err != nil {
			log.Printf("[Daemon] ADB device tracking terminated: %v", err)
			errChan <- err
		}
	}()

	// 3. Boot Telemetry polling loop thread
	go func() {
		e.runTelemetryLoop(gCtx)
	}()

	log.Printf("[Daemon] PC Dashboard core engine successfully booted.")

	// Wait for termination signal or errors
	select {
	case err := <-errChan:
		cancel()
		return err
	case <-ctx.Done():
		log.Printf("[Daemon] Shutting down core engine...")
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
				log.Printf("[Daemon] USB device connected: %s. Initiating bootstrap...", ev.Serial)
				go e.bootstrapDevice(ctx, ev.Serial)
			} else {
				log.Printf("[Daemon] USB device disconnected: %s.", ev.Serial)
			}
		}
	}
}

// bootstrapDevice executes wakeup, app launch, and reverse port tunneling.
func (e *Engine) bootstrapDevice(ctx context.Context, serial string) {
	// 1. Wake screen
	if err := e.adbClient.WakeDevice(ctx, serial); err != nil {
		log.Printf("[Daemon] Failed to wake screen on %s: %v", serial, err)
	}

	// 2. Launch Android Companion App
	pkg := e.cfg.ADB.TargetPackage
	act := e.cfg.ADB.TargetActivity
	if err := e.adbClient.LaunchApp(ctx, serial, pkg, act); err != nil {
		log.Printf("[Daemon] Failed to launch companion app on %s: %v", serial, err)
	}

	// 3. Reverse Tunneling
	localPort := e.cfg.Server.Port
	devicePort := e.cfg.Server.Port
	if err := e.adbClient.ReversePort(ctx, serial, localPort, devicePort); err != nil {
		log.Printf("[Daemon] Failed to configure reverse port tunnel on %s: %v", serial, err)
	} else {
		log.Printf("[Daemon] Successfully configured reverse port tunnel for serial %s", serial)
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
				log.Printf("[Daemon] Error collecting CPU metrics: %v", err)
			}

			ramMetrics, err := e.metrics.ReadRAM()
			if err != nil {
				log.Printf("[Daemon] Error collecting RAM metrics: %v", err)
			}

			gpuMetrics, err := e.metrics.ReadGPU()
			if err != nil {
				log.Printf("[Daemon] Error collecting GPU metrics: %v", err)
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
	log.Printf("[Daemon] Update interval changed to %d ms", intervalMs)
	duration := time.Duration(intervalMs) * time.Millisecond

	e.intervalMu.Lock()
	e.interval = duration
	e.intervalMu.Unlock()

	e.resetChan <- duration
}

// handleAction executes commands requested by companion dashboards.
func (e *Engine) handleAction(command string) {
	log.Printf("[Daemon] Control action requested: %s", command)

	switch command {
	case "suspend":
		log.Printf("[Daemon] Executing local system suspend...")
		// Security: Rigid execution using hardcoded path and static argument.
		cmd := exec.Command("systemctl", "suspend")
		if err := cmd.Run(); err != nil {
			log.Printf("[Daemon] System suspend execution failed: %v", err)
		}
	case "disconnect":
		log.Printf("[Daemon] Companion disconnect requested. Tearing down active telemetry streaming.")
		// The client closes its connection, which will trigger pool cleanup automatically.
	default:
		log.Printf("[Daemon] Unknown control action: %s", command)
	}
}
