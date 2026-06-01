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
	"github.com/noosxe/pc-dashboard-server/pkg/dpms"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"github.com/noosxe/pc-dashboard-server/pkg/power"
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
	pm := power.NewMockPowerProfilesManager(logger)
	dm := dpms.NewMockDpmsManager(logger)

	engine := NewEngine(cfg, mr, ac, nm, mm, lm, pm, dm, logger, logger)

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

	// Test power profile state trigger
	conn3, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to dial command socket for power profile: %v", err)
	}

	powerReq := UDSRequest{
		Type: "power_profile_state",
		Data: json.RawMessage(`{"active_profile": "performance", "available_profiles": [{"profile": "balanced"}, {"profile": "performance"}]}`),
	}

	if err := json.NewEncoder(conn3).Encode(powerReq); err != nil {
		t.Fatalf("failed to write power profile request: %v", err)
	}

	var powerResp UDSResponse
	if err := json.NewDecoder(conn3).Decode(&powerResp); err != nil {
		t.Fatalf("failed to read power profile response: %v", err)
	}
	conn3.Close()

	if !powerResp.Success {
		t.Errorf("expected power profile trigger success=true, got success=false, error=%q", powerResp.Error)
	}

	// Verify the power state caching inside engine
	engine.powerStateMu.RLock()
	cachedPowerState := engine.lastPowerState
	engine.powerStateMu.RUnlock()

	if cachedPowerState == nil {
		t.Errorf("expected lastPowerState to be cached, got nil")
	} else {
		if cachedPowerState.ActiveProfile != "performance" {
			t.Errorf("expected active profile to be 'performance', got %q", cachedPowerState.ActiveProfile)
		}
		if len(cachedPowerState.AvailableProfiles) != 2 {
			t.Errorf("expected 2 available profiles, got %d", len(cachedPowerState.AvailableProfiles))
		}
	}

	// Test DPMS state trigger
	conn4, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to dial command socket for DPMS: %v", err)
	}

	dpmsReq := UDSRequest{
		Type: "dpms",
		Data: json.RawMessage(`{"state": "off"}`),
	}

	if err := json.NewEncoder(conn4).Encode(dpmsReq); err != nil {
		t.Fatalf("failed to write DPMS request: %v", err)
	}

	var dpmsResp UDSResponse
	if err := json.NewDecoder(conn4).Decode(&dpmsResp); err != nil {
		t.Fatalf("failed to read DPMS response: %v", err)
	}
	conn4.Close()

	if !dpmsResp.Success {
		t.Errorf("expected DPMS trigger success=true, got success=false, error=%q", dpmsResp.Error)
	}

	// Verify the DPMS state caching inside engine
	engine.dpmsStateMu.RLock()
	cachedDpmsState := engine.lastDpmsState
	engine.dpmsStateMu.RUnlock()

	if cachedDpmsState == nil {
		t.Errorf("expected lastDpmsState to be cached, got nil")
	} else if cachedDpmsState.State != "off" {
		t.Errorf("expected cached lastDpmsState to be 'off', got %q", cachedDpmsState.State)
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
	pm := power.NewMockPowerProfilesManager(logger)
	dm := dpms.NewMockDpmsManager(logger)

	engine1 := NewEngine(cfg, mr, ac, nm, mm, lm, pm, dm, logger, logger)
	engine2 := NewEngine(cfg, mr, ac, nm, mm, lm, pm, dm, logger, logger)

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
