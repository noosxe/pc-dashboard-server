package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noosxe/pc-dashboard-server/pkg/adb"
	"github.com/noosxe/pc-dashboard-server/pkg/config"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
)

func TestEngine_LockStateCaching(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Setup mock configs & interfaces
	cfg := &config.Config{}
	cfg.Daemon.UpdateIntervalMS = 1000
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0 // Auto-allocated port by httptest

	mr := metrics.NewMockMetricsReader(logger)
	ac := adb.NewMockADBClient(logger)
	nm := notifications.NewMockNotificationManager(logger)
	mm := mpris.NewMockMPRISManager(logger)

	// We use a custom manual lock manager to explicitly trigger events
	lockEvents := make(chan lock.SessionLockEvent, 5)
	lm := &manualLockManager{events: lockEvents}

	engine := NewEngine(cfg, mr, ac, nm, mm, lm, logger, logger)

	// 2. Start runLockMonitor in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := engine.runLockMonitor(ctx); err != nil {
			logger.Error("runLockMonitor error", "err", err)
		}
	}()

	// 3. Trigger a lock event to cache it
	lockEvents <- lock.SessionLockEvent{Locked: true}

	// Wait a tiny bit for the event loop to receive the event and cache it
	time.Sleep(50 * time.Millisecond)

	// 4. Setup httptest WebSocket Server to handle client upgrading
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("failed to upgrade websocket connection: %v", err)
			return
		}
		go engine.pool.HandleClient(conn)
	}))
	defer server.Close()

	// 5. Connect as a client to the WebSocket server
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket server: %v", err)
	}
	defer clientConn.Close()

	// 6. Verify client immediately receives the cached lock state
	msgChan := make(chan SessionLockPayload, 1)
	errChan := make(chan error, 1)

	go func() {
		_, msgBytes, err := clientConn.ReadMessage()
		if err != nil {
			errChan <- err
			return
		}
		var payload SessionLockPayload
		if err := json.Unmarshal(msgBytes, &payload); err != nil {
			errChan <- err
			return
		}
		msgChan <- payload
	}()

	select {
	case payload := <-msgChan:
		if payload.Type != "session_lock" {
			t.Errorf("expected payload type 'session_lock', got %q", payload.Type)
		}
		if !payload.Data.Locked {
			t.Errorf("expected session lock event data Locked=true, got Locked=%v", payload.Data.Locked)
		}
	case err := <-errChan:
		t.Fatalf("failed to read websocket message: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial cached lock event to be pushed")
	}
}

type manualLockManager struct {
	events chan lock.SessionLockEvent
}

func (m *manualLockManager) Start(ctx context.Context) (<-chan lock.SessionLockEvent, error) {
	return m.events, nil
}

type trackingADBClient struct {
	wakeCalled   bool
	launchCalled bool
	closeCalled  bool
}

func (c *trackingADBClient) TrackDevices(ctx context.Context) (<-chan adb.DeviceEvent, error) {
	return nil, nil
}
func (c *trackingADBClient) ReversePort(ctx context.Context, serial string, localPort, devicePort int) error {
	return nil
}
func (c *trackingADBClient) LaunchApp(ctx context.Context, serial string, pkg, activity string) error {
	c.launchCalled = true
	return nil
}
func (c *trackingADBClient) WakeDevice(ctx context.Context, serial string) error {
	c.wakeCalled = true
	return nil
}
func (c *trackingADBClient) CloseApp(ctx context.Context, serial string, pkg string) error {
	c.closeCalled = true
	return nil
}

func TestEngine_NoAppControl(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := &config.Config{}
	cfg.ADB.NoAppControl = true

	ac := &trackingADBClient{}
	engine := NewEngine(cfg, nil, ac, nil, nil, nil, logger, logger)

	// Trigger bootstrapDevice
	engine.bootstrapDevice(context.Background(), "TEST_SERIAL")

	if ac.wakeCalled {
		t.Error("expected WakeDevice NOT to be called when NoAppControl is true")
	}
	if ac.launchCalled {
		t.Error("expected LaunchApp NOT to be called when NoAppControl is true")
	}

	// Trigger cleanupDevices
	engine.activeSerials["TEST_SERIAL"] = true
	engine.cleanupDevices()

	if ac.closeCalled {
		t.Error("expected CloseApp NOT to be called when NoAppControl is true")
	}
}

func TestEngine_AppControlEnabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := &config.Config{}
	cfg.ADB.NoAppControl = false

	ac := &trackingADBClient{}
	engine := NewEngine(cfg, nil, ac, nil, nil, nil, logger, logger)

	// Trigger bootstrapDevice
	engine.bootstrapDevice(context.Background(), "TEST_SERIAL")

	if !ac.wakeCalled {
		t.Error("expected WakeDevice to be called when NoAppControl is false")
	}
	if !ac.launchCalled {
		t.Error("expected LaunchApp to be called when NoAppControl is false")
	}

	// Trigger cleanupDevices
	engine.activeSerials["TEST_SERIAL"] = true
	engine.cleanupDevices()

	if !ac.closeCalled {
		t.Error("expected CloseApp to be called when NoAppControl is false")
	}
}
