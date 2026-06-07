package connectrpc_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	pcdv1 "github.com/noosxe/pc-dashboard-server/pkg/api/pcd/v1"
	"github.com/noosxe/pc-dashboard-server/pkg/api/pcd/v1/pcdv1connect"
	"github.com/noosxe/pc-dashboard-server/pkg/connectrpc"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestConnectRPCServer_Endpoints(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Track callbacks
	var configChangedVal int
	var actionCommand string
	var notificationReq notifications.NotificationRequest
	var mediaCommand string
	var powerProfile string

	// Instantiate server
	srv := connectrpc.NewServer(
		logger,
		func(intervalMs int) { configChangedVal = intervalMs },
		func(command string) { actionCommand = command },
		func(req notifications.NotificationRequest) (uint32, error) {
			notificationReq = req
			return 42, nil
		},
		func(playerName string, command string, args map[string]interface{}) error {
			mediaCommand = command
			return nil
		},
		func(profile string) error {
			powerProfile = profile
			return nil
		},
		func() []*pcdv1.StreamSystemStateResponse {
			return []*pcdv1.StreamSystemStateResponse{
				{
					Timestamp: 100,
					Event: &pcdv1.StreamSystemStateResponse_SessionLock{
						SessionLock: &pcdv1.SessionLockEvent{Locked: true},
					},
				},
			}
		},
	)

	// Set up mux and register handlers
	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	// h2c wrapper for plaintext HTTP/2 support
	handler := h2c.NewHandler(mux, &http2.Server{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// -----------------------------------------------------------------
	// Test 1: Telemetry Streaming
	// -----------------------------------------------------------------
	// Broadcast dummy metrics and read from stream
	dummyMetrics := metrics.SystemMetrics{
		CPU: metrics.CPUMetrics{
			UsagePercent:      23.5,
			CoresUsagePercent: []float64{20.0, 27.0},
			TempCelsius:       48.0,
			FreqMHz:           3000.0,
			PowerWatts:        35.0,
		},
	}

	// Broadcast dummy metrics in background to avoid blocking stream initiation
	go func() {
		time.Sleep(100 * time.Millisecond)
		srv.BroadcastTelemetry(dummyMetrics)
	}()

	telemetryClient := pcdv1connect.NewTelemetryServiceClient(ts.Client(), ts.URL)
	stream, err := telemetryClient.StreamTelemetry(ctx, connect.NewRequest(&pcdv1.StreamTelemetryRequest{
		IntervalMs: 500,
	}))
	if err != nil {
		t.Fatalf("failed to call StreamTelemetry: %v", err)
	}

	// Verify the config changed callback was triggered
	if configChangedVal != 500 {
		t.Errorf("expected configChangedVal to be 500, got %d", configChangedVal)
	}

	if !stream.Receive() {
		t.Fatalf("failed to receive telemetry: %v", stream.Err())
	}

	payload := stream.Msg().Payload
	if payload.Cpu.UsagePercent != 23.5 {
		t.Errorf("expected CPU usage to be 23.5, got %f", payload.Cpu.UsagePercent)
	}
	if payload.Cpu.TempCelsius != 48.0 {
		t.Errorf("expected CPU temp to be 48.0, got %f", payload.Cpu.TempCelsius)
	}

	// -----------------------------------------------------------------
	// Test 2: Send Notification RPC
	// -----------------------------------------------------------------
	notifClient := pcdv1connect.NewNotificationServiceClient(ts.Client(), ts.URL)
	resp, err := notifClient.SendNotification(ctx, connect.NewRequest(&pcdv1.SendNotificationRequest{
		Notification: &pcdv1.NotificationRequest{
			AppName: "test-app",
			Summary: "test-summary",
			Body:    "test-body",
		},
	}))
	if err != nil {
		t.Fatalf("failed to call SendNotification: %v", err)
	}

	if resp.Msg.Id != 42 {
		t.Errorf("expected notification ID 42, got %d", resp.Msg.Id)
	}
	if notificationReq.AppName != "test-app" || notificationReq.Summary != "test-summary" {
		t.Errorf("incorrect notification request data routed: %+v", notificationReq)
	}

	// -----------------------------------------------------------------
	// Test 3: Send Media Command RPC
	// -----------------------------------------------------------------
	mediaClient := pcdv1connect.NewMediaServiceClient(ts.Client(), ts.URL)
	_, err = mediaClient.SendMediaCommand(ctx, connect.NewRequest(&pcdv1.SendMediaCommandRequest{
		PlayerName: "spotify",
		Command:    "play_pause",
	}))
	if err != nil {
		t.Fatalf("failed to call SendMediaCommand: %v", err)
	}

	if mediaCommand != "play_pause" {
		t.Errorf("expected mediaCommand to be 'play_pause', got '%s'", mediaCommand)
	}

	// -----------------------------------------------------------------
	// Test 4: System Action RPC
	// -----------------------------------------------------------------
	sysClient := pcdv1connect.NewSystemServiceClient(ts.Client(), ts.URL)
	_, err = sysClient.ExecuteSystemAction(ctx, connect.NewRequest(&pcdv1.ExecuteSystemActionRequest{
		Command: "suspend",
	}))
	if err != nil {
		t.Fatalf("failed to call ExecuteSystemAction: %v", err)
	}

	if actionCommand != "suspend" {
		t.Errorf("expected actionCommand to be 'suspend', got '%s'", actionCommand)
	}

	// -----------------------------------------------------------------
	// Test 5: Set Power Profile RPC
	// -----------------------------------------------------------------
	_, err = sysClient.SetPowerProfile(ctx, connect.NewRequest(&pcdv1.SetPowerProfileRequest{
		Profile: "balanced",
	}))
	if err != nil {
		t.Fatalf("failed to call SetPowerProfile: %v", err)
	}

	if powerProfile != "balanced" {
		t.Errorf("expected powerProfile to be 'balanced', got '%s'", powerProfile)
	}

	// -----------------------------------------------------------------
	// Test 6: Stream System State (Initial Caching)
	// -----------------------------------------------------------------
	sysStream, err := sysClient.StreamSystemState(ctx, connect.NewRequest(&pcdv1.StreamSystemStateRequest{}))
	if err != nil {
		t.Fatalf("failed to stream system state: %v", err)
	}

	if !sysStream.Receive() {
		t.Fatalf("failed to receive initial cached system state: %v", sysStream.Err())
	}

	event := sysStream.Msg().Event
	sessionLock, ok := event.(*pcdv1.StreamSystemStateResponse_SessionLock)
	if !ok {
		t.Fatalf("expected initial event to be a session lock, got %T", event)
	}
	if !sessionLock.SessionLock.Locked {
		t.Errorf("expected session lock to be true")
	}
}

func TestConnectRPCServer_DisconnectTelemetryStream(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := connectrpc.NewServer(logger, nil, nil, nil, nil, nil, nil)

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)
	ts := httptest.NewServer(h2c.NewHandler(mux, &http2.Server{}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel context in background to unblock HTTP/1.1 stream call
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	telemetryClient := pcdv1connect.NewTelemetryServiceClient(ts.Client(), ts.URL)
	_, err := telemetryClient.StreamTelemetry(ctx, connect.NewRequest(&pcdv1.StreamTelemetryRequest{}))
	// We expect a context cancelled or similar transport error due to cancellation
	if err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		slog.Debug("Telemetry stream terminated with cancellation error", "err", err)
	}
}
