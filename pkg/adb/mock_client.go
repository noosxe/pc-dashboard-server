package adb

import (
	"context"
	"log/slog"
	"time"
)

// MockADBClient simulates hotplug USB and companion bootstrapping actions.
type MockADBClient struct {
	logger *slog.Logger
	serial string
}

// NewMockADBClient instantiates a mock client with a default serial.
func NewMockADBClient(logger *slog.Logger) *MockADBClient {
	return &MockADBClient{
		logger: logger,
		serial: "MOCK_DEVICE_12345",
	}
}

// TrackDevices simulates device connection event after 3 seconds.
func (c *MockADBClient) TrackDevices(ctx context.Context) (<-chan DeviceEvent, error) {
	out := make(chan DeviceEvent, 5)

	go func() {
		defer close(out)

		// 1. Wait for 3 seconds to simulate booting delays
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		// 2. Dispatch simulated online event
		c.logger.Info("Device online", "serial", c.serial)
		out <- DeviceEvent{Serial: c.serial, State: StateOnline}

		// 3. Keep running until context is cancelled
		<-ctx.Done()

		// 4. Log teardown
		c.logger.Info("Device offline. Cleared reverse port tunnels.", "serial", c.serial)
	}()

	return out, nil
}

// ReversePort mocks reverse port configuration.
func (c *MockADBClient) ReversePort(ctx context.Context, serial string, localPort, devicePort int) error {
	c.logger.Info("Reversing device port to host", "device_port", devicePort, "local_port", localPort, "serial", serial)
	return nil
}

// LaunchApp mocks starting target Android companion app activity.
func (c *MockADBClient) LaunchApp(ctx context.Context, serial string, pkg, activity string) error {
	c.logger.Info("Launching activity", "package", pkg, "activity", activity, "serial", serial)
	return nil
}

// WakeDevice mocks screen wakeup KEYCODE_WAKEUP event.
func (c *MockADBClient) WakeDevice(ctx context.Context, serial string) error {
	c.logger.Info("Waking screen", "serial", serial)
	return nil
}
