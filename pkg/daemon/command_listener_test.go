package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/noosxe/pc-dashboard-server/pkg/adb"
	"github.com/noosxe/pc-dashboard-server/pkg/config"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
)

func TestEngine_CommandListenerHandshake(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "pc-dashboard-test.sock")

	// 1. Setup mock configs
	cfg := &config.Config{}
	cfg.Daemon.SocketPath = socketPath
	cfg.Daemon.UpdateIntervalMS = 1000

	mr := metrics.NewMockMetricsReader(logger)
	ac := adb.NewMockADBClient(logger)
	nm := notifications.NewMockNotificationManager(logger)
	mm := mpris.NewMockMPRISManager(logger)
	lm := lock.NewMockLockManager(logger)

	engine := NewEngine(cfg, mr, ac, nm, mm, lm, logger, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Start UDS listener
	go func() {
		if err := engine.runCommandSocket(ctx); err != nil {
			logger.Error("runCommandSocket error", "err", err)
		}
	}()

	// Give the listener a tiny bit to start up and bind the socket
	time.Sleep(50 * time.Millisecond)

	// 3. Connect via UDS and send a valid session lock trigger request
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to dial command socket: %v", err)
	}

	req := UDSRequest{
		Type: "session_lock",
		Data: json.RawMessage(`{"locked": true}`),
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	var resp UDSResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	conn.Close()

	if !resp.Success {
		t.Errorf("expected UDS response success=true, got success=false, error=%q", resp.Error)
	}

	// Verify the lock state caching inside engine
	engine.lockStateMu.RLock()
	cachedState := engine.lastLockState
	engine.lockStateMu.RUnlock()

	if cachedState == nil {
		t.Errorf("expected lastLockState to be cached, got nil")
	} else if !cachedState.Locked {
		t.Errorf("expected cached lastLockState Locked=true, got %v", cachedState.Locked)
	}

	// 4. Send a command request with malformed inner data
	conn2, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to dial command socket again: %v", err)
	}

	req2 := UDSRequest{
		Type: "session_lock",
		Data: json.RawMessage(`{"locked": "not-a-bool"}`), // invalid boolean type
	}

	if err := json.NewEncoder(conn2).Encode(req2); err != nil {
		t.Fatalf("failed to write request 2: %v", err)
	}

	var resp2 UDSResponse
	if err := json.NewDecoder(conn2).Decode(&resp2); err != nil {
		t.Fatalf("failed to read response 2: %v", err)
	}
	conn2.Close()

	if resp2.Success {
		t.Errorf("expected malformed inner payload trigger to fail, got success=true")
	} else if resp2.Error == "" {
		t.Errorf("expected error message for malformed inner payload trigger, got empty string")
	}
}

func TestEngine_CommandListenerConflict(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "pc-dashboard-test.sock")

	cfg := &config.Config{}
	cfg.Daemon.SocketPath = socketPath

	mr := metrics.NewMockMetricsReader(logger)
	ac := adb.NewMockADBClient(logger)
	nm := notifications.NewMockNotificationManager(logger)
	mm := mpris.NewMockMPRISManager(logger)
	lm := lock.NewMockLockManager(logger)

	engine1 := NewEngine(cfg, mr, ac, nm, mm, lm, logger, logger)
	engine2 := NewEngine(cfg, mr, ac, nm, mm, lm, logger, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start engine 1
	go func() {
		_ = engine1.runCommandSocket(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Attempt starting engine 2 on the same socket (should fail because port is active)
	err := engine2.runCommandSocket(ctx)
	if err == nil {
		t.Fatal("expected engine2 startup to fail due to conflicting socket, but it succeeded")
	}
}
